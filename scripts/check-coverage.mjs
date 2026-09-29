#!/usr/bin/env node
// Go coverage ratchet.
//
// The invariant is the number of covered statements, not the percentage.
// A percentage falls whenever well-tested code is added with defensive error
// branches that only a real failure would reach, which is how adding a 20
// statement system-call wrapper with 12 covered could drop the percentage by
// 0.04 points while coverage actually improved. Covered statements express
// what regression means here: fewer statements are being exercised.
//
// The percentage is still reported, and a large percentage drop is still a
// failure, so the statement count cannot be gamed by deleting a few heavily
// covered tests while adding a large volume of trivially covered code.
//
// The recorded numbers are a floor, not a target. Concurrency tests in
// internal/runthrough interleave differently between runs, so the measurement
// moves by a statement or two; recording the floor keeps a noisy run from
// failing while still catching a real loss of covered statements.
//
// Coverage is computed from the profile rather than read from
// "go tool cover -func", whose total is rounded to one decimal. A true value on a
// rounding boundary, such as 60.15%, otherwise reports as 60.1 on one run and
// 60.2 on the next, so the same commit passes and fails.
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { readPreviousBaseline } from "./baseline-history.mjs";

// A percentage drop larger than this is treated as a regression even if the
// covered statement count held, which is the case where statement counts alone
// would be gameable.
const MAX_PERCENTAGE_DROP = 1;

// How many samples --baseline takes before recording a value.
//
// The measurement moves by a few statements between runs, because the concurrency
// tests in internal/runthrough interleave differently, and the floor has to sit at or
// below the true minimum or the gate flaps on scheduling rather than on coverage.
//
// Three samples was not enough, and the claim in the header comment that the floor
// "keeps a noisy run from failing" was false while it was three: four consecutive runs
// measured 6810, 6812, 6813, 6812, and the lowest of three samples had recorded 6811 —
// one above the minimum. So the gate failed roughly one run in four while nothing about
// coverage had changed.
//
const BASELINE_SAMPLES = 7;

// How far below its floor the measurement may land before the gate fails.
//
// This is not slack for its own sake; it is the measured nondeterminism, and it is
// stated rather than absorbed into the floor because a floor cannot be placed at or
// below the minimum of a distribution.
//
// Twenty-one measurements of one unchanged tree fell between 6809 and 6816, and
// `--baseline` on seven samples of the same tree gave 6810 through 6816. The source is
// two packages and only two: internal/runthrough moves between 1368 and 1370 covered
// statements and pkg/stow between 1287 and 1288, because their concurrency tests
// interleave differently. internal/storage/workspace measured 734 every single time.
//
// The floor is recorded at the lowest sample and this tolerance sits below it, so the
// effective failure threshold is 6806 — under everything observed. The two together are
// what make the gate stable; the tolerance alone would not be enough, because a
// distribution has no minimum to aim at. Ten consecutive runs after this change passed,
// where three samples had flapped about one run in four.
//
// The cost is real and named: a loss of one to four statements will not fail this gate.
// A loss that small is below the noise floor of the instrument, so it is not a
// detection the gate could have had anyway — but it is a detection it no longer has,
// and pretending otherwise would be the worse mistake.
const FLOOR_TOLERANCE = 4;

const repoRoot = resolve(import.meta.dirname, "..");
const baselinePath = resolve(repoRoot, "scripts/baselines/go-coverage.json");
const profilePath = resolve(repoRoot, ".cache/go-coverage.out");

// The gate runs only when this file is the entry point. The unit tests import the
// functions below, and while the run sat at module scope a failing gate called
// process.exit before a single test reported: an instrument that cannot be tested
// when it is broken only ever gets to prove that it works.
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main();
}

