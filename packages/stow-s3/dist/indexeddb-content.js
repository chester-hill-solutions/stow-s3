import { bookkeepingSize, deleteHandlePins, deleteStagePins, putPins } from "./content-accounting.js";
import { BrowserPersistenceError, validatePersistenceNamespace } from "./browser-types.js";
import { MAX_CONTENT_RECORD_BYTES } from "./content-types.js";
import { contentHash } from "./content-store.js";
import { contentDatabase, contentTransaction, corrupt, decodeBytes, decodeRecord, decodeStrings, namespaceRange, requestValue, rows } from "./content-idb.js";
import { collectContent, decodeHead, decodeSize, decodeUsage, recordBytes, writeUsage } from "./content-gc.js";
import { ownContentNamespace } from "./content-ownership.js";
export class IndexedDbContentAdapter {
    options;
    database;
    namespace;
    release;
    closed = false;
    quota;
    constructor(options = {}) {
        this.options = options;
        this.quota = options.quotaBytes ?? 1024 * 1024 * 1024;
        if (!Number.isSafeInteger(this.quota) || this.quota < 0)
            throw new RangeError("quotaBytes must be a non-negative integer");
    }
    async open(namespace) {
        validatePersistenceNamespace(namespace);
        if (this.closed)
            throw new BrowserPersistenceError("closed", "content adapter closed");
        if (this.release !== undefined)
            throw new BrowserPersistenceError("already_open", "content adapter already open");
        const factory = this.options.indexedDB ?? globalThis.indexedDB;
        if (factory === undefined)
            throw new BrowserPersistenceError("persistence_unsupported", "IndexedDB is unavailable");
        const name = this.options.databaseName ?? "chs-stow-content";
        this.release = await ownContentNamespace(this.options.lockManager ?? globalThis.navigator?.locks, `stow-content:${name}:${namespace}`);
        try {
            this.database = await contentDatabase(factory, name);
            this.namespace = namespace;
            await this.run("readwrite", async (transaction, namespace) => {
                const usage = decodeUsage(await requestValue(transaction.objectStore("namespace").get(namespace)));
                await deleteHandlePins(transaction, namespace, usage);
                writeUsage(transaction, namespace, usage);
            });
        }
        catch (error) {
            await this.close();
            throw error;
        }
    }
    getHead(name) {
        return this.run("readonly", async (transaction, namespace) => {
            const value = await requestValue(transaction.objectStore("heads").get([namespace, name]));
            return value === undefined ? { name, generation: 0, recordId: null } : decodeHead(value);
        });
    }
    getRecord(id) {
        return this.run("readonly", async (transaction, namespace) => {
            const value = await requestValue(transaction.objectStore("records").get([namespace, id]));
            return value === undefined ? null : decodeRecord(value);
        });
    }
    listRecords(options = {}) {
        const limit = options.limit ?? 100;
        if (!Number.isSafeInteger(limit) || limit < 1 || limit > 1000)
            throw new RangeError("page limit must be between 1 and 1000");
        return this.run("readonly", async (transaction, namespace) => {
            const values = await rows(transaction.objectStore("records"), namespaceRange(namespace, options.after), decodeRecord, limit + 1);
            return { records: values.slice(0, limit).map(({ value }) => value), next: values.length > limit ? values[limit - 1]?.key : undefined };
        });
    }
    getChunk(hash) {
        validateHash(hash);
        return this.run("readonly", async (transaction, namespace) => {
            const value = await requestValue(transaction.objectStore("payloads").get([namespace, hash]));
            return value === undefined ? null : decodeBytes(value);
        });
    }
    async stageChunk(stageId, hash, bytes) {
        validateHash(hash);
        validateId(stageId);
        const data = new Uint8Array(bytes);
        if (await contentHash(data) !== hash)
            throw corrupt("staged chunk hash differs");
        await this.run("readwrite", async (transaction, namespace) => {
            const chunks = transaction.objectStore("chunks");
            const [old, usageValue] = await Promise.all([
                requestValue(chunks.get([namespace, hash])),
                requestValue(transaction.objectStore("namespace").get(namespace)),
            ]);
            const usage = decodeUsage(usageValue);
            if (old === undefined) {
                usage.physicalBytes += data.byteLength;
                usage.chunks++;
                chunks.put(data.byteLength, [namespace, hash]);
                transaction.objectStore("payloads").put(data, [namespace, hash]);
            }
            else if (decodeSize(old) !== data.byteLength)
                throw corrupt("immutable chunk collision");
            await putPins(transaction, namespace, `stage:${stageId}:${hash}`, [hash], usage);
            this.checkQuota(usage);
            writeUsage(transaction, namespace, usage);
        });
    }
    listStages() {
        return this.run("readonly", async (transaction, namespace) => {
            const pins = await rows(transaction.objectStore("pins"), namespaceRange(namespace), decodeStrings);
            const stages = new Set();
            for (const { key } of pins)
                if (key.startsWith("stage:"))
                    stages.add(key.slice(6, key.lastIndexOf(":")));
            return [...stages].sort();
        });
    }
    releaseStage(stageId) {
        validateId(stageId);
        return this.run("readwrite", async (transaction, namespace) => {
            const usage = decodeUsage(await requestValue(transaction.objectStore("namespace").get(namespace)));
            await deleteStagePins(transaction, namespace, stageId, usage);
            writeUsage(transaction, namespace, usage);
        });
    }
    publish(publication) {
        const settings = { ...publication };
        const targetId = publication.record.id;
        const records = [...settings.additionalRecords ?? [], settings.record].map(decodeRecord);
        for (const record of records) {
            validateId(record.id);
            if (recordBytes(record) > MAX_CONTENT_RECORD_BYTES)
                throw new RangeError("record metadata and references exceed 1 MiB");
        }
        if (!Number.isSafeInteger(settings.expectedGeneration) || settings.expectedGeneration < 0)
            throw new RangeError("invalid generation");
        return this.run("readwrite", async (transaction, namespace) => {
            const value = await requestValue(transaction.objectStore("heads").get([namespace, settings.head]));
            const current = value === undefined ? 0 : decodeHead(value).generation;
            if (current !== settings.expectedGeneration)
                throw new BrowserPersistenceError("persistence_error", "content head generation conflict");
            const usage = decodeUsage(await requestValue(transaction.objectStore("namespace").get(namespace)));
            await publishRecords(transaction, namespace, records, usage);
            const head = { name: settings.head, generation: current + 1, recordId: targetId };
            if (bookkeepingSize(settings.head, head) > MAX_CONTENT_RECORD_BYTES)
                throw new RangeError("content head metadata exceeds 1 MiB");
            usage.bookkeepingBytes = (usage.bookkeepingBytes ?? 0) + bookkeepingSize(settings.head, head) -
                (value === undefined ? 0 : bookkeepingSize(settings.head, decodeHead(value)));
            await deleteStagePins(transaction, namespace, settings.stageId, usage);
            this.checkQuota(usage);
            transaction.objectStore("heads").put(head, [namespace, settings.head]);
            writeUsage(transaction, namespace, usage);
            return head;
        });
    }
    listProtections() {
        return this.run("readonly", async (transaction, namespace) => {
            const pins = await rows(transaction.objectStore("pins"), namespaceRange(namespace), decodeStrings);
            const protections = [];
            for (const { key, value } of pins) {
                if (key.startsWith("stage:"))
                    continue;
                protections.push({ name: key.slice(key.indexOf(":") + 1), recordIds: value,
                    lifetime: key.startsWith("handle:") ? "handle" : "durable" });
            }
            return protections;
        });
    }
    protect(name, recordIds, options = {}) {
        const ids = [...recordIds];
        const key = `${options.lifetime === "handle" ? "handle" : "record"}:${name}`;
        return this.run("readwrite", async (transaction, namespace) => {
            for (const id of ids) {
                if (await requestValue(transaction.objectStore("records").get([namespace, id])) === undefined)
                    throw corrupt("protected record missing");
            }
            const usage = decodeUsage(await requestValue(transaction.objectStore("namespace").get(namespace)));
            await putPins(transaction, namespace, key, ids, usage);
            this.checkQuota(usage);
            writeUsage(transaction, namespace, usage);
        });
    }
    collect(options = {}) {
        return this.run("readwrite", (transaction, namespace) => collectContent(transaction, namespace, options.retainRecordIds ?? []));
    }
    usage() {
        return this.run("readonly", async (transaction, namespace) => decodeUsage(await requestValue(transaction.objectStore("namespace").get(namespace))));
    }
    async close() {
        this.closed = true;
        this.database?.close();
        this.database = undefined;
        const release = this.release;
        this.release = undefined;
        await release?.();
    }
    run(mode, operation) {
        if (this.closed)
            throw new BrowserPersistenceError("closed", "content adapter closed");
        if (this.database === undefined || this.namespace === undefined)
            throw new BrowserPersistenceError("persistence_error", "content adapter not open");
        const namespace = this.namespace;
        return contentTransaction(this.database, mode, (transaction) => operation(transaction, namespace));
    }
    checkQuota(usage) {
        if (usage.physicalBytes + (usage.bookkeepingBytes ?? 0) > this.quota)
            throw new BrowserPersistenceError("quota_exceeded", "namespace content quota exceeded");
    }
}
async function publishRecords(transaction, namespace, records, usage) {
    const store = transaction.objectStore("records");
    const newIds = new Set(records.map((record) => record.id));
    if (newIds.size !== records.length)
        throw corrupt("duplicate publication record id");
    for (const record of records) {
        const previous = await requestValue(store.get([namespace, record.id]));
        if (previous !== undefined)
            throw corrupt("immutable content record id already exists");
        for (const reference of record.references) {
            if (!newIds.has(reference) && await requestValue(store.get([namespace, reference])) === undefined)
                throw corrupt("referenced record missing");
        }
        await verifyChunks(transaction, namespace, record);
        store.put(record, [namespace, record.id]);
        usage.physicalBytes += recordBytes(record);
        usage.records++;
    }
}
function validateHash(hash) { if (!/^[a-f0-9]{64}$/.test(hash))
    throw new RangeError("invalid content hash"); }
function validateId(id) { if (id.length === 0 || id.includes(":"))
    throw new RangeError("content id must be nonempty without colons"); }
async function verifyChunks(transaction, namespace, record) {
    for (const content of record.contents) {
        for (const chunk of content.chunks) {
            const size = await requestValue(transaction.objectStore("chunks").get([namespace, chunk.hash]));
            if (size === undefined || decodeSize(size) !== chunk.size)
                throw corrupt("referenced chunk missing");
        }
    }
}
//# sourceMappingURL=indexeddb-content.js.map