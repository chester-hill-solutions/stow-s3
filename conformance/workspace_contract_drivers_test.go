package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The contract is one case file read by three drivers, and the drivers share no
// type: a Go struct, a TypeScript interface, a Python dict. Nothing made them agree.
// `includeAdopted` was declared in TypeScript, read by TypeScript and Python, and
// absent from Go, so the three disagreed about what a prune step means and the case
// file passed anyway — a field no driver reads is not an error in any of the three
// languages. So the surfaces are compared here rather than trusted.
var (
	goStepFields   = regexp.MustCompile("`json:\"([a-zA-Z]+)\"`")
	tsStepFields   = regexp.MustCompile("readonly ([a-zA-Z]+)\\??:")
	pyStepReadings = regexp.MustCompile(`step(?:\.get\(|\[)"([a-zA-Z]+)"`)
)

// declaredGoStepFields reads the field tags off contractStep. It is anchored on the
// struct so a json tag elsewhere in the package cannot join the set.
func declaredGoStepFields(t *testing.T) []string {
	t.Helper()
	body := between(t, "workspace_contract_harness_test.go",
		"type contractStep struct {", "\n}")
	return matchAll(goStepFields, body)
}

func declaredTSStepFields(t *testing.T) []string {
	t.Helper()
	body := between(t, "../packages/stow-s3/test/workspace-contract-runner.ts",
		"export interface ContractStep {", "\n}")
	return matchAll(tsStepFields, body)
}

func readPythonStepFields(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../packages/stow-s3-py/tests/workspace_contract.py")
	if err != nil {
		t.Fatalf("read the Python contract driver: %v", err)
	}
	return matchAll(pyStepReadings, string(raw))
}

func TestContractDriversAgreeOnTheStepSurface(t *testing.T) {
	surfaces := []struct {
		driver string
		fields []string
	}{
		{"Go", declaredGoStepFields(t)},
		{"TypeScript", declaredTSStepFields(t)},
		{"Python", readPythonStepFields(t)},
	}
	want := fieldsOf(surfaces[0].fields)
	for _, surface := range surfaces[1:] {
		if got := fieldsOf(surface.fields); !equal(got, want) {
			t.Errorf("the %s driver reads a different set of step fields than the Go driver:\n  only in Go:   %v\n  only in %s: %v",
				surface.driver, missing(want, got), surface.driver, missing(got, want))
		}
	}
}

// TestContractCaseFileUsesOnlyKnownFields catches the other direction: a case file
// naming a field no driver reads. The step then asserts less than it appears to.
func TestContractCaseFileUsesOnlyKnownFields(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("workspace", "cases.json"))
	if err != nil {
		t.Fatalf("read the workspace contract: %v", err)
	}
	var doc struct {
		Steps []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse the workspace contract: %v", err)
	}
	known := fieldsOf(declaredGoStepFields(t))
	// Per step, not as a set: the step id is what a reader needs to
	// find the case.
	for _, step := range doc.Steps {
		for _, key := range sortedKeys(step) {
			if !contains(known, key) {
				t.Errorf("the workspace contract step %q names the field %q, which no driver reads, so the step does not do what it says",
					step["id"], key)
			}
		}
	}
}

func between(t *testing.T, path, start, end string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(raw)
	from := strings.Index(text, start)
	if from == -1 {
		t.Fatalf("%s has no %q", path, start)
	}
	text = text[from+len(start):]
	to := strings.Index(text, end)
	if to == -1 {
		t.Fatalf("%s has no %q after %q", path, end, start)
	}
	return text[:to]
}

func matchAll(re *regexp.Regexp, text string) []string {
	found := map[string]bool{}
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		found[match[1]] = true
	}
	out := make([]string, 0, len(found))
	for name := range found {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func fieldsOf(names []string) []string { return names }

func missing(want, got []string) []string {
	var out []string
	for _, name := range want {
		if !contains(got, name) {
			out = append(out, name)
		}
	}
	return out
}

func contains(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}

func equal(a, b []string) bool { return len(a) == len(b) && len(missing(a, b)) == 0 }

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// TestContractDriversIsolateTheDefaultRegistry is the check that should have existed
// before the first run of a suite that lacked it.
//
// Two drivers call the wrapper in-process and it spawns the binary without passing an
// environment, so a step naming no --registry-dir — which the contract does on
// purpose, because adopt taking no --registry-dir is under test — resolves against
// the test process's own HOME. Both suites registered workspaces in the developer's
// real config directory on every run and then swept it: Go saw the count it should
// and Python saw 72, and both were green. The Go driver cannot see that from inside
// its own process, so it reads the other two. A source check is a weak instrument
// for behaviour and the strongest available across three languages; the alternative
// is a test that damages the registry in order to notice.
func TestContractDriversIsolateTheDefaultRegistry(t *testing.T) {
	// Whole directories, not the two files that happened to hold the code when this
	// was written: the isolation is a concern of its own and has already been moved
	// out of the runner once, which a path-pinned check would have called a failure.
	drivers := map[string]string{
		"TypeScript": "../packages/stow-s3/test",
		"Python":     "../packages/stow-s3-py/tests",
	}
	for driver, dir := range drivers {
		text := driverSource(t, dir)
		for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "APPDATA"} {
			if !strings.Contains(text, name) {
				t.Errorf("the %s contract driver never mentions %s, so a step that names no --registry-dir is resolved from the developer's own home and the scenario registers and sweeps real workspaces",
					driver, name)
			}
		}
		// Both must put it back: a leaked override outlives the run.
		if !strings.Contains(text, "restore") && !strings.Contains(text, "finally") {
			t.Errorf("the %s contract driver sets a temporary home without putting it back, so every later test in the process inherits it", driver)
		}
	}
}

// driverSource concatenates a directory's Go, TypeScript and Python files, which is
// the unit a driver's behaviour lives in: a driver is a directory of test helpers,
// not one file.
func driverSource(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the %s contract driver directory: %v", dir, err)
	}
	var out strings.Builder
	for _, entry := range entries {
		switch filepath.Ext(entry.Name()) {
		case ".ts", ".py":
		default:
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		out.Write(raw)
	}
	if out.Len() == 0 {
		t.Fatalf("the %s contract driver directory holds no source", dir)
	}
	return out.String()
}
