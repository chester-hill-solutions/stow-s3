package conformance_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// contractOutcome is what one step produced: the JSON the verb printed, whether it
// succeeded, and anything it was supposed to write. Performing a step and judging
// it are separate, because several steps are supposed to fail and a runner that
// conflated the two could not express a refusal.
type contractOutcome struct {
	stdout    document
	rawStdout string
	stderr    string
	code      int
}

// perform runs one step and returns what came of it, without judging anything.
//
// The step arrives resolved, so nothing here resolves anything. That is the whole
// reason resolve is a separate pass: every reader of a step's text below can assume
// it is a real path or a real flag value, and none of them has to remember to ask.
func (r *contractRun) perform(step contractStep) contractOutcome {
	r.t.Helper()
	switch step.Verb {
	case "noop":
		r.applyWrites(step.Write)
		return contractOutcome{}
	case "read":
		return contractOutcome{stdout: r.readTree(step.Root)}
	case "prepare":
		return r.performPrepare(step)
	default:
		return r.performVerb(step)
	}
}

// argv builds the command line for a verb, and is the only place the registry
// directory is decided.
//
// A step that names --registry-dir is authoritative, and that is how the contract
// reaches a team partition: prepare reports the partition it created, and collect
// and destroy — which take --registry-dir but no --team — are pointed straight at
// it. Every other step in registry "a" gets the driver's own directory plus the
// step's --team, and the CLI composes the two as it does for a manifest. A driver
// reconstructing the partition path itself would be asserting a belief about the
// store rather than about the verb, and getting it wrong is silent.
func (r *contractRun) argv(step contractStep) []string {
	argv := []string{"workspace", step.Verb}
	for _, name := range sortedArgs(step.Args) {
		argv = append(argv, "--"+name, step.Args[name])
	}
	if _, declared := step.Args["registry-dir"]; !declared {
		if step.Registry == contractRegistryA && contractRegistryDirFlags[step.Verb] {
			argv = append(argv, "--registry-dir", filepath.Join(r.work, "registry-a"))
		}
	}
	if step.Team != "" {
		argv = append(argv, "--team", step.Team)
	}
	return argv
}

func (r *contractRun) performVerb(step contractStep) contractOutcome {
	r.t.Helper()
	argv := r.argv(step)
	if step.Tamper != "" {
		// The refusal has to come from the digest, so the file the verb reads is a
		// corrupt copy of a document this scenario really produced. A path that
		// simply does not exist would be refused too, and for the wrong reason.
		tampered := r.workPath("tampered-" + step.Tamper)
		r.tamper(r.workPath(step.Tamper), tampered)
		argv = replaceArgValue(argv, "--delta", tampered)
	}
	if step.RenameInTransit != nil {
		renamed := r.workPath("renamed.stowdelta")
		r.renameInTransit(step.RenameInTransit, renamed)
		argv = replaceArgValue(argv, "--delta", renamed)
	}

	cmd := exec.Command(r.binary, argv...)
	cmd.Env = r.childEnv()
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	out := contractOutcome{rawStdout: stdout.String(), stderr: stderr.String(), stdout: document{}}
	if cmd.ProcessState != nil {
		out.code = cmd.ProcessState.ExitCode()
	}
	if err != nil && out.code == 0 {
		r.t.Fatalf("step %q: running %v: %v", step.ID, argv, err)
	}
	if out.code == 0 && strings.TrimSpace(out.rawStdout) != "" {
		if decodeErr := json.Unmarshal([]byte(out.rawStdout), &out.stdout); decodeErr != nil {
			r.t.Fatalf("step %q: the verb printed something that is not a JSON object: %v\nstdout: %s\nstderr: %s",
				step.ID, decodeErr, out.rawStdout, out.stderr)
		}
	}
	// A step whose verb writes the document itself to the named path gets that
	// document, because there is nothing on stdout to decode. `handoff --output`
	// prints nothing, and a driver that decodes stdout sees an empty object — the bug
	// the TypeScript and Python wrappers both had, reproduced by a third
	// implementation written from the CLI's observable behaviour. Three drivers
	// making the same mistake is what makes this a contract rather than convention.
	if step.AlsoWritten != "" && step.Returns == contractReturnsFile {
		out.stdout = r.readDocument(r.workPath(step.AlsoWritten), step.ID)
	}
	return out
}

// readDocument returns the document a step asked the verb to write.
func (r *contractRun) readDocument(path, stepID string) document {
	r.t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		r.t.Fatalf("step %q: the verb was asked to write %q and did not: %v", stepID, path, err)
	}
	var decoded document
	if err := json.Unmarshal(raw, &decoded); err != nil {
		r.t.Fatalf("step %q: %q does not hold a JSON object: %v", stepID, path, err)
	}
	return decoded
}

