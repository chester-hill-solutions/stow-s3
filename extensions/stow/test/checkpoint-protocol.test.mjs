import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { access, chmod, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { test } from "node:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport, getDefaultEnvironment } from "@modelcontextprotocol/sdk/client/stdio.js";

const exec = promisify(execFile);
const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");

test("independent TypeScript client reads a bounded immutable checkpoint view", async t => {
  const temporary = await mkdtemp(join(tmpdir(), "stow-checkpoint-protocol-"));
  t.after(() => rm(temporary, { recursive: true, force: true }));
  const fixture = await prepareCheckpointFixture(temporary, "saved-workspace");
  const foreign = await prepareCheckpointFixture(temporary, "foreign-workspace", fixture.registry);
  const binary = await stowBinary(temporary);
  const connection = await connectSaved(binary, fixture, temporary);
  t.after(() => closeConnection(connection));

  const { client } = connection;
  assert.equal((await client.listTools()).tools.length, 0, "resource-only saved profile exposed tools");
  assert.equal((await client.listPrompts()).prompts.length, 0, "resource-only saved profile exposed prompts");
  const listed = await client.listResources();
  assert.ok(listed.resources.some(resource => resource.uri === fixture.overviewURI));
  const overview = await readJSON(client, fixture.overviewURI);
  assert.equal(overview.workspace_id, fixture.workspaceId);
  assert.equal(overview.checkpoint_id, fixture.checkpointId);
  assert.equal(overview.view, "checkpoint files");
  assert.deepEqual(overview.read_roots, ["inputs"]);
  assert.ok(overview.max_entries <= 100);
  assert.ok(overview.max_file_bytes <= 65536);
  assert.ok(overview.max_range_bytes <= 65536);
  assert.ok(overview.max_verify_file_bytes <= 64 << 20);
  assert.ok(overview.max_manifest_files <= 10000);
  assert.ok(overview.max_metadata_bytes <= 4 << 20);
  assert.ok(!JSON.stringify(overview).includes(fixture.workspaceRoot));
  assert.ok(!JSON.stringify(overview).includes(fixture.registry));

  const rootPage = await readJSON(client, overview.root_uri);
  assert.equal(rootPage.path, ".");
  const inputsURI = rootPage.entries.find(entry => entry.name === "inputs")?.uri;
  assert.ok(inputsURI);
  let page = await readJSON(client, inputsURI);
  const entries = [...page.entries];
  let continuationCount = 0;
  while (page.page.next_uri) {
    continuationCount++;
    page = await readJSON(client, page.page.next_uri);
    entries.push(...page.entries);
  }
  assert.ok(continuationCount > 0, "fixture did not exercise a later directory page");
  assert.equal(entries.length, fixture.checkpointFiles);
  assert.ok(entries.length > 100);
  assert.ok(!entries.some(entry => entry.name === ".env"));
  const note = entries.find(entry => entry.path === "inputs/a-note.txt");
  const large = entries.find(entry => entry.path === "inputs/z-large.bin");
  const empty = entries.find(entry => entry.path === "inputs/empty.txt");
  assert.ok(note?.uri && note.range_uri);
  assert.ok(large?.uri && large.range_uri);
  assert.ok(empty?.range_uri, "empty files still have a valid bounded range URI");
  const emptyRange = await readJSON(client, empty.range_uri);
  assert.equal(emptyRange.length, 0);
  assert.equal(emptyRange.text, "");

  await writeFile(join(fixture.workspaceRoot, "inputs", "a-note.txt"), "current working version");
  assert.equal(await readText(client, note.uri), "saved checkpoint version");
  const reconstructed = await reconstructBinary(client, large.range_uri, fixture.binaryBytes.length);
  assert.deepEqual(reconstructed.data, fixture.binaryBytes);
  assert.equal(reconstructed.sha256, createHash("sha256").update(fixture.binaryBytes).digest("hex"));

  await assert.rejects(client.readResource({ uri: checkpointFileURI(fixture, "outside/secret.txt") }));
  await assert.rejects(client.readResource({ uri: checkpointFileURI(fixture, "inputs/.env") }));
  await assert.rejects(client.readResource({ uri: overviewURI(fixture, foreign.checkpointId) }));
  await assert.rejects(client.readResource({ uri: `stow://workspaces/${fixture.workspaceId}/v1` }));

  await closeConnection(connection);
  await rm(fixture.workspaceRoot, { recursive: true, force: true });
  const withoutWorkingTree = await connectSaved(binary, fixture, temporary);
  try {
    assert.equal(await readText(withoutWorkingTree.client, note.uri), "saved checkpoint version");
  } finally {
    await closeConnection(withoutWorkingTree);
  }

  const payloadPath = join(fixture.registry, "checkpoints", fixture.checkpointId, "files", "inputs", "z-large.bin");
  const changed = await readFile(payloadPath);
  changed[100000] ^= 0xff;
  await chmod(payloadPath, 0o600);
  await writeFile(payloadPath, changed);
  await chmod(payloadPath, 0o400);
  const corrupted = await connectSaved(binary, fixture, temporary);
  try {
    await assert.rejects(corrupted.client.readResource({ uri: large.range_uri }), /checkpoint|resource|read/i,
      "a range read must verify corruption outside its requested window");
  } finally {
    await closeConnection(corrupted);
  }
});

async function prepareCheckpointFixture(temporary, name, registryOverride) {
  const sourceRoot = join(temporary, `${name}-source`);
  const workspaceRoot = join(temporary, name);
  const registry = registryOverride ?? join(temporary, `${name}-registry`);
  await mkdir(join(sourceRoot, "inputs"), { recursive: true });
  await mkdir(join(sourceRoot, "outside"), { recursive: true });
  const inputs = [];
  for (let index = 0; index < 103; index++) {
    const relative = `inputs/00-decoy-${String(index).padStart(3, "0")}.txt`;
    const source = join(sourceRoot, relative);
    await writeFile(source, `decoy ${index}`);
    inputs.push({ source, destination: relative });
  }
  const note = join(sourceRoot, "inputs", "a-note.txt");
  const empty = join(sourceRoot, "inputs", "empty.txt");
  const large = join(sourceRoot, "inputs", "z-large.bin");
  const privateFile = join(sourceRoot, "outside", "secret.txt");
  const sensitiveFile = join(sourceRoot, "inputs", ".env");
  const binaryBytes = Buffer.alloc(180000);
  for (let index = 0; index < binaryBytes.length; index++) binaryBytes[index] = (index * 31 + 7) & 0xff;
  await writeFile(note, "saved checkpoint version");
  await writeFile(empty, "");
  await writeFile(large, binaryBytes);
  await writeFile(privateFile, "OUT_OF_SCOPE_SENTINEL");
  await writeFile(sensitiveFile, "SENSITIVE_SENTINEL");
  inputs.push({ source: note, destination: "inputs/a-note.txt" });
  inputs.push({ source: empty, destination: "inputs/empty.txt" });
  inputs.push({ source: large, destination: "inputs/z-large.bin" });
  inputs.push({ source: privateFile, destination: "outside/secret.txt" });
  const manifestPath = join(temporary, `${name}-manifest.json`);
  await writeFile(manifestPath, JSON.stringify({
    version: 1, root: workspaceRoot, registry_dir: registry, working_directory: "inputs",
    max_bytes: 2 << 20, max_objects: 1000, max_checkpoints: 10, include_sensitive_inputs: true, inputs: [
      ...inputs, { source: sensitiveFile, destination: "inputs/.env" },
    ],
  }));
  const binary = await stowBinary(temporary);
  const { stdout: preparedOutput } = await exec(binary, ["workspace", "prepare", "--manifest", manifestPath], {
    cwd: repositoryRoot, maxBuffer: 4 << 20,
  });
  const prepared = JSON.parse(preparedOutput);
  const { stdout: checkpointOutput } = await exec(binary, ["workspace", "checkpoint", "--id", prepared.workspace_id,
    "--registry-dir", registry], { cwd: repositoryRoot, maxBuffer: 4 << 20 });
  const checkpoint = JSON.parse(checkpointOutput);
  return {
    workspaceId: prepared.workspace_id,
    workspaceRoot: prepared.root,
    registry: prepared.registry_dir,
    checkpointId: checkpoint.checkpoint_id,
    overviewURI: overviewURI({ workspaceId: prepared.workspace_id }, checkpoint.checkpoint_id),
    checkpointFiles: 106,
    binaryBytes,
    readRoots: ["inputs"],
  };
}

async function connectSaved(binary, fixture, temporary) {
  const client = new Client({ name: "stow-checkpoint-protocol-test", version: "1" });
  const transport = new StdioClientTransport({
    command: binary,
    args: ["mcp", "--registry-dir", fixture.registry, "--workspace-id", fixture.workspaceId,
      "--timeout", "10s", "--workspace-files", "--resources-only", "--read-root", "inputs",
      "--checkpoint-id", fixture.checkpointId],
    env: { ...getDefaultEnvironment(), OPENROUTER_API_KEY: "" },
    cwd: temporary,
    stderr: "pipe",
  });
  await client.connect(transport);
  return { client, transport };
}

async function closeConnection(connection) {
  await connection.client.close().catch(() => undefined);
  await connection.transport.close().catch(() => undefined);
}

async function reconstructBinary(client, firstURI, size) {
  const chunks = [];
  let uri = firstURI;
  let offset = 0;
  let sha256 = "";
  while (uri) {
    const range = await readJSON(client, uri);
    if (!sha256) sha256 = range.sha256;
    assert.equal(range.sha256, sha256);
    assert.equal(range.offset, offset);
    assert.equal(range.size, size);
    assert.ok(range.length > 0 && range.length <= 65536);
    assert.equal(range.path, "inputs/z-large.bin");
    const bytes = typeof range.blob_base64 === "string" ? Buffer.from(range.blob_base64, "base64") : Buffer.from(range.text, "utf8");
    assert.equal(bytes.length, range.length);
    chunks.push(bytes);
    offset += range.length;
    uri = range.next_uri ?? "";
  }
  assert.equal(offset, size);
  return { data: Buffer.concat(chunks), sha256 };
}

async function stowBinary(temporary) {
  const configured = process.env.STOW_BIN;
  if (configured) return resolve(repositoryRoot, configured);
  const existing = join(repositoryRoot, "bin", "stow-s3");
  try { await access(existing); return existing; } catch { /* Build only if the checkout has no binary. */ }
  const output = join(temporary, "stow-s3");
  await exec("go", ["build", "-o", output, "./cmd/stow-s3"], { cwd: repositoryRoot, maxBuffer: 8 << 20 });
  return output;
}

async function readText(client, uri) {
  const result = await client.readResource({ uri });
  assert.equal(result.contents.length, 1);
  return result.contents[0].text;
}

async function readJSON(client, uri) { return JSON.parse(await readText(client, uri)); }

function checkpointFileURI(fixture, path) {
  return `stow://workspaces/${fixture.workspaceId}/checkpoints/${fixture.checkpointId}/files/${encode(path)}`;
}

function overviewURI(fixture, checkpointId) {
  return `stow://workspaces/${fixture.workspaceId}/checkpoints/${checkpointId}/v1`;
}

function encode(path) { return Buffer.from(path).toString("base64url"); }