function main() {
  mkdirSync(resolve(repoRoot, ".cache"), { recursive: true });

  if (process.argv.includes("--baseline")) {
    // The measurement is noisy, so a single sample records a number the next run
    // may not reproduce, which is how this gate used to flap. The baseline is a
    // floor, so record the lowest reading rather than the luckiest one.
    const samples = [];
    for (let index = 0; index < BASELINE_SAMPLES; index += 1) {
      samples.push(measure());
    }
    const floor = lowestPerPackage(samples);
    writeFileSync(baselinePath, `${JSON.stringify(floor, null, 2)}\n`);
    const recorded = Object.keys(floor.packages).length;
    console.log(
      `Wrote ${baselinePath}: ${floor.percentage}% (${floor.coveredStatements}/${floor.totalStatements} statements) ` +
        `from the lowest of ${BASELINE_SAMPLES} samples: ${samples.map((s) => s.coveredStatements).join(", ")}, ` +
        `with ${recorded} per-package floors`,
    );
    return;
  }

  const measured = measure();
  if (!existsSync(baselinePath)) {
    console.error(`Coverage baseline missing: ${baselinePath}`);
    process.exit(2);
  }
  const baseline = JSON.parse(readFileSync(baselinePath, "utf8"));
  if (
    typeof baseline.coveredStatements !== "number" ||
    typeof baseline.percentage !== "number"
  ) {
    console.error(
      "Coverage baseline must record coveredStatements and percentage; rewrite it with --baseline",
    );
    process.exit(2);
  }

  let previous = null;
  try {
    previous = readPreviousBaseline(
      repoRoot,
      "scripts/baselines/go-coverage.json",
    );
  } catch (error) {
    console.error(error.message);
    process.exit(2);
  }
  const problems = coverageProblems(
    measured,
    baseline,
    previous,
    MAX_PERCENTAGE_DROP,
    FLOOR_TOLERANCE,
  );

  if (problems.length > 0) {
    for (const problem of problems) {
      console.error(`Go coverage ratchet: ${problem}`);
    }
    process.exit(1);
  }
  const summary = `${measured.percentage}%, ${measured.coveredStatements}/${measured.totalStatements} statements`;
  if (measured.coveredStatements > baseline.coveredStatements) {
    console.log(
      `Go coverage ratchet OK (${summary}); above the recorded floor of ${baseline.coveredStatements}, raise it with --baseline if the improvement is real`,
    );
  } else {
    console.log(`Go coverage ratchet OK (${summary})`);
  }
  reportThin(measured, baseline);
}

// reportThin names every package below the aggregate, on a passing run as well as a
// failing one.
//
// The per-package floors freeze what is true, and freezing 56.4% is a low bar. What
// keeps that honest is that the gate says so out loud: a floor is a floor, and a
// reader who is not told which packages sit under the aggregate has no way to tell a
// deliberate low floor from a forgotten one.
function reportThin(measured, baseline) {
  const thin = thinPackages(measured);
  if (thin.length === 0) {
    return;
  }
  console.log(
    `  ${thin.length} package(s) below the ${measured.percentage}% aggregate — floors, not targets: ` +
      thin
        .map(
          (entry) =>
            `${entry.name.replace(repoRoot + "/", "")} ${entry.percentage}%`,
        )
        .join(", "),
  );
  void baseline;
}

// measure runs the suite and reads the profile.
function measure() {
  execFileSync("go", ["test", "./...", "-coverprofile", profilePath], {
    cwd: repoRoot,
    stdio: "inherit",
  });
  return coverageFromProfile(readFileSync(profilePath, "utf8"));
}

// lowestPerPackage reduces several samples to the floor to record: the aggregate
// totals from the weakest run, and each package's own weakest reading.
//
// Per package rather than per run because a package's noisiest sample need not be the
// run with the lowest total, so taking the whole lowest run would record a lucky floor
// for some packages and a harsh one for others. The minimum is the conservative choice
// in every case.
export function lowestPerPackage(samples) {
  const lowest = samples.reduce((a, b) =>
    b.coveredStatements < a.coveredStatements ? b : a,
  );
  const packages = {};
  for (const sample of samples) {
    for (const [name, counts] of Object.entries(sample.packages ?? {})) {
      const held = packages[name];
      packages[name] =
        held === undefined || counts.coveredStatements < held.coveredStatements
          ? counts
          : held;
    }
  }
  return {
    percentage: lowest.percentage,
    coveredStatements: lowest.coveredStatements,
    totalStatements: lowest.totalStatements,
    packages,
  };
}

