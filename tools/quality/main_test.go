package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The scan: does it measure what it claims to, over the tree the gate scans. These
// predate the split of the scanner from the gate and are unchanged by it.

// The scan has to see the same code the gate runs over. A rule that never fires
// is indistinguishable from a rule that has been satisfied, so each metric is
// provoked on a real file and asserted to be reported.
func TestScanReportsEachMetricItClaimsToCheck(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "internal", "sample")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	source := `package sample

import "any"

// wide takes more than five parameters, so max-params must fire.
func wide(a, b, c, d, e, f int) any { return nil }

// deep is over the complexity ceiling of 15.
func deep(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			total++
		} else if i%3 == 0 {
			total--
		} else if i%5 == 0 {
			total += 2
		} else if i%7 == 0 {
			total -= 2
		} else if i%11 == 0 {
			total++
		} else if i%13 == 0 {
			total--
		} else if i%17 == 0 {
			total += 3
		} else if i%19 == 0 {
			total -= 3
		} else if i%23 == 0 {
			total++
		} else if i%29 == 0 {
			total--
		} else if i%31 == 0 {
			total += 4
		} else if i%37 == 0 {
			total -= 4
		} else if i%41 == 0 {
			total++
		} else if i%43 == 0 {
			total--
		} else if i%47 == 0 {
			total += 5
		} else if i%53 == 0 {
			total -= 5
		}
	}
	return total
}
`
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// scan walks fixed roots, so it is exercised against a temporary checkout
	// rather than the repository: the alternative is a fixture inside the tree,
	// which would itself be scanned and would need its own baseline entry.
	result, err := scanRoots([]string{filepath.Join(dir, "internal")})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	found := map[string]bool{}
	for _, item := range result.Violations {
		found[item.Rule] = true
	}
	for _, rule := range []string{"max-params", "complexity", "any"} {
		if !found[rule] {
			t.Errorf("scan did not report %s; violations were %+v", rule, result.Violations)
		}
	}
	if len(result.Violations) == 0 {
		t.Fatal("scan reported nothing at all for a file that breaks three rules")
	}
}

// A file over the line ceiling must be named, since file-size is the one metric
// whose failure is a whole file rather than a location inside it.
func TestScanReportsFunctionLengthOverTheCeiling(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "internal", "long")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var body strings.Builder
	body.WriteString("package long\n\nfunc enormous() {\n")
	for i := 0; i < 220; i++ {
		body.WriteString("\t_ = 1\n")
	}
	body.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(root, "long.go"), []byte(body.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := scanRoots([]string{filepath.Join(dir, "internal")})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	found := false
	for _, item := range result.Violations {
		if item.Rule == "function-lines" {
			found = true
		}
	}
	if !found {
		t.Errorf("scan did not report function-lines; violations were %+v", result.Violations)
	}
}

// A clean file must produce nothing. Without this, a scan that reported
// everything would pass every test above.
func TestScanIsQuietOnCleanCode(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "internal", "clean")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	source := "package clean\n\nfunc tidy(a int) int { return a + 1 }\n"
	if err := os.WriteFile(filepath.Join(root, "clean.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := scanRoots([]string{filepath.Join(dir, "internal")})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Errorf("clean code reported %+v", result.Violations)
	}
}

