package conformance_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

// The workspace contract in conformance/workspace/cases.json is read by three
// drivers: this one and the TypeScript and Python wrappers'. The point is not that
// the engine works — the rest of conformance/ proves that — but that a client cannot
// quietly disagree with the engine about what a verb returns.
//
// The two wrappers had never been run against the binary they wrap, and were wrong
// in the same way in both languages. A surface wrong on its first automated run
// should be assumed wrong in ways nobody has looked at yet, which is what this file
// makes cheap.

// document is one decoded JSON object, held as raw values rather than as a tree
// of interface{}.
//
// The raw form is deliberate. A driver that decodes into a tree of `any` has to
// re-marshal before it can compare, has to type-assert before it can read a field,
// and puts a use of `any` on nearly every line, which the quality gate counts. Raw
// means each matcher interprets exactly the value it needs, and comparisons run on a
// canonical encoding so key order cannot make two equal documents look different.
type document = map[string]json.RawMessage

// contractExpectation is one declared property of one field. Everything the
// scenario asserts is declared in the case file rather than written here, so
// adding a property to the contract does not mean editing three drivers.
type contractExpectation struct {
	Equals    *json.RawMessage `json:"equals"`
	NotEquals *json.RawMessage `json:"notEquals"`
	Matches   string           `json:"matches"`
	Length    *int             `json:"length"`
	MinLength *int             `json:"minLength"`
	Min       *float64         `json:"min"`
	Absent    bool             `json:"absent"`
}

// contractInput is one declared seed. The case file states the body rather than a
// path, because a path would be a fact about the machine that wrote the case file
// rather than a fact about the contract; the driver materialises the body and puts
// the real path in the manifest.
type contractInput struct {
	Body        string `json:"body"`
	Destination string `json:"destination"`
}

// contractManifestInput is the manifest's own input shape, kept apart from
// contractInput because the two are different contracts: one is what a case file
// declares, the other is what the CLI reads. Conflating them put a "body" field
// into a manifest that refuses unknown fields.
type contractManifestInput struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// contractManifestSpec is the manifest as the case file declares it: a root, a
// team, and seeds stated as bodies. The driver turns it into a contractManifest
// by writing the bodies out and naming the files.
type contractManifestSpec struct {
	Root   string          `json:"root"`
	Team   string          `json:"team"`
	Inputs []contractInput `json:"inputs"`
}

type contractManifest struct {
	Version     int                     `json:"version"`
	Root        string                  `json:"root"`
	Team        string                  `json:"team"`
	RegistryDir string                  `json:"registry_dir"`
	Inputs      []contractManifestInput `json:"inputs"`
}

type contractFailure struct {
	Contains string `json:"contains"`
}

// contractRename describes one substitution to perform on a produced document.
type contractRename struct {
	From string `json:"from"`
	Path string `json:"path"`
	To   string `json:"to"`
}

// contractWrite is one file a step puts into the workspace, the way an agent
// editing a file would between two checkpoints.
type contractWrite struct {
	Path string `json:"path"`
	Body string `json:"body"`
}

type contractStep struct {
	ID       string                `json:"id"`
	Verb     string                `json:"verb"`
	Registry string                `json:"registry"`
	Team     string                `json:"team"`
	Args     map[string]string     `json:"args"`
	Root     string                `json:"root"`
	Manifest *contractManifestSpec `json:"manifest"`
	Write    []contractWrite       `json:"write"`
	// Remove deletes a path out from under stow — the only condition a workspace is
	// pruneable under, and without it a case file can only prove prune does nothing.
	Remove string `json:"remove"`
	// DocumentFields names fields of the document the step wrote, checked by opening
	// it. Without it a step asserts only that the file appeared.
	DocumentFields map[string]string `json:"documentFields"`
	// IncludeAdopted is prune's opt-in to forgetting a caller's entry: the one flag
	// whose absence turns a refusal into a deletion.
	IncludeAdopted bool                           `json:"includeAdopted"`
	Capture        map[string]string              `json:"capture"`
	Expect         map[string]contractExpectation `json:"expect"`
	AlsoWritten    string                         `json:"alsoWritten"`
	// RenameInTransit builds a substituted document: the named change is moved to
	// a different path, in the change list and the content map together, and the
	// result re-encoded. It is how a document altered on the way to the receiver is
	// built, and the case file uses it to state the substitution as a refusal.
	RenameInTransit *contractRename `json:"renameInTransit"`
	// Returns says where the caller gets its document: "stdout" (the default) or
	// "file". The two verbs that take --output disagree, and that disagreement is
	// the contract rather than an inconsistency to smooth over. handoff writes the
	// document itself to the path and prints nothing, so a caller asking for a path
	// has to read the file; delta writes the transported document to the path and
	// prints its own result describing it, so the caller wants what was printed.
	Returns string           `json:"returns"`
	Tamper  string           `json:"tamper"`
	Fail    *contractFailure `json:"fail"`
}