// coverageProblems is the ratchet decision, as a pure function of what was
// measured, what is recorded, and what was recorded before.
//
// It is separated from the measurement so it can be tested. The gate decides
// whether a change is allowed to reduce what is tested, and until this was
// extracted there was no way to ask what it decides - the only evidence it had ever
// given was a number in a CI log.
export function coverageProblems(
  measured,
  baseline,
  previous,
  maxPercentageDrop,
  floorTolerance = 0,
) {
  const problems = [];
  if (
    measured.coveredStatements <
    baseline.coveredStatements - floorTolerance
  ) {
    problems.push(
      `covered statements fell: ${measured.coveredStatements} < ${baseline.coveredStatements}` +
        (floorTolerance > 0
          ? ` (allowing ${floorTolerance} for measurement noise)`
          : ""),
    );
  }
  const drop = round(baseline.percentage - measured.percentage);
  if (drop > maxPercentageDrop) {
    problems.push(
      `coverage percentage fell by ${drop} points (${baseline.percentage}% -> ${measured.percentage}%), above the ${maxPercentageDrop} point allowance`,
    );
  }
  // An improvement is reported, never a failure. The measurement varies by a
  // statement or two between runs because concurrency tests in internal/runthrough
  // interleave differently, so failing on an improvement would make the gate flap
  // on scheduling rather than on coverage. Record a real improvement deliberately
  // with --baseline, which is where the judgement belongs.
  if (
    previous &&
    baseline.coveredStatements < previous.coveredStatements - floorTolerance
  ) {
    problems.push(
      `the stored baseline was lowered: ${baseline.coveredStatements} < ${previous.coveredStatements}`,
    );
  }
  problems.push(
    ...packageProblems(
      measured.packages,
      baseline.packages,
      previous?.packages,
      floorTolerance,
    ),
  );
  return problems;
}

// packageProblems is the per-package half of the ratchet, and it exists because a
// single aggregate cannot tell a well-tested protocol surface from an untested one.
//
// The aggregate is not wrong: it is the right invariant for "did this change reduce
// what is tested". It is silent about a package that is barely covered at all, and the
// way it is silent is by being correct — an untested package drags the aggregate down
// while a well-tested one holds it up, and the recorded number does not say which did
// what. `cmd/stow-s3`, the package a user types into, sat at 56.4% against a 71.05%
// aggregate for as long as the aggregate was the only instrument.
//
// Floors are recorded at the value each package actually reaches, so recording them
// freezes what is true rather than asserting what would be nice, and a package can
// only move up. That is why the thin packages are not excluded: a floor of 56.4% is a
// low bar, but it is a bar, and reportThin names every package below the aggregate so
// the gap stays visible rather than being floored away silently.
export function packageProblems(
  measured,
  baseline,
  previous,
  floorTolerance = 0,
) {
  const problems = [];
  if (measured === undefined || baseline === undefined) {
    // A baseline with no per-package floors is the mode where this check silently
    // passes, so it is a failure with a remedy rather than a skip. An earlier attempt
    // wrote a baseline with zero per-package entries and reported success, because
    // nothing asked whether the measurement it recorded had any entries in it.
    problems.push(
      "the stored baseline records no per-package floors, so a package could lose all its coverage without this gate noticing; rewrite it with --baseline",
    );
    return problems;
  }
  for (const [name, floor] of Object.entries(baseline)) {
    const got = measured[name];
    if (got === undefined) {
      // The package is gone, or stopped being built. Both need a decision: a deleted
      // package leaves a stale floor, and one that is no longer measured loses its
      // coverage without any of it being removed.
      problems.push(
        `${name} is in the baseline but not in this measurement, so its ${floor.coveredStatements} covered statements are not being measured; delete the package or the floor, deliberately`,
      );
      continue;
    }
    if (got.coveredStatements < floor.coveredStatements - floorTolerance) {
      problems.push(
        `${name}: covered statements fell: ${got.coveredStatements} < ${floor.coveredStatements}` +
          (floorTolerance > 0 ? ` (allowing ${floorTolerance})` : ""),
      );
    }
    if (
      previous !== undefined &&
      previous[name] !== undefined &&
      floor.coveredStatements <
        previous[name].coveredStatements - floorTolerance
    ) {
      problems.push(
        `${name}: the stored floor was lowered: ${floor.coveredStatements} < ${previous[name].coveredStatements}`,
      );
    }
  }
  return problems;
}

