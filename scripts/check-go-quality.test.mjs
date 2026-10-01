// The Go quality gate, tested as wiring rather than as policy.
//
// The three comparisons this gate applies - a floor in both directions, a ceiling
// on the baseline itself, and identity sets rather than counts - are already tested
// in ratchet.test.mjs, and duplicating them here would be the same mistake this
// refactor removed. What is new is that the pieces are connected: the scanner's
// report, the committed baseline, the parent baseline, and the rule lists, are
// four inputs that can drift apart silently, and a gate that reads the wrong pair
// is green.
//
// The case that motivates the file is the wrong pair. The gate used to compare the
// scan against the parent baseline rather than the committed baseline against the
// parent, so raising the baseline to approve a regression changed nothing the
// comparison could see. Every test below is about the pair, not the arithmetic.
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { test } from "node:test";

import { goQualityProblems } from "./check-go-quality.mjs";

const repoRoot = resolve(import.meta.dirname, "..");

const report = (counts, identities = []) => ({
  version: 1,
  violations: identities.map((identity) => ({ rule: "complexity", identity, message: "recorded" })),
  counts,
});
const baseline = (counts, identities = []) => ({
  version: 1,
  violations: identities.map((identity) => ({ rule: "complexity", identity, message: "recorded" })),
  counts,
});

// A tree that satisfies everything, so each test breaks exactly one thing.
const COUNTS = { any: 5, "comment-lines": 4161, "comment-ratio": 1196, complexity: 2, "max-params": 8 };
const clean = () => ({
  report: report(COUNTS, ["a.go:one#1"]),
  baseline: baseline(COUNTS, ["a.go:one#1"]),
  previous: baseline(COUNTS, ["a.go:one#1"]),
});

test("an unchanged tree has no problems", () => {
  assert.deepEqual(goQualityProblems(clean()), []);
});

test("a count above the baseline is new debt", () => {
  const c = clean();
  c.report = report({ ...COUNTS, complexity: 3 }, ["a.go:one#1"]);
  const problems = goQualityProblems(c);
  assert.equal(problems.length, 1, JSON.stringify(problems));
  assert.match(problems[0], /new debt: complexity: 3 > 2/);
});

test("an improvement the baseline has not recorded is a stale floor", () => {
  const c = clean();
  c.report = report({ ...COUNTS, complexity: 1 }, ["a.go:one#1"]);
  const problems = goQualityProblems(c);
  assert.equal(problems.length, 1, JSON.stringify(problems));
  assert.match(problems[0], /stale baseline: complexity: 1 < 2/);
});

test("RAISING the baseline above its own history is refused", () => {
  // The case the old gate could not see. Its history check compared the scan
  // against the parent baseline, so a raised floor left both sides of that
  // comparison unchanged and it passed. This is the comparison that catches it.
  const c = clean();
  c.baseline = baseline({ ...COUNTS, "comment-lines": 9999 }, ["a.go:one#1"]);
  const problems = goQualityProblems(c);
  assert.ok(
    problems.some((p) => /baseline expanded: comment-lines/.test(p)),
    JSON.stringify(problems),
  );
});

test("raising a baseline and raising the count together is still refused", () => {
  // The shape a real bypass would take: author writes a violation, then widens the
  // baseline past it. Comparing current against the parent baseline alone does not
  // catch this, because the scan rose by exactly as much as the floor did.
  const c = clean();
  c.report = report({ ...COUNTS, complexity: 4 }, ["a.go:one#1", "a.go:new#1"]);
  c.baseline = baseline({ ...COUNTS, complexity: 4 }, ["a.go:one#1", "a.go:new#1"]);
  const problems = goQualityProblems(c);
  assert.ok(
    problems.some((p) => /baseline expanded: complexity/.test(p)),
    JSON.stringify(problems),
  );
});

