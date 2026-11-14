import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { access, mkdir, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";

const exec = promisify(execFile);
const progress = "Prior state: three accepted entries total 46 units. Continue from that verified total using the next batch.";
const nextBatch = "item,units\ndelta,7\nepsilon,13\n";

export async function prepareCheckpointContinuation(stowBinary, root, { signal, timeout = 30_000 } = {}) {
  const paths = fixturePaths(root);
  await createSourceInputs(paths.sourceTree);
  const prepared = await runJSON(stowBinary, root, ["workspace", "prepare", "--manifest", await writeManifest(paths)], { signal, timeout });
  const senderCheckpoint = await runJSON(stowBinary, root, ["workspace", "checkpoint", "--id", prepared.workspace_id,
    "--registry-dir", paths.sourceRegistry, "--portable"], { signal, timeout });
  const handoff = join(paths.bundle, "handoff.json");
  await runCommand(stowBinary, root, ["workspace", "handoff", "--id", prepared.workspace_id,
    "--registry-dir", paths.sourceRegistry, "--checkpoint-id", senderCheckpoint.checkpoint_id,
    "--archive", join(paths.bundle, "checkpoint.tar.gz"), "--output", handoff], { signal, timeout });
  await removeSender(paths);
  await assertSenderRemoved(paths);
  const adopted = await runJSON(stowBinary, root, ["workspace", "adopt", "--handoff", handoff,
    "--root", paths.receiverRoot, "--registry-dir", paths.receiverRegistry], { signal, timeout });
  if (adopted.checkpoint_id !== senderCheckpoint.checkpoint_id) throw new Error("adopted checkpoint identity changed");
  const receiverCheckpoint = await runJSON(stowBinary, root, ["workspace", "checkpoint", "--id", adopted.workspace_id,
    "--registry-dir", paths.receiverRegistry], { signal, timeout });
  if (receiverCheckpoint.workspace_id !== adopted.workspace_id) throw new Error("receiver checkpoint belongs to another workspace");
  await writeFile(join(paths.receiverRoot, "inputs", "next-batch.csv"), "item,units\ndelta,700\nepsilon,1300\n", { mode: 0o600 });
  const overviewURI = `stow://workspaces/${adopted.workspace_id}/checkpoints/${receiverCheckpoint.checkpoint_id}/v1`;
  return {
    workspaceId: adopted.workspace_id,
    checkpointId: receiverCheckpoint.checkpoint_id,
    registryDir: paths.receiverRegistry,
    workspaceRoot: paths.receiverRoot,
    overviewURI,
    progressURI: checkpointFileURI(adopted.workspace_id, receiverCheckpoint.checkpoint_id, "inputs/progress.txt"),
    dataURI: checkpointFileURI(adopted.workspace_id, receiverCheckpoint.checkpoint_id, "inputs/next-batch.csv"),
    sourceWorkspaceId: prepared.workspace_id,
    senderCheckpointId: senderCheckpoint.checkpoint_id,
    importedSourceCheckpointId: adopted.checkpoint_id,
    sourcePathsRemoved: true,
    expectedTotal: 66,
    expectedEntryCount: 5,
    savedProgress: progress,
    savedNextBatch: nextBatch,
  };
}

async function assertSenderRemoved(paths) {
  for (const path of [paths.sourceRoot, paths.sourceRegistry, paths.sourceTree]) {
    try {
      await access(path);
      throw new Error("sender checkpoint fixture path remains available");
    } catch (error) {
      if (error?.code !== "ENOENT") throw error;
    }
  }
}

export async function removeCheckpointFixture(fixtureRoot) {
  await rm(fixtureRoot, { recursive: true, force: true });
}

function fixturePaths(root) {
  return {
    root, sourceRoot: join(root, "source-workspace"), sourceTree: join(root, "source-tree"),
    sourceRegistry: join(root, "source-registry"), receiverRoot: join(root, "receiver"),
    receiverRegistry: join(root, "receiver-registry"), bundle: join(root, "bundle"),
  };
}

async function createSourceInputs(sourceTree) {
  await mkdir(join(sourceTree, "inputs"), { recursive: true, mode: 0o700 });
  await mkdir(join(sourceTree, "inputs-private"), { recursive: true, mode: 0o700 });
  await writeFile(join(sourceTree, "inputs", "progress.txt"), progress, { mode: 0o600 });
  await writeFile(join(sourceTree, "inputs", "next-batch.csv"), nextBatch, { mode: 0o600 });
  await writeFile(join(sourceTree, "inputs-private", "sentinel.txt"), "PRIVATE_OUT_OF_SCOPE", { mode: 0o600 });
}

async function writeManifest(paths) {
  await mkdir(paths.bundle, { recursive: true, mode: 0o700 });
  const manifest = join(paths.root, "manifest.json");
  await writeFile(manifest, JSON.stringify({
    version: 1, root: paths.sourceRoot, registry_dir: paths.sourceRegistry,
    working_directory: "inputs", max_bytes: 65536, max_objects: 10,
    inputs: [
      { source: join(paths.sourceTree, "inputs", "progress.txt"), destination: "inputs/progress.txt" },
      { source: join(paths.sourceTree, "inputs", "next-batch.csv"), destination: "inputs/next-batch.csv" },
      { source: join(paths.sourceTree, "inputs-private", "sentinel.txt"), destination: "inputs-private/sentinel.txt" },
    ],
  }), { mode: 0o600, flag: "wx" });
  return manifest;
}

async function removeSender(paths) {
  await rm(paths.sourceRoot, { recursive: true, force: true });
  await rm(paths.sourceRegistry, { recursive: true, force: true });
  await rm(paths.sourceTree, { recursive: true, force: true });
}

function checkpointFileURI(workspaceId, checkpointId, path) {
  return `stow://workspaces/${workspaceId}/checkpoints/${checkpointId}/files/${Buffer.from(path).toString("base64url")}`;
}

async function runJSON(binary, cwd, args, { signal, timeout }) {
  const { stdout } = await exec(binary, args, {
    cwd, signal, timeout, maxBuffer: 1 << 20,
    env: { PATH: process.env.PATH ?? "/usr/bin:/bin", HOME: join(cwd, "stow-home"), TMPDIR: cwd },
  });
  return JSON.parse(stdout);
}

async function runCommand(binary, cwd, args, { signal, timeout }) {
  await exec(binary, args, {
    cwd, signal, timeout, maxBuffer: 1 << 20,
    env: { PATH: process.env.PATH ?? "/usr/bin:/bin", HOME: join(cwd, "stow-home"), TMPDIR: cwd },
  });
}
