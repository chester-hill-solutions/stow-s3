import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { appendFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { after, test } from "node:test";
import { artifactReceipt, assertRun, executePlan, firstPlan, PUBLICATION_ORDER, runFirstPlan } from "./npm-first-release.mjs";
import { assertInteractive } from "./bootstrap-first-npm.mjs";
import { PACKAGE_NAMES, readReadiness, readinessProblems } from "./npm-publication.mjs";
import { integrityOf, main as oidcMain, registryResult } from "./publish-if-absent.mjs";

const dir = mkdtempSync(join(tmpdir(), "stow-first-test-"));
after(() => rmSync(dir, { recursive: true, force: true }));
let count = 0;
function pack(manifest) {
  const stage = join(dir, `stage${count++}`); mkdirSync(join(stage, "package"), { recursive: true });
  writeFileSync(join(stage, "package/package.json"), JSON.stringify(manifest));
  const file = `${stage}.tgz`; execFileSync("tar", ["-czf", file, "-C", stage, "package"]); return file;
}
const manifests = PACKAGE_NAMES.map(name => JSON.parse(readFileSync(new URL(`../packages/${name.split("/")[1]}/package.json`, import.meta.url))));
const tarballs = manifests.map(pack), source = "a".repeat(40), run = "123";
const receipt = artifactReceipt(tarballs, source, run);
const quiet = () => {};

test("waiver is exclusively five canonical public npm 0.3.5 identities", () => {
  assert.equal(receipt.version, "0.3.5");
  assert.match(receipt.liveProviderCompatibility, /NOT VERIFIED/);
  for (const mutate of [m => { m.version = "0.3.6"; }, m => { m.version = "0.3.4"; }, m => { m.name = "@else/thing"; }, m => { m.publishConfig.registry = "wrong"; }, m => { m.publishConfig.access = "restricted"; }, m => { m.repository.url = "wrong"; }]) {
    const manifest = structuredClone(manifests[0]); mutate(manifest);
    assert.throws(() => artifactReceipt([pack(manifest), ...tarballs.slice(1)], source, run));
  }
  for (const files of [tarballs.slice(1), [...tarballs, tarballs[0]], [tarballs[0], ...tarballs.slice(0, 4)]]) assert.throws(() => artifactReceipt(files, source, run), /exactly the five/);
});

test("rehearsal association, all hashes and metadata precede any registry lookup", async () => {
  for (const change of [{ source: "b".repeat(40) }, { run: "124" }, { registry: "wrong" }, { version: "0.3.6" }, { repository: "wrong" }]) {
    await assert.rejects(() => firstPlan(dir, { ...receipt, ...change }, source, run, () => { throw Error("must not lookup"); }), /identity mismatch/);
  }
  const bad = structuredClone(receipt); bad.packages[4].integrity = "changed";
  await assert.rejects(() => firstPlan(dir, bad, source, run, () => { throw Error("must not lookup"); }), /changed since rehearsal/);
  bad.packages[4].file = "../outside.tgz";
  await assert.rejects(() => firstPlan(dir, bad, source, run, quiet), /unsafe artifact/);
  const changed = manifests.map(pack), snapshot = artifactReceipt(changed, source, run); appendFileSync(changed[4], "tampered");
  await assert.rejects(() => firstPlan(dir, snapshot, source, run, quiet), /changed since rehearsal/);
});

test("registry absence/error distinctions and partial retries remain fail closed", async () => {
  const landed = new Map(); let writes = 0, consumers = 0;
  const lookup = name => landed.get(name) ?? null;
  const plan = await firstPlan(dir, receipt, source, run, lookup);
  assert.deepEqual(plan.map(step => step.name), PUBLICATION_ORDER);
  const options = { lookup, publish: step => { writes++; landed.set(step.name, { integrity: step.integrity }); }, verifyConsumers: () => { consumers++; } };
  // Simulate a previous partial completion, then retry only identical bytes.
  landed.set(plan[0].name, { integrity: plan[0].integrity });
  await executePlan(await firstPlan(dir, receipt, source, run, lookup), options);
  assert.equal(writes, 4); assert.equal(consumers, 1);
  await executePlan(await firstPlan(dir, receipt, source, run, lookup), options); assert.equal(writes, 4);
  landed.set(plan[0].name, { integrity: integrityOf(tarballs[0]) });
  await assert.rejects(() => firstPlan(dir, receipt, source, run, lookup), /immutable version conflict/);
  for (const status of [401, 403, 429, 500, 503]) await assert.rejects(() => firstPlan(dir, receipt, source, run, () => registryResult({ status })), /lookup failed/);
  await assert.rejects(() => firstPlan(dir, receipt, source, run, () => { throw Error("timeout"); }), /timeout/);
  await assert.rejects(() => executePlan(plan, { ...options, lookup: () => null }), /integrity not confirmed/);
});

test("only exact-source successful push CI and nonpublishing rehearsal qualify", () => {
  for (const workflow of ["ci.yml", "release.yml"]) {
    const record = { head_sha: source, status: "completed", conclusion: "success", path: `.github/workflows/${workflow}`, event: workflow === "ci.yml" ? "push" : "workflow_dispatch" };
    assertRun(record, source, workflow);
    for (const change of [{ head_sha: "b".repeat(40) }, { conclusion: "failure" }, { status: "in_progress" }, { path: "other.yml" }, { event: "pull_request" }]) assert.throws(() => assertRun({ ...record, ...change }, source, workflow));
  }
  assertInteractive({ env: {}, stdinTTY: true, stdoutTTY: true, username: "wra-sol" });
  for (const change of [{ env: { CI: "true" } }, { env: { GITHUB_ACTIONS: "true" } }, { stdinTTY: false }, { stdoutTTY: false }, { username: "other" }]) assert.throws(() => assertInteractive({ env: {}, stdinTTY: true, stdoutTTY: true, username: "wra-sol", ...change }));
});

test("preflight never publishes; first exception cannot enable OIDC or future live/PyPI gates", async () => {
  const publish = () => { throw Error("must not publish"); };
  await runFirstPlan(await firstPlan(dir, receipt, source, run, () => null), "--check", { publish, authorize: publish, verifyConsumers: publish });
  await assert.rejects(() => runFirstPlan([], "--dry-run", { publish, authorize: publish }), /explicit interactive/);
  await assert.rejects(() => runFirstPlan([], "--publish", { publish, authorize: () => { throw Error("not approved"); } }), /not approved/);
  assert.equal(await oidcMain(["node", "script", "--check", ...tarballs], { lookup: () => null, publish, log: quiet }), 0);
  await assert.rejects(() => oidcMain(["node", "script", ...tarballs], { publish }), /disabled by release policy/);
  assert.equal(JSON.parse(readFileSync(new URL("./release-policy.json", import.meta.url))).publishPackages, false);
  assert.ok(readinessProblems(readReadiness()).length >= 15);
  const workflow = readFileSync(new URL("../.github/workflows/release.yml", import.meta.url), "utf8");
  assert.match(workflow, /Require executed provider evidence[\s\S]*check-live-receipts\.mjs/);
  assert.match(workflow, /Publish to PyPI\n\s+if: startsWith\(github.ref, 'refs\/tags\/v'\) && needs.plan.outputs.publish_packages == 'true' && steps.pypi.outputs.status == 'enabled'/);
  assert.ok(!workflow.includes("bootstrap-first-npm.mjs"));
});
