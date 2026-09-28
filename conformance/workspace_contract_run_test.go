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
func (r *contractRun) perform(step contractStep) contractOutcome {
	r.t.Helper()
	switch step.Verb {
	case "noop":
		r.applyWrites(step.Write)
		return contractOutcome{}
	case "read":
		return contractOutcome{stdout: r.readTree(r.substitute(step.Root))}
	case "prepare":
		return r.performPrepare(step)
	default:
		return r.performVerb(step)
	}
}

func (r *contractRun) performVerb(step contractStep) contractOutcome {
	r.t.Helper()
	argv := append([]string{"workspace", step.Verb}, r.substituteArgs(step)...)
	if dir := r.registryDirFor(step); dir != "" {
		argv = append(argv, "--registry-dir", dir)
	}
	if step.Team != "" {
		argv = append(argv, "--team", step.Team)
	}
	if step.Tamper != "" {
		// The refusal has to come from the digest, so the file the verb reads is a
		// corrupt copy of a document this scenario really produced. A path that
		// simply does not exist would be refused too, and for the wrong reason.
		tampered := r.workPath("tampered-" + step.Tamper)
		r.tamper(r.workPath(step.Tamper), tampered)
		argv = replaceArgValue(argv, "--delta", tampered)
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
	// prints nothing, and a driver that decodes stdout sees an empty object — which
	// is precisely the bug the TypeScript and Python wrappers had, reproduced by a
	// third implementation written from the CLI's observable behaviour. Three
	// drivers making the same mistake is what makes this a contract rather than a
	// convention.
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
		Root:        r.substitute(step.Manifest.Root),
		Team:        r.substitute(step.Manifest.Team),
		RegistryDir: filepath.Join(r.work, "registry-a"),
	}
	for _, input := range step.Manifest.Inputs {
		destination := r.substitute(input.Destination)
		manifest.Inputs = append(manifest.Inputs, contractManifestInput{
			Source: r.writeInput(destination, input.Body), Destination: destination,
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

// registryDirFor names the explicit registry a step works in. The team partition
// inside it is not resolved here: the step states its team and the CLI composes
// the two exactly as it does for a manifest, so the driver never has to know the
// partition layout. Reconstructing it was the earlier approach and it was wrong in
// the way that matters — a driver that guesses where a team lives will quietly look
// in the wrong place and report a missing workspace.
func (r *contractRun) registryDirFor(step contractStep) string {
	// A step that names --registry-dir itself is authoritative, and that is how the
	// contract reaches a team partition: prepare reports the partition it created,
	// and collect and destroy — which take --registry-dir but no --team — are
	// pointed straight at it. Every other verb gets the driver's own directory plus
	// the team's --team flag and lets the CLI compose the two, because a driver that
	// reconstructed the partition path itself would be asserting a belief about the
	// store rather than about the verb.
	for _, name := range sortedArgs(step.Args) {
		if name == "registry-dir" {
			return r.substitute(step.Args[name])
		}
	}
	if step.Registry != contractRegistryA || !contractRegistryDirFlags[step.Verb] {
		return ""
	}
	return filepath.Join(r.work, "registry-a")
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
// This decodes the document, alters one byte of one file's payload, and re-encodes
// it: the result is a well-formed document whose content no longer matches the
// digest it carries, which is the substitution a receiver has to catch.
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
		full := filepath.Join(root, filepath.FromSlash(r.substitute(write.Path)))
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
