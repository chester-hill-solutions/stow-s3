import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { checkInstallable, classify, fetchUnauthenticated, packageNames, shouldBlock } from "./check-publish-visibility.mjs";
import { PACKAGE_NAMES, REGISTRY } from "./npm-publication.mjs";
const metadata = name => ({ status: 200, document: { name, versions: { "0.3.4": { version: "0.3.4" } } } });

test("only positive, correctly named anonymous metadata passes", async () => {
  const { checked, problems } = await checkInstallable(PACKAGE_NAMES, async name => metadata(name));
  assert.equal(checked.length, 5); assert.deepEqual(problems, []); assert.equal(shouldBlock(checked), false);
  for (const document of [undefined, {}, { name: "wrong", versions: { v: {} } }, { name: "a", versions: {} }]) {
    assert.equal(classify(200, document, "a").verdict, "unknown");
  }
});

test("absent, denied, unavailable and unknown are distinct and all block", async () => {
  for (const [status, verdict] of [[401, "denied"], [403, "denied"], [404, "absent"], [429, "unavailable"], [500, "unavailable"], [503, "unavailable"], [302, "unknown"]]) {
    assert.equal(classify(status).verdict, verdict);
    const { checked, problems } = await checkInstallable(["a"], async () => ({ status }));
    assert.equal(shouldBlock(checked), true); assert.equal(problems.length, 1);
    assert.doesNotMatch(problems[0], /exists and is private/);
  }
  assert.equal(shouldBlock([]), true);
});

test("one nonpublic native package blocks the whole five-package check", async () => {
  const { checked } = await checkInstallable(PACKAGE_NAMES, async name => name === PACKAGE_NAMES[4] ? { status: 404 } : metadata(name));
  assert.equal(shouldBlock(checked), true);
});

test("transport errors and malformed JSON cannot become safe absence", async () => {
  await assert.rejects(() => checkInstallable(PACKAGE_NAMES, async () => { throw new Error("ENOTFOUND"); }), /ENOTFOUND/);
  await assert.rejects(() => fetchUnauthenticated("a", REGISTRY, async () => ({ status: 200, json: async () => { throw new Error("invalid JSON"); } })), /invalid JSON/);
});

test("all same-version native packages must be present in the source declaration", () => {
  const manifest = JSON.parse(readFileSync(new URL("../packages/stow-s3/package.json", import.meta.url)));
  assert.deepEqual(packageNames(manifest), PACKAGE_NAMES);
  delete manifest.optionalDependencies[PACKAGE_NAMES[1]];
  assert.throws(() => packageNames(manifest), /all four/);
});

test("public requests have no auth, cookies or redirect following, and no registry override", async () => {
  let seen;
  await fetchUnauthenticated(PACKAGE_NAMES[0], REGISTRY, async (url, options) => { seen = { url, options }; return { status: 200, json: async () => metadata(PACKAGE_NAMES[0]).document }; });
  assert.equal(seen.url, `${REGISTRY}/%40chester-hill-solutions%2Fstow-s3`);
  assert.deepEqual(seen.options.headers, { accept: "application/json" });
  assert.equal(seen.options.credentials, "omit"); assert.equal(seen.options.redirect, "manual");
  await assert.rejects(() => fetchUnauthenticated("a", "https://npm.pkg.github.com"), /must target/);
});
