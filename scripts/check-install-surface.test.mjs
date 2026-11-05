import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { test } from "node:test";

import { docProblems, publishRegistry, reachabilityProblems, SURFACE } from "./check-install-surface.mjs";

// A surface where everything is published. This is the state the repository is
// heading toward and the one the gate had never been asked about.
const ALL_PUBLISHED = SURFACE.map((entry) => ({ ...entry, published: true }));

// A surface where nothing is published.
const NONE_PUBLISHED = SURFACE.map((entry) => ({ ...entry, published: false }));
const PARTLY_PUBLISHED = SURFACE.map((entry) => ({ ...entry, published: entry.ecosystem === "go" }));

const goEntry = SURFACE.find((entry) => entry.ecosystem === "go");

function docFor(surface, { disclaimer }) {
  const lines = surface.map((entry) => `Install with: ${entry.command}`);
  if (disclaimer) lines.push("The npm and PyPI packages are not published yet.");
  return lines.join("\n");
}

test("a doc with no disclaimer fails while something is unpublished", () => {
  const problems = docProblems(PARTLY_PUBLISHED, "README.md", docFor(PARTLY_PUBLISHED, { disclaimer: false }));
  assert.ok(
    problems.some((p) => p.includes("is unpublished")),
    `expected a disclaimer problem, got ${JSON.stringify(problems)}`,
  );
});

test("a doc with a disclaimer passes while something is unpublished", () => {
  const problems = docProblems(PARTLY_PUBLISHED, "README.md", docFor(PARTLY_PUBLISHED, { disclaimer: true }));
  assert.deepEqual(problems, [], `expected no problems, got ${JSON.stringify(problems)}`);
});

// The regression. Once every target is published there is nothing to warn a
// reader about, so a doc that simply states the install commands is correct and
// complete. The rule used to demand a disclaimer regardless, which meant the
// post-publish flip would have forced a false "not published" into four
// documents, and the branch that could see it never ran because two targets
// were still unpublished.
test("once everything is published, no disclaimer is owed", () => {
  const problems = docProblems(
    ALL_PUBLISHED,
    "README.md",
    docFor(ALL_PUBLISHED, { disclaimer: false }),
  );
  assert.deepEqual(
    problems,
    [],
    `a fully published surface must not demand a disclaimer, got ${JSON.stringify(problems)}`,
  );
});

test("once everything is published, a disclaimer is not forbidden either", () => {
  const problems = docProblems(
    ALL_PUBLISHED,
    "README.md",
    docFor(ALL_PUBLISHED, { disclaimer: true }),
  );
  assert.deepEqual(problems, [], `got ${JSON.stringify(problems)}`);
});

test("an unpublished target must still be named as unpublished when nothing else is", () => {
  // The disclaimer is owed for every unpublished target.
  const problems = docProblems(NONE_PUBLISHED, "README.md", docFor(NONE_PUBLISHED, { disclaimer: false }));
  assert.ok(
    problems.length >= 2,
    `expected one problem per unpublished target, got ${JSON.stringify(problems)}`,
  );
});

test("a published target that no doc mentions is a problem", () => {
  // The fixture's published Go module must be named in the document.
  const text = "The npm package is not published yet.";
  const problems = docProblems(PARTLY_PUBLISHED, "README.md", text);
  assert.ok(
    problems.some((p) => p.includes("does not mention the published")),
    `expected a missing-mention problem, got ${JSON.stringify(problems)}`,
  );
});

test("a stale repository reference is a problem in every mode", () => {
  for (const surface of [ALL_PUBLISHED, NONE_PUBLISHED]) {
    const problems = docProblems(
      surface,
      "README.md",
      `See https://github.com/chester-hill-solutions/stow for details. The npm package is not published yet.`,
    );
    assert.ok(
      problems.some((p) => p.includes("but the Go module lives at")),
      `expected a stale-repo problem, got ${JSON.stringify(problems)}`,
    );
  }
});

// The module path and the repository URL are the same string. They diverged
// once, when the module was renamed and the remote was not.
test("a doc may reference the module path as a repository URL", () => {
  const repo = goEntry.name.split("/pkg/")[0];
  const problems = docProblems(
    ALL_PUBLISHED,
    "README.md",
    `${docFor(ALL_PUBLISHED, { disclaimer: false })}\nSource: ${repo}`,
  );
  assert.deepEqual(problems, [], `got ${JSON.stringify(problems)}`);
});

// The reachability comparison is the half of the gate that decides whether a package
// is installable, and the mode that matters most is the one nobody has run: declared
// published, not yet public. That is a half-finished admin action, and the failure
// has to say so — otherwise it reads as the repository disagreeing with itself, and
// the next thing somebody does is flip the flag back.
test("a declared-published package that answers 401 is told what to do about it", () => {
  const npm = SURFACE.find((entry) => entry.ecosystem === "npm");
  const problems = reachabilityProblems({ ...npm, published: true }, false, "HTTP 401");
  assert.equal(problems.length, 2, "the mismatch and the remedy are both owed");
  assert.match(problems[1], /401/);
  assert.match(problems[1], /public npmjs/);
  assert.match(problems[1], /before claiming/);
});

test("a 401 remedy is not offered for a registry that does not use one", () => {
  const pypi = SURFACE.find((entry) => entry.ecosystem === "pypi");
  const problems = reachabilityProblems({ ...pypi, published: true }, false, "HTTP 401");
  assert.equal(problems.length, 1, "only the mismatch: a PyPI 401 means something else entirely");
});

test("confirmed absence and confirmed availability can match the declaration", () => {
  const npm = { ...SURFACE.find((entry) => entry.ecosystem === "npm"), published: false };
  assert.deepEqual(reachabilityProblems(npm, false, "HTTP 404"), []);
  assert.deepEqual(reachabilityProblems({ ...npm, published: true }, true, "HTTP 200"), []);
});

test("unpublished npm declarations cannot turn denied or failed reads into absence", () => {
  const npm = { ...SURFACE.find(entry => entry.ecosystem === "npm"), published: false };
  for (const detail of ["HTTP 401", "HTTP 403", "HTTP 429", "HTTP 500", "HTTP 503", "ETIMEDOUT"]) {
    assert.match(reachabilityProblems(npm, false, detail)[0], /not evidence of absence/);
  }
});

test("a genuinely reachable package declared unpublished is still a problem", () => {
  // The other direction: something is installable that this file says is not, which
  // is the state that makes a document's "not published" prose wrong.
  const npm = { ...SURFACE.find((entry) => entry.ecosystem === "npm"), published: false };
  const problems = reachabilityProblems(npm, true, "HTTP 200");
  assert.equal(problems.length, 1);
  assert.match(problems[0], /reachable \(HTTP 200\)/);
  assert.match(problems[0], /published=false/);
});

// Availability is still a separately maintained claim, checked on the publish registry.
test("the online check looks where the release publishes", () => {
  const registry = publishRegistry();
  const manifest = JSON.parse(readFileSync(join(resolve(import.meta.dirname, ".."), "packages/stow-s3/package.json"), "utf8"));
  assert.equal(registry, manifest.publishConfig.registry);
  assert.equal(registry, "https://registry.npmjs.org");
});
