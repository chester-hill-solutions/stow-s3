import assert from "node:assert/strict";
import { test } from "node:test";

import { checkInstallable, classify, fetchUnauthenticated, packageNames, shouldBlock } from "./check-publish-visibility.mjs";

// The failure this exists for was a release that published five private packages and
// reported success. So the cases that matter are the ones that would have let that
// through: a 200 for the wrapper while a platform package is private, an ambiguous 404
// treated as fine, and a private package reported as merely unverified.

test("a public package is installable", () => {
  assert.equal(classify(200).verdict, "installable");
});

test("a package the registry demands credentials for exists and is private", () => {
  for (const status of [401, 403]) {
    assert.equal(classify(status).verdict, "private", `HTTP ${status}`);
  }
});

test("404 is not reported as either published or absent", () => {
  // On GitHub Packages an unauthenticated 404 covers both "never published" and
  // "private". Calling it absent would green-light a private package; calling it
  // private would block a first release. The only claim the evidence supports is that
  // it cannot be told apart.
  assert.equal(classify(404).verdict, "unknown");
});

test("an uninterpreted status is unknown rather than a pass", () => {
  assert.equal(classify(502).verdict, "unknown");
  assert.equal(classify(500).verdict, "unknown");
});

test("the platform packages are checked, not just the wrapper", () => {
  // A scope that is public at the top and private per platform still fails to install
  // on some machines, and checking only the wrapper would pass that.
  const names = packageNames({
    name: "@chester-hill-solutions/stow-s3",
    version: "0.3.0",
    optionalDependencies: {
      "@chester-hill-solutions/stow-s3-linux-x64": "0.3.0",
      "@chester-hill-solutions/stow-s3-darwin-arm64": "0.3.0",
      "@chester-hill-solutions/stow-s3-unrelated": "1.2.3",
    },
  });
  assert.deepEqual(names, [
    "@chester-hill-solutions/stow-s3",
    "@chester-hill-solutions/stow-s3-linux-x64",
    "@chester-hill-solutions/stow-s3-darwin-arm64",
  ]);
});

test("a private platform package fails even when the wrapper is public", async () => {
  // The exact shape of the 0.2.0 release: the wrapper answered 200 to a token-bearing
  // request while the registry served nobody else.
  const statuses = new Map([
    ["@chester-hill-solutions/stow-s3", 200],
    ["@chester-hill-solutions/stow-s3-linux-x64", 401],
  ]);
  const { problems } = await checkInstallable([...statuses.keys()], async (name) => statuses.get(name));
  assert.equal(problems.length, 1, JSON.stringify(problems));
  assert.match(problems[0], /stow-s3-linux-x64/);
  assert.match(problems[0], /exists and is private/);
});

test("everything public reports no problems", async () => {
  const { checked, problems } = await checkInstallable(["a", "b"], async () => 200);
  assert.deepEqual(problems, []);
  assert.deepEqual(checked.map((entry) => entry.verdict), ["installable", "installable"]);
});

test("an unknown status is recorded per package and is not a problem", async () => {
  // A first release has nothing published, and blocking there would make a first
  // release impossible on the grounds that nothing exists to check.
  const { problems, checked } = await checkInstallable(["a"], async () => 404);
  assert.deepEqual(problems, []);
  assert.equal(checked[0].verdict, "unknown");
});

test("a network failure propagates rather than reading as unknown", async () => {
  // Downgrading a failed request to "unknown" would let the release proceed with the
  // check silently not running, which is the blind-gate problem this repository keeps
  // paying for. A request that did not happen must stop the check.
  let calls = 0;
  await assert.rejects(
    () => checkInstallable(["a"], async () => {
      calls += 1;
      throw new Error("getaddrinfo ENOTFOUND npm.pkg.github.com");
    }),
    /ENOTFOUND/,
  );
  assert.equal(calls, 1);
});

test("one failed request stops the check instead of half-answering it", async () => {
  // Otherwise the first three packages get a verdict and the fourth has none, and the
  // caller cannot tell a partial answer from a complete one.
  const seen = [];
  await assert.rejects(() => checkInstallable(["a", "b", "c"], async (name) => {
    seen.push(name);
    if (name === "c") throw new Error("socket hang up");
    return 200;
  }));
  assert.deepEqual(seen, ["a", "b", "c"]);
});

// A 401 blocks; an ambiguous 404 does not. Conflating the two is what a deliberate
// break did, and with the decision in the CLI block nothing caught it: the check
// exited 0 with every package private.
test("a private package blocks and an ambiguous 404 does not", () => {
  assert.equal(shouldBlock([{ name: "a", status: 401, verdict: "private" }]), true);
  assert.equal(shouldBlock([{ name: "a", status: 404, verdict: "unknown" }]), false);
  // One private among several is enough, whatever else was found.
  assert.equal(
    shouldBlock([
      { name: "a", status: 200, verdict: "installable" },
      { name: "b", status: 404, verdict: "unknown" },
      { name: "c", status: 401, verdict: "private" },
    ]),
    true,
  );
  assert.equal(shouldBlock([]), false);
});

// The test that cannot be done with a live break: a bogus token also gets a 401, so a
// live request cannot tell "sent no credentials" from "sent bad ones". This asserts the
// property directly instead of inferring it from a status code.
test("the request carries no credentials", async () => {
  let seen;
  const original = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    seen = { url, headers: init?.headers ?? {} };
    return { status: 200 };
  };
  try {
    await fetchUnauthenticated("@chester-hill-solutions/stow-s3", "https://registry.example");
  } finally {
    globalThis.fetch = original;
  }
  const headerNames = Object.keys(seen.headers).map((name) => name.toLowerCase());
  assert.ok(!headerNames.includes("authorization"), `request sent ${headerNames.join(", ")}`);
  assert.ok(!headerNames.includes("cookie"), `request sent ${headerNames.join(", ")}`);
  assert.match(seen.url, /registry\.example\/%40chester-hill-solutions%2Fstow-s3$/);
});
