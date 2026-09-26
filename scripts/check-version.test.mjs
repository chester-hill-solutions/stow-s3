import assert from "node:assert/strict";
import { test } from "node:test";

import { versionProblems } from "./check-version.mjs";

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
