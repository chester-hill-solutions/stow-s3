import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { access, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { test } from "node:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport, getDefaultEnvironment } from "@modelcontextprotocol/sdk/client/stdio.js";

const exec = promisify(execFile);
const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");

test("independent TypeScript client pages and reads a scoped native workspace", async t => {
  const temporary = await mkdtemp(join(tmpdir(), "stow-workspace-protocol-"));
  t.after(() => rm(temporary, { recursive: true, force: true }));
  const fixture = await prepareWorkspace(temporary);
  const binary = await stowBinary(temporary);
  const environment = { ...getDefaultEnvironment(), OPENROUTER_API_KEY: "" };
  const transport = new StdioClientTransport({
    command: binary,
    args: ["mcp", "--registry-dir", fixture.registry, "--workspace-id", fixture.workspaceId,
      "--timeout", "10s", "--workspace-files", "--resources-only", "--read-root", "inputs"],
    env: environment,
    cwd: fixture.workspaceRoot,
    stderr: "pipe",
  });
  const client = new Client({ name: "stow-workspace-protocol-test", version: "1" });
  t.after(async () => {
    await client.close().catch(() => undefined);
    await transport.close().catch(() => undefined);
  });
  await client.connect(transport);

  const listed = await client.listResources();
  assert.equal(listed.resources.length, 1);
  const overviewUri = `stow://workspaces/${fixture.workspaceId}/v1`;
  const overview = await readJSON(client, overviewUri);
  assert.deepEqual(overview.read_roots, ["inputs"]);
  assert.equal(overview.max_observation_entries, 10000);
  assert.equal(overview.max_observations, 8);

  const root = await readPage(client, `stow://workspaces/${fixture.workspaceId}/tree-pages/v1/Lg`);
  assert.deepEqual(root.entries.map(entry => entry.name), ["inputs"]);
  const first = await readPage(client, root.entries[0].uri);
  assert.equal(first.page.offset, 0);
  assert.equal(first.page.count, 100);
  assert.ok(first.page.next_uri);

  await writeFile(join(fixture.workspaceRoot, "inputs", "file-000.txt"), "host update");
  await writeFile(join(fixture.workspaceRoot, "inputs", "later.txt"), "created after observation");
  const names = first.entries.map(entry => entry.name);
  let nextUri = first.page.next_uri;
  let expectedOffset = first.page.count;
  while (nextUri) {
    const page = await readPage(client, nextUri);
    assert.equal(page.observation_id, first.observation_id);
    assert.equal(page.page.offset, expectedOffset);
    assert.equal(page.page.count, page.entries.length);
    names.push(...page.entries.map(entry => entry.name));
    expectedOffset += page.page.count;
    nextUri = page.page.next_uri;
  }
  assert.equal(names.length, 235);
  assert.equal(new Set(names).size, names.length);
  assert.ok(!names.includes("later.txt"));
  assert.equal(await readText(client, `stow://workspaces/${fixture.workspaceId}/files/${encode("inputs/file-000.txt")}`), "host update");
  assert.equal(await readText(client, `stow://workspaces/${fixture.workspaceId}/files/${encode("inputs/later.txt")}`), "created after observation");
  await assert.rejects(client.readResource({ uri: `stow://workspaces/${fixture.workspaceId}/files/${encode("inputs-private/secret.txt")}` }));
  await assert.rejects(client.readResource({ uri: `stow://workspaces/${fixture.workspaceId}/tree/${encode("inputs")}` }));
});

async function prepareWorkspace(temporary) {
  const source = join(temporary, "source");
  const workspaceRoot = join(temporary, "workspace");
  const registry = join(temporary, "registry");
  await mkdir(join(source, "inputs"), { recursive: true });
  await mkdir(join(source, "inputs-private"), { recursive: true });
  const inputs = [];
  for (let index = 0; index < 235; index++) {
    const name = `file-${String(index).padStart(3, "0")}.txt`;
    const inputPath = join(source, "inputs", name);
    await writeFile(inputPath, `value-${index}`);
    inputs.push({ source: inputPath, destination: `inputs/${name}` });
  }
  const privatePath = join(source, "inputs-private", "secret.txt");
  await writeFile(privatePath, "hidden");
  inputs.push({ source: privatePath, destination: "inputs-private/secret.txt" });
  const manifest = join(temporary, "manifest.json");
  await writeFile(manifest, JSON.stringify({ version: 1, root: workspaceRoot, registry_dir: registry,
    working_directory: "inputs", max_bytes: 1 << 20, inputs }));
  const { stdout } = await exec(await stowBinary(temporary), ["workspace", "prepare", "--manifest", manifest], { cwd: repositoryRoot, maxBuffer: 4 << 20 });
  const prepared = JSON.parse(stdout);
  return { workspaceRoot: prepared.root, registry: prepared.registry_dir, workspaceId: prepared.workspace_id };
}

async function stowBinary(temporary) {
  const configured = process.env.STOW_BIN;
  if (configured) return resolve(repositoryRoot, configured);
  const existing = join(repositoryRoot, "bin", "stow-s3");
  try { await access(existing); return existing; } catch { /* Build the checkout when its binary is absent. */ }
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

async function readPage(client, uri) {
  const page = await readJSON(client, uri);
  assert.equal(page.version, 1);
  assert.equal(page.observation_complete, true);
  assert.ok(page.observation_id);
  return page;
}

function encode(path) { return Buffer.from(path).toString("base64url"); }
