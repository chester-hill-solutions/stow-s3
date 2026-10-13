import { contentCursorKey, visitContentCursor } from "./content-cursor.js";
import { MAX_CONTENT_RECORD_BYTES } from "./content-types.js";
import { decodeStrings, requestValue } from "./content-idb.js";
export function bookkeepingSize(key, value) {
    return new TextEncoder().encode(JSON.stringify([key, value])).byteLength;
}
export async function putPins(transaction, namespace, key, ids, usage) {
    const store = transaction.objectStore("pins");
    const size = bookkeepingSize(key, ids);
    if (size > MAX_CONTENT_RECORD_BYTES)
        throw new RangeError("content protection metadata exceeds 1 MiB");
    const previous = await requestValue(store.get([namespace, key]));
    const before = previous === undefined ? 0 : bookkeepingSize(key, decodeStrings(previous));
    usage.bookkeepingBytes = (usage.bookkeepingBytes ?? 0) + (ids.length === 0 ? 0 : size) - before;
    if (ids.length === 0)
        store.delete([namespace, key]);
    else
        store.put(ids, [namespace, key]);
}
export async function deleteStagePins(transaction, namespace, stageId, usage) {
    await deletePinPrefix(transaction, namespace, `stage:${stageId}:`, usage);
}
export async function deleteHandlePins(transaction, namespace, usage) {
    await deletePinPrefix(transaction, namespace, "handle:", usage);
}
async function deletePinPrefix(transaction, namespace, prefix, usage) {
    await visitContentCursor(transaction.objectStore("pins"), IDBKeyRange.bound([namespace, prefix], [namespace, `${prefix}\uffff`]), (cursor) => {
        const key = contentCursorKey(cursor.key);
        usage.bookkeepingBytes = (usage.bookkeepingBytes ?? 0) - bookkeepingSize(key, decodeStrings(cursor.value));
        cursor.delete();
    });
}
//# sourceMappingURL=content-accounting.js.map