func (r *contractRun) performPrepare(step contractStep) contractOutcome {
	r.t.Helper()
	manifest := contractManifest{
		Version:     contractManifestVersion,
		Root:        step.Manifest.Root,
		Team:        step.Manifest.Team,
		RegistryDir: filepath.Join(r.work, "registry-a"),
	}
	for _, input := range step.Manifest.Inputs {
		manifest.Inputs = append(manifest.Inputs, contractManifestInput{
			Source: r.writeInput(input.Destination, input.Body), Destination: input.Destination,
		})
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		r.t.Fatalf("step %q: encode the manifest: %v", step.ID, err)
	}
	manifestPath := filepath.Join(r.work, "manifest-"+strings.ReplaceAll(step.ID, " ", "-")+".json")
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		r.t.Fatalf("step %q: write the manifest: %v", step.ID, err)
	}
	return r.performVerb(contractStep{ID: step.ID, Verb: "prepare",
		Args: map[string]string{"manifest": manifestPath}})
}

// childEnv isolates the default registry and strips ambient credentials. adopt
// takes no --registry-dir, so without a temporary home the scenario would write a
// workspace into the developer's real ~/.config and depend on whatever was already
// registered there. Stripping AWS_ and STOW_ is the same precaution the product
// makes about run-through mode: a test must not be able to reach a real account.
func (r *contractRun) childEnv() []string {
	env := []string{
		"HOME=" + r.home,
		"XDG_CONFIG_HOME=" + filepath.Join(r.home, ".config"),
		"APPDATA=" + filepath.Join(r.home, "AppData", "Roaming"),
	}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "HOME=") || strings.HasPrefix(entry, "XDG_CONFIG_HOME=") ||
			strings.HasPrefix(entry, "APPDATA=") ||
			strings.HasPrefix(entry, "AWS_") || strings.HasPrefix(entry, "STOW_") {
			continue
		}
		env = append(env, entry)
	}
	return env
}

// writeInput materialises one declared input outside the workspace and returns its
// path, so the manifest names a real file the way a caller's would.
func (r *contractRun) writeInput(destination, body string) string {
	r.t.Helper()
	source := filepath.Join(r.work, "inputs", filepath.FromSlash(destination))
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		r.t.Fatalf("create the input directory for %q: %v", destination, err)
	}
	if err := os.WriteFile(source, []byte(body), 0o600); err != nil {
		r.t.Fatalf("write the input %q: %v", destination, err)
	}
	return source
}

// tamper corrupts a produced document in the way that matters, which is not a
// flipped byte. A byte flip usually lands in the JSON and is refused by the parser,
// so a test built on one passes for a reason that has nothing to do with integrity.
// This alters one byte of one payload and re-encodes: the result is well-formed and
// no longer matches the digest it carries.
func (r *contractRun) tamper(source, target string) {
	r.t.Helper()
	raw, err := os.ReadFile(source)
	if err != nil {
		r.t.Fatalf("read %q to tamper with it: %v", source, err)
	}
	var decoded document
	if err := json.Unmarshal(raw, &decoded); err != nil {
		r.t.Fatalf("%q is not a JSON object, so it cannot be tampered with meaningfully: %v", source, err)
	}
	var content map[string]string
	if err := json.Unmarshal(decoded["content"], &content); err != nil || len(content) == 0 {
		r.t.Fatalf("%q carries no decodable content, so there is nothing to substitute", source)
	}
	names := sortedArgs(content)
	payload, err := base64.StdEncoding.DecodeString(content[names[0]])
	if err != nil {
		r.t.Fatalf("%q: the content of %q is not valid base64: %v", source, names[0], err)
	}
	if len(payload) == 0 {
		r.t.Fatalf("%q: the content of %q is empty, so there is nothing to substitute", source, names[0])
	}
	payload[0] ^= 0x01
	content[names[0]] = base64.StdEncoding.EncodeToString(payload)
	encodedContent, err := json.Marshal(content)
	if err != nil {
		r.t.Fatalf("re-encode the tampered content: %v", err)
	}
	decoded["content"] = encodedContent

	altered, err := json.Marshal(decoded)
	if err != nil {
		r.t.Fatalf("re-encode the tampered document: %v", err)
	}
	if err := os.WriteFile(target, altered, 0o600); err != nil {
		r.t.Fatalf("write the tampered copy %q: %v", target, err)
	}
}

