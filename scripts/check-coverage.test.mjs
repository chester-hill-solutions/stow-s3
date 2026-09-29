import assert from "node:assert/strict";
import { test } from "node:test";

import {
  coverageProblems,
  lowestPerPackage,
  packageProblems,
  statementsByPackage,
  thinPackages,
} from "./check-coverage.mjs";

// The coverage gate decides whether a change is allowed to reduce what is tested.
// It had no test: the decision lived inline between the measurement and the exit
// code, so the only evidence it had ever given was a number in a CI log. These
// cases are the questions a maintainer actually has about it.

const measured = (coveredStatements, percentage, packages) => ({
  coveredStatements,
  percentage,
  totalStatements: 1000,
  // Fixed rather than derived from the aggregate: a test about the aggregate should
  // not also trip the per-package check, and a test about that says so explicitly.
  packages: packages ?? {
    "example/pkg": { coveredStatements: 4000, totalStatements: 1000 },
  },
});

test("matching the floor passes", () => {
  const problems = coverageProblems(
    measured(4000, 65.0),
    measured(4000, 65.0),
    null,
    1,
  );
  assert.deepEqual(problems, []);
});

test("an improvement passes, and is not a reason to edit the baseline", () => {
  const problems = coverageProblems(
    measured(4200, 68.0),
    measured(4000, 65.0),
    null,
    1,
  );
  assert.deepEqual(problems, []);
});

// The invariant is covered statements, not the percentage. Adding well-tested code
// with defensive error branches can lower the percentage while coverage improves,
// so a percentage fall alone is tolerated within the allowance.
test("a percentage fall within the allowance passes", () => {
  const problems = coverageProblems(
    measured(4000, 64.5),
    measured(4000, 65.0),
    null,
    1,
  );
  assert.deepEqual(problems, []);
});

test("a percentage fall beyond the allowance fails even when statements hold", () => {
  const problems = coverageProblems(
    measured(4000, 63.0),
    measured(4000, 65.0),
    null,
    1,
  );
  assert.equal(problems.length, 1);
  assert.match(problems[0], /percentage fell by 2 points/);
});

test("losing covered statements fails even when the percentage holds", () => {
  // The case statement counts exist for: total statements fell with them, so the
  // percentage is unchanged and only the count reveals the loss.
  const problems = coverageProblems(
    measured(3800, 65.0, {
      "example/pkg": { coveredStatements: 3800, totalStatements: 5846 },
    }),
    measured(4000, 65.0, {
      "example/pkg": { coveredStatements: 4000, totalStatements: 6153 },
    }),
    null,
    1,
  );
  // Two, not one: the aggregate and the package both fell, and reporting only the
  // aggregate is what let a package be gutted while the total held.
  assert.equal(problems.length, 2);
  assert.match(problems[0], /covered statements fell: 3800 < 4000/);
  assert.match(
    problems[1],
    /example\/pkg: covered statements fell: 3800 < 4000/,
  );
});

test("all three failures are reported together", () => {
  const problems = coverageProblems(
    measured(3800, 60.0, {
      "example/pkg": { coveredStatements: 3000, totalStatements: 4000 },
    }),
    measured(4000, 65.0, {
      "example/pkg": { coveredStatements: 4000, totalStatements: 6000 },
    }),
    null,
    1,
  );
  // The aggregate statement fall, the percentage fall, and the per-package fall.
  assert.equal(problems.length, 3);
});

// A baseline that was lowered between runs is the one way to make a loss stick: the
// tree would then be measured against a floor already moved down to meet it. The
// recorded floor has to be non-decreasing.
test("a lowered stored baseline fails even when the tree meets it", () => {
  const baseline = measured(3800, 65.0, {
    "example/pkg": { coveredStatements: 3800, totalStatements: 6000 },
  });
  const problems = coverageProblems(
    measured(3800, 65.0, {
      "example/pkg": { coveredStatements: 3800, totalStatements: 6000 },
    }),
    baseline,
    measured(4000, 65.0, {
      "example/pkg": { coveredStatements: 4000, totalStatements: 6000 },
    }),
    1,
  );
  assert.equal(problems.length, 2);
  assert.match(problems[0], /stored baseline was lowered: 3800 < 4000/);
  assert.match(problems[1], /example\/pkg: the stored floor was lowered/);
});