// thinPackages names every measured package whose covered share is below the aggregate,
// so a floor is not mistaken for a target.
export function thinPackages(measured) {
  if (measured === undefined || measured.percentage === undefined) {
    return [];
  }
  return Object.entries(measured.packages ?? {})
    .filter(
      ([, counts]) =>
        round((counts.coveredStatements / counts.totalStatements) * 100) <
        measured.percentage,
    )
    .map(([name, counts]) => ({
      name,
      percentage: round(
        (counts.coveredStatements / counts.totalStatements) * 100,
      ),
    }))
    .sort((a, b) => a.percentage - b.percentage);
}

function coverageFromProfile(profile) {
  // Each line is "name.go:startLine.startCol,endLine.endCol numStatements count".
  const entry = /:\d+\.\d+,\d+\.\d+ (\d+) (\d+)$/;
  let totalStatements = 0;
  let coveredStatements = 0;
  for (const line of profile.split("\n")) {
    if (line.length === 0 || line.startsWith("mode:")) {
      continue;
    }
    const match = entry.exec(line);
    if (match === null) {
      continue;
    }
    const statements = Number(match[1]);
    const count = Number(match[2]);
    if (!Number.isFinite(statements) || !Number.isFinite(count)) {
      continue;
    }
    totalStatements += statements;
    if (count > 0) {
      coveredStatements += statements;
    }
  }
  if (totalStatements === 0) {
    console.error("Could not read statements from the Go coverage profile");
    process.exit(2);
  }
  return {
    percentage: round((coveredStatements / totalStatements) * 100),
    coveredStatements,
    totalStatements,
    packages: statementsByPackage(profile),
  };
}

// packageOf reads the package a profile line's file belongs to.
//
// A profile line is "<import path>/<file>.go:<start>.<col>,<end>.<col> N count", so
// the package is the import path with the file name removed. Taking the directory
// rather than the package's declared name is deliberate: a directory can hold
// `package foo_test` and `package foo`, and those are one unit of coverage, while a
// declared name says nothing about where the code lives.
function packageOf(nameWithFile) {
  return nameWithFile.replace(/\/[^/]*$/, "");
}

// statementsByPackage is the per-package measurement, as a pure function of a profile
// so it can be tested against a fixture rather than only by running the suite.
//
// It is separate from coverageFromProfile because the aggregate and the per-package
// views answer different questions, and because the aggregate was working while the
// per-package one did not exist at all: the profile's file name was matched by a
// regex that never captured it. A per-package floor built on a measurement nobody had
// tested would have been a floor on nothing.
export function statementsByPackage(profile) {
  const entry = /^(.+):\d+\.\d+,\d+\.\d+ (\d+) (\d+)$/;
  const byPackage = new Map();
  for (const line of profile.split("\n")) {
    if (line.length === 0 || line.startsWith("mode:")) {
      continue;
    }
    const match = entry.exec(line);
    if (match === null) {
      continue;
    }
    const statements = Number(match[2]);
    const count = Number(match[3]);
    if (!Number.isFinite(statements) || !Number.isFinite(count)) {
      continue;
    }
    const name = packageOf(match[1]);
    const seen = byPackage.get(name) ?? {
      coveredStatements: 0,
      totalStatements: 0,
    };
    seen.totalStatements += statements;
    if (count > 0) {
      seen.coveredStatements += statements;
    }
    byPackage.set(name, seen);
  }
  const packages = {};
  for (const name of [...byPackage.keys()].sort()) {
    packages[name] = byPackage.get(name);
  }
  return packages;
}

function round(value) {
  return Math.round(value * 100) / 100;
}
