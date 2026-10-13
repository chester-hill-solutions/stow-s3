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