test("a raised stored baseline passes the history check", () => {
  const problems = coverageProblems(
    measured(4000, 65.0),
    measured(4000, 65.0),
    measured(3800, 64.0),
    1,
  );
  assert.deepEqual(problems, []);
});

// No previous baseline means no history to check: a first run, or a repository with
// no parent commit. Failing on that would make the gate unsatisfiable rather than
// strict.
test("no previous baseline skips the history check", () => {
  assert.deepEqual(
    coverageProblems(measured(4000, 65.0), measured(4000, 65.0), undefined, 1),
    [],
  );
});

// The allowance is a parameter rather than a constant so the boundary is testable
// at 1 and at 0, and so a change to MAX_PERCENTAGE_DROP is a visible edit here.
test("the allowance boundary is inclusive", () => {
  assert.deepEqual(
    coverageProblems(measured(4000, 64.0), measured(4000, 65.0), null, 1),
    [],
  );
  assert.equal(
    coverageProblems(measured(4000, 64.0), measured(4000, 65.0), null, 0.99)
      .length,
    1,
  );
});

// Rounding is why the gate reads a profile rather than `go tool cover -func`: a
// true value on a rounding boundary reports differently on different runs, so the
// same commit passes and fails. Rounding a difference of 1.004 down to 1.00 brings
// it inside a 1 point allowance; unrounded it would fail.
test("percentage differences are rounded to two places before comparison", () => {
  assert.deepEqual(
    coverageProblems(measured(4000, 63.996), measured(4000, 65.0), null, 1),
    [],
  );
  // And rounding is not a loophole in the other direction: 0.999 becomes 1.00,
  // which is outside an allowance of 0.999.
  assert.equal(
    coverageProblems(measured(4000, 64.001), measured(4000, 65.0), null, 0.999)
      .length,
    1,
  );
});

// The per-package measurement, tested against a fixture rather than a live run.
//
// A previous attempt to add per-package floors wrote a baseline with zero per-package
// entries and had to be reverted, because nothing had ever checked that the profile's
// file name was read at all: the aggregate regex matched it and threw it away. A
// measurement tested only by running the suite is a measurement that reports nothing
// and says so in a shape nobody reads. So the fixture is the test, and it is here
// rather than in the live gate.
const P = "github.com/chester-hill-solutions/stow-s3";

const fixture = [
  "mode: atomic",
  `${P}/internal/storage/workspace/objects.go:19.68,25.20 3 1`,
  `${P}/internal/storage/workspace/objects.go:26.10,30.40 2 0`,
  `${P}/internal/storage/workspace/resolve.go:5.1,9.2 1 1`,
  `${P}/cmd/stow-s3/serve.go:1.1,4.2 6 0`,
  `${P}/cmd/stow-s3/version.go:1.1,2.2 2 1`,
  // A file directly in the module root: removing the file name must leave the import
  // path, not a trailing slash.
  `${P}/root.go:1.1,2.2 1 1`,
  // A line the parser must skip rather than count as zero.
  "this is not a profile line",
  "",
].join("\n");

test("statements are attributed to the package directory, not the file", () => {
  const packages = statementsByPackage(fixture);
  assert.deepEqual(packages[`${P}/internal/storage/workspace`], {
    coveredStatements: 4,
    totalStatements: 6,
  });
  assert.deepEqual(packages[`${P}/cmd/stow-s3`], {
    coveredStatements: 2,
    totalStatements: 8,
  });
  assert.deepEqual(packages[P], { coveredStatements: 1, totalStatements: 1 });
});

test("a line that is not a profile line is skipped, not counted as uncovered", () => {
  // The shape that would quietly under-report: treating an unparseable line as a
  // statement with count 0 lowers the floor for everybody rather than failing.
  const packages = statementsByPackage(fixture);
  const total = Object.values(packages).reduce(
    (sum, p) => sum + p.totalStatements,
    0,
  );
  assert.equal(total, 15);
});

test("a profile with nothing readable yields no packages rather than zeroes", () => {
  assert.deepEqual(statementsByPackage("mode: atomic\n"), {});
});

test("an empty profile yields no packages", () => {
  assert.deepEqual(statementsByPackage(""), {});
});

