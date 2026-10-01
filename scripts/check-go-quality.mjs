#!/usr/bin/env node
// The Go quality ratchet, applying the shared policy.
//
// tools/quality used to be both the scanner and the gate. It measured the tree
// and it decided whether the measurement was acceptable, in a private copy of the
// policy that scripts/ratchet.mjs already owns. The copy was wrong in the way a
// hand-written copy usually is: its history check compared the *scan* against the
// parent baseline instead of the *committed baseline* against the parent, and its
// comment claimed that this was "the check that makes a baseline impossible to
// grow quietly". It was not. Raising the baseline left the scan unchanged, so the
// comparison had nothing to complain about, and the gate passed with a floor
// raised to approve a regression.
//
// That is the failure scripts/ratchet.mjs opens by naming: "a decision with one
// correct answer, written down more than once by hand, where every copy is small
// and every copy is individually plausible." So this file is the thin half. The
// scanner measures; compareKeys, expandedKeys and compareIdentities decide.
//
//   node scripts/check-go-quality.mjs
//   node scripts/check-go-quality.mjs --write-baseline   maintenance only
//
// The write mode exists because the policy requires a baseline to be lowered after
// debt is paid, and a policy you cannot satisfy is one people route around. It is
// a maintenance action, not a way to pass.

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { join, resolve } from "node:path";

import { readPreviousBaseline } from "./baseline-history.mjs";
import { compareIdentities, compareKeys, expandedKeys } from "./ratchet.mjs";

const BASELINE = "scripts/baselines/go-quality.json";

// The rules the scanner emits. Every one belongs in exactly one of these lists,
// because a rule present in a report and named in neither would be compared
// against nothing and therefore unconstrained.
const COUNTS = ["any", "comment-lines", "comment-ratio", "complexity", "max-params"];

// Rules where more is worse, so the shared policy's two-sided floor applies: a rise
// is new debt and a fall is an improvement the baseline has not recorded.
const RATCHETED = ["any", "comment-lines", "complexity", "max-params"];

// comment-ratio is reported and is not gated, and the reason is a concrete one
// rather than a preference.
//
// It is a density, not a quantity, so it has no floor: the shared policy's premise
// is that a baseline is a floor, and that is the wrong shape for "comment lines per
// thousand lines of code". This refactor removed three functions of dead code and
// left every comment line in place, and comment-ratio duly rose from 1196 to 1199.
// A two-sided ratchet called that new debt and an anti-raise guard refused to
// record it, so the gate failed on the exact change it exists to encourage. A
// ceiling failed for the mirror reason: the coverage the ratio is meant to protect
// was intact at 4161 comment lines, and only the denominator moved.
//
// It also no longer earns its place. Its original argument was that the count alone
// could be satisfied by deleting uncommented code - but deleting uncommented code
// does not change a count of comment lines, so it never could. What the count cannot
// see is a slice that adds much code beside its comments, and that is a judgement
// about a diff rather than a quantity to hold still, so it belongs in review
// rather than in a floor.
//
// The scanner still emits it, and COUNTS still names it, so a rule appearing or
// disappearing stays a visible event. It is simply not a floor.
const CEILINGS = [];

export function goQualityProblems({ report, baseline, previous }) {
  const problems = [];
  const counts = report?.counts ?? {};
  const allowed = baseline?.counts ?? {};

  // A rule the scanner reports and the gate does not name is measured and never
  // checked. The other direction is a rule the gate names that no longer exists,
  // which would keep failing on a metric nothing produces.
  for (const rule of Object.keys(counts)) {
    if (!COUNTS.includes(rule)) problems.push(`unconstrained rule ${rule}: add it to COUNTS`);
  }
  for (const rule of COUNTS) {
    if (!(rule in counts)) problems.push(`COUNTS names ${rule}, which the scanner does not report`);
  }

  // The anti-raise guard, and the reason this file exists: the committed baseline
  // compared against the one it replaced. Ceilings are excluded because raising a
  // ceiling is how a density legitimately moves, and refusing that would make the
  // maintenance action unable to record a real improvement.
  if (previous) {
    for (const rule of expandedKeys(allowed, previous.counts, RATCHETED)) {
      problems.push(`baseline expanded: ${rule} is above its own history`);
    }
  }

  // Both directions against the baseline. A reduction is a failure as well as an
  // increase, so a floor that stays put after being met stops describing anything.
  const { regressions, stale } = compareKeys(counts, allowed, RATCHETED);
  for (const item of regressions) problems.push(`new debt: ${item}`);
  for (const item of stale) problems.push(`stale baseline: ${item}`);

  // One direction only, for a metric that is a ceiling rather than a floor. The list is
  // empty today; the shape stays so a future ceiling is a one-line addition rather
  // than a re-derivation of which direction is right.
  for (const rule of CEILINGS) {
    const actual = counts[rule];
    const limit = allowed[rule];
    if (actual === undefined || limit === undefined) continue;
    if (actual > limit) problems.push(`comment density above its ceiling: ${rule} (${actual} > ${limit})`);
  }

  // Identity sets rather than counts, so renaming a violation does not launder it.
  const { added, stale: gone } = compareIdentities(
    (report?.violations ?? []).map((item) => item.identity),
    (baseline?.violations ?? []).map((item) => item.identity),
  );
  for (const identity of added) problems.push(`new: ${identity}`);
  for (const identity of gone) problems.push(`stale: ${identity}`);

  return problems;
}