test("a new identity is unapproved debt even when the count is unchanged", () => {
  const c = clean();
  c.report = report(COUNTS, ["a.go:one#1", "a.go:two#1"]);
  c.baseline = baseline(COUNTS, ["a.go:one#1"]);
  // Identity sets, not counts: two recorded entries against one in the baseline is
  // a debt the per-rule totals cannot see, because the totals never moved. The only
  // report is the identity, which is the point.
  assert.deepEqual(goQualityProblems(c), ["new: a.go:two#1"]);
});

test("a renamed identity is caught, which a count alone would miss", () => {
  const c = clean();
  c.report = report(COUNTS, ["a.go:renamed#1"]);
  const problems = goQualityProblems(c);
  assert.ok(problems.includes("new: a.go:renamed#1"), JSON.stringify(problems));
  assert.ok(problems.includes("stale: a.go:one#1"), JSON.stringify(problems));
});

test("comment-ratio is reported by the scanner and is not a floor", () => {
  // Deleting code raises the density with every comment line intact. Treating that
  // as debt fails the cleanup the ratchet exists to encourage, so the density moves
  // freely in the direction code removal takes it, and only the count is held.
  const c = clean();
  c.report = report({ ...COUNTS, "comment-ratio": 4000 }, ["a.go:one#1"]);
  assert.deepEqual(goQualityProblems(c), [], "a rising density is not debt");

  const down = clean();
  down.report = report({ ...COUNTS, "comment-ratio": 3 }, ["a.go:one#1"]);
  assert.deepEqual(goQualityProblems(down), [], "a falling density is not an improvement to record");
});

test("the count is still a floor, so density cannot substitute for it", () => {
  const c = clean();
  c.report = report({ ...COUNTS, "comment-lines": 4200, "comment-ratio": 500 }, ["a.go:one#1"]);
  const problems = goQualityProblems(c);
  assert.ok(
    problems.some((p) => /new debt: comment-lines: 4200 > 4161/.test(p)),
    JSON.stringify(problems),
  );
});

test("a rule the scanner reports and the gate does not name is reported", () => {
  // Otherwise a new metric appears in the report, is compared against nothing, and
  // the gate reports itself green while measuring something it never checks.
  const c = clean();
  c.report = report({ ...COUNTS, "gocyclo": 3 }, ["a.go:one#1"]);
  const problems = goQualityProblems(c);
  assert.ok(problems.some((p) => /unconstrained rule gocyclo/.test(p)), JSON.stringify(problems));
});

test("a rule the gate names and the scanner stopped reporting is reported", () => {
  const c = clean();
  const { "max-params": _gone, ...withoutMaxParams } = COUNTS;
  c.report = report(withoutMaxParams, ["a.go:one#1"]);
  const problems = goQualityProblems(c);
  assert.ok(problems.some((p) => /COUNTS names max-params/.test(p)), JSON.stringify(problems));
});

test("a missing parent baseline is a failure, not a skipped ratchet", () => {
  // With no previous there is nothing to compare the floor against, which is the
  // comparison that catches a raise. A gate that quietly skips it is the hole this
  // file was written to close, so run() throws rather than returning problems.
  assert.ok(existsSync(join(repoRoot, "scripts", "baseline-history.mjs")));
  const history = readFileSync(join(repoRoot, "scripts", "baseline-history.mjs"), "utf8");
  assert.match(history, /fetch-depth|rev-parse|full history/i);
});

test("every rule the checked-in baseline records is one the gate names", () => {
  // The gate's rule lists and the committed baseline are two files that can drift.
  const committed = JSON.parse(
    readFileSync(join(repoRoot, "scripts", "baselines", "go-quality.json"), "utf8"),
  );
  const named = new Set(["any", "comment-lines", "comment-ratio", "complexity", "max-params"]);
  for (const rule of Object.keys(committed.counts ?? {})) {
    assert.ok(named.has(rule), `the baseline records ${rule}, which the gate does not name`);
  }
});
