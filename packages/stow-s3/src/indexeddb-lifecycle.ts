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
    const request = factory.open(options.name, options.version);
    request.onupgradeneeded = () => options.upgrade(request.result);
    request.onsuccess = () => {
      request.result.onversionchange = () => request.result.close();
      resolve(request.result);
    };
    request.onerror = () => reject(request.error ?? new Error(options.failedMessage));
    request.onblocked = () => reject(new BrowserPersistenceError("persistence_denied", options.blockedMessage));
  });
}
