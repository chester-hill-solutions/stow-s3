import { contentCursorKey, visitContentCursor } from "./content-cursor.js";
import { MAX_CONTENT_RECORD_BYTES } from "./content-types.js";
import type { ContentUsage } from "./content-types.js";
import { decodeStrings, requestValue } from "./content-idb.js";

export function bookkeepingSize(key: string, value: unknown): number {
  return new TextEncoder().encode(JSON.stringify([key, value])).byteLength;
}

export async function putPins(transaction: IDBTransaction, namespace: string, key: string, ids: string[], usage: ContentUsage): Promise<void> {
  const store = transaction.objectStore("pins");
  const size = bookkeepingSize(key, ids);
  if (size > MAX_CONTENT_RECORD_BYTES) throw new RangeError("content protection metadata exceeds 1 MiB");
  const previous: unknown = await requestValue(store.get([namespace, key]));
  const before = previous === undefined ? 0 : bookkeepingSize(key, decodeStrings(previous));
  usage.bookkeepingBytes = (usage.bookkeepingBytes ?? 0) + (ids.length === 0 ? 0 : size) - before;
  if (ids.length === 0) store.delete([namespace, key]);
  else store.put(ids, [namespace, key]);
}

export async function deleteStagePins(transaction: IDBTransaction, namespace: string, stageId: string, usage: ContentUsage): Promise<void> {
  await deletePinPrefix(transaction, namespace, `stage:${stageId}:`, usage);
}

export async function deleteHandlePins(transaction: IDBTransaction, namespace: string, usage: ContentUsage): Promise<void> {
  await deletePinPrefix(transaction, namespace, "handle:", usage);
}

async function deletePinPrefix(transaction: IDBTransaction, namespace: string, prefix: string, usage: ContentUsage): Promise<void> {
  await visitContentCursor(transaction.objectStore("pins"), IDBKeyRange.bound([namespace, prefix], [namespace, `${prefix}\uffff`]), (cursor) => {
    const key = contentCursorKey(cursor.key);
    usage.bookkeepingBytes = (usage.bookkeepingBytes ?? 0) - bookkeepingSize(key, decodeStrings(cursor.value));
    cursor.delete();
  });
}
