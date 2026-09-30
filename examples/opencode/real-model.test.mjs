import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { test } from "node:test";
import { mkdtemp, mkdir, writeFile, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { atomicJSON } from "./local-store.mjs";
import { binary, exec, recipientFixture, runWorkspace } from "./pilot-fixture.mjs";

const configPath = process.env.STOW_REAL_MODEL_CONFIG;
const required = process.env.STOW_REQUIRE_REAL_MODEL === "1";

test("real model continues an adopted repo from seeded files and saves actual work", {
  skip: !configPath && !required ? "set STOW_REAL_MODEL_CONFIG to opt in to a provider call" : false,
  timeout: 180000,
}, async t => {
  assert.ok(configPath, "STOW_REAL_MODEL_CONFIG is required for make test-agent-real");
  const { providerConfigPath, modelCatalogPath, ...template } = JSON.parse(await readFile(configPath, "utf8"));
  const temp = await mkdtemp(join(tmpdir(), "stow-real-model-"));
  // Preserve isolated logs/state on failure for diagnosis; no secrets enter the workspace.
  t.after(async () => {
    if (t.passed) await rm(temp, { recursive: true, force: true });
    else t.diagnostic(`Real-model evidence and caller state retained at ${temp}`);
  });
  const recipient = await recipientFixture(temp);
  const stateDir = join(temp, "caller-state");
  for (const [source, destination] of [
    [providerConfigPath, join(stateDir, "opencode", "home", ".config", "opencode", "opencode.jsonc")],
    [modelCatalogPath, join(stateDir, "opencode", "home", ".cache", "opencode", "models.json")],
  ]) {
    if (!source) continue;
    await mkdir(dirname(destination), { recursive: true, mode: 0o700 });
    await writeFile(destination, await readFile(source), { mode: 0o600 });
  }
  const callerPath = join(temp, "caller.json");
  await atomicJSON(callerPath, { ...template, registryDir: recipient.registryDir, workspaceID: recipient.workspace_id,
    stateDir, stowBinary: binary });
  const marker = randomUUID();
  const promptPath = join(temp, "prompt.txt");
  await writeFile(promptPath, `Continue from STOW_NOTES.md. Read orders.csv using file tools. Sum its amount column and count data rows. Write result.json with exactly {"total": <sum>, "rows": <count>}. Append a section headed "## Cross-host handoff summary" to STOW_NOTES.md containing the results and the marker ${marker}.`);
  const turn = JSON.parse((await exec(process.execPath, [resolve("examples/opencode/run.mjs"), callerPath, promptPath], { timeout: 150000, maxBuffer: 4 << 20 })).stdout);
  assert.equal(turn.phase, "saved");
  assert.equal(turn.terminal, "succeeded");
  assert.equal(turn.reviewRequired, false);
  assert.ok(turn.changes.modified.includes("repo/STOW_NOTES.md"));
  assert.ok(turn.changes.added.includes("repo/result.json"));
  assert.deepEqual(JSON.parse(await readFile(join(recipient.working_directory, "result.json"), "utf8")), { total: 31.5, rows: 3 });
  const notes = await readFile(join(recipient.working_directory, "STOW_NOTES.md"), "utf8");
  assert.ok(notes.includes("## Cross-host handoff summary"));
  assert.ok(notes.includes(marker));
  const restored = join(temp, "verified-checkpoint");
  await runWorkspace(["restore", "--checkpoint-id", turn.lastCheckpointID, "--root", restored, "--registry-dir", recipient.registryDir]);
  assert.equal(await readFile(join(restored, "repo", "STOW_NOTES.md"), "utf8"), notes);
  assert.deepEqual(JSON.parse(await readFile(join(restored, "repo", "result.json"), "utf8")), { total: 31.5, rows: 3 });
});