type contract struct {
	Version int            `json:"version"`
	Steps   []contractStep `json:"steps"`
}

// contractRegistries are the registries the driver owns. "a" is explicit, so a
// team partition inside it is created by the engine rather than guessed here.
// "default" is whatever the binary resolves with no --registry-dir, which is
// isolated under a temporary home so a test cannot write to a developer's real
// ~/.config.
const contractRegistryA = "a"

// contractManifestVersion is the only task manifest version this build accepts,
// and the driver writes it rather than reading it from the case file: a case file
// that could state a version the engine refuses would be a second thing to keep in
// step, and the refusal is already the engine's own test.
const contractManifestVersion = 1

// contractReturnsFile marks a step whose verb writes the document the caller wants
// to the named path and prints nothing else. See contractStep.Returns.
const contractReturnsFile = "file"

// checkContractRegistryFlags asserts that every step in registry "a" either names
// its own registry directory or is covered by the table, so a verb added without a
// table entry is reported as the missing entry rather than as what that verb does
// when handed the wrong registry.
func checkContractRegistryFlags(steps []contractStep) []string {
	var problems []string
	for _, step := range steps {
		if step.Registry != contractRegistryA || contractRegistryDirFlags[step.Verb] {
			continue
		}
		if _, declared := step.Args["registry-dir"]; declared || step.Manifest != nil {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"the workspace contract step %q runs %q in registry %q, but %q is not in contractRegistryDirFlags and the step declares no registry of its own: the verb would run against the default registry and report an empty one",
			step.ID, step.Verb, step.Registry, step.Verb))
	}
	return problems
}

var (
	contractBinaryOnce sync.Once
	contractBinaryPath string
	contractBinaryErr  error
)

// contractBinary builds stow-s3 once per run. The contract is about what a
// process writes to stdout and stderr, so there is nothing to assert without
// running one.
func contractBinary(t *testing.T) string {
	t.Helper()
	contractBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "stow-workspace-contract-*")
		if err != nil {
			contractBinaryErr = err
			return
		}
		out := filepath.Join(dir, "stow-s3")
		if runtime.GOOS == "windows" {
			out += ".exe"
		}
		build := exec.Command("go", "build", "-o", out, "../cmd/stow-s3")
		build.Env = append(os.Environ(), "GOFLAGS=")
		if output, err := build.CombinedOutput(); err != nil {
			contractBinaryErr = fmt.Errorf("go build: %v\n%s", err, output)
			return
		}
		contractBinaryPath = out
	})
	if contractBinaryErr != nil {
		t.Skipf("cannot build the stow-s3 binary: %v", contractBinaryErr)
	}
	return contractBinaryPath
}

