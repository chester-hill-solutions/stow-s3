#!/usr/bin/env node
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { randomUUID } from "node:crypto";
import { mkdtemp, readFile, realpath } from "node:fs/promises";
import { tmpdir, homedir } from "node:os";
import { join, resolve } from "node:path";
import { serveWorkspace } from "../../packages/stow-s3/dist/workspace.js";
import { atomicJSON, acquireCallerState } from "../opencode/local-store.mjs";
import { nativeStorage } from "../opencode/native-storage.mjs";
import { saveWithRetry } from "../opencode/controller.mjs";
import { CollaborationRoom } from "./room.mjs";
import { MultiplayerCoordinator } from "./coordinator.mjs";
import { participants, runDemo, seedDemo } from "./demo.mjs";
import { startDashboard } from "./dashboard.mjs";

const exec = promisify(execFile);
const args = process.argv.slice(2);
const option = name => args.includes(name) ? args[args.indexOf(name) + 1] : undefined;
const real = args.includes("--real");
const headless = args.includes("--headless");
const binary = resolve(process.env.STOW_BIN ?? "bin/stow-s3");

async function modelProfile() {
  const profilePath = option("--profile") ?? process.env.STOW_REAL_MODEL_CONFIG;
  let profile;
  if (profilePath) profile = JSON.parse(await readFile(profilePath, "utf8"));
  else {
    const selection = JSON.parse(await readFile(join(homedir(), ".local/state/opencode/model.json"), "utf8"));
    const model = selection.recent?.find(model => model.providerID === "opencode");
    if (!model) throw new Error("Pass --profile with an explicit OpenCode model profile");
    profile = { opencodeBinary: join(homedir(), ".opencode/bin/opencode"), model: { providerID: model.providerID, id: model.modelID },
      modelCatalogPath: join(homedir(), ".cache/opencode/models.json") };
  }
  if (!profile.model?.providerID || !profile.model?.id || !profile.opencodeBinary) throw new Error("model profile requires opencodeBinary and model providerID/id");
  // Explicit --real authorizes the existing Zen login for this local prototype.
  // Only the selected provider key enters the process environment, never output files.
  if (profile.model.providerID === "opencode" && !process.env.OPENCODE_API_KEY) {
    const auth = JSON.parse(await readFile(join(homedir(), ".local/share/opencode/auth.json"), "utf8"));
    if (auth.opencode?.type !== "api" || !auth.opencode.key) throw new Error("Existing Zen API login unavailable; supply OPENCODE_API_KEY");
    process.env.OPENCODE_API_KEY = auth.opencode.key;
  }
  return profile;
}

async function keepDashboard(dashboard, signal) {
  if (!signal.aborted) await new Promise(resolve => signal.addEventListener("abort", resolve, { once: true }));
  await dashboard.close();
}

