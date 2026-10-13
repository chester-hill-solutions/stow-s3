import { contentCursorKey, visitContentCursor } from "./content-cursor.js";
import { corrupt, decodeRecord, decodeStrings, namespaceRange, requestValue } from "./content-idb.js";
import { decodeHead } from "./content-gc-format.js";
export async function clearCollectionMarks(transaction, namespace) {
    for (const name of ["gcRecords", "gcChunks"]) {
        await visitContentCursor(transaction.objectStore(name), namespaceRange(namespace), (cursor) => { cursor.delete(); });
    }
}
export async function markContentRoots(transaction, namespace, retained) {
    for (const id of retained)
        await markRecord(transaction, namespace, id);
    await visitContentCursor(transaction.objectStore("heads"), namespaceRange(namespace), async (cursor) => {
        const head = decodeHead(cursor.value);
        if (head.recordId !== null)
            await markRecord(transaction, namespace, head.recordId);
    });
    await visitContentCursor(transaction.objectStore("pins"), namespaceRange(namespace), async (cursor) => {
        const staged = contentCursorKey(cursor.key).startsWith("stage:");
        for (const id of decodeStrings(cursor.value)) {
            if (staged)
                transaction.objectStore("gcChunks").put(1, [namespace, id]);
            else
                await markRecord(transaction, namespace, id);
        }
    });
}
export async function markReachableContent(transaction, namespace) {
    const records = transaction.objectStore("gcRecords");
    const pending = records.index("pending");
    while (true) {
        const cursor = await requestValue(pending.openCursor(IDBKeyRange.only([namespace, 0])));
        if (cursor === null)
            return;
        const id = contentCursorKey(cursor.primaryKey);
        records.put({ namespace, processed: 1 }, [namespace, id]);
        const value = await requestValue(transaction.objectStore("records").get([namespace, id]));
        if (value === undefined)
            throw corrupt("protected content record is missing");
        const record = decodeRecord(value);
        for (const content of record.contents) {
            for (const chunk of content.chunks)
                transaction.objectStore("gcChunks").put(1, [namespace, chunk.hash]);
        }
        for (const reference of record.references)
            await markRecord(transaction, namespace, reference);
    }
}
async function markRecord(transaction, namespace, id) {
    const store = transaction.objectStore("gcRecords");
    if (await requestValue(store.get([namespace, id])) === undefined) {
        store.put({ namespace, processed: 0 }, [namespace, id]);
    }
}
//# sourceMappingURL=content-gc-marks.js.map