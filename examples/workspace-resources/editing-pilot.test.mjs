import assert from "node:assert/strict";
import { test } from "node:test";
import { createHash } from "node:crypto";
import { access, mkdir, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";
import {
  connectEditingHost, handoffEditingFixture, hostVerifyAndCheckpoint, prepareEditingFixture,
  readCheckpointState, removeEditingFixture,
} from "./editing-fixture.mjs";
import { verifyEditingStageEvidence } from "./editing-verifier.mjs";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const binary = resolve(repoRoot, process.env.STOW_BIN ?? "bin/stow-s3");
const statePath = "docs/STATE.json";

test("fixture setup and SDK connect reject an already-aborted deadline before starting a child", async t => {
  const root = await mkdtemp(join(tmpdir(), "stow-editing-abort-test-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(prepareEditingFixture("/missing/stow-s3", join(root, "prepare"), { signal: controller.signal }), /abort/i);
  await assert.rejects(access(join(root, "prepare", "source-workspace")));

  const fixtureRoot = join(root, "connect");
  await mkdir(fixtureRoot, { recursive: true });
  const fixture = await prepareEditingFixture(binary, fixtureRoot);
  await assert.rejects(connectEditingHost(fixture, { signal: controller.signal }), /abort/i);
});

test("two-stage editing fixture verifies MCP saves and continues after sender removal", async t => {
  const root = await mkdtemp(join(tmpdir(), "stow-editing-pilot-"));
  const hosts = [];
  t.after(async () => { await Promise.all(hosts.map(host => host.close())); await removeEditingFixture(root); });
  const fixture = await prepareEditingFixture(binary, root);
  assert.notEqual(fixture.requestKeys.stage1, fixture.requestKeys.stage2);
  assert.notEqual(fixture.checkpointKeys.stage1, fixture.checkpointKeys.stage2);

  let host = await connectEditingHost(fixture);
  hosts.push(host);
  const first = await simulateStage(host.client, fixture, "stage1");
  assert.doesNotThrow(() => verifyEditingStageEvidence(first, fixture, "stage1"));
  const stage1Checkpoint = await hostVerifyAndCheckpoint(host.client, fixture, "stage1", first);
  await host.close();
  await handoffEditingFixture(binary, fixture, stage1Checkpoint.checkpointId, "stage1");
  assert.equal(fixture.sourcePathsRemoved, true);
  assert.notEqual(fixture.workspaceId, fixture.sourceWorkspaceId);

  host = await connectEditingHost(fixture);
  hosts.push(host);
  const second = await simulateStage(host.client, fixture, "stage2");
  assert.doesNotThrow(() => verifyEditingStageEvidence(second, fixture, "stage2"));
  const stage2Checkpoint = await hostVerifyAndCheckpoint(host.client, fixture, "stage2", second);
  await host.close();
  const saved = await readCheckpointState(fixture, stage2Checkpoint.checkpointId, { evidence: true });
  assert.deepEqual(saved.state, { total_units: 56, entry_count: 7, next_batch: null });
  assert.equal(saved.sha256, stage2Checkpoint.stateSHA256);
  assert.equal(saved.bytes, stage2Checkpoint.stateBytes);
});

test("editing verifier rejects missing, incorrect, out-of-scope and noncommitted evidence", () => {
  const fixture = verifierFixture();
  const valid = evidenceFor(fixture);
  assert.doesNotThrow(() => verifyEditingStageEvidence(valid, fixture, "stage1"));
  assert.throws(() => verifyEditingStageEvidence(valid, { ...fixture, readRoots: undefined }, "stage1"));
  assert.throws(() => verifyEditingStageEvidence(valid, { ...fixture, readRoots: ["docs-private"] }, "stage1"));

  const cases = [
    update(valid, evidence => { evidence.terminalStructured = undefined; }),
    update(valid, evidence => { evidence.terminalStructured.total_units = 25; }),
    update(valid, evidence => { evidence.terminalText = JSON.stringify(evidence.terminalStructured); evidence.terminalStructured = undefined; }),
    update(valid, evidence => { evidence.toolTrace = evidence.toolTrace.filter(item => item.input?.path !== "docs/batch-a.csv"); }),
    update(valid, evidence => { evidence.toolTrace.find(item => item.name === "stow_workspace_file_read_for_save" && item.input.path === "docs/batch-a.csv").output.data_base64 = Buffer.from("wrong body").toString("base64"); }),
    update(valid, evidence => { evidence.toolTrace[0].output.data_base64 = Buffer.from('{ "total_units":11,"entry_count":2,"next_batch":"docs/batch-a.csv" }').toString("base64"); }),
    update(valid, evidence => { evidence.toolTrace.find(item => item.name === "stow_workspace_file_save").input.observation_token = "other-token"; }),
    update(valid, evidence => { evidence.toolTrace.find(item => item.name === "stow_workspace_file_save").output.outcome = "unknown"; }),
    update(valid, evidence => { evidence.toolTrace.find(item => item.name === "stow_workspace_file_save").failed = true; }),
    extraReadCase(valid, "/private/host/path"),
    extraReadCase(valid, "docs-private/secret.csv"),
    extraReadCase(valid, "docs/../outside.csv"),
    extraReadCase(valid, "docs//extra.csv"),
    extraReadCase(valid, "docs/secret.csv", item => { item.output.path = "docs/other.csv"; }),
    extraReadCase(valid, "docs/secret.csv", item => { item.output.error = { code: "not_found" }; }),
    extraReadCase(valid, "docs/secret.csv", item => { item.output.absent = true; }),
    update(valid, evidence => { evidence.toolTrace[0].status = "failed"; }),
    update(valid, evidence => { evidence.toolTrace = [evidence.toolTrace[0], evidence.toolTrace[2], evidence.toolTrace[1]]; }),
    update(valid, evidence => { evidence.toolTrace = [evidence.toolTrace[1], evidence.toolTrace[2], evidence.toolTrace[0]]; }),
    update(valid, evidence => {
      const laterRead = structuredClone(evidence.toolTrace[0]);
      laterRead.output.observation_token = "late-token";
      evidence.toolTrace[2].input.observation_token = "late-token";
      evidence.toolTrace.push(laterRead);
    }),
    update(valid, evidence => { evidence.toolTrace.push({ name: "stow_workspace_file_release_observation", status: "completed", input: { observation_token: "unknown" }, output: { released: true } }); }),
    update(valid, evidence => { evidence.toolTrace.splice(1, 0, { name: "stow_workspace_file_release_observation", status: "completed", input: { observation_token: "observed-state" }, output: { released: true } }); }),
    update(valid, evidence => { evidence.toolTrace.push(
      { name: "stow_workspace_file_release_observation", status: "completed", input: { observation_token: "observed-state" }, output: { released: true } },
      { name: "stow_workspace_file_release_observation", status: "completed", input: { observation_token: "observed-state" }, output: { released: true } }); }),
  ];
  for (const invalid of cases) assert.throws(() => verifyEditingStageEvidence(invalid, fixture, "stage1"));

  const scopedExtra = update(valid, evidence => evidence.toolTrace.splice(2, 0, successfulRead("docs/notes.txt", "extra-read")));
  assert.doesNotThrow(() => verifyEditingStageEvidence(scopedExtra, fixture, "stage1"));
  const missingState = update(valid, evidence => { evidence.toolTrace = evidence.toolTrace.filter(item => item.input?.path !== statePath); });
  assert.throws(() => verifyEditingStageEvidence(missingState, fixture, "stage1"));
  const missingBatch = update(valid, evidence => { evidence.toolTrace = evidence.toolTrace.filter(item => item.input?.path !== "docs/batch-a.csv"); });
  assert.throws(() => verifyEditingStageEvidence(missingBatch, fixture, "stage1"));
});

async function simulateStage(client, fixture, stage) {
  const batchPath = stage === "stage1" ? "docs/batch-a.csv" : "docs/batch-b.csv";
  const next = stage === "stage1"
    ? { total_units: 26, entry_count: 5, next_batch: "docs/batch-b.csv" }
    : { total_units: 56, entry_count: 7, next_batch: null };
  const priorPath = fixture.requestKeys[stage];
  const trace = [];
  const state = await tracedCall(client, trace, "stow_workspace_file_read_for_save", { path: statePath });
  const batch = await tracedCall(client, trace, "stow_workspace_file_read_for_save", { path: batchPath });
  const extraPath = stage === "stage1" ? "docs/batch-b.csv" : "docs/batch-a.csv";
  await tracedCall(client, trace, "stow_workspace_file_read_for_save", { path: extraPath });
  assert.equal(JSON.parse(Buffer.from(state.data_base64, "base64").toString("utf8")).total_units, stage === "stage1" ? 11 : 26);
  assert.ok(Buffer.from(batch.data_base64, "base64").length > 0);
  const body = JSON.stringify(next);
  const saved = await tracedCall(client, trace, "stow_workspace_file_save", {
    path: statePath, text: body, request_key: priorPath, observation_token: state.observation_token,
  });
  return { terminalStructured: { total_units: next.total_units, entry_count: next.entry_count, outcome: "committed",
    path: statePath, request_key: priorPath, generation: saved.generation }, toolTrace: trace };
}

async function tracedCall(client, trace, name, input) {
  const response = await client.callTool({ name, arguments: input });
  const output = parsePayload(response);
  trace.push({ name, status: "completed", input, output, failed: response.isError === true });
  if (response.isError) throw new Error(`fixture MCP call failed: ${name}`);
  return output;
}

function evidenceFor(fixture) {
  const stateBytes = Buffer.from(JSON.stringify({ total_units: 11, entry_count: 2, next_batch: "docs/batch-a.csv" }));
  const batchBytes = Buffer.from("item,units\nalpha,3\nbeta,5\ngamma,7\n");
  const next = { total_units: 26, entry_count: 5, next_batch: "docs/batch-b.csv" };
  const saved = JSON.stringify(next);
  const generation = "generation-1";
  const stateRead = { name: "stow_workspace_file_read_for_save", status: "completed", input: { path: statePath }, output: {
    outcome: "observed", available: true, absent: false, path: statePath, observation_token: "observed-state", data_base64: stateBytes.toString("base64"),
  } };
  const batchRead = { name: "stow_workspace_file_read_for_save", status: "completed", input: { path: "docs/batch-a.csv" }, output: {
    outcome: "observed", available: true, absent: false, path: "docs/batch-a.csv", observation_token: "observed-batch", data_base64: batchBytes.toString("base64"),
  } };
  const save = { name: "stow_workspace_file_save", status: "completed", input: {
    path: statePath, text: saved, request_key: fixture.requestKeys.stage1, observation_token: "observed-state",
  }, output: { outcome: "committed", generation, object: { path: statePath } } };
  return { terminalStructured: { total_units: 26, entry_count: 5, outcome: "committed", path: statePath,
    request_key: fixture.requestKeys.stage1, generation }, toolTrace: [stateRead, batchRead, save] };
}

function verifierFixture() {
  const bytes = Buffer.from(JSON.stringify({ total_units: 11, entry_count: 2, next_batch: "docs/batch-a.csv" }));
  return { readRoots: ["docs"], requestKeys: { stage1: "test-request-key", stage2: "test-request-key-2" },
    priorStateDigests: { stage1: { bytes: bytes.length, sha256: createHash("sha256").update(bytes).digest("hex") } } };
}
function successfulRead(path, token) {
  return { name: "stow_workspace_file_read_for_save", status: "completed", input: { path }, output: {
    outcome: "observed", available: true, absent: false, path, observation_token: token,
    data_base64: Buffer.from("extra file").toString("base64"),
  } };
}
function update(original, change) { const copy = structuredClone(original); change(copy); return copy; }
function extraReadCase(original, path, change = () => {}) {
  return update(original, evidence => {
    const item = successfulRead(path, "invalid-extra-token");
    change(item);
    evidence.toolTrace.splice(2, 0, item);
  });
}
function parsePayload(response) {
  if (response.structuredContent && typeof response.structuredContent === "object") return response.structuredContent;
  const text = response.content?.find(item => item.type === "text")?.text;
  assert.ok(text, "MCP tool returned no structured result");
  return JSON.parse(text);
}
