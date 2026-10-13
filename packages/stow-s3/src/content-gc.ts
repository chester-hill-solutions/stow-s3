import { decodeSize, decodeUsage, recordBytes, writeUsage } from "./content-gc-format.js";
export { decodeHead, decodeSize, decodeUsage, recordBytes, writeUsage } from "./content-gc-format.js";
import { contentCursorKey, visitContentCursor } from "./content-cursor.js";
import { clearCollectionMarks, markContentRoots, markReachableContent } from "./content-gc-marks.js";
import type { ContentUsage } from "./content-types.js";
import { decodeRecord, namespaceRange, requestValue } from "./content-idb.js";

export async function collectContent(transaction: IDBTransaction, namespace: string, retained: string[]): Promise<ContentUsage> {
  await clearCollectionMarks(transaction, namespace);
  await markContentRoots(transaction, namespace, retained);
  await markReachableContent(transaction, namespace);
  const usage = decodeUsage(await requestValue<unknown>(transaction.objectStore("namespace").get(namespace)));
  await sweepRecords(transaction, namespace, usage);
  await sweepChunks(transaction, namespace, usage);
  await clearCollectionMarks(transaction, namespace);
  writeUsage(transaction, namespace, usage);
  return usage;
}

async function sweepRecords(transaction: IDBTransaction, namespace: string, usage: ContentUsage): Promise<void> {
  await visitContentCursor(transaction.objectStore("records"), namespaceRange(namespace), async (cursor) => {
    const id = contentCursorKey(cursor.key);
    if (await requestValue<unknown>(transaction.objectStore("gcRecords").get([namespace, id])) !== undefined) return;
    usage.physicalBytes -= recordBytes(decodeRecord(cursor.value));
    usage.records--;
    cursor.delete();
  });
}

async function sweepChunks(transaction: IDBTransaction, namespace: string, usage: ContentUsage): Promise<void> {
  await visitContentCursor(transaction.objectStore("chunks"), namespaceRange(namespace), async (cursor) => {
    const hash = contentCursorKey(cursor.key);
    if (await requestValue<unknown>(transaction.objectStore("gcChunks").get([namespace, hash])) !== undefined) return;
    usage.physicalBytes -= decodeSize(cursor.value);
    usage.chunks--;
    cursor.delete();
    transaction.objectStore("payloads").delete([namespace, hash]);
  });
}
