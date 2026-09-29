import assert from "node:assert/strict";
import { test } from "node:test";
import { createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdtemp, writeFile, readFile, mkdir, rename, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { TurnController } from "./controller.mjs";
import { nativeStorage } from "./native-storage.mjs";
import { jsonStore, atomicJSON } from "./local-store.mjs";

const exec = promisify(execFile);
const binary = resolve(process.env.STOW_BIN ?? "bin/stow-s3");
const run = async args => JSON.parse((await exec(binary, ["workspace", ...args], { maxBuffer: 4<<20 })).stdout);

test("native checkpoint survives lost reply and an unedited moved handoff", async t => {
  const temp = await mkdtemp(join(tmpdir(), "stow-continuity-"));
  t.after(() => rm(temp, { recursive: true, force: true }));
  const registryDir = join(temp, "sender-registry");
  const workspace = join(temp, "sender-work");
  const seed = join(temp, "seed.txt");
  await writeFile(seed, "invoice=2400\n");
  const manifest = join(temp, "prepare.json");
  await atomicJSON(manifest, { version: 1, root: workspace, registry_dir: registryDir, inputs: [{ source: seed, destination: "invoice.txt" }], max_checkpoints: 1 });
  const prepared = await run(["prepare", "--manifest", manifest]);
  const storage = nativeStorage({ stowBinary: binary, registryDir, workspaceID: prepared.workspace_id, deadlineMs: 10000, maxBytes: 1<<20, maxFiles: 100 });
  let state;
  let captures = 0;
  const controller = new TurnController({
    deadlineMs: 10000,
    store: { load: async () => state, save: async value => { state = structuredClone(value); } },
    storage: { resolve: storage.resolve, capture: async (request, signal) => { captures++; await storage.capture(request, signal); throw new Error("simulated lost reply"); } },
    agent: { execute: async () => { await writeFile(join(workspace, "STOW_NOTES.md"), "Invoice parsed. Next: generate receipt.\n"); return { outcome: "succeeded", sessionID: "fixture", messageID: "fixture-turn" }; } },
    quiesce: async () => {},
    writeProgress: progress => atomicJSON(join(workspace, "STOW_PROGRESS.json"), progress),
  });
  const saved = await controller.turn("Read the invoice and leave continuation notes");
  assert.equal(saved.phase, "saved");
  assert.equal(captures, 1);
  await recoverWithoutOpenCode({ temp, registryDir, workspaceID: prepared.workspace_id, pending: { ...saved, phase: "saving" } });
  const bundle = join(temp, "bundle");
  await mkdir(bundle);
  const handoff = join(bundle, "handoff.json");
  await run(["handoff", "--id", prepared.workspace_id, "--registry-dir", registryDir, "--checkpoint-id", saved.lastCheckpointID, "--archive", join(bundle, "checkpoint.tar.gz"), "--output", handoff]).catch(async error => {
    // File-output commands intentionally have no stdout.
    if (!(error instanceof SyntaxError)) throw error;
  });
  const moved = join(temp, "moved");
  await rename(bundle, moved);
  await rm(workspace, { recursive: true });
  await rm(registryDir, { recursive: true });
  const recipient = join(temp, "recipient");
  const adopted = await run(["adopt", "--handoff", join(moved, "handoff.json"), "--root", recipient, "--registry-dir", join(temp, "receiver-registry")]);
  assert.notEqual(adopted.workspace_id, prepared.workspace_id);
  assert.equal(await readFile(join(recipient, "invoice.txt"), "utf8"), "invoice=2400\n");
  assert.match(await readFile(join(recipient, "STOW_NOTES.md"), "utf8"), /generate receipt/);
  const receipt = { totalCents: Number((await readFile(join(recipient, "invoice.txt"), "utf8")).split("=")[1]) };
  await atomicJSON(join(recipient, "receipt.json"), receipt);
  assert.equal(receipt.totalCents, 2400);
});

async function recoverWithoutOpenCode({ temp, registryDir, workspaceID, pending }) {
  const stateDir = join(temp, "recovery-state");
  await mkdir(stateDir);
  const config = { registryDir, workspaceID, stateDir, stowBinary: binary, opencodeBinary: join(temp, "not-installed"),
    model: { providerID: "not-configured", id: "not-configured" }, deadlineMs: 10000, maxBytes: 1<<20, maxFiles: 100 };
  const identity = createHash("sha256").update(JSON.stringify(config)).digest("hex");
  await jsonStore(join(stateDir, "turn.json"), identity).save(pending);
  const path = join(temp, "recovery-config.json");
  await atomicJSON(path, config);
  const output = await exec(process.execPath, [resolve("examples/opencode/run.mjs"), path, "--recover"]);
  assert.equal(JSON.parse(output.stdout).lastCheckpointID, pending.lastCheckpointID);
}
