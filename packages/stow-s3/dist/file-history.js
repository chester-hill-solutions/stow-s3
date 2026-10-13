import { enqueueOperation, operationQueues } from "./operation-queue.js";
import { currentVersion, fileStatus } from "./file-history-format.js";
import { FileHistoryStorage } from "./file-history-storage.js";
import { syncFile } from "./file-history-sync.js";
import { FileHistoryError, } from "./file-history-types.js";
/** Durable file stages over the browser store. Networking happens only on an explicit read miss or sync. */
export class BrowserFileHistory {
    storage;
    remote;
    queue;
    constructor(storage, remote, queue) {
        this.storage = storage;
        this.remote = remote;
        this.queue = queue;
    }
    static async open(store, options) {
        const maxVersions = options.maxVersions ?? 50;
        if (!options.remoteBucket || !Number.isSafeInteger(maxVersions) || maxVersions < 2) {
            throw new FileHistoryError("invalid_options", "remoteBucket and maxVersions >= 2 are required");
        }
        const bucket = options.historyBucket ?? "stow-file-history";
        const storage = new FileHistoryStorage(store, bucket, options.remoteBucket, maxVersions);
        const history = new BrowserFileHistory(storage, options.remote, operationQueues(store, `history:${bucket}`));
        await history.enqueue(() => storage.initialize());
        return history;
    }
    read(key) {
        return this.enqueue(async () => {
            let record = await this.storage.load(key);
            if (!record) {
                const object = await this.getRemote(key);
                record = await this.storage.append(key, null, object.data, {
                    label: "Fetched from remote", contentType: object.contentType, metadata: object.metadata,
                }, object.etag);
            }
            return this.result(record);
        });
    }
    save(key, data, options = {}) {
        return this.append(key, data, options, (record) => this.result(record));
    }
    saveReceipt(key, data, options = {}) {
        return this.append(key, data, options, (record) => Promise.resolve({
            version: structuredClone(currentVersion(record)), status: fileStatus(record),
        }));
    }
    append(key, data, options, result) {
        const captured = new Uint8Array(data);
        const capturedOptions = structuredClone(options);
        return this.enqueue(async () => {
            const previous = await this.storage.load(key);
            const record = await this.storage.append(key, previous, captured, capturedOptions);
            return result(record);
        });
    }
    status(key) {
        return this.enqueue(async () => fileStatus(await this.requireRecord(key)));
    }
    /** Holds the source history stable while copying one retained revision at a time. */
    exportRetained(key, visit) {
        return this.enqueue(async () => {
            const record = await this.requireRecord(key);
            if (record.attempt)
                throw new FileHistoryError("uncertain_write", "reconcile the original remote save before migrating its history");
            const history = { key, remoteBucket: record.remoteBucket, versions: structuredClone(record.versions), currentVersionId: currentVersion(record).id };
            for (const version of record.versions)
                await visit(structuredClone(version), await this.storage.bytes(record, version), history);
            return history;
        });
    }
    undo(key) {
        return this.move(key, -1);
    }
    redo(key) {
        return this.move(key, 1);
    }
    /** Publish only the current stage; older stages stay local. */
    sync(key) {
        return enqueueOperation(this.queue, "syncTail", async () => fileStatus(await syncFile(this.storage, key, this.requireRemote(), (operation) => this.enqueue(operation))));
    }
    /** Explicitly choose the remote file after a conflict; retained local stages remain undoable. */
    adoptRemote(key) {
        return this.enqueue(async () => {
            const previous = await this.storage.load(key);
            if (previous?.attempt)
                throw new FileHistoryError("uncertain_write", "retry sync to reconcile the uncertain write before adopting remote data");
            const object = await this.getRemote(key);
            const record = await this.storage.append(key, previous, object.data, {
                label: "Adopted from remote", contentType: object.contentType, metadata: object.metadata,
            }, object.etag);
            return this.result(record);
        });
    }
    move(key, direction) {
        return this.enqueue(async () => {
            const record = await this.requireRecord(key);
            const cursor = record.cursor + direction;
            if (cursor < 0 || cursor >= record.versions.length)
                throw new FileHistoryError("history_boundary", "no further retained stage");
            await this.storage.bytes(record, record.versions[cursor]);
            return this.result(await this.storage.write({ ...record, cursor }));
        });
    }
    async result(record) {
        return { data: await this.storage.bytes(record), version: structuredClone(currentVersion(record)), status: fileStatus(record) };
    }
    async requireRecord(key) {
        const record = await this.storage.load(key);
        if (!record)
            throw new FileHistoryError("not_found", `no local history for ${key}`);
        return record;
    }
    async getRemote(key) {
        const object = await this.requireRemote().getObject(this.storage.remoteBucket, key);
        if (!object)
            throw new FileHistoryError("not_found", `remote file ${key} does not exist`);
        if (!object.etag)
            throw new FileHistoryError("remote_unavailable", "remote did not return an ETag");
        return object;
    }
    requireRemote() {
        if (!this.remote)
            throw new FileHistoryError("remote_unavailable", "no remote transport is configured");
        return this.remote;
    }
    enqueue(operation) {
        return enqueueOperation(this.queue, "tail", operation);
    }
}
//# sourceMappingURL=file-history.js.map