// The comment budget covers non-test Go alone, and this is what holds that. It
// is a test about scope rather than about a number, so it cannot be satisfied by
// editing the baseline: adding comment lines to a _test.go must move neither
// count, and adding one to a production file must move both.
//
// The failure this guards against is a ratchet that stopped measuring the code it
// was written for. Test prose was half the budget, so a slice that documented its
// tests properly had to delete documentation from shipped code it never touched.
func TestCommentBudgetCountsNonTestGoOnly(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "internal", "scoped")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	production := "package scoped\n\n" +
		"// A line of production prose.\n" +
		"func kept() int { return 1 }\n"
	if err := os.WriteFile(filepath.Join(root, "kept.go"), []byte(production), 0o644); err != nil {
		t.Fatalf("write production: %v", err)
	}

	measure := func() (comment, ratio int) {
		t.Helper()
		result, err := scanRoots([]string{filepath.Join(dir, "internal")})
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		return result.Counts[commentLinesRule], result.Counts[commentRatioRule]
	}

	beforeComment, beforeRatio := measure()
	if beforeComment != 1 {
		t.Fatalf("comment-lines = %d, want the single production comment", beforeComment)
	}
	if beforeRatio == 0 {
		t.Fatal("comment-ratio = 0, so the ratio is not being computed at all")
	}

	// A test file full of prose moves neither count. It is still scanned for the
	// structural rules, which is the half of the scope that matters.
	tests := "package scoped\n\n" +
		"// A line of test prose.\n" +
		"// Another, because a test explaining what it proves is the point.\n" +
		"// A third.\n" +
		"func kept() int { return 1 }\n"
	if err := os.WriteFile(filepath.Join(root, "kept_test.go"), []byte(tests), 0o644); err != nil {
		t.Fatalf("write test: %v", err)
	}
	if got, _ := measure(); got != beforeComment {
		t.Errorf("comment-lines = %d after adding test prose, want it unchanged at %d", got, beforeComment)
	}

	// And prose in shipped code still costs what it always did.
	if err := os.WriteFile(filepath.Join(root, "kept.go"), []byte(production+"// More production prose.\n"), 0o644); err != nil {
		t.Fatalf("rewrite production: %v", err)
	}
	gotComment, gotRatio := measure()
	if gotComment != beforeComment+1 {
		t.Errorf("comment-lines = %d after adding production prose, want %d", gotComment, beforeComment+1)
	}
	if gotRatio <= beforeRatio {
		t.Errorf("comment-ratio = %d after adding production prose, want it above %d", gotRatio, beforeRatio)
	}
}

// The tool's whole user-facing surface - the flags, the JSON contract a gate reads,
// and the baseline write - is here. It had no coverage at all, which the
// per-package coverage floor reported the moment the ratchet policy tests went with
// the policy they tested.
//
// The JSON shape is the load-bearing part: scripts/check-go-quality.mjs parses it,
// so a field renamed here is a gate reading undefined rather than a compile error
// anywhere. That is why the shape is asserted against a decoded value rather than a
// substring.

func fixedReport() report {
	return report{
		Version: baselineVersion,
		Violations: []violation{
			{Rule: "complexity", Identity: "a.go:one#1", Message: "too complex"},
			{Rule: "max-params", Identity: "a.go:two#1", Message: "too many"},
		},
		Counts: map[string]int{"complexity": 2, "any": 5},
	}
}

func fixedScanner() func() (report, error) {
	return func() (report, error) { return fixedReport(), nil }
}

func TestJSONOutputIsTheShapeTheGateReads(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-json"}, &stdout, &stderr, fixedScanner()); code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}

	var decoded struct {
		Version    int `json:"version"`
		Violations []struct {
			Rule     string `json:"rule"`
			Identity string `json:"identity"`
			Message  string `json:"message"`
		} `json:"violations"`
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("the gate's parse of this output would fail: %v\n%s", err, stdout.String())
	}
	if decoded.Version != baselineVersion {
		t.Errorf("version = %d, want %d", decoded.Version, baselineVersion)
	}
	if len(decoded.Violations) != 2 {
		t.Fatalf("violations = %d, want 2", len(decoded.Violations))
	}
	if decoded.Violations[0].Identity != "a.go:one#1" || decoded.Violations[0].Rule != "complexity" {
		t.Errorf("first violation = %+v, want rule and identity preserved", decoded.Violations[0])
	}
	if decoded.Counts["any"] != 5 {
		t.Errorf("counts = %v, want the scanner's own numbers", decoded.Counts)
	}
}

func TestJSONOutputEndsWithExactlyOneDocument(t *testing.T) {
	// Two documents on stdout would be read as one malformed document by a gate
	// that pipes it, and the failure would look like a scan problem rather than an
	// encoding one.
	var stdout, stderr bytes.Buffer
	run([]string{"-json"}, &stdout, &stderr, fixedScanner())
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var first report
	if err := decoder.Decode(&first); err != nil {
		t.Fatalf("first decode: %v", err)
	}
	var second report
	if err := decoder.Decode(&second); err == nil {
		t.Error("a second document followed the first; a gate reading this would see a concatenated stream")
	}
}

