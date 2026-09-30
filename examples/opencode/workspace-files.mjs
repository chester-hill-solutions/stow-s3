import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { open, readdir, realpath, stat } from "node:fs/promises";
import { isAbsolute, join, relative, sep } from "node:path";

export async function agentDirectory(ready) {
  if (typeof ready.working_directory !== "string" || !isAbsolute(ready.working_directory)) {
    throw new Error("workspace readiness requires an absolute working_directory");
  }
  const root = await realpath(ready.root);
  const directory = await realpath(ready.working_directory);
  const rel = relative(root, directory);
  if (rel === ".." || rel.startsWith(`..${sep}`) || isAbsolute(rel)) {
    throw new Error("working_directory must be inside the workspace");
  }
  if (!(await stat(directory)).isDirectory()) throw new Error("working_directory must be a directory");
  return directory;
}

// Observe file changes before the caller writes its own progress. This is task
// evidence for the file-only pilot, not a sandbox or proof of task correctness.
export async function snapshotWorkspace(root, { maxBytes, maxFiles, signal }) {
  const files = Object.create(null);
  let bytes = 0;
  let count = 0;
  const buffer = Buffer.alloc(64 * 1024);
  async function visit(directory, prefix = "") {
    signal?.throwIfAborted();
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      signal?.throwIfAborted();
      const name = entry.name.toLowerCase();
      if (name === ".git" || (!prefix && (name === ".stow" || entry.name === "STOW_PROGRESS.json"))) continue;
      const path = join(directory, entry.name);
      const key = prefix + entry.name;
      if (entry.isDirectory()) { await visit(path, key + "/"); continue; }
      if (!entry.isFile()) throw new Error(`task snapshot requires regular files: ${key}`);
      if (++count > maxFiles) throw new Error("task snapshot exceeds maxFiles");
      const file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
      try {
        const info = await file.stat();
        if (!info.isFile()) throw new Error(`task snapshot requires regular files: ${key}`);
        const hash = createHash("sha256");
        let size = 0;
        while (true) {
          signal?.throwIfAborted();
          const { bytesRead } = await file.read(buffer);
          if (!bytesRead) break;
          size += bytesRead;
          bytes += bytesRead;
          if (bytes > maxBytes) throw new Error("task snapshot exceeds maxBytes");
          hash.update(buffer.subarray(0, bytesRead));
        }
        if (size !== info.size) throw new Error(`task file changed during snapshot: ${key}`);
        files[key] = `${info.mode & 0o777}:${hash.digest("hex")}`;
      } finally { await file.close(); }
    }
  }
  await visit(root);
  return files;
}

export function workspaceChanges(before, after) {
  const changes = { added: [], modified: [], deleted: [] };
  for (const key of Object.keys(after).sort()) {
    if (!Object.hasOwn(before, key)) changes.added.push(key);
    else if (before[key] !== after[key]) changes.modified.push(key);
  }
  for (const key of Object.keys(before).sort()) {
    if (!Object.hasOwn(after, key)) changes.deleted.push(key);
  }
  return changes;
}