func loadContract(t *testing.T) contract {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("workspace", "cases.json"))
	if err != nil {
		t.Fatalf("read the workspace contract: %v", err)
	}
	var c contract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parse the workspace contract: %v", err)
	}
	if c.Version != 1 {
		t.Fatalf("the workspace contract is version %d, and this driver reads version 1", c.Version)
	}
	if len(c.Steps) == 0 {
		t.Fatal("the workspace contract declares no steps, so it asserts nothing")
	}
	// A "noop" step exists to change the workspace the way an agent would, and its
	// assertion is the next step's: the checkpoint after a write is what proves the
	// write happened. Every other step has to state what it expects, or the file
	// can rot into a sequence of calls that passes because nothing failed.
	for _, step := range c.Steps {
		if step.Verb == "noop" {
			continue
		}
		if len(step.Expect) == 0 && step.Fail == nil {
			t.Errorf("the workspace contract step %q declares neither an expectation nor a refusal, so it asserts nothing", step.ID)
		}
	}
	for _, problem := range checkContractRegistryFlags(c.Steps) {
		t.Error(problem)
	}
	// Checked here rather than in a separate test: loading the case file is already
	// the moment the driver is about to run every verb.
	takes, verbs := registryFlagsFromBinary(t, contractBinary(t))
	for _, problem := range checkRegistryFlagsAgainstBinary(takes, verbs) {
		t.Error(problem)
	}
	return c
}

// contractRun carries the state one pass through the scenario accumulates. It is
// the driver's whole mutable surface, which keeps the drivers comparable: each one
// has to reach the same conclusions from the same file with the same amount of
// state.
type contractRun struct {
	t        *testing.T
	work     string
	home     string
	binary   string
	captures map[string]string
	// broken names the step that already failed. The steps share a registry and a
	// work directory, so continuing past a failure reports one missing workspace
	// as a dozen later failures and buries the one that broke. t.Failed() cannot
	// do this job: inside a subtest it reports that subtest, which has not failed
	// yet.
	broken string
}

func newContractRun(t *testing.T) *contractRun {
	t.Helper()
	work := t.TempDir()
	home := filepath.Join(work, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create the temporary home: %v", err)
	}
	return &contractRun{
		t:        t,
		work:     work,
		home:     home,
		binary:   contractBinary(t),
		captures: map[string]string{"work": work},
	}
}

// substitute replaces the {{...}} references a step's text may carry. The work
// directory and every capture are in one namespace, so a capture called "work" would
// shadow the directory; the case file is the contract and it does not do that.
func (r *contractRun) substitute(text string) string {
	for name, value := range r.captures {
		text = strings.ReplaceAll(text, "{{"+name+"}}", value)
	}
	return text
}

var contractCapturePattern = regexp.MustCompile(`\{\{([a-zA-Z0-9_]+)\}\}`)

// resolve returns the step with every {{...}} reference replaced by the value the named
// capture holds, so a reference nothing captured is a broken case file rather than a
// silently empty argument.
//
// It is one pass that cannot be skipped, which is the point: resolving wherever a
// resolved value was wanted left a call site reading the raw arguments, pointing a
// step at a directory literally named "{{registryA}}" and reporting an empty
// registry rather than an error.
//
// It also covers the expectations. A step that expects "the root the manifest
// named" is stating a fact about the contract, and comparing that against the
// literal text {{work}}/workspace fails on the substitution, not the property.
func (r *contractRun) resolve(step contractStep) contractStep {
	step.Args = r.resolveArgs(step, step.Args)
	step.Root = r.resolveText(step, "root", step.Root)
	step.Team = r.resolveText(step, "team", step.Team)
	step.AlsoWritten = r.resolveText(step, "alsoWritten", step.AlsoWritten)
	step.Tamper = r.resolveText(step, "tamper", step.Tamper)
	step.Write = r.resolveWrites(step, step.Write)
	step.Manifest = r.resolveManifest(step, step.Manifest)
	step.RenameInTransit = r.resolveRename(step, step.RenameInTransit)
	step.Expect = r.resolveExpectations(step, step.Expect)
	return step
}

// resolveText resolves one text and fails on a reference no capture produced.
// substitute has already replaced every reference it could, so a reference still
// standing names a capture that does not exist — and handing the verb the literal
// text is how a step passes without testing the thing it names.
func (r *contractRun) resolveText(step contractStep, what, text string) string {
	resolved := r.substitute(text)
	if match := contractCapturePattern.FindStringSubmatch(resolved); match != nil {
		if _, ok := r.captures[match[1]]; !ok {
			r.t.Fatalf("step %q: its %s refers to {{%s}}, which no earlier step captured",
				step.ID, what, match[1])
		}
	}
	return resolved
}