async function main() {
  const abort = new AbortController();
  const cancel = () => abort.abort(new Error("coordinator interrupted"));
  process.once("SIGINT", cancel); process.once("SIGTERM", cancel);
  if (option("--view")) {
    const evidencePath = resolve(option("--view"));
    const evidence = JSON.parse(await readFile(evidencePath, "utf8"));
    const dashboard = await startDashboard({ root: evidence.root, getState: async () => JSON.parse(await readFile(evidencePath, "utf8")) });
    console.log(`Multiplayer replay: ${dashboard.url}`);
    await keepDashboard(dashboard, abort.signal);
    return;
  }
  const profile = real ? await modelProfile() : { model: { providerID: "fixture", id: "deterministic" } };
  const temp = await realpath(await mkdtemp(join(tmpdir(), "stow-multiplayer-PROTOTYPE-")));
  const registryDir = join(temp, "registry");
  const stateDir = join(temp, "caller-state");
  const manifest = join(temp, "manifest.json");
  const seed = join(temp, "seed.json");
  await atomicJSON(seed, { prototype: "shared-workspace-collaboration" });
  await atomicJSON(manifest, { version: 1, root: join(temp, "workspace"), registry_dir: registryDir, working_directory: ".",
    inputs: [{ source: seed, destination: "PROTOTYPE.json" }] });
  const prepared = JSON.parse((await exec(binary, ["workspace", "prepare", "--manifest", manifest])).stdout);
  process.env.STOW_BIN = binary;
  let holder, release, room, coordinator, dashboard, failure;
  const state = { title: "Revenue Observatory / three collaborators, one workspace", phase: "Preparing", workspaceID: prepared.workspace_id,
    root: prepared.root, model: `${profile.model.providerID}/${profile.model.id}`, prototype: true };
  const evidencePath = join(temp, "evidence.json");
  const snapshot = () => ({ ...state, ...coordinator?.snapshot() });
  try {
    holder = await serveWorkspace({ id: prepared.workspace_id, registryDir, signal: abort.signal });
    state.root = await realpath(holder.ready.root);
    release = await acquireCallerState(stateDir, state.root);
    await seedDemo(state.root);
    if (real && !profile.providerConfigPath) {
      profile.providerConfigPath = join(stateDir, "provider.jsonc");
      await atomicJSON(profile.providerConfigPath, { provider: { opencode: { options: { baseURL: "https://opencode.ai/zen/v1" } } } });
    }
    room = await CollaborationRoom.open({ root: state.root, stateDir: join(stateDir, "room") });
    coordinator = new MultiplayerCoordinator({ room, root: state.root, stateDir: join(stateDir, "agents"), profile, simulate: !real, signal: abort.signal });
    if (!headless) {
      dashboard = await startDashboard({ root: state.root, getState: snapshot });
      console.log(`Multiplayer live: ${dashboard.url}`);
    }
    console.log(`Mode: ${real ? "real OpenCode agents" : "deterministic actors"}; model ${state.model}`);
    console.log(`Artifacts: ${temp}`);
    const result = await runDemo(coordinator, { interruptDesigner: args.includes("--interrupt-designer"), onPhase: phase => { state.phase = phase; console.log(phase); } });
    state.phase = "Stopping every writer before capture";
    await coordinator.stopWriters();
    // Acks remain pending until publication is confirmed. Portable progress states
    // which messages were applied, so replay can be reconciled against saved work.
    await room.exportState();
    await room.queue;
    const storageConfig = { stowBinary: binary, registryDir, workspaceID: prepared.workspace_id, deadlineMs: 30000, maxBytes: 16 << 20, maxFiles: 1000 };
    const request = { key: randomUUID() };
    await atomicJSON(join(stateDir, "capture.json"), { config: storageConfig, request });
    state.phase = "Capturing shared checkpoint";
    const saved = await saveWithRetry(nativeStorage(storageConfig), request, { signal: AbortSignal.any([abort.signal, AbortSignal.timeout(30000)]) });
    state.checkpointID = saved.checkpoint.id;
    await room.acknowledge("designer", result.applied);
    assert.equal((await room.pending("designer")).length, 0);
    for (const delivery of coordinator.deliveries) delivery.status = "acknowledged";
    const restored = join(temp, "restored");
    await exec(binary, ["workspace", "restore", "--checkpoint-id", state.checkpointID, "--root", restored, "--registry-dir", registryDir]);
    for (const path of ["data/metrics.json", "site/index.html", "docs/data-quality.md", `collaboration/messages/${result.dataMessage.id}.json`, "collaboration/state.json"]) {
      assert.equal(await readFile(join(restored, path), "utf8"), await readFile(join(state.root, path), "utf8"));
    }
    const replay = await CollaborationRoom.open({ root: restored, stateDir: join(temp, "replay-state") });
    try {
      for (const spec of participants) await replay.register({ ...spec, ownedFiles: spec.files });
      assert.deepEqual((await replay.pending("designer")).map(m => m.id), result.applied);
      assert.equal((await replay.pending("auditor")).length, 0);
      assert.equal(replay.snapshot().claims.length, 0);
    } finally { replay.close(); }
    state.verification = { ...result.verification, restoredArtifactsMatch: true, replayPreservesRecordedRouting: true, recoveredClaimsEmpty: true };
    state.result = result.expected;
    state.phase = "Complete / checkpoint verified";
    await atomicJSON(evidencePath, snapshot());
    console.log(JSON.stringify({ verdict: "PASS", evidencePath, checkpointID: state.checkpointID, verification: state.verification, metrics: coordinator.snapshot().metrics }));
  } catch (error) {
    failure = error;
    state.phase = "Failed / retained for diagnosis"; state.error = error.message;
    await atomicJSON(evidencePath, snapshot()).catch(() => {});
    throw error;
  } finally {
    let stopFailed = failure?.agentStopUnconfirmed === true;
    if (coordinator) {
      try { await coordinator.stopWriters(); } catch { stopFailed = true; }
    }
    room?.close();
    if (!stopFailed) {
      if (holder) await holder.close();
      if (release) await release();
    } else {
      console.error("Writer exit remains unconfirmed; workspace holder and admission retained. Confirm all agent processes stopped before recovery.");
      // Keep the owner process alive so its storage claim really remains held.
      if (!abort.signal.aborted) await new Promise(resolve => abort.signal.addEventListener("abort", resolve, { once: true }));
    }
    if (dashboard) {
      if (failure) await dashboard.close();
      else { console.log(`Explore the finished room at ${dashboard.url}; Ctrl-C closes the view.`); await keepDashboard(dashboard, abort.signal); }
    }
    process.off("SIGINT", cancel); process.off("SIGTERM", cancel);
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
