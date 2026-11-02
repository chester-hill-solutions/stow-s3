import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { assertPublisherContext, assertReadiness, manifestProblems, PACKAGE_NAMES, readReadiness, REGISTRY, REPOSITORY, WORKFLOW } from "./npm-publication.mjs";

const now = Date.parse("2026-11-01T12:00:00Z");
function ready() {
  return { registry: REGISTRY, repository: REPOSITORY, workflow: WORKFLOW, packages: Object.fromEntries(PACKAGE_NAMES.map(name => [name, {
    publicPackageVerified: true, trustedPublisherVerified: true, allowPublishVerified: true,
    trustCreatedAt: "2026-11-01T00:00:00Z", oidcPublishVerified: false,
  }])) };
}
const env = { GITHUB_ACTIONS: "true", RUNNER_ENVIRONMENT: "github-hosted", GITHUB_REPOSITORY: REPOSITORY,
  GITHUB_EVENT_NAME: "push", GITHUB_REF: "refs/tags/v0.3.4", GITHUB_WORKFLOW_REF: `${REPOSITORY}/.github/workflows/release.yml@refs/tags/v0.3.4`,
  ACTIONS_ID_TOKEN_REQUEST_URL: "https://example.invalid/oidc", ACTIONS_ID_TOKEN_REQUEST_TOKEN: "fixture-only" };

test("all five source and packed manifests identify npmjs, public access and the exact repository", () => {
  for (const name of PACKAGE_NAMES) {
    const path = new URL(`../packages/${name.split("/")[1]}/package.json`, import.meta.url);
    const manifest = JSON.parse(readFileSync(path));
    assert.deepEqual(manifestProblems(manifest), []);
    for (const change of [m => { m.publishConfig.registry = "https://npm.pkg.github.com"; }, m => { delete m.publishConfig.access; },
      m => { m.publishConfig.access = "restricted"; }, m => { m.repository.url = "git+https://github.com/other/repo.git"; }]) {
      const bad = structuredClone(manifest); change(bad); assert.ok(manifestProblems(bad).length);
    }
  }
});

test("default readiness and policy remain disabled, without treating account ownership as proof", () => {
  assert.throws(() => assertReadiness(readReadiness(), now), /not ready/);
  const result = spawnSync(process.execPath, ["scripts/check-npm-publish-readiness.mjs"], { encoding: "utf8" });
  assert.notEqual(result.status, 0); assert.match(result.stderr, /not confirmed/);
  assert.equal(JSON.parse(readFileSync(new URL("./release-policy.json", import.meta.url))).publishPackages, false);
});

test("every package needs explicit public, trust and direct-publish readiness", () => {
  assert.doesNotThrow(() => assertReadiness(ready(), now));
  for (const name of PACKAGE_NAMES) for (const flag of ["publicPackageVerified", "trustedPublisherVerified", "allowPublishVerified"]) {
    const record = ready(); record.packages[name][flag] = false;
    assert.throws(() => assertReadiness(record, now), /not confirmed/);
  }
  for (const field of ["registry", "repository", "workflow"]) {
    const record = ready(); record[field] = "wrong"; assert.throws(() => assertReadiness(record, now), /exact repository/);
  }
  const missing = ready(); delete missing.packages[PACKAGE_NAMES[0]]; assert.throws(() => assertReadiness(missing, now), /exactly the five/);
});

test("new trust expires after two days until an OIDC publish is verified", () => {
  const record = ready(); record.packages[PACKAGE_NAMES[0]].trustCreatedAt = "2026-10-30T12:00:00Z";
  assert.throws(() => assertReadiness(record, now), /two days/);
  record.packages[PACKAGE_NAMES[0]].oidcPublishVerified = true;
  assert.doesNotThrow(() => assertReadiness(record, now));
});

test("future publication requires supported tools and exact tagged hosted workflow identity", () => {
  assert.doesNotThrow(() => assertPublisherContext(env, "22.14.0", "11.5.1", "0.3.4"));
  for (const [node, npm] of [["22.13.9", "11.5.1"], ["24.0.0", "11.5.0"], ["20.19.0", "12.0.0"]]) {
    assert.throws(() => assertPublisherContext(env, node, npm, "0.3.4"), /requires Node/);
  }
  for (const field of ["GITHUB_ACTIONS", "RUNNER_ENVIRONMENT", "GITHUB_REPOSITORY", "GITHUB_EVENT_NAME", "GITHUB_REF", "GITHUB_WORKFLOW_REF"]) {
    assert.throws(() => assertPublisherContext({ ...env, [field]: "wrong" }, "24.0.0", "11.5.1", "0.3.4"));
  }
  assert.throws(() => assertPublisherContext({ ...env, GITHUB_EVENT_NAME: "workflow_dispatch" }, "24.0.0", "11.5.1", "0.3.4"), /tagged release/);
  assert.throws(() => assertPublisherContext({ ...env, ACTIONS_ID_TOKEN_REQUEST_TOKEN: "" }, "24.0.0", "11.5.1", "0.3.4"), /OIDC capability/);
});
