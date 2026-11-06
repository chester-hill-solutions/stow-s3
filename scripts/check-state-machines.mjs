#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { readPreviousBaseline } from "./baseline-history.mjs";
import { compareIdentities } from "./ratchet.mjs";
import { scanTypeScript } from "./state-machine-typescript.mjs";

const root = resolve(import.meta.dirname, "..");
const baselineFile = "scripts/baselines/state-machines.json";
const readJSON = (path) => JSON.parse(readFileSync(path, "utf8"));

export function loadRules(repoRoot) {
  const rules = readJSON(join(repoRoot, "config/state-machine-rules.json"));
  return { ...rules, lifecycleNames: rules.lifecycleNames.flatMap((name) => [name, `is${name}`, `has${name}`]) };
}

function sources(directory, match) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    if (["node_modules", "dist", ".git"].includes(entry.name)) return [];
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return sources(path, match);
    return entry.isFile() && match(entry.name) ? [path] : [];
  });
}

export function identifyFindings(findings, repoRoot) {
  const occurrences = new Map();
  return findings.map((finding) => {
    const path = relative(repoRoot, finding.path).split("\\").join("/");
    const key = `${path}:${finding.owner}:${finding.rule}[${finding.flags.join(",")}]`;
    const occurrence = (occurrences.get(key) ?? 0) + 1;
    occurrences.set(key, occurrence);
    return { ...finding, path, identity: `${key}#${occurrence}` };
  }).sort((a, b) => a.identity.localeCompare(b.identity));
}

export function scanRepository(repoRoot) {
  const config = readJSON(join(repoRoot, "config/scan-roots.json"));
  const rules = loadRules(repoRoot);
  const files = config.go.flatMap((directory) => sources(join(repoRoot, directory), (name) => name.endsWith(".go") && !name.endsWith("_test.go")));
  const go = JSON.parse(execFileSync("go", ["run", "./tools/state-machines"], {
    cwd: root, input: JSON.stringify({ files, lifecycleNames: rules.lifecycleNames }), encoding: "utf8", maxBuffer: 8 * 1024 * 1024,
  }));
  const typescript = sources(join(repoRoot, config.typescriptPackage, "src"), (name) => /\.tsx?$/.test(name) && !name.endsWith(".d.ts"))
    .flatMap((path) => scanTypeScript(path, readFileSync(path, "utf8"), rules));
  return identifyFindings([...go, ...typescript], repoRoot);
}

function identities(baseline) {
  if (baseline?.version !== 1 || !Array.isArray(baseline.violations) || baseline.violations.some((value) => typeof value !== "string")) {
    throw new Error("invalid state-machine baseline");
  }
  if (new Set(baseline.violations).size !== baseline.violations.length) throw new Error("duplicate state-machine baseline identities");
  return baseline.violations;
}

export function stateMachineProblems({ findings, baseline, previous }) {
  const allowed = identities(baseline);
  const history = previous === null ? [] : compareIdentities(allowed, identities(previous)).added;
  const { added, stale } = compareIdentities(findings.map((finding) => finding.identity), allowed);
  return [
    ...history.map((identity) => `baseline expanded: ${identity}`),
    ...added.map((identity) => `new: ${identity}`),
    ...stale.map((identity) => `stale: ${identity}`),
  ];
}

export function run({ repoRoot = root, writeBaseline = false, report = false, scan = scanRepository } = {}) {
  const findings = scan(repoRoot);
  if (report) return { findings, problems: [] };
  const path = join(repoRoot, baselineFile);
  const previous = readPreviousBaseline(repoRoot, baselineFile);
  const baseline = existsSync(path) ? readJSON(path) : null;
  if (!writeBaseline && baseline === null) throw new Error(`${baselineFile} is missing`);
  if (writeBaseline) {
    const current = findings.map((finding) => finding.identity);
    for (const recorded of [baseline, previous].filter((value) => value !== null)) {
      const added = compareIdentities(current, identities(recorded)).added;
      if (added.length) throw new Error(`Cannot baseline new implicit state machines:\n${added.join("\n")}`);
    }
    writeFileSync(path, `${JSON.stringify({ version: 1, violations: current }, null, 2)}\n`);
    return { findings, problems: [] };
  }
  return { findings, problems: stateMachineProblems({ findings, baseline, previous }) };
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    const options = process.argv.slice(2);
    if (options.some((option) => !["--write-baseline", "--report"].includes(option))) throw new Error("usage: check-state-machines.mjs [--write-baseline | --report]");
    const { findings, problems } = run({ writeBaseline: options.includes("--write-baseline"), report: options.includes("--report") });
    if (options.includes("--report")) console.log(JSON.stringify(findings, null, 2));
    else if (problems.length) {
      console.error(`Implicit state-machine ratchet violation\n${problems.map((problem) => `  ${problem}`).join("\n")}`);
      console.error("Use named states and transitions; remove stale baseline entries. Do not add exemptions.");
      process.exitCode = 1;
    } else console.log(`Implicit state-machine ratchet OK (${findings.length} baseline entries)`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
