import { BrowserPersistenceError } from "./browser-types.js";
export async function ownContentNamespace(locks, name) {
    if (locks === undefined)
        throw new BrowserPersistenceError("persistence_unsupported", "content persistence requires Web Locks for cross-context ownership");
    let release;
    const held = new Promise((resolve) => { release = resolve; });
    let acquired = false;
    let completion;
    await new Promise((resolve, reject) => {
        completion = locks.request(name, { mode: "exclusive", ifAvailable: true }, async (lock) => {
            if (lock === null) {
                reject(new BrowserPersistenceError("already_open", "content namespace already open"));
                return;
            }
            acquired = true;
            resolve();
            await held;
        });
        completion.catch((error) => { if (!acquired)
            reject(error); });
    });
    return async () => { release?.(); await completion; };
}
//# sourceMappingURL=content-ownership.js.map