import { reconcileRemote } from "./remote-reconciliation.js";
import { currentVersion } from "./file-history-format.js";
import { FileHistoryError, } from "./file-history-types.js";
/** Network requests hold a retained revision, rather than the local editing queue. */
export async function syncFile(storage, key, remote, exclusive) {
    const prepared = await exclusive(() => prepare(storage, key));
    if (prepared) {
        await completeAttempt(storage, prepared, remote, exclusive);
        if (prepared.reconcile) {
            const next = await exclusive(() => prepare(storage, key));
            if (next)
                await completeAttempt(storage, next, remote, exclusive);
        }
    }
    return exclusive(() => requireRecord(storage, key));
}
async function requireRecord(storage, key) {
    const record = await storage.load(key);
    if (!record)
        throw new FileHistoryError("not_found", `no local history for ${key}`);
    return record;
}
async function prepare(storage, key) {
    let record = await requireRecord(storage, key);
    const reconcile = record.attempt !== null;
    if (!record.attempt) {
        if (currentVersion(record).id === record.syncedVersionId)
            return null;
        record = await storage.write({
            ...record, attempt: { id: crypto.randomUUID(), version: currentVersion(record), basis: record.basis },
        });
    }
    const attempt = record.attempt;
    if (!attempt)
        throw new FileHistoryError("history_corrupt", "sync attempt is missing");
    return { record, data: await storage.bytes(record, attempt.version), reconcile };
}
async function completeAttempt(storage, prepared, remote, exclusive) {
    const { record, data, reconcile } = prepared;
    const attempt = record.attempt;
    if (!attempt)
        throw new FileHistoryError("history_corrupt", "sync attempt is missing");
    const reconciled = () => reconcileRemote(() => remote.getObject(record.remoteBucket, record.key), (object) => matchesAttempt(object, attempt.id, data, attempt.version), (object) => acknowledge(storage, record, object.etag, exclusive));
    if (reconcile && await reconciled())
        return;
    let written;
    try {
        written = await remote.putObject(record.remoteBucket, record.key, data, {
            basis: attempt.basis, saveId: attempt.id,
            contentType: attempt.version.contentType, metadata: attempt.version.metadata,
        });
    }
    catch (error) {
        if (!(error instanceof FileHistoryError && error.code === "conflict"))
            throw error;
        if (await reconciled())
            return;
        await exclusive(async () => storage.clearAttempt(await matchingRecord(storage, record)));
        throw error;
    }
    await acknowledge(storage, record, written.etag, exclusive);
}
async function matchingRecord(storage, attempted) {
    const latest = await requireRecord(storage, attempted.key);
    if (latest.attempt?.id !== attempted.attempt?.id) {
        throw new FileHistoryError("uncertain_write", "sync attempt changed before acknowledgement");
    }
    return latest;
}
async function acknowledge(storage, record, etag, exclusive) {
    await exclusive(async () => storage.acknowledge(await matchingRecord(storage, record), etag));
}
function matchesAttempt(object, saveId, data, version) {
    return object !== null && object.etag.length > 0 && object.saveId === saveId
        && object.data.length === data.length
        && object.data.every((value, index) => value === data[index])
        && (object.contentType ?? "application/octet-stream") === (version.contentType ?? "application/octet-stream")
        && normalizedMetadata(object.metadata) === normalizedMetadata(version.metadata);
}
function normalizedMetadata(metadata = {}) {
    const entries = Object.entries(metadata).map(([key, value]) => [key.toLowerCase(), value]);
    return JSON.stringify(entries.sort(([left = ""], [right = ""]) => left.localeCompare(right)));
}
//# sourceMappingURL=file-history-sync.js.map