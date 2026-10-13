import { contentCursorKey, visitContentCursor } from "./content-cursor.js";
import { openIndexedDatabase } from "./indexeddb-lifecycle.js";
import { BrowserPersistenceError } from "./browser-types.js";
import { validateContent } from "./content-store.js";
export const CONTENT_STORES = ["namespace", "chunks", "payloads", "records", "heads", "pins", "gcRecords", "gcChunks"];
export function contentDatabase(factory, name) {
    return openIndexedDatabase(factory, {
        name, version: 2, failedMessage: "content database open failed", blockedMessage: "content database upgrade blocked",
        upgrade: (database) => {
            for (const name of CONTENT_STORES) {
                if (database.objectStoreNames.contains(name))
                    continue;
                const store = database.createObjectStore(name);
                if (name === "gcRecords")
                    store.createIndex("pending", ["namespace", "processed"]);
            }
        },
    });
}
export function requestValue(request) {
    return new Promise((resolve, reject) => {
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
    });
}
export function contentTransaction(database, mode, run) {
    return new Promise((resolve, reject) => {
        const transaction = database.transaction(CONTENT_STORES, mode);
        let value;
        let failure;
        transaction.oncomplete = () => resolve(value);
        transaction.onerror = () => reject(failure ?? mapContentError(transaction.error));
        transaction.onabort = () => reject(failure ?? mapContentError(transaction.error));
        run(transaction).then((result) => { value = result; }).catch((error) => {
            failure = mapContentError(error);
            try {
                transaction.abort();
            }
            catch {
                reject(failure);
            }
        });
    });
}
export function namespaceRange(namespace, after) {
    return IDBKeyRange.bound([namespace, after ?? ""], [namespace, []], after !== undefined, true);
}
export async function rows(store, range, decode, limit = Number.MAX_SAFE_INTEGER) {
    const result = [];
    await visitContentCursor(store, range, (cursor) => {
        if (result.length >= limit)
            return false;
        result.push({ key: contentCursorKey(cursor.key), value: decode(cursor.value) });
    });
    return result;
}
export function decodeRecord(value) {
    if (!isObject(value) || typeof value.id !== "string" || !(value.metadata instanceof Uint8Array) ||
        !Array.isArray(value.contents) || !Array.isArray(value.references) ||
        !value.references.every((entry) => typeof entry === "string"))
        throw corrupt("invalid content record");
    const contents = value.contents.map((entry) => {
        if (!isObject(entry) || typeof entry.size !== "number" || !Array.isArray(entry.chunks))
            throw corrupt("invalid content reference");
        const chunks = entry.chunks.map((chunk) => {
            if (!isObject(chunk) || typeof chunk.hash !== "string" || typeof chunk.size !== "number")
                throw corrupt("invalid chunk reference");
            return { hash: chunk.hash, size: chunk.size };
        });
        const content = { size: entry.size, chunks };
        validateContent(content);
        return content;
    });
    return { id: value.id, metadata: new Uint8Array(value.metadata), references: [...value.references], contents };
}
export function decodeStrings(value) {
    if (!Array.isArray(value) || !value.every((entry) => typeof entry === "string"))
        throw corrupt("invalid content pins");
    return value;
}
export function decodeBytes(value) {
    if (!(value instanceof Uint8Array))
        throw corrupt("invalid stored content bytes");
    return value;
}
export function isObject(value) { return typeof value === "object" && value !== null; }
export function corrupt(message) { return new BrowserPersistenceError("persistence_corrupt", message); }
export function mapContentError(error) {
    if (error instanceof BrowserPersistenceError)
        return error;
    if (isObject(error) && error.name === "QuotaExceededError")
        return new BrowserPersistenceError("quota_exceeded", "content quota exceeded");
    return new BrowserPersistenceError("persistence_error", error instanceof Error ? error.message : String(error));
}
//# sourceMappingURL=content-idb.js.map