import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { test } from "node:test";
import { identifyFindings, loadRules, run, scanRepository, stateMachineProblems } from "./check-state-machines.mjs";
import { scanTypeScript } from "./state-machine-typescript.mjs";

const root = resolve(import.meta.dirname, "..");
const rules = loadRules(root);
const baseline = (...violations) => ({ version: 1, violations });
const finding = (identity) => ({ identity });
const scan = (source) => scanTypeScript("fixture.ts", source, rules);

for (const [name, source, rule] of [
  ["reader regression", "function openRead() { let closed = false; let closePromise: Promise<void> | undefined; }", "lifecycle-cleanup-promise"],
  ["class flags", "class Server { private started = false; private closed: boolean; }", "lifecycle-flags"],
  ["grouped flags", "function run() { let started = false, closed = false; }", "lifecycle-flags"],
  ["private flags", "class Server { #closed = false; _closing = false; }", "lifecycle-flags"],
  ["prefixed names", "class Server { isClosed = false; hasStarted = false; }", "lifecycle-flags"],
  ["optional class promise", "class Reader { closed = false; closePromise?: Promise<void>; }", "lifecycle-cleanup-promise"],
  ["nested callback", "function run() { return () => { let closed = false; let closing: Promise<void> | undefined; }; }", "lifecycle-cleanup-promise"],
]) {
  test(`TypeScript detects ${name}`, () => {
    const found = scan(source);
    assert.equal(found.length, 1);
    assert.equal(found[0].rule, rule);
  });
}

for (const [name, source] of [
  ["single boolean", "class Store { closed = false; }"],
  ["explicit machine", 'class Reader { state: "open" | "closing" | "closed" = "open"; closePromise?: Promise<void>; }'],
  ["capabilities", "class Store { persistent = false; multipart = false; closed = false; }"],
  ["independent scopes", "function run() { let started = false; return () => { let closed = false; }; }"],
  ["independent blocks", "function run() { { let started = false; } { let closed = false; } }"],
  ["unrelated promise", "function run() { let closed = false; let requestPromise: Promise<void> | undefined; }"],
  ["constant bindings", "function run() { const started = false; const closed = true; }"],
  ["nonoptional promise", "class Reader { closed = false; closePromise: Promise<void>; }"],
  ["comments and strings", '// let started = false; let closed = false;\nconst text = "let started = false; let closed = false;";'],
]) {
  test(`TypeScript permits ${name}`, () => assert.deepEqual(scan(source), []));
}

test("TypeScript parse errors fail instead of dropping a file", () => {
  assert.throws(() => scan("class Broken {"), /fixture.ts:/);
});

test("identities survive line movement and count repeated owners separately", () => {
  const source = "function run() { { let started = false; let closed = false; } { let started = false; let closed = false; } }";
  const first = identifyFindings(scan(source), root);
  const second = identifyFindings(scan(`\n\n${source}`), root);
  assert.deepEqual(first.map((value) => value.identity), second.map((value) => value.identity));
  assert.equal(new Set(first.map((value) => value.identity)).size, 2);
  assert.notEqual(first[0].line, second[0].line);
});

test("ratchet rejects new debt, stale debt and same-count baseline replacement", () => {
  assert.deepEqual(stateMachineProblems({ findings: [finding("old")], baseline: baseline("old"), previous: baseline("old") }), []);
  assert.deepEqual(stateMachineProblems({ findings: [finding("new")], baseline: baseline(), previous: baseline() }), ["new: new"]);
  assert.deepEqual(stateMachineProblems({ findings: [], baseline: baseline("old"), previous: baseline("old") }), ["stale: old"]);
  assert.deepEqual(stateMachineProblems({ findings: [finding("new")], baseline: baseline("new"), previous: baseline("old") }), ["baseline expanded: new"]);
  assert.deepEqual(stateMachineProblems({ findings: [], baseline: baseline(), previous: baseline("old") }), []);
});

test("invalid baseline schema and duplicate identities fail", () => {
  for (const value of [{}, { version: 2, violations: [] }, baseline(7), baseline("old", "old")]) {
    assert.throws(() => stateMachineProblems({ findings: [], baseline: value, previous: null }), /baseline/);
  }
});

