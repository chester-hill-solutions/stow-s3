import { BrowserPersistenceError, type LockManagerLike } from "./browser-types.js";

export async function ownContentNamespace(locks: LockManagerLike | undefined, name: string): Promise<() => Promise<void>> {
  if (locks === undefined) throw new BrowserPersistenceError("persistence_unsupported", "content persistence requires Web Locks for cross-context ownership");
  let release: (() => void) | undefined;
  const held = new Promise<void>((resolve) => { release = resolve; });
  let acquired = false;
  let completion: Promise<void> | undefined;
  await new Promise<void>((resolve, reject) => {
    completion = locks.request(name, { mode: "exclusive", ifAvailable: true }, async (lock) => {
      if (lock === null) { reject(new BrowserPersistenceError("already_open", "content namespace already open")); return; }
      acquired = true;
      resolve();
      await held;
    });
    completion.catch((error: unknown) => { if (!acquired) reject(error); });
  });
  return async () => { release?.(); await completion; };
}
