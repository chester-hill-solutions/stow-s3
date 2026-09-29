#!/usr/bin/env node
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdtemp, mkdir, writeFile, readdir, stat, rm } from "node:fs/promises";
import { tmpdir, platform, arch } from "node:os";
import { join, resolve } from "node:path";
import { randomBytes, randomUUID } from "node:crypto";
import { performance } from "node:perf_hooks";

const files = Number(process.argv[2] ?? 256);
const bytes = Number(process.argv[3] ?? 8388608);
const runs = Number(process.argv[4] ?? 5);
const changesPerCapture = Number(process.argv[5] ?? 0);
if (![files, bytes, runs, changesPerCapture].every(Number.isSafeInteger) || files < 1 || files > 100000 || bytes < files || bytes > 1<<30 || runs < 1 || runs > 20 || changesPerCapture < 0 || changesPerCapture > files) throw new Error("usage: benchmark-portable.mjs [files<=100000] [bytes<=1GiB] [runs<=20] [changed-files<=files]");
const binary = resolve(process.env.STOW_BIN ?? "bin/stow-s3");
const execute = promisify(execFile);
const root = await mkdtemp(join(tmpdir(), "stow-portable-cost-"));
try {
  const inputs = join(root, "inputs");
  await mkdir(inputs);
  let remaining = bytes;
  for (let index = 0; index < files; index++) {
    const count = index === files - 1 ? remaining : Math.floor(bytes / files);
    await writeFile(join(inputs, `file-${index}`), randomBytes(count));
    remaining -= count;
  }
  const registry = join(root, "registry");
  const manifest = join(root, "prepare.json");
  await writeFile(manifest, JSON.stringify({ version: 1, root: join(root, "work"), registry_dir: registry, max_bytes: bytes * 2, max_objects: files * 2, inputs: [{ source: inputs, destination: "data" }] }));
  const prepared = JSON.parse((await execute(binary, ["workspace", "prepare", "--manifest", manifest])).stdout);
  const times = [];
  let parent;
  for (let index = 0; index < runs; index++) {
    if (index > 0) {
      for (let changed = 0; changed < changesPerCapture; changed++) {
        const count = changed === files - 1 ? bytes - Math.floor(bytes / files) * (files - 1) : Math.floor(bytes / files);
        await writeFile(join(root, "work", "data", `file-${changed}`), randomBytes(count));
      }
    }
    const args = ["workspace", "checkpoint", "--registry-dir", registry, "--id", prepared.workspace_id, "--portable", "--request-key", randomUUID(), "--timeout", "2m", "--max-bytes", String(bytes * 2), "--max-files", String(files * 2)];
    if (parent) args.push("--parent", parent);
    const started = performance.now();
    const saved = JSON.parse((await execute(binary, args)).stdout);
    times.push(performance.now() - started);
    parent = saved.result.checkpoint.id;
  }
  const sorted = [...times].sort((a, b) => a - b);
  const subsequent = times.slice(1).sort((a, b) => a - b);
  const retained = await directoryBytes(join(registry, "checkpoints"));
  console.log(JSON.stringify({ version: 1, platform: `${platform()}-${arch()}`, files, logicalBytes: bytes, runs, changesPerCapture, milliseconds: times, medianMs: sorted[Math.floor(sorted.length / 2)], firstCaptureMs: times[0], subsequentMedianMs: subsequent[Math.floor(subsequent.length / 2)], retainedFileBytes: retained.references, uniqueInodeFileBytes: retained.unique, note: "Local capture measurements, includes native process launch; no model or network. Unique inode bytes are apparent file sizes, not physical disk allocation. Not a service-level promise." }, null, 2));
} finally { await rm(root, { recursive: true, force: true }); }

async function directoryBytes(directory, seen = new Set()) {
  const total = { references: 0, unique: 0 };
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      const nested = await directoryBytes(path, seen);
      total.references += nested.references;
      total.unique += nested.unique;
    } else {
      const info = await stat(path);
      const identity = `${info.dev}:${info.ino}`;
      total.references += info.size;
      if (!seen.has(identity)) total.unique += info.size;
      seen.add(identity);
    }
  }
  return total;
}