// renameInTransit builds the substitution the document digest exists to catch: a
// change is moved to a different path, in the change list and the content map
// together, and the result re-encoded.
//
// The two have to move together or the document stops being self-consistent and is
// refused for the wrong reason. The result passes every check the document makes
// about itself — the per-file digest still matches the bytes it carries, and an
// addition's precondition passes because the new name is absent from the base — so
// only a digest over the document as a whole can catch it, which is what
// --expect-sha256 supplies.
func (r *contractRun) renameInTransit(rename *contractRename, target string) {
	r.t.Helper()
	raw, err := os.ReadFile(r.workPath(rename.From))
	if err != nil {
		r.t.Fatalf("read %q to substitute it: %v", rename.From, err)
	}
	var document document
	if err := json.Unmarshal(raw, &document); err != nil {
		r.t.Fatalf("%q is not a JSON object, so it cannot be substituted: %v", rename.From, err)
	}
	if !r.renameChange(&document, rename) {
		r.t.Fatalf("%q has no change naming %q, so there is nothing to substitute", rename.From, rename.Path)
	}
	if !r.moveContent(&document, rename) {
		r.t.Fatalf("%q carries no content for %q, so a rename cannot stay self-consistent", rename.From, rename.Path)
	}

	altered, err := json.Marshal(document)
	if err != nil {
		r.t.Fatalf("re-encode the substituted document: %v", err)
	}
	if err := os.WriteFile(target, altered, 0o600); err != nil {
		r.t.Fatalf("write the substituted document %q: %v", target, err)
	}
}

// renameChange moves one change's declared path, in the change list and in the
// change's own to-metadata. It reports whether it found the change at all, so a
// case file naming a path the document does not carry fails as a broken case rather
// than as a substitution that quietly did nothing.
func (r *contractRun) renameChange(doc *document, rename *contractRename) bool {
	var changes []json.RawMessage
	raw, ok := (*doc)["changes"]
	if !ok || json.Unmarshal(raw, &changes) != nil {
		r.t.Fatalf("the document carries no change list")
	}
	renamed := false
	for i, entry := range changes {
		var change document
		if json.Unmarshal(entry, &change) != nil {
			continue
		}
		// The comparison is against the decoded string. Comparing the raw JSON
		// would include the quotes, and a case file that names a real path would
		// silently match nothing — which is a broken case file reported as a
		// substitution that did nothing.
		if decoded, err := decodeString(change["path"]); err != nil || decoded != rename.Path {
			continue
		}
		change["path"] = r.encodeJSON(rename.To)
		if to, ok := change["to"]; ok {
			var target document
			if json.Unmarshal(to, &target) == nil {
				target["path"] = r.encodeJSON(rename.To)
				change["to"] = r.encodeJSON(target)
			}
		}
		changes[i] = r.encodeJSON(change)
		renamed = true
	}
	(*doc)["changes"] = r.encodeJSON(changes)
	return renamed
}

// moveContent moves the payload a change carries, so the renamed document stays
// self-consistent. Without this the substitution is caught by the document refusing
// to decode, which is a different refusal and would make the digest look like it
// was working when it was not the thing being tested.
func (r *contractRun) moveContent(doc *document, rename *contractRename) bool {
	var content map[string]json.RawMessage
	raw, ok := (*doc)["content"]
	if !ok || json.Unmarshal(raw, &content) != nil {
		r.t.Fatalf("the document carries no content map")
	}
	payload, ok := content[rename.Path]
	if !ok {
		return false
	}
	delete(content, rename.Path)
	content[rename.To] = payload
	(*doc)["content"] = r.encodeJSON(content)
	return true
}

func replaceArgValue(argv []string, flag, value string) []string {
	out := append([]string(nil), argv...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == flag {
			out[i+1] = value
		}
	}
	return out
}

// applyWrites changes the workspace the way an agent would, between checkpoints.
func (r *contractRun) applyWrites(writes []contractWrite) {
	r.t.Helper()
	root := r.captures["root"]
	if root == "" {
		r.t.Fatal("a step writes into the workspace before any step has prepared one")
	}
	for _, write := range writes {
		full := filepath.Join(root, filepath.FromSlash(write.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			r.t.Fatalf("create the parent of %q: %v", write.Path, err)
		}
		if err := os.WriteFile(full, []byte(write.Body), 0o600); err != nil {
			r.t.Fatalf("write %q: %v", write.Path, err)
		}
	}
}

// readTree turns a workspace root into the flat relative-path map a "read" step
// asserts against, so the scenario can state what is on disk rather than how to
// go looking for it.
func (r *contractRun) readTree(root string) document {
	r.t.Helper()
	files := document{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(string(body))
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = encoded
		return nil
	})
	if err != nil {
		r.t.Fatalf("read the workspace tree at %q: %v", root, err)
	}
	return files
}

func describeOutcome(out contractOutcome) string {
	return fmt.Sprintf("exit %d\nstdout: %s\nstderr: %s", out.code, out.rawStdout, out.stderr)
}
