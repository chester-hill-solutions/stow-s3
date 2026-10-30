import assert from "node:assert/strict";
import { test } from "node:test";

import {
  capabilityFieldNames,
  capabilityProblems,
  readinessProtocolProblems,
  versionProblems,
} from "./check-version.mjs";

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

// The readiness version is a second, independent version. The build number and the
// wire protocol move for different reasons, so a build bump must not read as a
// protocol bump. Both rules below are pure for the same reason as versionProblems:
// the failure worth testing is a discovery pattern that stops matching.

const agreeing = { goProtocol: "1", typescriptProtocol: "1", pythonProtocol: "1" };

test("three agreeing readiness protocol versions pass", () => {
  assert.deepEqual(readinessProtocolProblems(agreeing), []);
});

test("an unreadable readiness version is a failure, not agreement", () => {
  const problems = readinessProtocolProblems({ ...agreeing, goProtocol: undefined });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /ready\.go no longer declares ProtocolVersion/);
});

test("an unreadable client readiness version is a failure", () => {
  assert.match(
    readinessProtocolProblems({ ...agreeing, typescriptProtocol: undefined })[0],
    /ready\.ts no longer declares READY_PROTOCOL_VERSION/,
  );
  assert.match(
    readinessProtocolProblems({ ...agreeing, pythonProtocol: undefined })[0],
    /ready\.py no longer declares READY_PROTOCOL_VERSION/,
  );
});

test("a readiness version skew names all three", () => {
  const problems = readinessProtocolProblems({ ...agreeing, typescriptProtocol: "2" });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /Go 1, TypeScript 2, Python 1/);
});

test("capabilityFieldNames reads a declaration block", () => {
  const names = capabilityFieldNames({
    text: 'type Capabilities struct {\n\tA bool `json:"a"`\n\tB int64 `json:"b"`\n}\n',
    marker: "type Capabilities struct {",
    terminators: ["\n}"],
    pattern: /json:"([A-Za-z0-9_]+)"/g,
  });
  assert.deepEqual(names, ["a", "b"]);
});

// The block reader is where the pattern risk lives, so the block boundary has to
// stop at the closing brace rather than running on into the next declaration.
test("capabilityFieldNames stops at the end of the block", () => {
  const names = capabilityFieldNames({
    text: 'type Capabilities struct {\n\tA bool `json:"a"`\n}\ntype Other struct {\n\tC bool `json:"c"`\n}\n',
    marker: "type Capabilities struct {",
    terminators: ["\n}"],
    pattern: /json:"([A-Za-z0-9_]+)"/g,
  });
  assert.deepEqual(names, ["a"]);
});

// A marker that stops matching must return null, not an empty list that the
// comparison below would then read as a set with nothing missing.
test("capabilityFieldNames returns null when the declaration is gone", () => {
  assert.equal(
    capabilityFieldNames({
      text: "nothing here",
      marker: "type Capabilities struct {",
      terminators: ["\n}"],
      pattern: /json:"([A-Za-z0-9_]+)"/g,
    }),
    null,
  );
});

// The convention difference the normalisation exists for: the wire and TypeScript
// are camelCase, the Python dataclass is snake_case, and that is not a disagreement.
test("capability sets agree across naming conventions", () => {
  assert.deepEqual(
    capabilityProblems({
      sets: [
        { language: "Go", names: ["maxBytes", "maxMultipartUploads"] },
        { language: "TypeScript", names: ["maxBytes", "maxMultipartUploads"] },
        { language: "Python", names: ["max_bytes", "max_multipart_uploads"] },
      ],
    }),
    [],
  );
});

// The failure that motivated the gate: a field the writer publishes and a reader
// never declares is a capability no client can report.
test("a capability missing from a reader is reported with both sides named", () => {
  const problems = capabilityProblems({
    sets: [
      { language: "Go", names: ["maxBytes", "maxMultipartUploads"] },
      { language: "TypeScript", names: ["maxBytes"] },
      { language: "Python", names: ["max_bytes", "max_multipart_uploads"] },
    ],
  });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /declared in Go, Python but not in TypeScript/);
});

// The same failure in the other direction: a field a reader declares and the
// writer never sends is one the reader cannot rely on.
test("a capability the writer does not publish is reported", () => {
  const problems = capabilityProblems({
    sets: [
      { language: "Go", names: ["maxBytes"] },
      { language: "TypeScript", names: ["maxBytes", "imaginary"] },
      { language: "Python", names: ["max_bytes"] },
    ],
  });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /imaginary is declared in TypeScript but not in Go, Python/);
});

// A declaration that could not be found must fail loudly. Reporting a field per
// field against an empty set would bury the cause in noise.
test("an undiscovered capability set fails rather than comparing as empty", () => {
  const problems = capabilityProblems({
    sets: [
      { language: "Go", names: null },
      { language: "TypeScript", names: ["maxBytes"] },
      { language: "Python", names: ["max_bytes"] },
    ],
  });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /no longer declared in Go/);
});
