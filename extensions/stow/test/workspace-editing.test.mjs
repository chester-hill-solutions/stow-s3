import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { execFile } from "node:child_process";
import { access, mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { test } from "node:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { getDefaultEnvironment } from "@modelcontextprotocol/sdk/client/stdio.js";

const exec = promisify(execFile);
const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const saveTool = "stow_workspace_file_save";
const readTool = "stow_workspace_file_read_for_save";
const resolveTool = "stow_workspace_file_resolve_save";
const releaseTool = "stow_workspace_file_release_observation";
const checkpointTool = "stow_checkpoint_create";
const notePath = "docs/note.txt";

test("two SDK clients serialize workspace edits and recover a lost reply", async t => {
  if (process.platform !== "darwin" && process.platform !== "linux") {
    return t.skip("native workspace editor requires Darwin or Linux");
  }
  const temporary = await mkdtemp(join(tmpdir(), "stow-workspace-editing-"));
  let server;
  const connections = [];
  t.after(async () => {
    await Promise.all(connections.map(closeConnection));
    if (server) await stopServer(server, "SIGKILL");
    await rm(temporary, { recursive: true, force: true });
  });

  const fixture = await prepareWorkspace(temporary);
  const binary = await stowBinary(temporary);
  server = await startEditingServer(binary, fixture, false);
  let lostReply = null;
  let lostReplyCount = 0;
  let armed = false;
  const clientA = await connectClient(server.endpoint, async (input, init) => {
    const shouldLose = armed && isSaveForKey(init?.body, "lost-reply");
    const response = await fetch(input, init);
    if (!shouldLose) return response;
    lostReplyCount++;
    const body = await response.text();
    lostReply = workspaceRPCResult(body);
    throw new TypeError("simulated lost MCP save response");
  });
  const clientB = await connectClient(server.endpoint);
  connections.push(clientA, clientB);
  assertEditingTools(await clientA.client.listTools());
  assertEditingTools(await clientB.client.listTools());

  const [basisA, basisB] = await Promise.all([
    callTool(clientA.client, readTool, { path: notePath }),
    callTool(clientB.client, readTool, { path: notePath }),
  ]);
  assert.equal(basisA.path, notePath);
  assert.equal(basisB.path, notePath);
  assert.equal(basisA.absent, false);
  assert.equal(basisB.absent, false);
  assert.equal(basisA.sha256, basisB.sha256);
  assert.equal(basisA.generation, basisB.generation);
  assert.notEqual(basisA.observation_token, basisB.observation_token);
  assert.deepEqual(readBody(basisA), Buffer.from("initial bytes"));
  assert.deepEqual(readBody(basisB), Buffer.from("initial bytes"));

  const attempts = [
    { client: clientA.client, token: basisA.observation_token, key: "race-a", text: "client A bytes" },
    { client: clientB.client, token: basisB.observation_token, key: "race-b", text: "client B bytes" },
  ];
  const results = await Promise.all(attempts.map(async attempt => ({
    ...attempt,
    result: await callTool(attempt.client, saveTool, {
      path: notePath, text: attempt.text, request_key: attempt.key, observation_token: attempt.token,
    }),
  })));
  const summary = results.map(({ key, text, result }) => ({ key, text, result }));
  assert.equal(results.filter(item => item.result.outcome === "committed").length, 1, JSON.stringify(summary));
  const refused = results.find(item => item.result.outcome !== "committed");
  assert.equal(refused.result.outcome, "not_committed");
  assert.equal(refused.result.error?.code, "conflict");
  const winner = results.find(item => item.result.outcome === "committed");
  assertSaveMetadataOnly(winner.result, winner.text);
  assert.equal(await readFile(join(fixture.workspaceRoot, notePath), "utf8"), winner.text);
  const checkpoint = await callTool(clientA.client, checkpointTool, {
    workspace_id: fixture.workspaceId, request_key: "caller-checkpoint-after-save",
  });
  assert.equal(checkpoint.outcome, "committed", "caller-requested checkpoint did not complete");

  armed = true;
  await assert.rejects(clientA.client.callTool({
    name: saveTool,
    arguments: { path: notePath, text: "saved before lost reply", request_key: "lost-reply", replace: true },
  }));
  armed = false;
  assert.equal(lostReplyCount, 1, "the client did not resend the mutation automatically");
  assert.equal(lostReply.outcome, "committed", "captured completed response is test oracle only");
  assertSaveMetadataOnly(lostReply, "saved before lost reply");
  assert.equal(await readFile(join(fixture.workspaceRoot, notePath), "utf8"), "saved before lost reply");
  const observedAfterSave = await callTool(clientB.client, readTool, { path: notePath });
  assert.equal(observedAfterSave.generation, lostReply.generation);
  assert.deepEqual(readBody(observedAfterSave), Buffer.from("saved before lost reply"));
  await callTool(clientB.client, releaseTool, { observation_token: observedAfterSave.observation_token });

  await Promise.all(connections.map(closeConnection));
  connections.length = 0;
  await stopServer(server, "SIGKILL");
  server = null;
  const laterBytes = "newer host bytes after server death";
  await writeFile(join(fixture.workspaceRoot, notePath), laterBytes);
  const beforeResolve = await stat(join(fixture.workspaceRoot, notePath), { bigint: true });

  server = await startEditingServer(binary, fixture, true);
  const recoveryClient = await connectClient(server.endpoint);
  connections.push(recoveryClient);
  const resolved = await callTool(recoveryClient.client, resolveTool, { path: notePath, request_key: "lost-reply" });
  assert.equal(resolved.outcome, "committed");
  assert.equal(resolved.replayed, true);
  assertSaveMetadataOnly(resolved, "saved before lost reply");
  assert.equal(resolved.generation, lostReply.generation);
  assert.deepEqual(resolved.object, lostReply.object);
  assert.equal(await readFile(join(fixture.workspaceRoot, notePath), "utf8"), laterBytes);
  const afterResolve = await stat(join(fixture.workspaceRoot, notePath), { bigint: true });
  assert.equal(afterResolve.dev, beforeResolve.dev);
  assert.equal(afterResolve.ino, beforeResolve.ino);
  assert.equal(afterResolve.mtimeNs, beforeResolve.mtimeNs);
});

async function prepareWorkspace(temporary) {
  const sourceRoot = join(temporary, "source");
  const workspaceRoot = join(temporary, "workspace");
  const registry = join(temporary, "registry");
  const note = join(sourceRoot, notePath);
  await mkdir(dirname(note), { recursive: true });
  await writeFile(note, "initial bytes");
  const manifest = join(temporary, "manifest.json");
  await writeFile(manifest, JSON.stringify({ version: 1, root: workspaceRoot, registry_dir: registry,
    working_directory: "docs", max_bytes: 1 << 20, max_objects: 100,
    inputs: [{ source: note, destination: notePath }] }));
  const { stdout } = await exec(await stowBinary(temporary), ["workspace", "prepare", "--manifest", manifest], {
    cwd: repositoryRoot, maxBuffer: 4 << 20,
  });
  const prepared = JSON.parse(stdout);
  return { workspaceRoot: prepared.root, registry: prepared.registry_dir, workspaceId: prepared.workspace_id };
}

async function stowBinary(temporary) {
  const configured = process.env.STOW_BIN;
  if (configured) return resolve(repositoryRoot, configured);
  const existing = join(repositoryRoot, "bin", "stow-s3");
  try { await access(existing); return existing; } catch { /* Build only if absent. */ }
  const output = join(temporary, "stow-s3");
  await exec("go", ["build", "-o", output, "./cmd/stow-s3"], { cwd: repositoryRoot, maxBuffer: 8 << 20 });
  return output;
}

async function startEditingServer(binary, fixture, recover) {
  const args = ["mcp", "--registry-dir", fixture.registry, "--workspace-id", fixture.workspaceId,
    "--timeout", "10s", "--workspace-files", "--workspace-edit", "--coordination", "host_quiesced",
    "--read-root", "docs", "--write-root", "docs", "--http-addr", "127.0.0.1:0"];
  if (recover) args.push("--recover-file-saves");
  const child = spawn(binary, args, { cwd: fixture.workspaceRoot,
    env: { ...getDefaultEnvironment(), OPENROUTER_API_KEY: "" }, stdio: ["ignore", "pipe", "pipe"] });
  child.stderrText = "";
  child.stderr.setEncoding("utf8").on("data", chunk => { child.stderrText += chunk; });
  child.stdout.setEncoding("utf8");
  let stdout = "";
  child.stdout.on("data", chunk => { stdout += chunk; });
  child.exited = new Promise(resolveExit => child.once("exit", (code, signal) => resolveExit({ code, signal })));
  try {
    const endpoint = await waitForEndpoint(child, () => stdout);
    return { child, endpoint, exited: child.exited };
  } catch (error) {
    child.kill("SIGKILL");
    await child.exited;
    throw error;
  }
}

async function waitForEndpoint(child, output) {
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    const match = output().match(/mcp http listening on (http:\/\/127\.0\.0\.1:\d+)/);
    if (match) return match[1];
    if (child.exitCode !== null) throw new Error(`MCP server exited early: ${child.stderrText}`);
    await new Promise(resolveDelay => setTimeout(resolveDelay, 10));
  }
  throw new Error(`MCP server did not report an endpoint: ${child.stderrText}`);
}

