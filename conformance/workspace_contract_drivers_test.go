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
// type: a Go struct, a TypeScript interface, a Python dict. Nothing made them agree,
// and `includeAdopted` is how that showed: declared in two, read by two, absent from
// the third, with the case file passing throughout.
var (
	goStepFields   = regexp.MustCompile("`json:\"([a-zA-Z]+)\"`")
	tsStepFields   = regexp.MustCompile("readonly ([a-zA-Z]+)\\??:")
	pyStepReadings = regexp.MustCompile(`step(?:\.get\(|\[)"([a-zA-Z]+)"`)
)

// The field tags off contractStep, anchored on the struct so a tag elsewhere cannot join.
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

// TestContractCaseFileUsesOnlyKnownFields catches the other direction: a field no
// driver reads, so the step asserts less than it appears to.
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
	// Per step, not as a set: the step id is what a reader needs.
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
// Two drivers call the wrapper in-process and it spawns the binary without an
// environment, so a step naming no --registry-dir — which the contract does on purpose,
// because adopt takes none — resolved against the developer's own config directory and
// the scenario then swept it: Go saw the count it should, Python saw 72, both green.
func TestContractDriversIsolateTheDefaultRegistry(t *testing.T) {
	// Directories, not the files that held the code when written: it has moved once.
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
// the unit a driver's behaviour lives in: a driver is a directory, not one file.
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

// TestContractStepVocabularyIsDocumented keeps the step vocabulary in agreement
// with the drivers. A schema that cannot fail is a copy somebody will trust, and the
// failure it catches — a driver quietly not implementing a field — is invisible from
// inside any one language.
func TestContractStepVocabularyIsDocumented(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "workspace-contract-steps.md"))
	if err != nil {
		t.Fatalf("read the step vocabulary: %v", err)
	}
	doc := string(raw)
	for _, field := range declaredGoStepFields(t) {
		if !strings.Contains(doc, "`"+field+"`") {
			t.Errorf("the step field %q is declared by the drivers and documented nowhere; a reader of the case file cannot know what it does", field)
		}
	}
	// And the other direction, so the page cannot accumulate fields that were removed.
	for _, field := range documentedStepFields(doc) {
		if !contains(declaredGoStepFields(t), field) {
			t.Errorf("docs/workspace-contract-steps.md documents the step field %q, which no driver declares", field)
		}
	}
}

// The first column of the vocabulary table, the only place a backticked identifier is a name.
func documentedStepFields(doc string) []string {
	var out []string
	inTable := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "| Field |") {
			inTable = true
			continue
		}
		if inTable && !strings.HasPrefix(line, "|") {
			break
		}
		if !inTable {
			continue
		}
		if name := regexp.MustCompile("^\\|\\s*`([a-zA-Z]+)`").FindStringSubmatch(line); name != nil {
			out = append(out, name[1])
		}
	}
	return out
}
