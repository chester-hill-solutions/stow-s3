#!/usr/bin/env node
// The refusal matrix, as a thing that runs rather than a thing someone remembers.
//
// A test that asserts a policy denies an operation cannot tell a working check from a
// wrong one: with the operation denied, a check for *any* other operation is refused too,
// because default denial spans operations. So a suite can pass with a check aimed
// anywhere, or deleted. That was measured here - pointing GetObject's check at
// object.write produced zero failures in TestEveryOperationReachesThePolicy, and only an
// unrelated test about S3 prefix semantics caught it.
//
// This closes it by construction rather than by inspection. Every enforcement site is
// mutated to consult an operation no policy grants, and the site is proven only if the
// refusal suite then fails *and names that operation*. A check that is never reached, or
// that is not the one the case covers, leaves the suite green and is reported as a hole.
//
// The mutation is a sentinel rather than a deletion on purpose. Deleting a call site also
// breaks compilation whenever it leaves an import unused, which reads like a failure but
// proves nothing about reach. A sentinel compiles everywhere and can only be caught by
// something actually consulting it.
//
// Not part of `make standards`: it edits source files, so it cannot share a working tree
// with anything else. It is its own command, and its result is a table.

import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

export const SENTINEL = "authority.Operation(\"mutation.not.granted.by.any.policy\")";

// A refusal names the operation by its value ("object.write"), not by the Go constant
// ("ObjectWrite"), so matching the identifier reported every site as a hole. That is the
// same class of mistake as this whole check exists to catch: reporting a finding that the
// evidence does not support.
export function operationValues(repoRoot) {
  const source = readFileSync(join(repoRoot, "internal/authority/authority.go"), "utf8");
  const values = new Map();
  for (const match of source.matchAll(/(\w+)\s+Operation = "([a-z.]+)"/g)) values.set(match[1], match[2]);
  return values;
}