async function connectClient(endpoint, fetchOverride) {
  const client = new Client({ name: "stow-workspace-editing-e2", version: "1" });
  const transport = new StreamableHTTPClientTransport(new URL(endpoint), fetchOverride ? { fetch: fetchOverride } : undefined);
  await client.connect(transport);
  return { client, transport };
}

async function closeConnection(connection) {
  await connection.client.close().catch(() => undefined);
  await connection.transport.close().catch(() => undefined);
}

async function stopServer(server, signal) {
  if (server.child.exitCode !== null || server.child.signalCode !== null) return;
  server.child.kill(signal);
  const exited = await server.exited;
  if (signal === "SIGKILL") assert.equal(exited.signal, "SIGKILL");
}

async function callTool(client, name, args) {
  const response = await client.callTool({ name, arguments: args });
  return parseToolPayload(response);
}

function assertEditingTools(listing) {
  const names = new Set(listing.tools.map(tool => tool.name));
  for (const name of [readTool, saveTool, resolveTool, releaseTool]) assert.ok(names.has(name), `missing ${name}`);
}

function parseToolPayload(response) {
  if (response.structuredContent && typeof response.structuredContent === "object") return response.structuredContent;
  const text = response.content?.find(item => item.type === "text")?.text;
  assert.ok(text, "tool returned neither structured content nor JSON text");
  return JSON.parse(text);
}