function repository(t, historical = JSON.stringify(baseline("old"))) {
  const directory = mkdtempSync(join(tmpdir(), "stow-state-machines-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  mkdirSync(join(directory, "scripts/baselines"), { recursive: true });
  const path = join(directory, "scripts/baselines/state-machines.json");
  const git = (...args) => execFileSync("git", args, { cwd: directory, stdio: "pipe", env: { ...process.env, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null" } });
  git("init", "-q");
  writeFileSync(path, historical);
  git("add", ".");
  git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "baseline");
  git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "next");
  return { directory, path };
}

test("baseline writer can shrink but cannot accept new findings", (t) => {
  const { directory, path } = repository(t);
  run({ repoRoot: directory, writeBaseline: true, scan: () => [] });
  assert.deepEqual(JSON.parse(readFileSync(path, "utf8")), baseline());
  assert.throws(() => run({ repoRoot: directory, writeBaseline: true, scan: () => [finding("new")] }), /Cannot baseline new/);
  assert.deepEqual(JSON.parse(readFileSync(path, "utf8")), baseline());
});

test("on-disk baseline replacement is checked against Git history", (t) => {
  const { directory, path } = repository(t);
  writeFileSync(path, JSON.stringify(baseline("new")));
  const result = run({ repoRoot: directory, scan: () => [finding("new")] });
  assert.deepEqual(result.problems, ["baseline expanded: new"]);
});

test("scanner and unavailable history failures propagate", (t) => {
  const directory = mkdtempSync(join(tmpdir(), "stow-state-machines-no-history-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  assert.throws(() => run({ repoRoot: directory, scan: () => { throw new Error("scanner failed"); } }), /scanner failed/);
  assert.throws(() => run({ repoRoot: directory, scan: () => [] }), /Cannot resolve ratchet base/);
});

test("malformed historical JSON cannot masquerade as the first baseline", (t) => {
  const { directory, path } = repository(t, "{broken JSON");
  writeFileSync(path, JSON.stringify(baseline()));
  assert.throws(() => run({ repoRoot: directory, scan: () => [] }), SyntaxError);
});

test("real scanners feed Go and TypeScript regressions into the gate", (t) => {
  const { directory, path } = repository(t);
  mkdirSync(join(directory, "config"));
  mkdirSync(join(directory, "go"));
  mkdirSync(join(directory, "client/src"), { recursive: true });
  writeFileSync(join(directory, "config/scan-roots.json"), JSON.stringify({ go: ["go"], typescriptPackage: "client" }));
  writeFileSync(join(directory, "config/state-machine-rules.json"), readFileSync(join(root, "config/state-machine-rules.json")));
  writeFileSync(path, JSON.stringify(baseline()));
  const goPath = join(directory, "go/server.go");
  const tsPath = join(directory, "client/src/reader.ts");
  writeFileSync(goPath, "package fixture; type Server struct { started, closed bool }");
  writeFileSync(tsPath, "function openRead() { let closed = false; let closePromise: Promise<void> | undefined; }");
  writeFileSync(join(directory, "go/ignored_test.go"), "invalid test source");
  writeFileSync(join(directory, "go/generated.go"), "// Code generated by fixture. DO NOT EDIT.\npackage fixture; type Machine struct { started, closed bool }");
  writeFileSync(join(directory, "client/src/ignored.d.ts"), "invalid declaration source");
  const result = run({ repoRoot: directory });
  assert.equal(result.findings.length, 2);
  assert.equal(result.problems.length, 2);
  assert.ok(result.problems.every((problem) => problem.startsWith("new:")));
  writeFileSync(goPath, "package fixture; type Server struct { lifecycle serverLifecycle }");
  writeFileSync(tsPath, 'function openRead() { let state: "open" | "closing" | "closed" = "open"; }');
  assert.deepEqual(run({ repoRoot: directory }).problems, []);
  writeFileSync(goPath, "package fixture; type Broken struct {");
  assert.throws(() => scanRepository(directory), /Command failed/);
});

test("standards and script tests include the new gate", () => {
  const makefile = readFileSync(join(root, "Makefile"), "utf8");
  assert.match(makefile, /^standards:.*\bcheck-state-machines\b/m);
  assert.match(makefile, /check-state-machines:\n\t\$\(NPM_INSTALL\)\n\tnode scripts\/check-state-machines.mjs/);
  assert.match(makefile, /node --test scripts\/\*\.test\.mjs/);
});
