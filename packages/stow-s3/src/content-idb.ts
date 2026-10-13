import { contentCursorKey, visitContentCursor } from "./content-cursor.js";
import { openIndexedDatabase } from "./indexeddb-lifecycle.js";
import { BrowserPersistenceError } from "./browser-types.js";
import type { ContentRecord } from "./content-types.js";
import { validateContent } from "./content-store.js";

export const CONTENT_STORES = ["namespace", "chunks", "payloads", "records", "heads", "pins", "gcRecords", "gcChunks"];
export function contentDatabase(factory: IDBFactory, name: string): Promise<IDBDatabase> {
  return openIndexedDatabase(factory, {
    name, version: 2, failedMessage: "content database open failed", blockedMessage: "content database upgrade blocked",
    upgrade: (database) => {
      for (const name of CONTENT_STORES) {
        if (database.objectStoreNames.contains(name)) continue;
        const store = database.createObjectStore(name);
        if (name === "gcRecords") store.createIndex("pending", ["namespace", "processed"]);
      }
    },
  });
}

export function requestValue<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

export function contentTransaction<T>(database: IDBDatabase, mode: IDBTransactionMode, run: (transaction: IDBTransaction) => Promise<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    const transaction = database.transaction(CONTENT_STORES, mode);
    let value: T;
    let failure: unknown;
    transaction.oncomplete = () => resolve(value);
    transaction.onerror = () => reject(failure ?? mapContentError(transaction.error));
    transaction.onabort = () => reject(failure ?? mapContentError(transaction.error));
    run(transaction).then((result) => { value = result; }).catch((error: unknown) => {
      failure = mapContentError(error);
      try { transaction.abort(); } catch { reject(failure); }
    });
  });
}

export function namespaceRange(namespace: string, after?: string): IDBKeyRange {
  return IDBKeyRange.bound([namespace, after ?? ""], [namespace, []], after !== undefined, true);
}

export async function rows<T>(store: IDBObjectStore, range: IDBKeyRange, decode: (value: unknown) => T, limit = Number.MAX_SAFE_INTEGER): Promise<{ key: string; value: T }[]> {
  const result: { key: string; value: T }[] = [];
  await visitContentCursor(store, range, (cursor) => {
    if (result.length >= limit) return false;
    result.push({ key: contentCursorKey(cursor.key), value: decode(cursor.value) });
  });
  return result;
}

export function decodeRecord(value: unknown): ContentRecord {
  if (!isObject(value) || typeof value.id !== "string" || !(value.metadata instanceof Uint8Array) ||
      !Array.isArray(value.contents) || !Array.isArray(value.references) ||
      !value.references.every((entry: unknown) => typeof entry === "string")) throw corrupt("invalid content record");
  const contents = value.contents.map((entry: unknown) => {
    if (!isObject(entry) || typeof entry.size !== "number" || !Array.isArray(entry.chunks)) throw corrupt("invalid content reference");
    const chunks = entry.chunks.map((chunk: unknown) => {
      if (!isObject(chunk) || typeof chunk.hash !== "string" || typeof chunk.size !== "number") throw corrupt("invalid chunk reference");
      return { hash: chunk.hash, size: chunk.size };
    });
    const content = { size: entry.size, chunks };
    validateContent(content);
    return content;
  });
  return { id: value.id, metadata: new Uint8Array(value.metadata), references: [...value.references], contents };
}

export function decodeStrings(value: unknown): string[] {
  if (!Array.isArray(value) || !value.every((entry: unknown) => typeof entry === "string")) throw corrupt("invalid content pins");
  return value;
}
export function decodeBytes(value: unknown): Uint8Array {
  if (!(value instanceof Uint8Array)) throw corrupt("invalid stored content bytes");
  return value;
}
export function isObject(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null; }
export function corrupt(message: string): BrowserPersistenceError { return new BrowserPersistenceError("persistence_corrupt", message); }
export function mapContentError(error: unknown): BrowserPersistenceError {
  if (error instanceof BrowserPersistenceError) return error;
  if (isObject(error) && error.name === "QuotaExceededError") return new BrowserPersistenceError("quota_exceeded", "content quota exceeded");
  return new BrowserPersistenceError("persistence_error", error instanceof Error ? error.message : String(error));
}
