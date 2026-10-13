import { decodeContent, isRecord } from "./project-format.js";
import { EmbeddedStowError } from "./embedded.js";
export class ContentObjectCatalog {
    store;
    headName;
    constructor(store, headName) {
        this.store = store;
        this.headName = headName;
    }
    async load() {
        const head = await this.store.adapter.getHead(this.headName);
        if (!head.recordId)
            return { head, catalog: { formatVersion: 1, buckets: [], objects: [] } };
        const record = await this.store.adapter.getRecord(head.recordId);
        if (!record)
            throw corrupt("object catalogue is missing");
        const value = JSON.parse(new TextDecoder().decode(record.metadata));
        if (!isRecord(value) || value.formatVersion !== 1 || !Array.isArray(value.buckets) || !Array.isArray(value.objects))
            throw corrupt("invalid object catalogue");
        const buckets = value.buckets.map((item) => {
            if (!isRecord(item) || typeof item.name !== "string")
                throw corrupt("invalid bucket metadata");
            return { name: item.name, creationDate: typeof item.creationDate === "string" ? item.creationDate : undefined };
        });
        const objects = value.objects.map((item) => {
            if (!isRecord(item) || typeof item.id !== "string")
                throw corrupt("invalid object entry");
            return { id: item.id, object: decodeObject(item.object) };
        });
        if (new Set(buckets.map((bucket) => bucket.name)).size !== buckets.length ||
            new Set(objects.map((entry) => JSON.stringify([entry.object.bucket, entry.object.key]))).size !== objects.length ||
            JSON.stringify([...record.references].sort()) !== JSON.stringify(objects.map((entry) => entry.id).sort()))
            throw corrupt("object catalogue references are inconsistent");
        return { head, catalog: { formatVersion: 1, buckets, objects } };
    }
    requireBucket(catalog, bucket) {
        if (!catalog.buckets.some((item) => item.name === bucket))
            throw new EmbeddedStowError("bucket_not_found", "bucket is missing");
    }
    entry(catalog, bucket, key) {
        this.requireBucket(catalog, bucket);
        const entry = catalog.objects.find((item) => item.object.bucket === bucket && item.object.key === key);
        if (!entry)
            throw new EmbeddedStowError("object_not_found", "object is missing");
        return entry;
    }
    async content(entry) {
        const record = await this.store.adapter.getRecord(entry.id);
        if (!record || record.contents.length !== 1)
            throw corrupt("object content record is missing");
        const content = record.contents[0];
        if (!content)
            throw corrupt("object content is missing");
        if (content.size !== entry.object.size)
            throw corrupt("object size differs from its content");
        const descriptor = JSON.parse(new TextDecoder().decode(record.metadata));
        if (!isRecord(descriptor) || descriptor.formatVersion !== 1 || objectIdentity(decodeObject(descriptor.object)) !== objectIdentity(entry.object))
            throw corrupt("object metadata differs from its catalogue entry");
        return content;
    }
    async publish(loaded, catalog, stageId = crypto.randomUUID(), records = []) {
        await this.store.adapter.publish({
            head: this.headName, expectedGeneration: loaded.head.generation, stageId,
            record: { id: crypto.randomUUID(), metadata: new TextEncoder().encode(JSON.stringify(catalog)), contents: [], references: catalog.objects.map((entry) => entry.id) },
            additionalRecords: records,
        });
    }
    record(object, content) {
        return { id: crypto.randomUUID(), metadata: new TextEncoder().encode(JSON.stringify({ formatVersion: 1, object })), contents: [content], references: [] };
    }
}
function objectIdentity(object) {
    return JSON.stringify([object.bucket, object.key, object.size, object.etag, object.contentType,
        object.lastModified, Object.entries(object.metadata ?? {}).sort(([a], [b]) => a.localeCompare(b))]);
}
function decodeObject(value) {
    if (!isRecord(value) || typeof value.bucket !== "string" || typeof value.key !== "string" ||
        typeof value.etag !== "string" || typeof value.size !== "number" || !Number.isSafeInteger(value.size) || value.size < 0)
        throw corrupt("invalid object metadata");
    let metadata;
    if (value.metadata !== undefined) {
        if (!isRecord(value.metadata) || !Object.values(value.metadata).every((item) => typeof item === "string"))
            throw corrupt("invalid user metadata");
        metadata = Object.fromEntries(Object.entries(value.metadata).map(([key, item]) => [key, String(item)]));
    }
    return { bucket: value.bucket, key: value.key, size: value.size, etag: value.etag,
        contentType: typeof value.contentType === "string" ? value.contentType : undefined,
        lastModified: typeof value.lastModified === "string" ? value.lastModified : undefined, metadata };
}
export function hostContent(value) { return decodeContent(value); }
function corrupt(message) { return new EmbeddedStowError("persistence_corrupt", message); }
//# sourceMappingURL=content-object-catalog.js.map