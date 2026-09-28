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
	"strings"
	"sync"
	"testing"
)

// The workspace contract in conformance/workspace/cases.json is read by three
// drivers: this one, the TypeScript wrapper's, and the Python wrapper's. The point
// is not that the engine works — conformance/ already proves that against the S3
// surface — but that a client cannot quietly disagree with the engine about what a
// verb returns.
//
// The two wrappers had never been run against the binary they wrap. They were
// wrong in the same way in both languages, and the only reason anyone found out is
// that a divergence finally put the surface into CI. A surface that was wrong on
// its first automated run should be assumed to be wrong in ways nobody has looked
// at yet, which is what this file exists to make cheap.

// document is one decoded JSON object, held as raw values rather than as a tree
// of interface{}.
//
// The raw form is deliberate. A driver that decodes into a tree of `any` has to
// re-marshal before it can compare, has to type-assert before it can read a field,
// and puts a use of `any` on nearly every line — which this repository's quality
// gate counts. Keeping the JSON raw means each matcher interprets exactly the value
// it needs, comparisons run on a canonical encoding so object key order cannot make
// two equal documents look different, and the driver says what it is about.
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
// into a manifest that refuses unknown fields, which is a sharper failure than a
// missing seed would have been.
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
	ID          string                         `json:"id"`
	Verb        string                         `json:"verb"`
	Registry    string                         `json:"registry"`
	Team        string                         `json:"team"`
	Args        map[string]string              `json:"args"`
	Root        string                         `json:"root"`
	Manifest    *contractManifestSpec          `json:"manifest"`
	Write       []contractWrite                `json:"write"`
	Capture     map[string]string              `json:"capture"`
	Expect      map[string]contractExpectation `json:"expect"`
	AlsoWritten string                         `json:"alsoWritten"`
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

var contractRegistryDirFlags = map[string]bool{
	"checkpoint": true, "diff": true, "export": true, "import": true,
	"restore": true, "resume": true, "handoff": true, "delta": true,
	"apply": true, "destroy": true, "collect": true,
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

// substitute replaces the {{...}} references a step's arguments may carry. The
// work directory and every capture are in one namespace, so a capture called
// "work" would shadow the directory; the case file is the contract and it does not
// do that.
func (r *contractRun) substitute(text string) string {
	for name, value := range r.captures {
		text = strings.ReplaceAll(text, "{{"+name+"}}", value)
	}
	return text
}

var contractCapturePattern = regexp.MustCompile(`\{\{([a-zA-Z0-9_]+)\}\}`)

// substituteArgs substitutes every declared argument and fails on one that names a
// capture nobody produced. A silently empty argument is a scenario that quietly
// stopped testing what it says it tests, which is the failure mode a shared
// contract cannot have.
func (r *contractRun) substituteArgs(step contractStep) []string {
	var argv []string
	for _, name := range sortedArgs(step.Args) {
		value := r.substitute(step.Args[name])
		if match := contractCapturePattern.FindStringSubmatch(value); match != nil {
			if _, ok := r.captures[match[1]]; !ok {
				r.t.Fatalf("step %q: argument %q refers to {{%s}}, which no earlier step captured",
					step.ID, name, match[1])
			}
		}
		argv = append(argv, "--"+name, value)
	}
	return argv
}

// sortedKeys is a map walked in a stable order, so a failure is reported against
// the first field the case file declares rather than whichever one the runtime
// happened to yield.
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

// sortedFieldNames is a map walked in a stable order, so a failure is reported
// against the first field the case file declares rather than whichever one the
// runtime happened to yield. The order is part of the contract: three drivers
// reporting a different "first" failure for the same defect is three bug reports.
//
// Two small functions rather than one generic, because the generic's type
// parameter would need `any` and this repository ratchets against `any`.
// sortedExpectations and sortedArgs are the two maps the driver walks, each in a
// stable order so a failure is reported against the first field the case file
// declares rather than whichever one the runtime happened to yield. The order is
// part of the contract: three drivers reporting a different "first" failure for the
// same defect is three bug reports.
//
// Two small functions rather than one generic, because the generic's type
// parameter would need `any` and this repository ratchets against `any`.
func sortedExpectations(fields map[string]contractExpectation) []string {
	return slices.Sorted(maps.Keys(fields))
}

func sortedArgs(args map[string]string) []string {
	return slices.Sorted(maps.Keys(args))
}
