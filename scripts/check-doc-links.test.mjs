import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { localLinks, linkProblems } from "./check-doc-links.mjs";

test("documentation links exclude examples, external destinations and same-page anchors", () => {
  const markdown = '[Guide](guide.md#read) [Reference][ref]\n[ref]: ../README.md\n[Web](https://example.com) [Section](#here)\n```md\n[Example](not-a-file.md)\n```';
  assert.deepEqual(localLinks(markdown), ["guide.md#read", "../README.md"]);
});

test("moving a guide breaks its old local link until the reference is repaired", (t) => {
  const root = mkdtempSync(join(tmpdir(), "stow-doc-links-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "docs/history"), { recursive: true });
  writeFileSync(join(root, "docs/history/old.md"), "# History");
  assert.equal(linkProblems("[Old](old.md)", "docs/README.md", root).length, 1);
  assert.deepEqual(linkProblems("[Old](history/old.md)", "docs/README.md", root), []);
});

test("machine-specific paths and references outside the repository fail", () => {
  assert.match(linkProblems("[Source](/Users/me/source.go)", "docs/guide.md")[0], /machine-specific/);
  assert.match(linkProblems("[Outside](../../outside.md)", "docs/guide.md")[0], /leaves the repository/);
});

test("site documentation routes validate their sources without a site build", (t) => {
  const root = mkdtempSync(join(tmpdir(), "stow-doc-routes-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "docs"), { recursive: true });
  writeFileSync(join(root, "docs/browser-file-history.md"), "# Browser history");
  assert.deepEqual(linkProblems("[History](/docs/browser-file-history/#sync)", "site/agent.md", root), []);
  assert.equal(linkProblems("[Typo](/docs/browser-file-historie/)", "site/agent.md", root).length, 1);
});

test("a stale built page cannot hide a missing route source", (t) => {
  const root = mkdtempSync(join(tmpdir(), "stow-doc-stale-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "site/.build/docs/browser-file-history"), { recursive: true });
  writeFileSync(join(root, "site/.build/docs/browser-file-history/index.html"), "Stale output");
  assert.match(linkProblems("[History](/docs/browser-file-history/)", "site/agent.md", root)[0], /missing local target/);
});

test("static website assets still resolve without a generated route", (t) => {
  const root = mkdtempSync(join(tmpdir(), "stow-doc-assets-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "site/assets"), { recursive: true });
  writeFileSync(join(root, "site/assets/icon.svg"), "<svg/>");
  assert.deepEqual(linkProblems("[Icon](/assets/icon.svg)", "site/agent.md", root), []);
  assert.equal(linkProblems("[Missing](/assets/missing.svg)", "site/agent.md", root).length, 1);
});
