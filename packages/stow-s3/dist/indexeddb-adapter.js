import { openIndexedDatabase } from "./indexeddb-lifecycle.js";
const DATABASE_VERSION = 1;
const DEFAULT_DATABASE_NAME = "chs-stow-adapter";
const OBJECT_STORE = "objects";
const MAX_KEYS_DEFAULT = 1000;
export class IndexedDbAdapterError extends Error {
    code;
    constructor(code, message) {
        super(message);
        this.name = "IndexedDbAdapterError";
        this.code = code;
    }
}
export class IndexedDbAdapter {
    database;
    now;
    closed = false;
    constructor(database, now) {
        this.database = database;
        this.now = now;
    }
    static async open(options = {}) {
        const factory = options.indexedDB ?? globalIndexedDb();
        const database = await openIndexedDatabase(factory, {
            name: options.databaseName ?? DEFAULT_DATABASE_NAME,
            version: DATABASE_VERSION,
            failedMessage: "IndexedDB open failed",
            blockedMessage: "IndexedDB open was blocked",
            upgrade: (database) => {
                if (!database.objectStoreNames.contains(OBJECT_STORE)) {
                    database.createObjectStore(OBJECT_STORE, { keyPath: ["bucket", "key"] });
                }
            },
        });
        return new IndexedDbAdapter(database, options.now ?? (() => Date.now()));
    }
    async get(bucket, key) {
        const record = await this.read(bucket, key);
        if (record === undefined) {
            throw new IndexedDbAdapterError("not_found", `object not found: ${bucket}/${key}`);
        }
        return { metadata: metadataOf(record), data: new Uint8Array(record.data.slice(0)) };
    }
    async put(bucket, key, data, options = {}) {
        return this.write(bucket, key, data, options, undefined);
    }
    async conditionalPut(bucket, key, data, options) {
        return this.write(bucket, key, data, options, options.condition);
    }
    async delete(bucket, key) {
        this.assertOpen();
        await new Promise((resolve, reject) => {
            const transaction = this.database.transaction(OBJECT_STORE, "readwrite");
            const store = transaction.objectStore(OBJECT_STORE);
            let missing = false;
            const request = store.get([bucket, key]);
            request.onsuccess = () => {
                if (request.result === undefined) {
                    missing = true;
                    return;
                }
                store.delete([bucket, key]);
            };
            transaction.oncomplete = () => {
                if (missing)
                    reject(new IndexedDbAdapterError("not_found", `object not found: ${bucket}/${key}`));
                else
                    resolve();
            };
            transaction.onerror = () => reject(transaction.error ?? new Error("IndexedDB delete failed"));
            transaction.onabort = () => reject(transaction.error ?? new Error("IndexedDB delete aborted"));
        });
    }
    async list(bucket, options = {}) {
        this.assertOpen();
        const prefix = options.prefix ?? "";
        const delimiter = options.delimiter ?? "";
        const limit = options.maxKeys && options.maxKeys > 0 ? options.maxKeys : MAX_KEYS_DEFAULT;
        const range = listRange(bucket, prefix, options);
        return new Promise((resolve, reject) => {
            const transaction = this.database.transaction(OBJECT_STORE, "readonly");
            const store = transaction.objectStore(OBJECT_STORE);
            const objects = [];
            const prefixes = [];
            let lastKey = "";
            let lastGroup = "";
            let truncated = false;
            const request = store.openCursor(range);
            request.onsuccess = () => {
                const cursor = request.result;
                if (cursor === null)
                    return;
                const record = cursor.value;
                if (objects.length + prefixes.length >= limit) {
                    truncated = true;
                    return;
                }
                const group = delimiter === "" ? "" : groupOf(record.key, prefix, delimiter);
                if (group !== "") {
                    if (!prefixes.includes(group))
                        prefixes.push(group);
                    lastGroup = group;
                    lastKey = "";
                    cursor.continue([bucket, group + "\uffff"]);
                    return;
                }
                objects.push(metadataOf(record));
                lastKey = record.key;
                lastGroup = "";
                cursor.continue();
            };
            transaction.oncomplete = () => {
                resolve({
                    objects,
                    commonPrefixes: prefixes,
                    isTruncated: truncated,
                    nextContinuationToken: truncated ? encodeContinuation(lastKey, lastGroup) : "",
                });
            };
            transaction.onerror = () => reject(transaction.error ?? new Error("IndexedDB list failed"));
            transaction.onabort = () => reject(transaction.error ?? new Error("IndexedDB list aborted"));
        });
    }
    close() {
        if (this.closed)
            return;
        this.closed = true;
        this.database.close();
    }
    async read(bucket, key) {
        this.assertOpen();
        return new Promise((resolve, reject) => {
            const transaction = this.database.transaction(OBJECT_STORE, "readonly");
            const request = transaction.objectStore(OBJECT_STORE).get([bucket, key]);
            request.onsuccess = () => resolve(request.result);
            request.onerror = () => reject(request.error ?? new Error("IndexedDB read failed"));
        });
    }
    write(bucket, key, data, options, condition) {
        this.assertOpen();
        return digest(data).then((etag) => new Promise((resolve, reject) => {
            const transaction = this.database.transaction(OBJECT_STORE, "readwrite");
            const store = transaction.objectStore(OBJECT_STORE);
            const record = {
                bucket, key, data: data.slice().buffer, etag, size: data.byteLength, modified: this.now(),
                contentType: options.contentType ?? "", metadata: { ...(options.metadata ?? {}) },
            };
            let refusal;
            const publish = () => {
                if (refusal !== undefined)
                    return;
                store.put(record);
            };
            if (condition === undefined) {
                publish();
            }
            else {
                const current = store.get([bucket, key]);
                current.onsuccess = () => {
                    const existing = current.result;
                    if (condition.ifAbsent === true && existing !== undefined) {
                        refusal = new IndexedDbAdapterError("precondition_failed", `object already exists: ${bucket}/${key}`);
                    }
                    else if (condition.ifMatch !== undefined && existing?.etag !== condition.ifMatch) {
                        refusal = new IndexedDbAdapterError("precondition_failed", `entity tag does not match: ${bucket}/${key}`);
                    }
                    publish();
                };
            }
            transaction.oncomplete = () => {
                if (refusal !== undefined)
                    reject(refusal);
                else
                    resolve(metadataOf(record));
            };
            transaction.onerror = () => reject(transaction.error ?? new Error("IndexedDB write failed"));
            transaction.onabort = () => reject(transaction.error ?? new Error("IndexedDB write aborted"));
        }));
    }
    assertOpen() {
        if (this.closed)
            throw new IndexedDbAdapterError("closed", "persistence adapter is closed");
    }
}
function globalIndexedDb() {
    const factory = globalThis.indexedDB;
    if (factory === undefined) {
        throw new IndexedDbAdapterError("unsupported", "IndexedDB is not available in this host");
    }
    return factory;
}
function metadataOf(record) {
    return {
        key: record.key,
        size: record.size,
        etag: record.etag,
        modified: new Date(record.modified).toISOString(),
        contentType: record.contentType,
        metadata: { ...record.metadata },
    };
}
function groupOf(key, prefix, delimiter) {
    const relative = key.slice(prefix.length);
    const index = relative.indexOf(delimiter);
    return index === -1 ? "" : prefix + relative.slice(0, index + delimiter.length);
}
function listRange(bucket, prefix, options) {
    // Every range carries the prefix's upper bound. A continuation used to drop
    // it, so page two of a prefixed listing walked past the prefix and returned
    // keys the caller never asked for. The group skip keeps the same bound.
    const upper = [bucket, prefix + "\uffff"];
    const continuation = decodeContinuation(options.continuationToken);
    const after = continuation.after ?? options.startAfter;
    if (continuation.group !== undefined) {
        return IDBKeyRange.bound([bucket, continuation.group + "\uffff"], upper, true);
    }
    if (after !== undefined && after !== "") {
        return IDBKeyRange.bound([bucket, after], upper, true);
    }
    return IDBKeyRange.bound([bucket, prefix], upper);
}
function encodeContinuation(after, group) {
    const value = group !== "" ? { group } : { after };
    return btoa(JSON.stringify(value)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
function decodeContinuation(token) {
    if (token === undefined || token === "")
        return {};
    const normalized = token.replace(/-/g, "+").replace(/_/g, "/");
    try {
        const parsed = JSON.parse(atob(normalized));
        if (typeof parsed !== "object" || parsed === null)
            throw new Error("not an object");
        const value = parsed;
        if (typeof value.after !== "string" && typeof value.group !== "string")
            throw new Error("empty continuation");
        return { after: typeof value.after === "string" ? value.after : undefined, group: typeof value.group === "string" ? value.group : undefined };
    }
    catch {
        throw new IndexedDbAdapterError("invalid", "continuation token is not valid for this adapter");
    }
}
async function digest(data) {
    const subtle = globalThis.crypto?.subtle;
    if (subtle === undefined) {
        throw new IndexedDbAdapterError("unsupported", "SHA-256 is not available in this host");
    }
    const buffer = await subtle.digest("SHA-256", data.slice().buffer);
    return [...new Uint8Array(buffer)].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}
//# sourceMappingURL=indexeddb-adapter.js.map