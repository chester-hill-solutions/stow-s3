import { BrowserPersistenceError } from "./browser-types.js";
interface DatabaseOpenOptions {
  name: string;
  version: number;
  upgrade(database: IDBDatabase): void;
  failedMessage: string;
  blockedMessage: string;
}
export function openIndexedDatabase(factory: IDBFactory, options: DatabaseOpenOptions): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    let state: "pending" | "opened" | "rejected" = "pending";
    const request = factory.open(options.name, options.version);
    request.onupgradeneeded = () => {
      if (state !== "pending") { request.transaction?.abort(); return; }
      options.upgrade(request.result);
    };
    request.onsuccess = () => {
      if (state !== "pending") { request.result.close(); return; }
      state = "opened";
      request.result.onversionchange = () => request.result.close();
      resolve(request.result);
    };
    request.onerror = () => {
      state = "rejected";
      reject(request.error ?? new Error(options.failedMessage));
    };
    request.onblocked = () => {
      state = "rejected";
      reject(new BrowserPersistenceError("persistence_denied", options.blockedMessage));
    };
  });
}
