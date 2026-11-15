import { randomUUID, createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { access, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { editingReadRoots, expectedEditingState, verifyEditingStageEvidence } from "./editing-verifier.mjs";

const exec = promisify(execFile);
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const extensionRequire = createRequire(join(repoRoot, "extensions/stow/package.json"));
const statePath = "docs/STATE.json";
const sourceFiles = {
  "docs/batch-a.csv": "item,units\nalpha,3\nbeta,5\ngamma,7\n",
  "docs/batch-b.csv": "item,units\ndelta,13\nepsilon,17\n",
};

export async function prepareEditingFixture(binary, root, options = {}) {
  const executionOptions = boundedExecutionOptions(options);
  assertExecutionTime(executionOptions);
  const paths = fixturePaths(root);
  await mkdir(join(paths.sourceTree, "docs"), { recursive: true, mode: 0o700 });
  await mkdir(paths.bundle, { recursive: true, mode: 0o700 });
  const initialState = Buffer.from(JSON.stringify({ total_units: 11, entry_count: 2, next_batch: "docs/batch-a.csv" }));
  await writeFile(join(paths.sourceTree, statePath), initialState, { mode: 0o600 });
  for (const [path, data] of Object.entries(sourceFiles)) await writeFile(join(paths.sourceTree, path), data, { mode: 0o600 });
  const requestKeys = { stage1: randomUUID(), stage2: randomUUID() };
  const checkpointKeys = { stage1: randomUUID(), stage2: randomUUID() };
  await writeFile(paths.keysFile, JSON.stringify({ requestKeys, checkpointKeys }), { mode: 0o600, flag: "wx" });
  const manifest = join(root, "editing-manifest.json");
  await writeFile(manifest, JSON.stringify({
    version: 1, root: paths.sourceWorkspace, registry_dir: paths.sourceRegistry,
    working_directory: "docs", max_bytes: 1 << 20, max_objects: 100,
    inputs: [
      { source: join(paths.sourceTree, statePath), destination: statePath },
      ...Object.entries(sourceFiles).map(([path]) => ({ source: join(paths.sourceTree, path), destination: path })),
    ],
  }), { mode: 0o600, flag: "wx" });
  const prepared = await runJSON(binary, root, ["workspace", "prepare", "--manifest", manifest], executionOptions);
  const fixture = {
    ...paths, binary: resolve(binary), workspaceRoot: paths.sourceWorkspace, registryDir: paths.sourceRegistry,
    workspaceId: prepared.workspace_id, sourceWorkspaceId: prepared.workspace_id, requestKeys, checkpointKeys,
    readRoots: Object.freeze([...editingReadRoots]),
    priorStateDigests: { stage1: { sha256: sha256(initialState), bytes: initialState.length } },
    unchangedSHA256: Object.fromEntries(Object.entries(sourceFiles).map(([path, data]) => [path, sha256(data)])),
  };
  Object.defineProperty(fixture, "executionOptions", { value: executionOptions, enumerable: false });
  return fixture;
}

export async function connectEditingHost(fixture, { recover = false, signal, deadlineAt } = {}) {
  const { Client } = extensionRequire("@modelcontextprotocol/sdk/client/index.js");
  const { StdioClientTransport, getDefaultEnvironment } = extensionRequire("@modelcontextprotocol/sdk/client/stdio.js");
  const args = ["mcp", "--registry-dir", fixture.registryDir, "--workspace-id", fixture.workspaceId,
    "--timeout", "10s", "--workspace-files", "--workspace-edit", "--coordination", "host_quiesced",
    ...fixture.readRoots.flatMap(root => ["--read-root", root]), "--write-root", "docs"];
  if (recover) args.push("--recover-file-saves");
  const executionOptions = boundedExecutionOptions({ ...(fixture.executionOptions ?? {}), signal: signal ?? fixture.executionOptions?.signal,
    deadlineAt: deadlineAt ?? fixture.executionOptions?.deadlineAt });
  assertExecutionTime(executionOptions);
  const transport = new StdioClientTransport({ command: fixture.binary, args, cwd: fixture.workspaceRoot,
    env: { ...getDefaultEnvironment(), OPENROUTER_API_KEY: "" }, stderr: "pipe" });
  const client = new Client({ name: "stow-workspace-editing-fixture", version: "1" });
  let closed = false;
  const close = async () => {
    if (closed) return;
    closed = true;
    executionOptions.signal?.removeEventListener("abort", abortConnection);
    await client.close().catch(() => undefined);
    await transport.close().catch(() => undefined);
  };
  const abortConnection = () => { void close(); };
  executionOptions.signal?.addEventListener("abort", abortConnection, { once: true });
  try {
    assertExecutionTime(executionOptions);
    await client.connect(transport, requestOptions(executionOptions));
    assertExecutionTime(executionOptions);
    return { client, close };
  } catch (error) {
    await close();
    throw error;
  }
}

export async function hostVerifyAndCheckpoint(client, fixture, stage, evidence) {
  verifyEditingStageEvidence(evidence, fixture, stage);
  const stageName = normalizeStage(stage);
  const key = fixture.requestKeys[stageName];
  const options = fixture.executionOptions ?? boundedExecutionOptions();
  const receipt = await callTool(client, "stow_workspace_file_resolve_save", { path: statePath, request_key: key }, options);
  requireCommittedReceipt(receipt, evidence.terminalStructured.generation);
  const stateRead = await callTool(client, "stow_workspace_file_read_for_save", { path: statePath }, options);
  const stateBytes = decodeBody(stateRead);
  requireExpectedState(stateBytes, stage);
  await callTool(client, "stow_workspace_file_release_observation", { observation_token: stateRead.observation_token }, options);
  await assertUnchangedInputs(fixture);
  const capture = await callTool(client, "stow_checkpoint_create", {
    workspace_id: fixture.workspaceId, request_key: fixture.checkpointKeys[stageName],
    ...(stageName === "stage2" ? { parent_id: fixture.importedCheckpointId } : {}),
  }, options);
  if (capture.outcome !== "committed" || !capture.capture?.checkpoint?.id || capture.capture.checkpoint.workspace_id !== fixture.workspaceId) {
    throw new Error("workspace checkpoint was not committed for the selected workspace");
  }
  const stateSHA256 = sha256(stateBytes);
  const stateInfo = { sha256: stateSHA256, bytes: stateBytes.length };
  if (stageName === "stage1") fixture.priorStateDigests.stage2 = stateInfo;
  return { checkpointId: capture.capture.checkpoint.id, receipt, stateSHA256, stateBytes: stateBytes.length };
}

function normalizeStage(stage) {
  if (stage === 1 || stage === "stage1") return "stage1";
  if (stage === 2 || stage === "stage2") return "stage2";
  throw new Error("unknown editing fixture stage");
}

export async function handoffEditingFixture(binary, fixture, checkpointId, stage) {
  const archive = join(fixture.bundle, `${stage}.tar.gz`);
  const handoff = join(fixture.bundle, `${stage}-handoff.json`);
  const options = fixture.executionOptions ?? boundedExecutionOptions();
  await runCommand(binary, fixture.root, ["workspace", "handoff", "--id", fixture.workspaceId,
    "--registry-dir", fixture.registryDir, "--checkpoint-id", checkpointId, "--archive", archive, "--output", handoff], options);
  const oldRoot = fixture.workspaceRoot;
  const oldRegistry = fixture.registryDir;
  const oldWorkspaceId = fixture.workspaceId;
  await rm(oldRoot, { recursive: true, force: true });
  await rm(oldRegistry, { recursive: true, force: true });
  await rm(fixture.sourceTree, { recursive: true, force: true });
  await assertMissing([oldRoot, oldRegistry, fixture.sourceTree]);
  const nextRoot = join(fixture.root, `${stage}-receiver`);
  const nextRegistry = join(fixture.root, `${stage}-receiver-registry`);
  const adopted = await runJSON(binary, fixture.root, ["workspace", "adopt", "--handoff", handoff,
    "--root", nextRoot, "--registry-dir", nextRegistry], options);
  if (adopted.checkpoint_id !== checkpointId || adopted.workspace_id === oldWorkspaceId) {
    throw new Error("adopted editing checkpoint identity did not bind to a new workspace");
  }
  const receiver = await runJSON(binary, fixture.root, ["workspace", "checkpoint", "--id", adopted.workspace_id,
    "--registry-dir", nextRegistry], options);
  if (receiver.workspace_id !== adopted.workspace_id) throw new Error("receiver checkpoint belongs to another workspace");
  fixture.workspaceRoot = nextRoot;
  fixture.registryDir = nextRegistry;
  fixture.workspaceId = adopted.workspace_id;
  fixture.importedCheckpointId = receiver.checkpoint_id;
  fixture.sourcePathsRemoved = true;
  return fixture;
}

export async function readCheckpointState(fixture, checkpointId, { evidence = false } = {}) {
  const { Client } = extensionRequire("@modelcontextprotocol/sdk/client/index.js");
  const { StdioClientTransport, getDefaultEnvironment } = extensionRequire("@modelcontextprotocol/sdk/client/stdio.js");
  const args = ["mcp", "--registry-dir", fixture.registryDir, "--workspace-id", fixture.workspaceId,
    "--timeout", "10s", "--workspace-files", "--resources-only",
    ...fixture.readRoots.flatMap(root => ["--read-root", root]), "--checkpoint-id", checkpointId];
  const transport = new StdioClientTransport({ command: fixture.binary, args, cwd: fixture.workspaceRoot,
    env: { ...getDefaultEnvironment(), OPENROUTER_API_KEY: "" }, stderr: "pipe" });
  const client = new Client({ name: "stow-editing-checkpoint-proof", version: "1" });
  const options = fixture.executionOptions ?? boundedExecutionOptions();
  assertExecutionTime(options);
  let closed = false;
  const close = async () => {
    if (closed) return;
    closed = true;
    options.signal?.removeEventListener("abort", abortConnection);
    await client.close().catch(() => undefined);
    await transport.close().catch(() => undefined);
  };
  const abortConnection = () => { void close(); };
  options.signal?.addEventListener("abort", abortConnection, { once: true });
  try {
    await client.connect(transport, requestOptions(options));
  } catch (error) {
    await close();
    throw error;
  }
  try {
    assertExecutionTime(options);
    const uri = `stow://workspaces/${fixture.workspaceId}/checkpoints/${checkpointId}/files/${Buffer.from(statePath).toString("base64url")}`;
    const response = await client.readResource({ uri }, requestOptions(options));
    assertExecutionTime(options);
    const text = response.contents?.find(item => item.uri === uri)?.text;
    if (typeof text !== "string") throw new Error("saved checkpoint state was unavailable");
    const state = JSON.parse(text);
    return evidence ? { state, sha256: sha256(Buffer.from(text, "utf8")), bytes: Buffer.byteLength(text, "utf8") } : state;
  } finally {
    await close();
  }
}

export async function removeEditingFixture(root) {
  await rm(root, { recursive: true, force: true });
}

export function editingInputFiles() { return { ...sourceFiles }; }
export function editingStatePath() { return statePath; }

async function assertUnchangedInputs(fixture) {
  for (const [path, digest] of Object.entries(fixture.unchangedSHA256)) {
    const bytes = await readFile(join(fixture.workspaceRoot, path));
    if (sha256(bytes) !== digest) throw new Error("an input file changed during the editing stage");
  }
}

function requireCommittedReceipt(receipt, generation) {
  if (receipt.outcome !== "committed" || (receipt.path ?? receipt.object?.path) !== statePath ||
      receipt.generation !== generation || !receipt.generation) {
    throw new Error("retained workspace save receipt did not match the stage request");
  }
}

function requireExpectedState(bytes, stage) {
  const expected = expectedEditingState(stage);
  const actual = parseState(bytes);
  if (actual.total_units !== expected.total_units || actual.entry_count !== expected.entry_count || actual.next_batch !== expected.next_batch) {
    throw new Error("workspace state file did not match the completed stage");
  }
}

function parseState(bytes) {
  try {
    const state = JSON.parse(bytes.toString("utf8"));
    if (!state || typeof state !== "object" || Array.isArray(state) ||
        Object.keys(state).sort().join(",") !== "entry_count,next_batch,total_units" ||
        !Number.isSafeInteger(state.total_units) || !Number.isSafeInteger(state.entry_count) ||
        !(typeof state.next_batch === "string" || state.next_batch === null)) throw new Error();
    return state;
  } catch { throw new Error("workspace state file was invalid"); }
}

function decodeBody(result) {
  if (typeof result.data_base64 !== "string") throw new Error("workspace observation had no complete bytes");
  const bytes = Buffer.from(result.data_base64, "base64");
  if (bytes.toString("base64") !== result.data_base64) throw new Error("workspace observation had invalid base64");
  return bytes;
}

function parsePayload(response) {
  const value = response.structuredContent;
  if (value && typeof value === "object") return value;
  if (typeof value === "string") return JSON.parse(value);
  const text = response.content?.find(item => item.type === "text")?.text;
  if (!text) throw new Error("MCP tool response had no structured result");
  return JSON.parse(text);
}

async function callTool(client, name, args, options = boundedExecutionOptions()) {
  assertExecutionTime(options);
  const response = await client.callTool({ name, arguments: args }, undefined, requestOptions(options));
  assertExecutionTime(options);
  if (response.isError) throw new Error(`MCP tool ${name} failed`);
  return parsePayload(response);
}

function fixturePaths(root) {
  return { root, sourceWorkspace: join(root, "source-workspace"), sourceRegistry: join(root, "source-registry"),
    sourceTree: join(root, "source-tree"), bundle: join(root, "bundle"), keysFile: join(root, "request-keys.json") };
}

async function runJSON(binary, cwd, args, options = boundedExecutionOptions()) {
  const { stdout } = await runCommand(binary, cwd, args, options);
  return JSON.parse(stdout);
}

async function runCommand(binary, cwd, args, options = boundedExecutionOptions()) {
  assertExecutionTime(options);
  return exec(binary, args, { cwd: repoRoot, maxBuffer: 4 << 20, env: { PATH: process.env.PATH ?? "/usr/bin:/bin",
    HOME: join(cwd, "stow-home"), TMPDIR: cwd, OPENROUTER_API_KEY: "" },
    signal: options.signal, timeout: remainingExecutionTime(options), killSignal: "SIGKILL" });
}

async function assertMissing(paths) {
  for (const path of paths) {
    try { await access(path); throw new Error("source path remains after checkpoint handoff"); }
    catch (error) { if (error?.code !== "ENOENT") throw error; }
  }
}

function sha256(value) { return createHash("sha256").update(value).digest("hex"); }

function boundedExecutionOptions(options = {}) {
  return { signal: options.signal, deadlineAt: options.deadlineAt ?? Date.now() + 30_000 };
}

function remainingExecutionTime(options) {
  assertExecutionTime(options);
  return Math.max(1, Math.min(30_000, options.deadlineAt - Date.now()));
}

function assertExecutionTime(options) {
  if (options.signal?.aborted) throw options.signal.reason ?? Object.assign(new Error("operation aborted"), { name: "AbortError" });
  if (!Number.isFinite(options.deadlineAt) || options.deadlineAt <= Date.now()) {
    throw Object.assign(new Error("operation deadline elapsed"), { code: "ETIMEDOUT" });
  }
}

function requestOptions(options) {
  return { signal: options.signal, timeout: remainingExecutionTime(options) };
}