test("the aggregate and the per-package view agree on the totals", () => {
  // The two views are computed from one profile by different code. If they ever
  // disagree, one of the floors is measuring something the other is not.
  const packages = statementsByPackage(fixture);
  const sum = Object.values(packages).reduce(
    (acc, p) => ({
      covered: acc.covered + p.coveredStatements,
      total: acc.total + p.totalStatements,
    }),
    { covered: 0, total: 0 },
  );
  assert.equal(sum.covered, 7);
  assert.equal(sum.total, 15);
});

// The per-package half. These are the cases the aggregate cannot express at all, and
// the reason they are here rather than inferred: the aggregate was correct throughout
// while `cmd/stow-s3` sat at 56.4% against a 71.05% total.

const pk = (covered, total) => ({
  coveredStatements: covered,
  totalStatements: total,
});

test("a package losing coverage fails even when the aggregate holds", () => {
  // The case the aggregate cannot see: a well-tested package carries the total while
  // another is gutted, so the recorded number does not move at all.
  const problems = packageProblems(
    { "good/pkg": pk(3000, 3000), "thin/pkg": pk(200, 2000) },
    { "good/pkg": pk(3000, 3000), "thin/pkg": pk(900, 2000) },
    undefined,
  );
  assert.equal(problems.length, 1);
  assert.match(problems[0], /thin\/pkg: covered statements fell: 200 < 900/);
});

test("a package gaining coverage passes", () => {
  assert.deepEqual(
    packageProblems(
      { "a/pkg": pk(1000, 1000) },
      { "a/pkg": pk(900, 1000) },
      undefined,
    ),
    [],
  );
});

test("a package that stopped being measured is a failure, not a skip", () => {
  // Either the package is gone, leaving a stale floor, or it is no longer built, which
  // loses its coverage without any of it being removed. Both need a decision.
  const problems = packageProblems(
    { "a/pkg": pk(1000, 1000) },
    { "a/pkg": pk(1000, 1000), "gone/pkg": pk(500, 500) },
    undefined,
  );
  assert.equal(problems.length, 1);
  assert.match(
    problems[0],
    /gone\/pkg is in the baseline but not in this measurement/,
  );
});

test("a new package with no recorded floor passes, and is named by the report", () => {
  assert.deepEqual(
    packageProblems({ "new/pkg": pk(1, 100) }, {}, undefined),
    [],
  );
  assert.deepEqual(
    thinPackages({ percentage: 71.05, packages: { "new/pkg": pk(1, 100) } }),
    [{ name: "new/pkg", percentage: 1 }],
  );
});

test("a lowered per-package floor fails even when the tree meets it", () => {
  const problems = packageProblems(
    { "a/pkg": pk(1000, 1000) },
    { "a/pkg": pk(1000, 1000) },
    { "a/pkg": pk(1200, 1200) },
  );
  assert.equal(problems.length, 1);
  assert.match(
    problems[0],
    /a\/pkg: the stored floor was lowered: 1000 < 1200/,
  );
});

test("a baseline with no per-package floors fails rather than passing silently", () => {
  // The mode the previous attempt shipped: a measurement that recorded nothing, in a
  // shape nothing complained about.
  const problems = packageProblems(
    { "a/pkg": pk(1, 100) },
    undefined,
    undefined,
  );
  assert.equal(problems.length, 1);
  assert.match(problems[0], /records no per-package floors/);
});

test("thinPackages names every package below the aggregate, thinnest first", () => {
  const thin = thinPackages({
    percentage: 71.05,
    packages: {
      "well/pkg": pk(90, 100),
      "middle/pkg": pk(70, 100),
      "thin/pkg": pk(56, 100),
      "also/thin/pkg": pk(10, 100),
    },
  });
  assert.deepEqual(
    thin.map((entry) => entry.name),
    ["also/thin/pkg", "thin/pkg", "middle/pkg"],
  );
});

test("thinPackages is empty when every package is at or above the aggregate", () => {
  assert.deepEqual(
    thinPackages({ percentage: 50, packages: { "a/pkg": pk(60, 100) } }),
    [],
  );
});

