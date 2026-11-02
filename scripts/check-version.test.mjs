import assert from "node:assert/strict";
import { test } from "node:test";

import { optionalLockProblems, versionProblems } from "./check-version.mjs";

// The version gate exists because a skew publishes a tarball or a wheel whose
// binary is from a different release than the code that resolves it. Every version
// it compares is found by a regex over a source file or read from a JSON field,
// so the failure this covers is a pattern that quietly stops matching.

const all = (version) => ({
  goVersion: version,
  packageVersion: version,
  pythonProjectVersion: version,
  pythonInitVersion: version,
});

test("four agreeing versions pass", () => {
  assert.deepEqual(versionProblems(all("0.2.0")), []);
});

// The case the gate has to survive: the Go constant is renamed or reformatted and
// the regex no longer matches. A gate that treats "could not read it" as "it agrees"
// is worse than no gate, because the skew it exists to catch ships silently.
test("an unreadable Go version is a failure, not agreement", () => {
  const problems = versionProblems({ ...all("0.2.0"), goVersion: undefined });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /declares no version/);
});

test("an unreadable pyproject version is a failure", () => {
  const problems = versionProblems({ ...all("0.2.0"), pythonProjectVersion: undefined });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /no top-level version/);
});

// A missing __version__ in __init__.py is the same story, and it is reported
// against the pyproject version it disagrees with rather than silently skipped.
test("a missing __version__ is named as missing", () => {
  const problems = versionProblems({ ...all("0.2.0"), pythonInitVersion: undefined });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /__version__=missing does not match pyproject version=0\.2\.0/);
});

test("an npm skew is reported", () => {
  const problems = versionProblems({ ...all("0.2.0"), packageVersion: "0.1.0" });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /internal\/version=0\.2\.0, packages\/stow-s3=0\.1\.0/);
});

test("a PyPI skew is reported", () => {
  const problems = versionProblems({ ...all("0.2.0"), pythonProjectVersion: "0.1.0" });
  // The pyproject skew against Go, and the __version__ that still says 0.2.0.
  assert.equal(problems.length, 2);
  assert.ok(problems.some((p) => /packages\/stow-s3-py=0\.1\.0/.test(p)));
  assert.ok(problems.some((p) => /does not match pyproject/.test(p)));
});

// Every disagreement at once, because a release that bumps nothing and everything
// is the case where a report of only the first would waste a round trip.
test("every disagreement is reported, not just the first", () => {
  const problems = versionProblems({
    goVersion: "0.2.0",
    packageVersion: "0.1.0",
    pythonProjectVersion: "0.1.0",
    pythonInitVersion: "0.0.9",
  });
  assert.equal(problems.length, 3);
});

// Two empty versions are not two "missing" reports, and neither is agreement.
test("nothing found anywhere is still a failure", () => {
  const problems = versionProblems({
    goVersion: undefined,
    packageVersion: undefined,
    pythonProjectVersion: undefined,
    pythonInitVersion: undefined,
  });
  assert.ok(problems.length >= 2);
  assert.ok(problems.every((p) => !/mismatch.*undefined.*undefined/.test(p)));
});

const nativeOptional = {
  "@chester-hill-solutions/stow-s3-darwin-arm64": "0.3.5",
  "@chester-hill-solutions/stow-s3-darwin-x64": "0.3.5",
  "@chester-hill-solutions/stow-s3-linux-arm64": "0.3.5",
  "@chester-hill-solutions/stow-s3-linux-x64": "0.3.5",
};
const optionalLock = () => ({ packages: {
  "": { optionalDependencies: { ...nativeOptional } },
  ...Object.fromEntries(Object.keys(nativeOptional).map((name) => [`node_modules/${name}`, { optional: true }])),
} });

test("unpublished optional carriers retain lock records without claiming registry artifacts exist", () => {
  assert.deepEqual(optionalLockProblems(nativeOptional, optionalLock()), []);
  const lock = optionalLock();
  for (const record of Object.values(lock.packages)) if (record.optional) record.version = "0.3.5";
  assert.deepEqual(optionalLockProblems(nativeOptional, lock), []);
});

test("each dropped native carrier lock record is rejected", () => {
  for (const name of Object.keys(nativeOptional)) {
    const lock = optionalLock();
    delete lock.packages[`node_modules/${name}`];
    assert.ok(optionalLockProblems(nativeOptional, lock).some((problem) => problem.includes(name)));
  }
});

test("nonoptional or wrong-version carrier records are rejected", () => {
  for (const name of Object.keys(nativeOptional)) {
    const lock = optionalLock();
    lock.packages[`node_modules/${name}`] = { optional: false };
    assert.ok(optionalLockProblems(nativeOptional, lock).some((problem) => problem.includes(name)));
    lock.packages[`node_modules/${name}`] = { optional: true, version: "0.3.4" };
    assert.ok(optionalLockProblems(nativeOptional, lock).some((problem) => problem.includes(name)));
  }
});

test("missing or mismatched root optional dependency edges are rejected", () => {
  for (const name of Object.keys(nativeOptional)) {
    const lock = optionalLock();
    delete lock.packages[""].optionalDependencies[name];
    assert.ok(optionalLockProblems(nativeOptional, lock).some((problem) => problem.includes(name)));
  }
});
