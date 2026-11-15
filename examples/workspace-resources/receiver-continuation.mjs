import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { access, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";

const exec = promisify(execFile);
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const extensionRequire = createRequire(join(repoRoot, "extensions/stow/package.json"));
const cli = resolve(repoRoot, process.env.STOW_BIN ?? "bin/stow-s3");
const savedProgress = "Sender saved: compare the recorded value 41 with the next source.";
const savedEvidence = "item,value\nalpha,41\n";

export async function runReceiverContinuation() {
  const temporary = await mkdtemp(join(tmpdir(), "stow-receiver-continuation-"));
  let client;
  let transport;
  try {
    const sourceWorkspace = join(temporary, "source-workspace");
    const sourceRegistry = join(temporary, "source-registry");
    const receiverWorkspace = join(temporary, "receiver-workspace");
    const receiverRegistry = join(temporary, "receiver-registry");
    const sourceTree = join(temporary, "source-tree");
    const bundle = join(temporary, "bundle");
    await mkdir(join(sourceTree, "inputs"), { recursive: true });
    await mkdir(bundle);
    await writeFile(join(sourceTree, "inputs", "progress.txt"), savedProgress);
    await writeFile(join(sourceTree, "inputs", "evidence.csv"), savedEvidence);
    const manifest = join(temporary, "source-manifest.json");
    await writeFile(manifest, JSON.stringify({
      version: 1,
      root: sourceWorkspace,
      registry_dir: sourceRegistry,
      working_directory: "inputs",
      max_bytes: 1 << 20,
      max_objects: 100,
      inputs: [
        { source: join(sourceTree, "inputs", "progress.txt"), destination: "inputs/progress.txt" },
        { source: join(sourceTree, "inputs", "evidence.csv"), destination: "inputs/evidence.csv" },
      ],
    }), { mode: 0o600 });

    const prepared = await runJSON(["workspace", "prepare", "--manifest", manifest]);
    const sourceCheckpoint = await runJSON(["workspace", "checkpoint", "--id", prepared.workspace_id,
      "--registry-dir", sourceRegistry, "--portable"]);
    const handoffPath = join(bundle, "handoff.json");
    await exec(cli, ["workspace", "handoff", "--id", prepared.workspace_id, "--registry-dir", sourceRegistry,
      "--checkpoint-id", sourceCheckpoint.checkpoint_id, "--archive", join(bundle, "checkpoint.tar.gz"), "--output", handoffPath],
    { cwd: repoRoot, maxBuffer: 4 << 20 });

    await rm(sourceWorkspace, { recursive: true, force: true });
    await rm(sourceRegistry, { recursive: true, force: true });
    await rm(sourceTree, { recursive: true, force: true });
    assert.equal(await pathExists(sourceWorkspace), false);
    assert.equal(await pathExists(sourceRegistry), false);

    const adopted = await runJSON(["workspace", "adopt", "--handoff", handoffPath,
      "--root", receiverWorkspace, "--registry-dir", receiverRegistry]);
    assert.notEqual(adopted.workspace_id, prepared.workspace_id);
    assert.equal(adopted.checkpoint_id, sourceCheckpoint.checkpoint_id);
    const receiverCheckpoint = await runJSON(["workspace", "checkpoint", "--id", adopted.workspace_id,
      "--registry-dir", receiverRegistry]);
    await writeFile(join(receiverWorkspace, "inputs", "progress.txt"), "Receiver made a later working-tree edit.");

    const { Client } = extensionRequire("@modelcontextprotocol/sdk/client/index.js");
    const { StdioClientTransport, getDefaultEnvironment } = extensionRequire("@modelcontextprotocol/sdk/client/stdio.js");
    transport = new StdioClientTransport({
      command: cli,
      args: ["mcp", "--registry-dir", receiverRegistry, "--workspace-id", adopted.workspace_id,
        "--timeout", "10s", "--workspace-files", "--resources-only", "--read-root", "inputs",
        "--checkpoint-id", receiverCheckpoint.checkpoint_id],
      env: { ...getDefaultEnvironment(), OPENROUTER_API_KEY: "" },
      cwd: receiverWorkspace,
      stderr: "pipe",
    });
    client = new Client({ name: "stow-receiver-continuation", version: "1" });
    await client.connect(transport);
    assert.equal((await client.listTools()).tools.length, 0);
    assert.equal((await client.listPrompts()).prompts.length, 0);
    const overviewURI = `stow://workspaces/${adopted.workspace_id}/checkpoints/${receiverCheckpoint.checkpoint_id}/v1`;
    const overview = JSON.parse((await client.readResource({ uri: overviewURI })).contents[0].text);
    const savedProgressURI = checkpointFileURI(adopted.workspace_id, receiverCheckpoint.checkpoint_id, "inputs/progress.txt");
    const savedEvidenceURI = checkpointFileURI(adopted.workspace_id, receiverCheckpoint.checkpoint_id, "inputs/evidence.csv");
    const progress = (await client.readResource({ uri: savedProgressURI })).contents[0].text;
    const evidence = (await client.readResource({ uri: savedEvidenceURI })).contents[0].text;
    assert.equal(progress, savedProgress);
    assert.equal(evidence, savedEvidence);
    const progressSHA256 = sha256(progress);
    const evidenceSHA256 = sha256(evidence);
    assert.equal(progressSHA256, sha256(savedProgress));
    assert.equal(evidenceSHA256, sha256(savedEvidence));
    return {
      status: "passed",
      source_workspace_id: prepared.workspace_id,
      source_checkpoint_id: sourceCheckpoint.checkpoint_id,
      source_paths_removed: true,
      receiver_workspace_id: adopted.workspace_id,
      imported_source_checkpoint_id: adopted.checkpoint_id,
      receiver_checkpoint_id: receiverCheckpoint.checkpoint_id,
      resource_root_uri: overviewURI,
      overview_manifest_sha256: overview.manifest_sha256,
      receiver_saved_files: {
        "inputs/progress.txt": { exact_content_verified: true, byte_length: Buffer.byteLength(progress), sha256: progressSHA256 },
        "inputs/evidence.csv": { exact_content_verified: true, byte_length: Buffer.byteLength(evidence), sha256: evidenceSHA256 },
      },
    };
  } finally {
    await client?.close().catch(() => undefined);
    await transport?.close().catch(() => undefined);
    await rm(temporary, { recursive: true, force: true });
  }
}

function checkpointFileURI(workspaceID, checkpointID, path) {
  return `stow://workspaces/${workspaceID}/checkpoints/${checkpointID}/files/${Buffer.from(path).toString("base64url")}`;
}

function sha256(value) { return createHash("sha256").update(value).digest("hex"); }

async function runJSON(args) {
  const { stdout } = await exec(cli, args, { cwd: repoRoot, maxBuffer: 4 << 20 });
  return JSON.parse(stdout);
}

async function pathExists(path) {
  try { await access(path); return true; } catch { return false; }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  runReceiverContinuation().then(result => process.stdout.write(`${JSON.stringify(result)}\n`)).catch(error => {
    process.stderr.write(`receiver continuation failed: ${error.message}\n`);
    process.exitCode = 1;
  });
}
