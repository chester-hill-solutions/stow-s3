// Command quality records the ratcheted Go maintainability metrics for a tree.
//
// It is a scanner. Whether a report is acceptable is a ratchet decision, and that
// decision lives once, in scripts/ratchet.mjs, applied by
// scripts/check-go-quality.mjs. This tool measures and prints.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const baselineVersion = 1

type violation struct {
	Rule     string `json:"rule"`
	Identity string `json:"identity"`
	Message  string `json:"message"`
}

type report struct {
	Version    int            `json:"version"`
	Violations []violation    `json:"violations"`
	Counts     map[string]int `json:"counts"`
}

// main prints the report and decides nothing: comparing a report to a baseline, and a
// baseline to its own history, is the ratchet policy, which lives once in
// scripts/ratchet.mjs and is applied by scripts/check-go-quality.mjs. This tool used to
// hold its own copy of those comparisons and got the history check wrong, so raising the
// floor passed; see docs/CODE_STANDARDS.md. main is a shim over run, so the flags and the
// JSON contract a gate reads are reachable from a test.
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, nil)) }

// run is the tool. It returns the exit code rather than exiting, so every path is
// reachable from a test. A nil scan means measure the tree.
func run(args []string, stdout, stderr io.Writer, scan func() (report, error)) int {
	flags := flag.NewFlagSet("quality", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var writeBaseline bool
	var asJSON bool
	var baselinePath string
	flags.BoolVar(&writeBaseline, "write-baseline", false, "write the current report as the baseline")
	flags.BoolVar(&asJSON, "json", false, "print the report as JSON, for a gate to apply a policy to")
	flags.StringVar(&baselinePath, "baseline", filepath.Join("scripts", "baselines", "go-quality.json"), "baseline path, for -write-baseline")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if scan == nil {
		scan = measure
	}

	current, err := scan()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(current); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}

	if writeBaseline {
		if err := writeBaselineFile(baselinePath, current); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		fmt.Fprintf(stdout, "Wrote %s (%d violations)\n", baselinePath, len(current.Violations))
		return 0
	}

	// The summary reports, and does not pass or fail. A scanner that exits non-zero
	// for a reason the caller cannot see is a gate whose verdict cannot be reviewed,
	// and this tool no longer owns a verdict at all.
	for _, item := range current.Violations {
		fmt.Fprintf(stdout, "%s: %s (%s)\n", item.Rule, item.Identity, item.Message)
	}
	rules := make([]string, 0, len(current.Counts))
	for rule := range current.Counts {
		rules = append(rules, rule)
	}
	sort.Strings(rules)
	parts := make([]string, 0, len(rules))
	for _, rule := range rules {
		parts = append(parts, fmt.Sprintf("%s=%d", rule, current.Counts[rule]))
	}
	fmt.Fprintf(stdout, "Go quality report: %d violations, %s\n", len(current.Violations), strings.Join(parts, ", "))
	return 0
}

