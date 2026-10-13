import { BrowserPersistenceError } from "./browser-types.js";

export function contentCursorKey(key: IDBValidKey): string {
  if (!Array.isArray(key) || typeof key[1] !== "string") {
    throw new BrowserPersistenceError("persistence_corrupt", "invalid content cursor key");
  }
  return key[1];
}

export function visitContentCursor(
  store: IDBObjectStore, range: IDBKeyRange,
  visit: (cursor: IDBCursorWithValue) => boolean | void | Promise<boolean | void>,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const request = store.openCursor(range);
    request.onerror = () => reject(request.error);
    request.onsuccess = () => {
      const cursor = request.result;
      if (cursor === null) { resolve(); return; }
      Promise.resolve().then(() => visit(cursor)).then((continueVisit) => {
        if (continueVisit === false) resolve();
        else cursor.continue();
      }).catch(reject);
    };
  });
}