func TestTheSummaryReportsAndDoesNotJudge(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr, fixedScanner())
	if code != 0 {
		t.Fatalf("code = %d, want 0: a scanner that fails here is a gate again", code)
	}
	out := stdout.String()
	for _, want := range []string{
		"complexity: a.go:one#1 (too complex)",
		"max-params: a.go:two#1 (too many)",
		"Go quality report: 2 violations",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary is missing %q:\n%s", want, out)
		}
	}
}

func TestTheSummaryOrdersItsCountsSoTwoRunsAgree(t *testing.T) {
	// Iterating a map to build a line makes the same tree print differently on
	// different runs, which is a diff nobody can read and a CI log that looks like a
	// change when nothing changed.
	first := summaryOf(t)
	for i := 0; i < 50; i++ {
		if got := summaryOf(t); got != first {
			t.Fatalf("the summary varies between runs:\n%s\n---\n%s", first, got)
		}
	}
	if !strings.Contains(first, "any=5, complexity=2") {
		t.Errorf("summary = %q, want the counts in sorted order", first)
	}
}

func summaryOf(t *testing.T) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	run(nil, &stdout, &stderr, fixedScanner())
	return stdout.String()
}

func TestWritingTheBaselineCreatesItsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "go-quality.json")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-write-baseline", "-baseline", path}, &stdout, &stderr, fixedScanner()); code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the baseline was not written where it was asked for: %v", err)
	}
	var written report
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("the written baseline is not a report: %v", err)
	}
	if len(written.Violations) != 2 || written.Counts["any"] != 5 {
		t.Errorf("written baseline = %+v, want the scanned report", written)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("the written baseline does not end in a newline, so the next write shows a two-line diff")
	}
}

func TestWritingTheBaselineOverAnUnwritablePathIsAFailureNotASilentSkip(t *testing.T) {
	// A maintenance action that reports success without writing leaves a baseline
	// describing code that no longer exists, which is the stale floor the coverage
	// ratchet complains about.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "not-a-directory"), []byte("x"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"-write-baseline", "-baseline", filepath.Join(dir, "not-a-directory", "b.json")}, &stdout, &stderr, fixedScanner())
	if code == 0 {
		t.Fatal("code = 0, want a failure when the baseline cannot be written")
	}
	if stderr.Len() == 0 {
		t.Error("a failure said nothing about why")
	}
}

func TestAFailedScanIsReportedAndNotSwallowed(t *testing.T) {
	var stdout, stderr bytes.Buffer
	boom := func() (report, error) { return report{}, errScan }
	code := run(nil, &stdout, &stderr, boom)
	if code != 2 {
		t.Fatalf("code = %d, want 2: a scan failure is not a pass", code)
	}
	if !strings.Contains(stderr.String(), errScan.Error()) {
		t.Errorf("stderr = %q, want the scan's own reason", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing printed when the scan failed", stdout.String())
	}
}

func TestAFailedScanIsNotPrintedAsAReportEvenWithJSONAsked(t *testing.T) {
	// The failure path has to come before the output path, or a gate reading JSON
	// gets an empty document and reports a tree with no violations.
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-json"}, &stdout, &stderr, func() (report, error) { return report{}, errScan }); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want no document emitted for a failed scan", stdout.String())
	}
}

func TestAnUnknownFlagIsAFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-nonsense"}, &stdout, &stderr, fixedScanner()); code == 0 {
		t.Fatal("code = 0, want a failure for a flag that does not exist")
	}
}

func TestTheDefaultScanIsTheTree(t *testing.T) {
	// run's nil scanner is measure, and measure is the tree rather than a fixture.
	// A default that quietly scanned nothing would make every other case here pass
	// while the tool reported an empty repository.
	got, err := measure()
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if len(got.Violations) == 0 {
		t.Error("the default scan found no violations, so it is not scanning the tree")
	}
	if len(got.Counts) == 0 {
		t.Error("the default scan reported no counts at all")
	}
}

var errScan = errString("the scan failed")

type errString string

func (e errString) Error() string { return string(e) }
