import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { generateReferences } from "./generate-api-reference.mjs";

const references = generateReferences();

test("reference preserves nullable results and exposes public methods without implementation bodies", () => {
  const project = references.get("docs/api/project.md");
  assert.match(project, /migrationProgress\(token: string\): Promise<string \| undefined>/);
  assert.match(project, /readonly retentionLimit: number/);
  assert.doesNotMatch(project, /constructor\([^;\n]+\):/);
  assert.doesNotMatch(project, /private constructor|this\.storage|private readonly/);
  const worker = references.get("docs/api/worker.md");
  assert.match(worker, /putObjectStream\(/);
  assert.match(worker, /discardAbandonedStage\(/);
  assert.match(worker, /\| `source` \|/);
});

test("native aliases include public methods and typed constants; Python retains keyword defaults", () => {
  const go = references.get("docs/api/go.md");
  assert.match(go, /const BackendMemory Backend = "memory"/);
  assert.match(go, /func \(a Authority\) With\(ops \.\.\.Operation\) Authority/);
  assert.match(go, /## ErrMutationCommitted/);
  const mcp = references.get("docs/api/mcp.md");
  assert.match(mcp, /## stow_object_save/);
  assert.match(mcp, /"request_key"/);
  assert.match(mcp, /## stow_handoff_adopt/);
  const python = references.get("docs/api/python.md");
  assert.match(python, /### Session\.s3_client/);
  assert.match(python, /\| `max_concurrent_requests` \| `int \\?\| None`/);
  assert.match(python, /\| `max_bytes` \| `int` \| No \| DEFAULT_SESSION_MAX_BYTES \|/);
});

test("saved reference includes every current public entry point and matches the current declarations", () => {
  for (const [path, text] of references) assert.equal(readFileSync(path, "utf8"), text, `${path} must be regenerated`);
  const inventory = JSON.parse(references.get("docs/api/inventory.json"));
  assert.equal(inventory.length, 12);
  for (const page of inventory) {
    const markdown = references.get(page.path);
    for (const symbol of page.symbols) assert.ok(markdown.includes(`## ${symbol}\n`), `${page.path}: ${symbol}`);
    for (const member of page.members) assert.ok(markdown.includes(`### ${member}\n`), `${page.path}: ${member}`);
  }
});
