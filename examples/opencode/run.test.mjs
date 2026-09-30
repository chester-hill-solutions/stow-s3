import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdtemp, writeFile, readFile, rm, realpath } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { atomicJSON } from "./local-store.mjs";
import { binary, exec, recipientFixture, runWorkspace } from "./pilot-fixture.mjs";

test("full caller uses adopted repo cwd and flags a saved successful no-op", async t => {
  const temp = await mkdtemp(join(tmpdir(), "stow-caller-cwd-"));
  t.after(() => rm(temp, { recursive: true, force: true }));
  const recipient = await recipientFixture(temp);
  const opencode = join(temp, "opencode");
  await writeFile(opencode, `#!${process.execPath}
const fs = require('node:fs');
const http = require('node:http');
if (process.argv.includes('--version')) { console.log('opencode v2.0.16'); process.exit(0); }
let model;
const server = http.createServer(async (req, res) => {
  let raw = ''; for await (const chunk of req) raw += chunk;
  const body = raw ? JSON.parse(raw) : undefined;
  let data;
  if (req.url === '/api/session' && req.method === 'POST') {
    if (body.location.directory !== process.cwd()) { res.writeHead(400); res.end(); return; }
    model = body.model;
    fs.appendFileSync(${JSON.stringify(join(temp, "directories.jsonl"))}, JSON.stringify({ cwd: process.cwd(), location: body.location.directory }) + '\\n');
    data = { id: 'ses_fixture' };
  } else if (req.url.endsWith('/prompt')) {
    if (!body.text.includes('Read STOW_NOTES.md and ../STOW_PROGRESS.json')) { res.writeHead(400); res.end(); return; }
    if (model.id === 'write') {
      const lines = fs.readFileSync('orders.csv', 'utf8').trim().split('\\n').slice(1);
      const total = lines.reduce((sum, row) => sum + Number(row.split(',')[1]), 0);
      fs.writeFileSync('STOW_NOTES.md', 'Cross-host handoff summary: total ' + total + '\\n');
      fs.writeFileSync('result.json', JSON.stringify({ total, rows: lines.length }));
    }
    data = { id: body.id, time: { created: 10 } };
  } else if (req.url.endsWith('/wait')) {
    res.writeHead(204); res.end(); return;
  } else if (req.url.endsWith('/context')) { data = []; }
  else { data = { outcome: 'succeeded', time: { idle: 11 } }; }
  res.setHeader('content-type', 'application/json'); res.end(JSON.stringify({ data }));
});
server.listen(0, '127.0.0.1', () => {
  console.log('server listening on http://127.0.0.1:' + server.address().port);
  console.log('server password fixture-password');
});
`, { mode: 0o700 });
  const config = { registryDir: recipient.registryDir, workspaceID: recipient.workspace_id,
    stateDir: join(temp, "writer-state"), stowBinary: binary, opencodeBinary: opencode,
    model: { providerID: "fixture", id: "write" }, deadlineMs: 10000, maxBytes: 1 << 20, maxFiles: 100 };
  const configPath = join(temp, "caller.json");
  const promptPath = join(temp, "prompt.txt");
  await atomicJSON(configPath, config);
  await writeFile(promptPath, "Read orders.csv and write the total.");
  const run = () => exec(process.execPath, [resolve("examples/opencode/run.mjs"), configPath, promptPath], { timeout: 30000 });
  const changed = JSON.parse((await run()).stdout);
  assert.equal(changed.phase, "saved");
  assert.equal(changed.reviewRequired, false);
  assert.deepEqual(changed.changes.added, ["repo/result.json"]);
  assert.deepEqual(changed.changes.modified, ["repo/STOW_NOTES.md"]);
  assert.deepEqual(JSON.parse(await readFile(join(recipient.working_directory, "result.json"), "utf8")), { total: 31.5, rows: 3 });
  const restored = join(temp, "verified-work");
  await runWorkspace(["restore", "--checkpoint-id", changed.lastCheckpointID, "--root", restored, "--registry-dir", recipient.registryDir]);
  assert.deepEqual(JSON.parse(await readFile(join(restored, "repo", "result.json"), "utf8")), { total: 31.5, rows: 3 });
  assert.equal(await readFile(join(restored, "repo", "STOW_NOTES.md"), "utf8"), await readFile(join(recipient.working_directory, "STOW_NOTES.md"), "utf8"));
  assert.equal((await readFile(join(temp, "directories.jsonl"), "utf8")).trim().split("\n").map(JSON.parse)[0].cwd, await realpath(recipient.working_directory));

  config.stateDir = join(temp, "noop-state");
  config.model.id = "noop";
  await atomicJSON(configPath, config);
  let result;
  await assert.rejects(run(), error => {
    assert.equal(error.code, 2);
    result = JSON.parse(error.stdout);
    return true;
  });
  assert.equal(result.terminal, "succeeded");
  assert.equal(result.phase, "saved");
  assert.equal(result.reviewRequired, true);
  assert.deepEqual(result.changes, { added: [], modified: [], deleted: [] });
  const progress = JSON.parse(await readFile(join(recipient.root, "STOW_PROGRESS.json"), "utf8"));
  assert.equal(progress.reviewRequired, true);
  assert.deepEqual(progress.changes, result.changes);
  const reviewed = join(temp, "verified-noop");
  await runWorkspace(["restore", "--checkpoint-id", result.lastCheckpointID, "--root", reviewed, "--registry-dir", recipient.registryDir]);
  assert.equal(JSON.parse(await readFile(join(reviewed, "STOW_PROGRESS.json"), "utf8")).reviewRequired, true);
  await assert.rejects(run(), /explicit caller review/);
  assert.equal((await readFile(join(temp, "directories.jsonl"), "utf8")).trim().split("\n").length, 2);
});