function isSaveForKey(body, requestKey) {
  if (typeof body !== "string") return false;
  try {
    const request = JSON.parse(body);
    const messages = Array.isArray(request) ? request : [request];
    return messages.some(message => message.method === "tools/call" && message.params?.name === saveTool &&
      message.params?.arguments?.request_key === requestKey);
  } catch {
    return false;
  }
}

function workspaceRPCResult(body) {
  const message = JSON.parse(body);
  const response = Array.isArray(message) ? message.find(item => item.result) : message;
  return parseToolPayload(response.result);
}

function readBody(result) {
  if (typeof result.data_base64 === "string") return Buffer.from(result.data_base64, "base64");
  if (typeof result.text === "string") return Buffer.from(result.text, "utf8");
  if (typeof result.body?.data_base64 === "string") return Buffer.from(result.body.data_base64, "base64");
  if (typeof result.body?.text === "string") return Buffer.from(result.body.text, "utf8");
  assert.fail("file observation has no text or base64 body");
}

function assertSaveMetadataOnly(result, expectedText) {
  assert.equal(typeof result.generation, "string");
  assert.ok(result.generation.length > 0);
  assert.ok(result.object && typeof result.object === "object");
  assert.equal(result.object.size, Buffer.byteLength(expectedText));
  assert.equal(typeof result.object.etag, "string");
  assert.ok(result.object.etag.length > 0);
  assert.ok(Number.isFinite(Date.parse(result.object.last_modified)));
  for (const field of ["text", "data_base64", "data"]) {
    assert.equal(Object.hasOwn(result, field), false, `save result exposed ${field}`);
    assert.equal(Object.hasOwn(result.object, field), false, `save object exposed ${field}`);
  }
}
