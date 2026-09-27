#!/usr/bin/env node
// Compare a fresh session-density measurement against the committed baseline.
//
//   node scripts/check-density.mjs
//   node scripts/check-density.mjs --write     # re-record the baseline
//
// The baseline in docs/benchmarks/session-baseline.json is a committed
// measurement that nothing re-measured, so it could drift without anyone
// noticing and the numbers would quietly stop describing the code. This gives it a
// comparison.
//
// It is deliberately not in `make standards`. RSS depends on the machine as much
// as on the program, and a threshold that fails on a loaded shared runner is worse
// than no threshold — this repository has already had a required check that could
// never pass and spent a release cycle fixing it. The numbers it compares are
// ratios rather than wall-clock, which is why a threshold is meaningful at all:
// rssMiBPerPayloadMiB moved 4.24 -> 4.26 between runs on the same machine, so the
// tolerance below is roughly fifty times the observed noise while still catching a
// real change in how much memory a session costs.
//
// Both directions fail. A large regression is a defect. A large improvement means
// the baseline is stale, and leaving it high is the same failure as leaving it low
// for a ratchet that cannot see regressions: the next contributor is told the
// wrong number.
import { execFileSync } from "node:child_process";
import { copyFileSync, readFileSync, rmSync } from "node:fs";
import { resolve } from "node:path";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const BASELINE = "docs/benchmarks/session-baseline.json";
const TOLERANCE = 0.25;
const WRITE = process.argv.includes("--write");

function fail(message) {
  console.error(`Session density check: ${message}`);
  process.exit(1);
}

function committedBaseline() {
  try {
    const raw = execFileSync("git", ["show", `HEAD:${BASELINE}`], { encoding: "utf8" });
    return JSON.parse(raw);
  } catch {
    fail(`${BASELINE} is not committed; nothing to compare against`);
  }
}

function ratio(fresh, recorded, name) {
  if (typeof recorded !== "number" || recorded <= 0) {
    fail(`the committed baseline has no usable ${name}`);
  }
  return { change: (fresh - recorded) / recorded, recorded };
}

// The sweep rewrites the baseline in place, so it is saved and put back. A check
// that leaves the working tree dirty is a check people stop running.
const backup = join(mkdtempSync(join(tmpdir(), "density-")), "baseline.json");
copyFileSync(BASELINE, backup);

try {
  execFileSync("node", ["packages/stow-s3/scripts/benchmark-session.mjs", "--sweep"], {
    stdio: ["ignore", "ignore", "inherit"],
  });

  if (WRITE) {
    console.log(`Re-recorded ${BASELINE}`);
    process.exit(0);
  }

  const fresh = JSON.parse(readFileSync(BASELINE, "utf8"));
  const before = committedBaseline();
  const problems = [];

  for (const [name, path] of [
    ["rssMiBPerPayloadMiB", ["derived", "rssMiBPerPayloadMiB"]],
    ["baselineRssMiB", ["derived", "baselineRssMiB"]],
  ]) {
    const value = path.reduce((node, key) => (node ? node[key] : undefined), fresh);
    if (typeof value !== "number") {
      fail(`the fresh measurement has no ${name}; run the sweep with --json`);
    }
    const { change, recorded } = ratio(value, before.derived?.[name], name);
    const direction = change > 0 ? "worse" : "better";
    console.log(
      `  ${name}: ${recorded} -> ${value} (${change >= 0 ? "+" : ""}${(change * 100).toFixed(1)}%, ${direction})`,
    );
    if (Math.abs(change) > TOLERANCE) {
      problems.push(
        `${name} moved ${(change * 100).toFixed(1)}%, past the ${TOLERANCE * 100}% tolerance`,
      );
    }
  }

  if (problems.length > 0) {
    for (const problem of problems) {
      console.error(`  ${problem}`);
    }
    if (problems.some((p) => p.startsWith("rssMiB") || p.startsWith("baseline"))) {
      console.error("If the change is real and intended, re-record with --write.");
    }
    fail(`${problems.length} density measurement(s) drifted`);
  }
  console.log(`Session density within ${TOLERANCE * 100}% of the recorded baseline`);
} finally {
  copyFileSync(backup, BASELINE);
  rmSync(backup, { force: true });
}
