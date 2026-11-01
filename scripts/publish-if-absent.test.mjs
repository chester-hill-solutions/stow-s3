import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { appendFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, test } from "node:test";
import { integrityOf, isPublished, main, manifestOf, publishEnvironment, publishPlan, publishTarball, registryResult } from "./publish-if-absent.mjs";
import { PACKAGE_NAMES, REGISTRY } from "./npm-publication.mjs";

const directory = mkdtempSync(join(tmpdir(), "stow-publish-test-"));
after(() => rmSync(directory, { recursive: true, force: true }));
let count = 0;
function fixture(manifest) {
  const stage = join(directory, String(count++)); mkdirSync(join(stage, "package"), { recursive: true });
  writeFileSync(join(stage, "package/package.json"), JSON.stringify(manifest));
  const path = `${stage}.tgz`; execFileSync("tar", ["-czf", path, "-C", stage, "package"]); return path;
}
const manifests = PACKAGE_NAMES.map(name => JSON.parse(readFileSync(new URL(`../packages/${name.split("/")[1]}/package.json`, import.meta.url))));
const tarballs = manifests.map(fixture);
const receipt = (_name, _version, path) => ({ integrity: integrityOf(path) });

test("all five packed identities are verified before a registry lookup", async () => {
  assert.deepEqual(tarballs.map(path => manifestOf(path).name), PACKAGE_NAMES);
  for (const change of [m => { m.publishConfig.registry = "https://npm.pkg.github.com"; }, m => { delete m.publishConfig.access; }, m => { m.repository.url = "wrong"; }]) {
    const manifest = structuredClone(manifests[0]); change(manifest);
    await assert.rejects(() => publishPlan([fixture(manifest)], () => { throw Error("lookup should not run"); }), /must target|public access|must identify/);
  }
});

test("initial, complete and partial retries preserve order and immutable bytes", async () => {
  assert.deepEqual((await publishPlan(tarballs, () => null)).map(s => s.action), Array(5).fill("publish"));
  assert.deepEqual((await publishPlan(tarballs, receipt)).map(s => s.action), Array(5).fill("skip"));
  const plan = await publishPlan([...tarballs].reverse(), (name, version, path) => name === PACKAGE_NAMES[2] ? null : receipt(name, version, path));
  assert.deepEqual(plan.map(s => s.tarball), [...tarballs].reverse());
  assert.deepEqual(plan.filter(s => s.action === "publish").map(s => s.name), [PACKAGE_NAMES[2]]);
  await assert.rejects(() => publishPlan(tarballs, () => ({ integrity: integrityOf(tarballs[1]) })), /immutable version conflict/);
  await assert.rejects(() => publishPlan([tarballs[0], tarballs[0]], () => null), /duplicate package/);
});

test("only HTTP 404 is exact-version absence; read failures cannot authorize a write", async () => {
  assert.equal(registryResult({ status: 404 }), null);
  for (const status of [401, 403, 429, 500, 503, 302]) assert.throws(() => registryResult({ status }), /lookup failed/);
  for (const document of [{}, { name: PACKAGE_NAMES[0], version: "wrong", dist: { integrity: integrityOf(tarballs[0]) } }]) {
    assert.throws(() => registryResult({ status: 200, document }, PACKAGE_NAMES[0], "0.3.4"), /invalid identity/);
  }
  await assert.rejects(() => isPublished(PACKAGE_NAMES[0], "0.3.4", null, async () => { throw Error("ETIMEDOUT"); }), /ETIMEDOUT/);
  await assert.rejects(() => isPublished(PACKAGE_NAMES[0], "0.3.4", null, async () => ({ status: 200, json: async () => { throw Error("invalid JSON"); } })), /invalid JSON/);
  let writes = 0;
  await assert.rejects(() => main(["node", "script", ...tarballs], { authorize: () => {}, lookup: () => { throw Error("offline"); }, publish: () => { writes++; } }), /offline/);
  assert.equal(writes, 0);
});

test("version preflight uses anonymous public-registry metadata with exact artifact identity", async () => {
  const result = await isPublished(PACKAGE_NAMES[0], "0.3.4", null, async (url, options) => {
    assert.equal(url, `${REGISTRY}/%40chester-hill-solutions%2Fstow-s3/0.3.4`);
    assert.equal(options.credentials, "omit"); assert.equal(options.redirect, "manual");
    assert.deepEqual(options.headers, { accept: "application/json" });
    return { status: 200, json: async () => ({ name: PACKAGE_NAMES[0], version: "0.3.4", dist: receipt(null, null, tarballs[0]) }) };
  });
  assert.equal(result.integrity, integrityOf(tarballs[0]));
});

test("preflight and script dry-run never call publish, stage or authorization", async () => {
  for (const flag of ["--check", "--dry-run"]) {
    assert.equal(await main(["node", "script", flag, ...tarballs], { lookup: () => null, log: () => {},
      publish: () => { throw Error("must not publish"); }, authorize: () => { throw Error("must not authorize"); } }), 0);
  }
  await assert.rejects(() => main(["node", "script", tarballs[0]]), /disabled by release policy/);
});

test("a mocked publish confirms the exact receipt and a rerun does no write", async () => {
  const landed = new Map(); let writes = 0;
  const options = { authorize: () => {}, log: () => {}, lookup: name => landed.get(name) ?? null,
    publish: step => { writes++; landed.set(step.name, { integrity: step.integrity }); } };
  assert.equal(await main(["node", "script", ...tarballs], options), 0); assert.equal(writes, 5);
  assert.equal(await main(["node", "script", ...tarballs], options), 0); assert.equal(writes, 5);
  await assert.rejects(() => main(["node", "script", tarballs[0]], { ...options, lookup: () => null }), /integrity not confirmed/);
});

test("future npm writes use a private verified copy and empty auth configuration", async () => {
  const [step] = await publishPlan([tarballs[0]], () => null);
  let calls = 0;
  publishTarball(step, (command, args, options) => {
    calls++; assert.equal(command, "npm"); assert.equal(args[0], "publish");
    assert.ok(args.includes("--access=public")); assert.ok(args.includes(`--registry=${REGISTRY}`));
    assert.notEqual(args.at(-1), step.tarball); assert.equal(integrityOf(args.at(-1)), step.integrity);
    for (const prefix of ["--userconfig=", "--globalconfig="]) assert.equal(readFileSync(args.find(a => a.startsWith(prefix)).slice(prefix.length), "utf8"), "");
    assert.deepEqual(options.env, { GITHUB_ACTIONS: "true" });
  }, { NODE_AUTH_TOKEN: "fixture", NPM_TOKEN: "fixture", npm_config_registry: "wrong", GITHUB_ACTIONS: "true" });
  assert.equal(calls, 1);
  assert.deepEqual(publishEnvironment({ NPM_CONFIG_USERCONFIG: "wrong", PATH: "bin" }), { PATH: "bin" });
  const changed = fixture(manifests[0]), [snapshot] = await publishPlan([changed], () => null);
  appendFileSync(changed, "changed");
  assert.throws(() => publishTarball(snapshot, () => { throw Error("must not run npm"); }), /artifact changed/);
});
