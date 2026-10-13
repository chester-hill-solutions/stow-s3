import { corrupt, isObject } from "./content-idb.js";
export function decodeHead(value) {
    if (!isObject(value) || typeof value.name !== "string" || !Number.isSafeInteger(value.generation) ||
        typeof value.generation !== "number" || value.generation < 0 ||
        (value.recordId !== null && typeof value.recordId !== "string"))
        throw corrupt("invalid content head");
    return { name: value.name, generation: value.generation, recordId: value.recordId };
}
export function decodeSize(value) {
    if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0)
        throw corrupt("invalid content size");
    return value;
}
export function decodeUsage(value) {
    if (value === undefined)
        return { physicalBytes: 0, bookkeepingBytes: 0, chunks: 0, records: 0 };
    if (!isObject(value) || value.version !== 1)
        throw corrupt("invalid content namespace format");
    return { physicalBytes: decodeSize(value.physicalBytes), bookkeepingBytes: decodeSize(value.bookkeepingBytes ?? 0), chunks: decodeSize(value.chunks), records: decodeSize(value.records) };
}
export function writeUsage(transaction, namespace, usage) {
    transaction.objectStore("namespace").put({ version: 1, ...usage }, namespace);
}
export function recordBytes(record) {
    return record.metadata.byteLength + new TextEncoder().encode(JSON.stringify({ id: record.id, contents: record.contents, references: record.references })).byteLength;
}
//# sourceMappingURL=content-gc-format.js.map