// resolveArgs resolves the flags into a new map, so the case file the run loaded is
// not edited underneath it.
func (r *contractRun) resolveArgs(step contractStep, args map[string]string) map[string]string {
	resolved := make(map[string]string, len(args))
	for name, value := range args {
		resolved[name] = r.resolveText(step, "argument "+name, value)
	}
	return resolved
}

func (r *contractRun) resolveWrites(step contractStep, writes []contractWrite) []contractWrite {
	resolved := make([]contractWrite, len(writes))
	for i, write := range writes {
		// Body is a literal the case file states rather than a path it names, so it
		// is the one field here that is left exactly as written.
		resolved[i] = contractWrite{
			Path: r.resolveText(step, "write path", write.Path),
			Body: write.Body,
		}
	}
	return resolved
}

func (r *contractRun) resolveManifest(step contractStep, spec *contractManifestSpec) *contractManifestSpec {
	if spec == nil {
		return nil
	}
	resolved := *spec
	resolved.Root = r.resolveText(step, "manifest root", spec.Root)
	resolved.Team = r.resolveText(step, "manifest team", spec.Team)
	resolved.Inputs = make([]contractInput, len(spec.Inputs))
	for i, input := range spec.Inputs {
		resolved.Inputs[i] = contractInput{
			Body:        input.Body,
			Destination: r.resolveText(step, "input destination", input.Destination),
		}
	}
	return &resolved
}

// resolveRename resolves only the source. Path names a path inside the transported
// document and To is the path that document will claim instead; neither is a fact
// about this machine, so no capture produces either.
func (r *contractRun) resolveRename(step contractStep, rename *contractRename) *contractRename {
	if rename == nil {
		return nil
	}
	return &contractRename{
		From: r.resolveText(step, "rename source", rename.From),
		Path: rename.Path,
		To:   rename.To,
	}
}

func (r *contractRun) resolveExpectations(step contractStep, expect map[string]contractExpectation) map[string]contractExpectation {
	resolved := make(map[string]contractExpectation, len(expect))
	for path, want := range expect {
		want.Equals = r.resolveValue(step, path, want.Equals)
		want.NotEquals = r.resolveValue(step, path, want.NotEquals)
		resolved[path] = want
	}
	return resolved
}

// resolveValue resolves a reference inside an expected value, which the case file
// states as a JSON string. A value that is not a string is a number, a boolean, an
// object or an array, and none of those can carry one: every path a capture produces
// is a string, and a case that buried one inside an object would be stating a fact
// no capture can supply.
func (r *contractRun) resolveValue(step contractStep, path string, raw *json.RawMessage) *json.RawMessage {
	if raw == nil {
		return nil
	}
	text, err := decodeString(*raw)
	if err != nil {
		return raw
	}
	encoded := r.encodeJSON(r.resolveText(step, "expected "+path, text))
	return &encoded
}

// workPath resolves a path a case file names. A bare name means a file in the work
// directory, so a case can say "handoff.json" rather than repeating the work
// directory on every artifact; an absolute path is taken as given. Resolving this
// in one place is what keeps a scenario from writing into the tree it was run
// from, which is how a contract runner ends up deleting something.
func (r *contractRun) workPath(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(r.work, name)
}

func sortedDocumentFields(fields map[string]string) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// sortedExpectations and sortedArgs are the two maps the driver walks, each in a
// stable order so a failure is reported against the first field the case file
// declares rather than whichever one the runtime happened to yield. The order is
// part of the contract: three drivers reporting a different "first" failure for the
// same defect is three bug reports.
func sortedExpectations(fields map[string]contractExpectation) []string {
	return slices.Sorted(maps.Keys(fields))
}

func sortedArgs(args map[string]string) []string {
	return slices.Sorted(maps.Keys(args))
}
