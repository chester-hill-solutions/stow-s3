import assert from "node:assert/strict";
import { test } from "node:test";

import { coverageProblems } from "./check-coverage.mjs";

// The coverage gate decides whether a change is allowed to reduce what is tested.
// It had no test: the decision lived inline between the measurement and the exit
// code, so the only evidence it had ever given was a number in a CI log. These
// cases are the questions a maintainer actually has about it.

const measured = (coveredStatements, percentage) => ({
  coveredStatements,
  percentage,
  totalStatements: 1000,
});

test("matching the floor passes", () => {
  const problems = coverageProblems(measured(4000, 65.0), measured(4000, 65.0), null, 1);
  assert.deepEqual(problems, []);
});

test("an improvement passes, and is not a reason to edit the baseline", () => {
  const problems = coverageProblems(measured(4200, 68.0), measured(4000, 65.0), null, 1);
  assert.deepEqual(problems, []);
});

// The invariant is covered statements, not the percentage. Adding well-tested code
// with defensive error branches can lower the percentage while coverage improves,
// so a percentage fall alone is tolerated within the allowance.
test("a percentage fall within the allowance passes", () => {
  const problems = coverageProblems(measured(4000, 64.5), measured(4000, 65.0), null, 1);
  assert.deepEqual(problems, []);
});

test("a percentage fall beyond the allowance fails even when statements hold", () => {
  const problems = coverageProblems(measured(4000, 63.0), measured(4000, 65.0), null, 1);
  assert.equal(problems.length, 1);
  assert.match(problems[0], /percentage fell by 2 points/);
});

test("losing covered statements fails even when the percentage holds", () => {
  // The case statement counts exist for: total statements fell with them, so the
  // percentage is unchanged and only the count reveals the loss.
  const problems = coverageProblems(
    { coveredStatements: 3800, percentage: 65.0, totalStatements: 5846 },
    { coveredStatements: 4000, percentage: 65.0, totalStatements: 6153 },
    null,
    1,
  );
  assert.equal(problems.length, 1);
  assert.match(problems[0], /covered statements fell: 3800 < 4000/);
});

test("both failures are reported together", () => {
  const problems = coverageProblems(measured(3800, 60.0), measured(4000, 65.0), null, 1);
  assert.equal(problems.length, 2);
});

// A baseline that was lowered between runs is the one way to make a loss stick:
// the tree would then be measured against a floor that had already been moved down
// to meet it. The recorded floor has to be non-decreasing.
test("a lowered stored baseline fails even when the tree meets it", () => {
  const baseline = measured(3800, 65.0);
  const problems = coverageProblems(measured(3800, 65.0), baseline, measured(4000, 65.0), 1);
  assert.equal(problems.length, 1);
  assert.match(problems[0], /stored baseline was lowered: 3800 < 4000/);
});

test("a raised stored baseline passes the history check", () => {
  const problems = coverageProblems(measured(4000, 65.0), measured(4000, 65.0), measured(3800, 64.0), 1);
  assert.deepEqual(problems, []);
});

// No previous baseline means no history to check: a first run, or a repository with
// no parent commit. Failing on that would make the gate unsatisfiable rather than
// strict.
test("no previous baseline skips the history check", () => {
  assert.deepEqual(coverageProblems(measured(4000, 65.0), measured(4000, 65.0), undefined, 1), []);
});

// The allowance is a parameter rather than a constant so the boundary is testable
// at 1 and at 0, and so a change to MAX_PERCENTAGE_DROP is a visible edit here.
test("the allowance boundary is inclusive", () => {
  assert.deepEqual(coverageProblems(measured(4000, 64.0), measured(4000, 65.0), null, 1), []);
  assert.equal(coverageProblems(measured(4000, 64.0), measured(4000, 65.0), null, 0.99).length, 1);
});

// Rounding is why the gate reads a profile rather than `go tool cover -func`: a
// true value on a rounding boundary reports differently on different runs, so the
// same commit passes and fails. Rounding a difference of 1.004 down to 1.00 brings
// it inside a 1 point allowance; unrounded it would fail.
test("percentage differences are rounded to two places before comparison", () => {
  assert.deepEqual(coverageProblems(measured(4000, 63.996), measured(4000, 65.0), null, 1), []);
  // And rounding is not a loophole in the other direction: 0.999 becomes 1.00,
  // which is outside an allowance of 0.999.
  assert.equal(coverageProblems(measured(4000, 64.001), measured(4000, 65.0), null, 0.999).length, 1);
});
