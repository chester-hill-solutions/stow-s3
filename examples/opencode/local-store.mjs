import { open, readFile, rename, mkdir, rm, realpath } from "node:fs/promises";
import { dirname, join, relative, isAbsolute, sep } from "node:path";
import { randomUUID } from "node:crypto";

export function jsonStore(path, identity) {
  return {
    async load() {
      let raw;
      try { raw = await readFile(path, "utf8"); } catch (error) { if (error.code === "ENOENT") return undefined; throw error; }
      const stored = JSON.parse(raw);
      if (stored.identity !== identity) throw new Error("caller state belongs to different configuration");
      return stored.value;
    },
    async save(value) { await atomicJSON(path, { identity, value }); },
  };
}

export async function atomicJSON(path, value) {
  const temporary = `${path}.${randomUUID()}.tmp`;
  const file = await open(temporary, "wx", 0o600);
  try {
    await file.writeFile(JSON.stringify(value, null, 2) + "\n");
    await file.sync();
  } finally { await file.close(); }
  try { await rename(temporary, path); } finally { await rm(temporary, { force: true }); }
  const parent = await open(dirname(path), "r");
  try { await parent.sync(); } finally { await parent.close(); }
}

export async function acquireCallerState(directory, workspaceRoot) {
  await mkdir(directory, { recursive: true, mode: 0o700 });
  const root = await realpath(workspaceRoot);
  const state = await realpath(directory);
  const rel = relative(root, state);
  if (rel === "" || (rel !== ".." && !rel.startsWith(`..${sep}`) && !isAbsolute(rel))) throw new Error("caller state must be outside the captured workspace");
  const lock = join(state, "admission.lock");
  await mkdir(lock, { mode: 0o700 });
  return async () => { await rm(lock, { recursive: true }); };
}