test("lowestPerPackage records each package's own weakest sample", () => {
  // Taking the run with the lowest total would record a lucky floor for whichever
  // package happened to read well in it.
  const floor = lowestPerPackage([
    {
      coveredStatements: 500,
      totalStatements: 1000,
      packages: { "a/pkg": pk(400, 500), "b/pkg": pk(100, 500) },
    },
    {
      coveredStatements: 400,
      totalStatements: 1000,
      packages: { "a/pkg": pk(380, 500), "b/pkg": pk(20, 500) },
    },
  ]);
  assert.equal(floor.coveredStatements, 400);
  assert.deepEqual(floor.packages["a/pkg"], pk(380, 500));
  assert.deepEqual(floor.packages["b/pkg"], pk(20, 500));
});

// The floor tolerance, which exists because the measurement is noisy and a floor
// cannot be placed at or below the minimum of a distribution.
//
// Eight consecutive measurements of one unchanged tree gave 6810, 6810, 6811, 6811,
// 6811, 6812, 6812, 6812, and a later run gave 6809. The source is two packages:
// internal/runthrough moves by two statements and pkg/stow by one, because their
// concurrency tests interleave differently. So the gate allows four and the cost is
// named — a loss of one to four statements does not fail it, which is below the noise
// floor of the instrument but is still a detection it no longer has.

test("a fall inside the tolerance passes", () => {
  assert.deepEqual(
    coverageProblems(measured(4000, 65.0), measured(4004, 65.0), null, 1, 4),
    [],
  );
});

test("a fall beyond the tolerance fails, and the message says what was allowed", () => {
  const problems = coverageProblems(
    measured(4000, 65.0),
    measured(4005, 65.0),
    null,
    1,
    4,
  );
  assert.equal(problems.length, 1);
  assert.match(
    problems[0],
    /covered statements fell: 4000 < 4005 \(allowing 4 for measurement noise\)/,
  );
});

test("no tolerance is exact, so the default is a floor of zero", () => {
  // The parameter defaults rather than reaching for the constant, so a caller that
  // forgets it gets the strict behaviour instead of the slack one.
  assert.equal(
    coverageProblems(measured(4000, 65.0), measured(4001, 65.0), null, 1)
      .length,
    1,
  );
});

test("the tolerance reaches the per-package check too", () => {
  assert.deepEqual(
    packageProblems(
      { "a/pkg": pk(996, 1000) },
      { "a/pkg": pk(1000, 1000) },
      undefined,
      4,
    ),
    [],
  );
  const problems = packageProblems(
    { "a/pkg": pk(995, 1000) },
    { "a/pkg": pk(1000, 1000) },
    undefined,
    4,
  );
  assert.equal(problems.length, 1);
  assert.match(
    problems[0],
    /a\/pkg: covered statements fell: 995 < 1000 \(allowing 4\)/,
  );
});

test("a lowered floor is still a failure regardless of tolerance", () => {
  // The tolerance is about the measurement being noisy, not about the floor being
  // movable. A floor that was lowered to meet a measurement is the one thing the
  // history check exists to catch, and slack must not reach it.
  const problems = coverageProblems(
    measured(3800, 65.0),
    measured(3800, 65.0),
    measured(4000, 65.0),
    1,
    4,
  );
  assert.equal(problems.length, 1);
  assert.match(problems[0], /stored baseline was lowered/);
});

// The history checks take the same tolerance as the measurement check, for the same
// reason and symmetrically. A floor recorded from a noisy sample can land below the
// stored one, and refusing that would mean --baseline could produce a tree that fails
// its own next run — on the history check rather than on coverage. "Lowered" therefore
// means lowered by more than the noise, on both sides, and a real lowering is still
// several orders of magnitude larger than four statements.

test("a floor lowered by less than the noise is a re-record, not a lowering", () => {
  const problems = coverageProblems(
    measured(4000, 65.0),
    measured(3999, 65.0),
    measured(4002, 65.0),
    1,
    4,
  );
  assert.deepEqual(problems, []);
});

test("a floor lowered by more than the noise still fails", () => {
  const problems = coverageProblems(
    measured(3900, 65.0),
    measured(3900, 65.0),
    measured(4000, 65.0),
    1,
    4,
  );
  assert.equal(problems.length, 1);
  assert.match(problems[0], /stored baseline was lowered: 3900 < 4000/);
});

test("a per-package floor lowered by less than the noise is a re-record", () => {
  assert.deepEqual(
    packageProblems(
      { "a/pkg": pk(999, 1000) },
      { "a/pkg": pk(1000, 1000) },
      { "a/pkg": pk(1002, 1000) },
      4,
    ),
    [],
  );
});