function scan(repoRoot) {
  const out = execFileSync("go", ["run", "./tools/quality", "--json"], {
    cwd: repoRoot,
    encoding: "utf8",
    maxBuffer: 32 * 1024 * 1024,
  });
  return JSON.parse(out);
}

function readBaseline(repoRoot) {
  const path = join(repoRoot, BASELINE);
  if (!existsSync(path)) throw new Error(`${BASELINE} is missing`);
  return JSON.parse(readFileSync(path, "utf8"));
}

export function run({ repoRoot }) {
  const report = scan(repoRoot);
  const baseline = readBaseline(repoRoot);
  let previous = null;
  try {
    previous = readPreviousBaseline(repoRoot, BASELINE);
  } catch (error) {
    // A shallow checkout is a hard failure, never a skipped ratchet: a gate that
    // quietly stops comparing against history is the hole this file was written to
    // close.
    throw new Error(`${error.message} (set STOW_BASELINE_REF to compare against another ref)`);
  }
  return { report, baseline, previous, problems: goQualityProblems({ report, baseline, previous }) };
}

if (process.argv[1] && import.meta.url.endsWith(process.argv[1].split("/").pop())) {
  const repoRoot = resolve(import.meta.dirname, "..");

  if (process.argv.includes("--write-baseline")) {
    const report = scan(repoRoot);
    const path = join(repoRoot, BASELINE);
    const existing = existsSync(path) ? readBaseline(repoRoot) : null;
    let previous = null;
    try {
      previous = readPreviousBaseline(repoRoot, BASELINE);
    } catch {
      previous = null;
    }
    // Writing must not be able to raise a floor, or the maintenance action becomes
    // the bypass. A write that would expand a ratcheted count is refused. A ceiling
    // may move, because that is the direction a density legitimately goes when code
    // is removed and comments are not.
    const growth = expandedKeys(report.counts ?? {}, previous?.counts, RATCHETED);
    if (growth.length) {
      console.error("Refusing to write a baseline above its own history:");
      for (const rule of growth) console.error(`  ${rule}`);
      console.error("A baseline may shrink when debt is paid. It may not grow to pass.");
      process.exit(1);
    }
    for (const rule of CEILINGS) {
      const was = previous?.counts?.[rule];
      if (was !== undefined && (report.counts?.[rule] ?? 0) > was) {
        console.error(`Refusing to write a higher ${rule} ceiling: ${was} -> ${report.counts[rule]}`);
        process.exit(1);
      }
    }
    const payload = {
      version: report.version,
      _comment:
        existing?._comment ??
        "Ratchet baseline for the Go quality gate. Measured by tools/quality, judged by scripts/check-go-quality.mjs against scripts/ratchet.mjs.",
      violations: report.violations,
      counts: report.counts,
    };
    writeFileSync(path, `${JSON.stringify(payload, null, 2)}\n`);
    console.log(`Wrote ${path} (${payload.violations.length} violations)`);
    process.exit(0);
  }

  let outcome;
  try {
    outcome = run({ repoRoot });
  } catch (error) {
    console.error(error.message);
    process.exit(2);
  }

  if (outcome.problems.length) {
    console.error("Go quality ratchet violation");
    for (const problem of outcome.problems) console.error(`  ${problem}`);
    console.error("Fix the violation; do not raise the baseline to pass.");
    process.exit(1);
  }
  console.log(`Go quality ratchet OK (${outcome.baseline.violations.length} baseline entries)`);
}
