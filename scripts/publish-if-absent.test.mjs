import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

// The re-runnability claim, tested without a registry.
//
// publish-if-absent.mjs exists because a release is several independent writes and
// any of them can fail after an earlier one has already landed. Re-running the
// whole release then fails on the packages that succeeded, which turns one
// transient error into a version that can never be completed. These cases pin the
// plan that makes the second run publish only what is missing.

// The registry lookup is injected, so no case here contacts npm.
import { integrityOf, registryResult } from "./publish-if-absent.mjs";
const nothingPublished = () => null;
const everythingPublished = (_name, _version, tarball) => ({integrity:integrityOf(tarball)});
const publishedSet = (names) => (name, version, tarball) => names.has(name) ? everythingPublished(name,version,tarball) : null;

// A tarball is a real gzipped tar in production, so these fixtures build one with
// tar rather than faking the reader. The script's own manifestOf is what reads it,
// which means these cases also cover that.
const TARBALLS = [
  ["@chester-hill-solutions/stow-s3-linux-x64", "0.2.0"],
  ["@chester-hill-solutions/stow-s3-linux-arm64", "0.2.0"],
  ["@chester-hill-solutions/stow-s3-darwin-x64", "0.2.0"],
];

let fixtures = null;

function tarballs() {
  if (fixtures !== null) {
    return fixtures;
  }
  const dir = mkdtempSync(join(tmpdir(), "publish-plan-"));
  fixtures = TARBALLS.map(([name, version]) => {
    const stage = join(dir, `${name.replace(/[^a-z0-9]/gi, "-")}-${version}`);
    mkdirSync(join(stage, "package"), { recursive: true });
    writeFileSync(
      join(stage, "package", "package.json"),
      JSON.stringify({ name, version, publishConfig: {registry:"https://registry.npmjs.org",access:"public"} }),
    );
    const tarball = `${stage}.tgz`;
    execFileSync("tar", ["-czf", tarball, "-C", stage, "package"]);
    return tarball;
  });
  return fixtures;
}

test("a first run publishes everything", async () => {
  const { publishPlan } = await import("./publish-if-absent.mjs");
  const plan = publishPlan(tarballs(), nothingPublished);
  assert.equal(plan.length, 3);
  assert.deepEqual(
    plan.map((step) => step.action),
    ["publish", "publish", "publish"],
  );
});

// The property the script exists for: a second run publishes nothing, rather than
// failing on the packages the first run already landed.
test("a second run after a complete first run publishes nothing", async () => {
  const { publishPlan } = await import("./publish-if-absent.mjs");
  const plan = publishPlan(tarballs(), everythingPublished);
  assert.deepEqual(
    plan.map((step) => step.action),
    ["skip", "skip", "skip"],
  );
});

// The realistic failure: the third package failed and the first two landed. The
// retry must publish exactly the third.
test("a partial release retries only what is missing", async () => {
  const { publishPlan } = await import("./publish-if-absent.mjs");
  const landed = new Set([TARBALLS[0][0], TARBALLS[1][0]]);
  const plan = publishPlan(tarballs(), publishedSet(landed));
  assert.deepEqual(
    plan.map((step) => step.action),
    ["skip", "skip", "publish"],
  );
  const retry = plan.find((step) => step.action === "publish");
  assert.equal(retry.name, TARBALLS[2][0]);
});

// A name and version have to come out of the tarball, or the registry lookup
// cannot be made and the publish is aimed at the wrong thing.
test("the plan carries the name and version read from each tarball", async () => {
  const { publishPlan } = await import("./publish-if-absent.mjs");
  const plan = publishPlan(tarballs(), nothingPublished);
  assert.deepEqual(
    plan.map((step) => `${step.name}@${step.version}`),
    TARBALLS.map(([name, version]) => `${name}@${version}`),
  );
});

// Order is preserved, because the platform packages are published in the order
// the release built them and a reordering would change what a failure interrupts.
test("the plan preserves the order it was given", async () => {
  const { publishPlan } = await import("./publish-if-absent.mjs");
  const reversed = [...tarballs()].reverse();
  const plan = publishPlan(reversed, nothingPublished);
  assert.deepEqual(
    plan.map((step) => step.tarball),
    reversed,
  );
});

 test("only an explicit E404 means absent", () => {
   assert.equal(registryResult({status:1,stdout:JSON.stringify({error:{code:"E404"}})}),null);
   for (const code of ["E401","E403","E429","E500","ETIMEDOUT"]) {
     assert.throws(()=>registryResult({status:1,stdout:JSON.stringify({error:{code}})}),new RegExp(code));
   }
 });
 test("rerun refuses different bytes under the same version",async()=>{
   const {publishPlan}=await import("./publish-if-absent.mjs");
   assert.throws(()=>publishPlan(tarballs(),()=>({integrity:"sha512-different"})),/immutable version conflict/);
 });
