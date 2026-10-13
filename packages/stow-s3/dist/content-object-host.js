import { ContentObjectCatalog } from "./content-object-catalog.js";
import { decodeEmbeddedResponse, EmbeddedStowError } from "./embedded.js";
import { isRecord } from "./project-format.js";
const queues = new WeakMap();
/** Host persistence beneath the shared Go object runtime; bytes are never replayed at open. */
export class ContentObjectHost {
    store;
    /** Installed by the Go host-backed runtime to evaluate its shared atomic write predicates. */
    checkConditions;
    catalog;
    puts = new Map();
    reads = new Map();
    closed = false;
    constructor(store, options = {}) {
        this.store = store;
        this.catalog = new ContentObjectCatalog(store, options.name ?? "embedded-object-catalog");
        this.maxStreams = options.maxStreams ?? 16;
        if (!Number.isSafeInteger(this.maxStreams) || this.maxStreams < 1)
            throw new RangeError("maxStreams must be positive");
    }
    maxStreams;
    hasActiveStage(stageId) { return this.puts.has(stageId); }
    call(metadata, data) {
        const captured = new Uint8Array(data);
        const result = (queues.get(this.store) ?? Promise.resolve()).then(async () => {
            try {
                const request = JSON.parse(metadata);
                if (!isRecord(request) || request.version !== 2 || typeof request.op !== "string")
                    throw new EmbeddedStowError("invalid_request", "invalid host request");
                if (this.closed && request.op !== "close")
                    throw new EmbeddedStowError("closed", "object host is closed");
                const output = await this.dispatch(request, captured);
                return { metadata: JSON.stringify({ version: 2, ok: true, result: output.result }), data: output.data };
            }
            catch (error) {
                const code = error instanceof EmbeddedStowError ? error.code : error instanceof Error && "code" in error ? String(error.code) : "persistence_error";
                return { metadata: JSON.stringify({ version: 2, ok: false, error: { code, message: error instanceof Error ? error.message : "host storage failed" } }) };
            }
        });
        queues.set(this.store, result.then(() => undefined, () => undefined));
        return result;
    }
    async dispatch(request, data) {
        const op = String(request.op);
        if (op === "startPut")
            return { result: this.startPut(request) };
        if (op === "writePut") {
            await this.writePut(request, data);
            return { result: null };
        }
        if (op === "commitPut")
            return { result: await this.commitPut(request) };
        if (op === "abortPut") {
            const token = field(request, "token");
            await this.store.adapter.releaseStage(token);
            this.puts.delete(token);
            return { result: null };
        }
        if (op === "readObject")
            return { result: null, data: await this.readObject(request) };
        if (op === "closeObject") {
            await this.closeRead(field(request, "token"));
            return { result: null };
        }
        if (op === "close") {
            await this.close();
            return { result: null };
        }
        if (op === "openObject")
            return { result: await this.openObject(request) };
        return { result: await this.catalogOperation(op, request) };
    }
    async catalogOperation(op, request) {
        const loaded = await this.catalog.load();
        const catalog = loaded.catalog;
        if (op === "listBuckets")
            return { buckets: catalog.buckets };
        const bucket = field(request, op === "copyObject" ? "destinationBucket" : "bucket");
        if (op === "createBucket") {
            if (catalog.buckets.some((item) => item.name === bucket))
                throw new EmbeddedStowError("bucket_exists", "bucket exists");
            catalog.buckets.push({ name: bucket, creationDate: new Date().toISOString() });
        }
        else if (op === "deleteBucket") {
            this.catalog.requireBucket(catalog, bucket);
            if (catalog.objects.some((entry) => entry.object.bucket === bucket))
                throw new EmbeddedStowError("bucket_not_empty", "bucket is not empty");
            catalog.buckets = catalog.buckets.filter((item) => item.name !== bucket);
        }
        else if (op === "listObjects")
            return this.listObjects(catalog, bucket, request.list);
        else if (op === "headObject")
            return this.catalog.entry(catalog, bucket, field(request, "key")).object;
        else if (op === "deleteObject") {
            this.catalog.requireBucket(catalog, bucket);
            const key = field(request, "key");
            catalog.objects = catalog.objects.filter((entry) => entry.object.bucket !== bucket || entry.object.key !== key);
        }
        else if (op === "copyObject") {
            this.catalog.requireBucket(catalog, bucket);
            const source = this.catalog.entry(catalog, field(request, "sourceBucket"), field(request, "sourceKey"));
            const object = { ...source.object, bucket, key: field(request, "destinationKey"), lastModified: new Date().toISOString() };
            const record = this.catalog.record(object, await this.catalog.content(source));
            catalog.objects = replaceEntry(catalog, { id: record.id, object });
            await this.catalog.publish(loaded, catalog, crypto.randomUUID(), [record]);
            return object;
        }
        else
            throw new EmbeddedStowError("invalid_request", `unknown host operation ${op}`);
        await this.catalog.publish(loaded, catalog);
        return null;
    }
    listObjects(catalog, bucket, raw) {
        this.catalog.requireBucket(catalog, bucket);
        const options = isRecord(raw) ? raw : {};
        const prefix = typeof options.prefix === "string" ? options.prefix : "";
        const after = typeof options.cursor === "string" ? options.cursor : "";
        const limit = typeof options.limit === "number" && options.limit > 0 ? options.limit : 1000;
        const all = catalog.objects.filter((entry) => entry.object.bucket === bucket && entry.object.key.startsWith(prefix) && entry.object.key > after).map((entry) => entry.object).sort((a, b) => a.key < b.key ? -1 : a.key > b.key ? 1 : 0);
        const objects = all.slice(0, limit);
        return { objects, truncated: all.length > limit, nextCursor: all.length > limit ? objects.at(-1)?.key : undefined };
    }
    startPut(request) {
        this.checkStreamLimit();
        const token = crypto.randomUUID();
        this.puts.set(token, { bucket: field(request, "bucket"), key: field(request, "key"), request, content: { size: 0, chunks: [] } });
        return { token };
    }
    async writePut(request, data) {
        const token = field(request, "token");
        const session = this.puts.get(token);
        if (!session || data.byteLength > 256 * 1024 || numberField(request, "offset") !== session.content.size)
            throw new EmbeddedStowError("invalid_request", "invalid staged write");
        const staged = await this.store.stage(data, { stageId: token });
        session.content.chunks.push(...staged.content.chunks);
        session.content.size += data.byteLength;
    }
    async commitPut(request) {
        const token = field(request, "token");
        const session = this.puts.get(token);
        if (!session || numberField(request, "size") !== session.content.size)
            throw new EmbeddedStowError("invalid_request", "staged size differs");
        const loaded = await this.catalog.load();
        this.catalog.requireBucket(loaded.catalog, session.bucket);
        const previous = loaded.catalog.objects.find((entry) => entry.object.bucket === session.bucket && entry.object.key === session.key);
        const ifMatch = session.request.ifMatch;
        const ifNoneMatch = session.request.ifNoneMatch;
        if (ifMatch || ifNoneMatch) {
            if (!this.checkConditions)
                throw new EmbeddedStowError("conditional_write_unsupported", "conditional writes require the shared runtime predicate");
            const result = JSON.parse(this.checkConditions(JSON.stringify({ ifMatch, ifNoneMatch, existing: previous ? { etag: previous.object.etag } : null })));
            decodeEmbeddedResponse(result, 2);
        }
        const object = { bucket: session.bucket, key: session.key, size: session.content.size, etag: field(request, "etag"), lastModified: field(request, "lastModified") };
        if (typeof session.request.contentType === "string")
            object.contentType = session.request.contentType;
        if (isRecord(session.request.metadata))
            object.metadata = Object.fromEntries(Object.entries(session.request.metadata).map(([key, value]) => [key, String(value)]));
        const record = this.catalog.record(object, session.content);
        loaded.catalog.objects = replaceEntry(loaded.catalog, { id: record.id, object });
        await this.catalog.publish(loaded, loaded.catalog, token, [record]);
        this.puts.delete(token);
        return object;
    }
    async openObject(request) {
        this.checkStreamLimit();
        const loaded = await this.catalog.load();
        const entry = this.catalog.entry(loaded.catalog, field(request, "bucket"), field(request, "key"));
        const content = await this.catalog.content(entry);
        const token = crypto.randomUUID();
        await this.store.adapter.protect(token, [entry.id], { lifetime: "handle" });
        this.reads.set(token, { recordId: entry.id, content });
        return { token, object: entry.object };
    }
    async readObject(request) {
        const session = this.reads.get(field(request, "token"));
        const start = numberField(request, "offset");
        const length = numberField(request, "length");
        if (!session || start < 0 || length < 0 || length > 256 * 1024 || start > session.content.size)
            throw new EmbeddedStowError("invalid_request", "invalid stream range");
        return this.store.readRange(session.content, start, Math.min(start + length, session.content.size));
    }
    async closeRead(token) { await this.store.adapter.protect(token, [], { lifetime: "handle" }); this.reads.delete(token); }
    checkStreamLimit() {
        if (this.puts.size + this.reads.size >= this.maxStreams)
            throw new EmbeddedStowError("quota_exceeded", "too many open streams");
    }
    async close() {
        for (const token of this.puts.keys())
            await this.store.adapter.releaseStage(token);
        for (const token of this.reads.keys())
            await this.closeRead(token);
        this.puts.clear();
        this.closed = true;
    }
}
function field(request, key) {
    const value = request[key];
    if (typeof value !== "string" || !value)
        throw new EmbeddedStowError("invalid_request", `missing ${key}`);
    return value;
}
function numberField(request, key) {
    const value = request[key] ?? 0;
    if (typeof value !== "number" || !Number.isSafeInteger(value))
        throw new EmbeddedStowError("invalid_request", `invalid ${key}`);
    return value;
}
function replaceEntry(catalog, entry) {
    return [...catalog.objects.filter((item) => item.object.bucket !== entry.object.bucket || item.object.key !== entry.object.key), entry];
}
//# sourceMappingURL=content-object-host.js.map