import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdir, writeFile, rm } from "node:fs/promises";
import { join, resolve } from "node:path";
import { atomicJSON } from "./local-store.mjs";

export const exec = promisify(execFile);
export const binary = resolve(process.env.STOW_BIN ?? "bin/stow-s3");
export const runWorkspace = async args => JSON.parse((await exec(binary, ["workspace", ...args], { maxBuffer: 4 << 20 })).stdout);

// The receiver has no sender tree/registry and must use the adopted repo cwd.
export async function recipientFixture(temp) {
  const sender = join(temp, "sender");
  const registry = join(temp, "sender-registry");
  const seed = join(temp, "orders.csv");
  const notes = join(temp, "notes.md");
  await writeFile(seed, "order_id,amount\n1001,10.25\n1002,20.50\n1003,0.75\n");
  await writeFile(notes, "Sender checked the header. Next: sum the amount column and record a cross-host handoff summary.\n");
  const manifest = join(temp, "manifest.json");
  await atomicJSON(manifest, { version: 1, root: sender, registry_dir: registry, working_directory: "repo",
    inputs: [{ source: seed, destination: "repo/orders.csv" }, { source: notes, destination: "repo/STOW_NOTES.md" }] });
  const prepared = await runWorkspace(["prepare", "--manifest", manifest]);
  const checkpoint = await runWorkspace(["checkpoint", "--id", prepared.workspace_id, "--registry-dir", registry, "--portable"]);
  const bundle = join(temp, "bundle");
  await mkdir(bundle);
  await exec(binary, ["workspace", "handoff", "--id", prepared.workspace_id, "--registry-dir", registry,
    "--checkpoint-id", checkpoint.checkpoint_id, "--archive", join(bundle, "checkpoint.tar.gz"), "--output", join(bundle, "handoff.json")]);
  await rm(sender, { recursive: true });
  await rm(registry, { recursive: true });
  await rm(seed);
  await rm(notes);
  const registryDir = join(temp, "receiver-registry");
  const adopted = await runWorkspace(["adopt", "--handoff", join(bundle, "handoff.json"), "--root", join(temp, "receiver"), "--registry-dir", registryDir]);
  return { ...adopted, registryDir };
}