// The enforcement spellings the repo recognises, and the operation each site consults.
// A site missing from this list is a site this check cannot prove, which the scan at the
// bottom reports rather than passing over.
export const SITES = [
  { file: "internal/runtime/buckets.go", pattern: /i\.checkCollection\(authority\.(BucketCreate|BucketDelete),/, operations: ["BucketCreate", "BucketDelete"], pkg: "./internal/runtime" },
  { file: "internal/runtime/buckets.go", pattern: /i\.check\(authority\.BucketList\)/, operations: ["BucketList"], pkg: "./internal/runtime" },
  { file: "internal/runtime/instance.go", pattern: /i\.check\(authority\.EnvironmentReset\)/, operations: ["EnvironmentReset"], pkg: "./internal/runtime" },
  { file: "internal/runtime/objects.go", pattern: /i\.checkResource\(authority\.(ObjectRead|ObjectWrite|ObjectDelete|ObjectList),/, operations: ["ObjectRead", "ObjectWrite", "ObjectDelete", "ObjectList"], pkg: "./internal/runtime" },
  { file: "internal/runtime/guarded_save.go", pattern: /i\.checkResource\(authority\.(ObjectRead|ObjectWrite),/, operations: ["ObjectRead", "ObjectWrite"], pkg: "./internal/runtime" },
  { file: "internal/runtime/object_recovery.go", pattern: /i\.check(Resource)?\(authority\.(ObjectRead|ObjectWrite),?/, operations: ["ObjectRead", "ObjectWrite"], pkg: "./internal/runtime" },
  { file: "internal/runtime/multipart.go", pattern: /i\.checkUpload\(authority\.(ObjectRead|ObjectWrite)\)/, operations: ["ObjectRead", "ObjectWrite"], pkg: "./internal/runtime" },
  { file: "internal/runtime/save_requests.go", pattern: /i\.checkResource\(authority\.ObjectRead,/, operations: ["ObjectRead"], pkg: "./internal/runtime" },
  { file: "pkg/stow/checkpoint.go", pattern: /w\.Runtime\.Authorize\(authority\.WorkspaceCapture,/, operations: ["WorkspaceCapture"], pkg: "./pkg/stow" },
  { file: "pkg/stow/workspace.go", pattern: /w\.Runtime\.Authorize\(authority\.EnvironmentDestroy,/, operations: ["EnvironmentDestroy"], pkg: "./pkg/stow" },
  // The environment grant on the out-of-session path. It is the only gate when no policy
  // is in force, so it is an enforcement site like any other - the coverage scan found it,
  // which is what the uncovered-calls report is for.
  { file: "pkg/stow/checkpoint_external.go", pattern: /principal\.Environment\.Check\(authority\.(WorkspaceCapture)\)/, operations: ["WorkspaceCapture"], pkg: "./pkg/stow" },
];

// mutationsFor lists every single-site mutation in one file, so the file is written once
// per operation rather than once per occurrence. Matching every occurrence of one
// constant in a file is deliberate: a file may consult the same operation from two verbs,
// and both are real enforcement that has to be reached.
// One mutation per occurrence, not per file. Mutating every occurrence of an operation in
// a file at once breaks several verbs together, and then the refusal names whichever of
// them happened to be reached first - so a file with three real sites could pass on one
// and the other two would go unproven. Proving a site means breaking that site.
export function mutationsFor(file, site) {
  const text = readFileSync(file, "utf8");
  const found = [];
  for (const op of site.operations) {
    const hits = [...text.matchAll(new RegExp(`authority\\.${op}`, "g"))];
    hits.forEach((hit, index) => {
      const at = hit.index;
      found.push({
        file,
        operation: op,
        line: text.slice(0, at).split("\n").length,
        occurrences: hits.length,
        mutate: (source) => `${source.slice(0, at)}${SENTINEL}${source.slice(at + hit[0].length)}`,
      });
    });
  }
  return found;
}

// Three outcomes, not two, because they are three different claims.
//
// "policy" is the strong one: a policy refused and named the operation, so a policy
// demonstrably reaches this site.
// "environment" is weaker and worth naming rather than counting as proven: the suite
// failed because the environment refuses an operation it does not know, which proves the
// site is *executed* on the path but says nothing about a policy reaching it. A site
// exercised only by tests that install no policy has a weaker guarantee than one exercised
// under a policy, and reporting it as proven would overstate.
// "unreached" is the hole: nothing noticed the edit at all.
export function classify(result, value) {
  if (result.status === 0) return { kind: "unreached", why: "the refusal suite passed, so nothing consulted the mutated operation" };
  const output = `${result.stdout || ""}${result.stderr || ""}`;
  if (output.includes(value)) return { kind: "policy", why: `a policy refused, naming ${value}` };
  return {
    kind: "environment",
    why: "refused by the environment refusing an operation it does not know, so the site runs but no policy was shown to reach it",
  };
}

// unrecognisedSites finds enforcement calls the mutation list does not cover. A site that
// no mutation can prove is a hole in the matrix, and it is reported as one rather than
// left for someone to notice.
export function unrecognisedSites(repoRoot) {
  const known = new Set(SITES.map((site) => site.file));
  const out = [];
  for (const dir of ["internal/runtime", "pkg/stow"]) {
    const visit = (path) => {
      for (const entry of readdirSync(path, { withFileTypes: true })) {
        const full = join(path, entry.name);
        if (entry.isDirectory()) { visit(full); continue; }
        if (!entry.name.endsWith(".go") || entry.name.endsWith("_test.go")) continue;
        const source = readFileSync(full, "utf8");
        const lines = source.split("\n");
        lines.forEach((line, index) => {
          if (!/\.(check|checkResource|checkUpload|checkCollection|Allows|Check|Authorize)\(authority\./.test(line)) return;
          const relative = full.replace(`${repoRoot}/`, "");
          if (known.has(relative)) return;
          out.push(`${relative}:${index + 1} ${line.trim().slice(0, 70)}`);
        });
      }
    };
    visit(join(repoRoot, dir));
  }
  return out;
}

export function run(repoRoot = resolve(import.meta.dirname, "..")) {
  const values = operationValues(repoRoot);
  const table = [];
  for (const site of SITES) {
    const path = join(repoRoot, site.file);
    if (!existsSync(path)) {
      table.push({ site: site.file, operation: "(missing)", line: 0, kind: "unreached", why: `${site.file} does not exist` });
      continue;
    }
    const original = readFileSync(path, "utf8");
    for (const mutation of mutationsFor(path, site)) {
      writeFileSync(path, mutation.mutate(original));
      let result;
      try {
        result = spawnSync("go", ["test", "-count=1", site.pkg], { cwd: repoRoot, encoding: "utf8", timeout: 600000 });
      } finally {
        writeFileSync(path, original);
      }
      const value = values.get(mutation.operation);
      const { kind, why } = classify(result, value ?? mutation.operation);
      table.push({
        site: site.file.replace(`${repoRoot}/`, ""),
        operation: mutation.operation,
        line: mutation.line,
        kind,
        why,
      });
    }
  }
  return { table, unrecognised: unrecognisedSites(repoRoot) };
}

if (process.argv[1] && import.meta.url.endsWith(process.argv[1].split("/").pop())) {
  const repoRoot = resolve(import.meta.dirname, "..");
  const goVersion = readFileSync(join(repoRoot, ".go-version"), "utf8").trim();
  process.env.GOTOOLCHAIN = `go${goVersion}`;
  console.log("Mutating each enforcement site and requiring a named refusal:");
  const { table, unrecognised } = run(repoRoot);
  const mark = { policy: "ok  ", environment: "weak", unreached: "HOLE" };
  for (const row of table) {
    console.log(`  ${mark[row.kind]} ${row.operation.padEnd(18)} ${row.site}:${row.line} — ${row.why}`);
  }
  const holes = table.filter((row) => row.kind === "unreached");
  const weak = table.filter((row) => row.kind === "environment");
  if (unrecognised.length > 0) {
    console.log("");
    console.log(`  ${unrecognised.length} enforcement call(s) no mutation covers, so the matrix cannot prove them:`);
    for (const line of unrecognised) console.log(`    ${line}`);
  }
  console.log("");
  const byPolicy = table.filter((row) => row.kind === "policy").length;
  console.log(`${table.length} sites: ${byPolicy} reached by a policy naming the operation, ${weak.length} reached only as far as the environment, ${holes.length} unreached.`);
  if (weak.length > 0) {
    console.log("");
    console.log(`  ${weak.length} site(s) run but are not shown to be reached by a policy. Not a failure, and not a pass either:`);
    for (const row of weak) console.log(`    ${row.site}:${row.line} ${row.operation}`);
  }
  console.log("");
  if (holes.length > 0 || unrecognised.length > 0) {
    console.error(`Refusal matrix incomplete: ${holes.length} site(s) unreached, ${unrecognised.length} call(s) uncovered.`);
    process.exit(1);
  }
  console.log("Every enforcement site is reached. The sites marked weak still need a policy-exercised test.");
}