// writeBaselineFile creates the baseline and its directory.
func writeBaselineFile(path string, current report) error {
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// measure is the real scan, so run's default is a value.
func measure() (report, error) { return scan() }

func scan() (report, error) {
	roots, err := scanRootsFromConfig()
	if err != nil {
		return report{}, err
	}
	return scanRoots(roots)
}

// scanRootsFromConfig reads the Go roots from config/scan-roots.json. A missing or
// malformed file is an error rather than a fallback: falling back to a built-in
// list would restore the two-copies problem this exists to remove, quietly.
func scanRootsFromConfig() ([]string, error) {
	path := filepath.Join(repoRoot(), "config", "scan-roots.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scan roots: %w", err)
	}
	var config struct {
		Go []string `json:"go"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if len(config.Go) == 0 {
		return nil, fmt.Errorf("%s lists no go roots", path)
	}
	// Resolved against the repo root; bare names were walked against the caller's cwd.
	roots := make([]string, 0, len(config.Go))
	for _, root := range config.Go {
		roots = append(roots, filepath.Join(repoRoot(), root))
	}
	return roots, nil
}

// repoRoot is the directory containing go.mod, found by walking up from the
// working directory so the tool works from anywhere in the tree.
func repoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// commentRatioRule and commentLinesRule are the repo-wide comment volume, ratcheted
// so they can only go down.
//
// They exist because every other rule here measures something countable, and comment
// volume was the one maintainability cost with no gate on it. The coverage ratchet has
// the same blind spot in the other direction: it fails when covered statements
// decrease, so a whole verb added with no tests leaves it satisfied.
//
// Both are measured because neither is sufficient alone. The ratio cannot be satisfied
// by deleting code, but across 35,000 scanned lines it is an integer and four added
// comments move it by zero. The count is exact, so one line fails, but deleting
// uncommented code would satisfy it. A per-file ceiling is worse than both.
//
// The budget covers non-test Go only. Test prose and production prose are different
// claims: a test comment says what a test proves, and a production comment says why the
// system is shaped as it is. Counting them in one budget made them compete, and the
// competition ran the wrong way. Half the budget was test prose, so a slice that
// documented its tests properly had to fund them by deleting documentation from code it
// never touched — writing about why a policy denies a workspace destroy cost the
// two-level admin rule out of internal/s3api and the upstream status mapping out of
// internal/s3api/errors.go. That made prose in shipped code the cheapest thing in the
// repository to delete, which is the opposite of what this ratchet is for.
//
// Nothing is lost. Test files are held to the coverage ratchet, to gofmt and vet, to the
// per-file size ceiling and to every structural rule here bar `any`, which was already
// scoped away. What is lost is only the pressure to keep a shipped file under a number
// set partly by a test it has nothing to do with.
const (
	commentLinesRule = "comment-lines"
	commentRatioRule = "comment-ratio"
)

// lineTally counts lines and comment lines across the scanned tree.
type lineTally struct {
	total   int
	comment int
}

// add counts one file. A comment line is one whose first non-space characters are
// //; this codebase does not use block comments, and a line that is a comment and
// nothing else is unambiguous where an inline trailing comment is not.
func (t *lineTally) add(path string) {
	source, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(source), "\n") {
		t.total++
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			t.comment++
		}
	}
}

func scanRoots(roots []string) (report, error) {
	result := report{Version: baselineVersion, Counts: map[string]int{}}
	seen := map[string]int{}
	lines := &lineTally{}
	for _, root := range roots {
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			// Test files are scanned for every structural rule and excluded from the
			// comment budget alone. See commentLinesRule for why the two are counted
			// apart.
			if !strings.HasSuffix(path, "_test.go") {
				lines.add(path)
			}
			return scanFile(path, &result, seen)
		}); err != nil {
			return report{}, err
		}
	}
	// Counts, not Violations: there is no per-site thing to record. Whether an increase
	// fails is the gate's decision, not the scanner's.
	if lines.total > 0 {
		result.Counts[commentLinesRule] = lines.comment
		result.Counts[commentRatioRule] = lines.comment * 10000 / lines.total
	}
	sort.Slice(result.Violations, func(i, j int) bool {
		return result.Violations[i].Identity < result.Violations[j].Identity
	})
	return result, nil
}

// checker accumulates the violations found in one file.
//
// It is a type rather than a set of closures inside scanFile because a closure's
// branches count towards the enclosing function's complexity, and scanFile was
// over the ceiling for exactly that reason: it was holding four closures whose
// logic had nothing to do with walking a file.
type checker struct {
	path   string
	fset   *token.FileSet
	result *report
	seen   map[string]int
	// testFile scopes the `any` rule. See checkEscapes for why the rule does not
	// apply to a test and why it still applies to everything it does apply to.
	testFile bool
}

// relativeTo turns a scanned path into an identity a baseline can hold. Roots resolve
// against the repo root, so an absolute path would name the checkout, not the code.
func (c *checker) relativeTo(path string) string {
	slashed := filepath.ToSlash(path)
	root := filepath.ToSlash(repoRoot())
	if root != "." && strings.HasPrefix(slashed, root+"/") {
		return strings.TrimPrefix(slashed, root+"/")
	}
	return slashed
}

func (c *checker) add(rule, identity, message string) {
	occurrenceKey := rule + "\x00" + identity
	c.seen[occurrenceKey]++
	identity = fmt.Sprintf("%s#%d", identity, c.seen[occurrenceKey])
	c.result.Violations = append(c.result.Violations, violation{Rule: rule, Identity: identity, Message: message})
	c.result.Counts[rule]++
}

func (c *checker) checkFunction(name string, body *ast.BlockStmt, params *ast.FieldList, start int) {
	if body == nil {
		return
	}
	end := c.fset.Position(body.End()).Line
	lines := end - start + 1
	complexity := cyclomaticComplexity(body)
	paramCount := parameterCount(params)
	base := fmt.Sprintf("%s:%s", c.relativeTo(c.path), name)
	if lines > 200 {
		c.add("function-lines", base, fmt.Sprintf("function %s has %d lines (maximum 200)", name, lines))
	}
	if complexity > 15 {
		c.add("complexity", base, fmt.Sprintf("function %s has complexity %d (maximum 15)", name, complexity))
	}
	if paramCount > 5 {
		c.add("max-params", base, fmt.Sprintf("function %s has %d parameters (maximum 5)", name, paramCount))
	}
}

// checkLiterals reports the function literals nested inside a function. They are
// real functions with their own length, complexity and parameter count, and a
// closure-heavy function can otherwise hide all three behind its parent's numbers.
func (c *checker) checkLiterals(fn *ast.FuncDecl) {
	literalNumber := 0
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		ast.Inspect(node, func(child ast.Node) bool {
			literal, isLiteral := child.(*ast.FuncLit)
			if !isLiteral {
				return true
			}
			literalNumber++
			name := fmt.Sprintf("%s/func-literal-%d", fn.Name.Name, literalNumber)
			c.checkFunction(name, literal.Body, literal.Type.Params, c.fset.Position(literal.Pos()).Line)
			walk(literal.Body)
			return false
		})
	}
	walk(fn.Body)
}

// checkEscapes reports the two type and safety escape hatches: `any` and `panic`.
//
// The `any` rule is scoped to non-test code, and the scope is load-bearing rather than
// convenient. `any` is a defect where a value crosses a boundary a type could have
// described: it is where a lost assertion becomes a silent wrong answer. In a test
// comparing values of an unknown shape there is no boundary to type.
//
// Forcing it on tests anyway makes a conformance matcher compare raw JSON with a
// hand-rolled canonicaliser, satisfying the ratchet while the code gets worse. The
// recorded count is unchanged by a fix elsewhere, so the gate still fails on `any` in
// production code.
func (c *checker) checkEscapes(file *ast.File) {
	location := c.relativeTo(c.path)
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.Ident:
			if typed.Name == "any" && !c.testFile {
				c.add("any", c.relativeTo(location), "use of any")
			}
		case *ast.CallExpr:
			if ident, ok := typed.Fun.(*ast.Ident); ok && ident.Name == "panic" {
				c.add("panic", c.relativeTo(location), "panic call")
			}
		}
		return true
	})
}

func scanFile(path string, result *report, seen map[string]int) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, 0)
	if err != nil {
		return err
	}

	c := &checker{
		path:     path,
		fset:     fset,
		result:   result,
		seen:     seen,
		testFile: strings.HasSuffix(filepath.ToSlash(path), "_test.go"),
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		c.checkFunction(fn.Name.Name, fn.Body, fn.Type.Params, fset.Position(fn.Pos()).Line)
		c.checkLiterals(fn)
	}
	c.checkEscapes(file)
	return nil
}

func cyclomaticComplexity(body *ast.BlockStmt) int {
	complexity := 1
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			complexity++
		case *ast.CaseClause:
			if typed.List != nil {
				complexity++
			}
		case *ast.CommClause:
			if typed.Comm != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			if typed.Op == token.LAND || typed.Op == token.LOR {
				complexity++
			}
		}
		return true
	})
	return complexity
}

func parameterCount(params *ast.FieldList) int {
	if params == nil {
		return 0
	}
	count := 0
	for _, field := range params.List {
		if len(field.Names) == 0 {
			count++
			continue
		}
		count += len(field.Names)
	}
	return count
}
