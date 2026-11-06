import { BrowserPersistenceError, validatePersistenceNamespace, } from "./browser-types.js";
import { clearNamespace, commitChanges, openDatabase, readNamespace, } from "./indexeddb-store.js";
import { normalizeChange, normalizeSnapshot } from "./persistence-format.js";
const DEFAULT_DATABASE_NAME = "chs-stow";
const DEFAULT_MANIFEST_STORE = "manifest";
const DEFAULT_BUCKET_STORE = "buckets";
const DEFAULT_OBJECT_STORE = "objects";
const activeNamespaces = new Set();
export class IndexedDbPersistenceAdapter {
    factory;
    databaseName;
    manifestStoreName;
    bucketStoreName;
    objectStoreName;
    lockManager;
    lockName;
    lifecycle = { kind: "idle" };
    constructor(options = {}) {
        this.factory = options.indexedDB ?? globalIndexedDb();
        this.databaseName = options.databaseName ?? DEFAULT_DATABASE_NAME;
        this.manifestStoreName = options.manifestStoreName ?? DEFAULT_MANIFEST_STORE;
        this.bucketStoreName = options.bucketStoreName ?? DEFAULT_BUCKET_STORE;
        this.objectStoreName = options.objectStoreName ?? DEFAULT_OBJECT_STORE;
        this.lockManager = options.lockManager ?? globalLockManager();
        this.lockName = options.lockName ?? ((namespace) => `stow:${this.databaseName}:${namespace}`);
    }
    async open(namespace) {
        validatePersistenceNamespace(namespace);
        if (this.lifecycle.kind !== "idle") {
            if (this.lifecycle.kind === "opening" || this.lifecycle.kind === "open") {
                throw new BrowserPersistenceError("already_open", "persistence namespace is already open or opening");
            }
            throw new BrowserPersistenceError("closed", "persistence adapter is closed");
        }
        const completion = this.acquireResources(namespace);
        this.lifecycle = { kind: "opening", completion };
        try {
            const resources = await completion;
            // Close owns the pending acquisition once it transitions to closing.
            if (this.lifecycle.kind !== "opening")
                throw new BrowserPersistenceError("closed", "persistence adapter is closed");
            this.lifecycle = { kind: "open", resources };
        }
        catch (error) {
            if (this.lifecycle.kind === "opening")
                this.lifecycle = { kind: "idle" };
            throw mapAdapterError(error);
        }
    }
    async acquireResources(namespace) {
        const ownership = await acquireOwnership(this.lockName(namespace), this.lockManager);
        try {
            const database = await openDatabase(this.factory, this.databaseName, this.manifestStoreName, this.bucketStoreName, this.objectStoreName);
            return { database, namespace, ownership };
        }
        catch (error) {
            try {
                await ownership.release();
            }
            catch {
                // Preserve the database-open failure.
            }
            throw mapAdapterError(error);
        }
    }
    get stores() {
        return {
            manifest: this.manifestStoreName,
            buckets: this.bucketStoreName,
            objects: this.objectStoreName,
        };
    }
    async load(namespace) {
        const { database } = this.resourcesFor(namespace);
        try {
            const raw = await readNamespace(database, namespace, this.stores);
            return raw === null ? null : normalizeSnapshot(raw);
        }
        catch (error) {
            throw mapAdapterError(error);
        }
    }
    async commit(namespace, expectedGeneration, changes) {
        const { database } = this.resourcesFor(namespace);
        validateGeneration(expectedGeneration);
        const normalizedChanges = changes.map(normalizeChange);
        try {
            return await commitChanges(database, namespace, expectedGeneration, normalizedChanges, this.stores);
        }
        catch (error) {
            throw mapAdapterError(error);
        }
    }
    async clear(namespace, expectedGeneration) {
        const { database } = this.resourcesFor(namespace);
        validateGeneration(expectedGeneration);
        try {
            return await clearNamespace(database, namespace, expectedGeneration, this.stores);
        }
        catch (error) {
            throw mapAdapterError(error);
        }
    }
    close() {
        const previous = this.lifecycle;
        if (previous.kind === "closing" || previous.kind === "closed" || previous.kind === "failed")
            return previous.completion;
        const completion = Promise.resolve().then(() => this.closeInternal(previous)).then(() => { this.lifecycle = { kind: "closed", completion }; }, (error) => { this.lifecycle = { kind: "failed", completion }; throw mapAdapterError(error); });
        this.lifecycle = { kind: "closing", completion };
        return completion;
    }
    async closeInternal(previous) {
        if (previous.kind === "idle")
            return;
        let resources;
        if (previous.kind === "opening") {
            try {
                resources = await previous.completion;
            }
            catch {
                return;
            } // Failed acquisition releases its own ownership.
        }
        else
            resources = previous.resources;
        let firstError;
        try {
            resources.database.close();
        }
        catch (error) {
            firstError = error;
        }
        try {
            await resources.ownership.release();
        }
        catch (error) {
            firstError ??= error;
        }
        if (firstError !== undefined) {
            throw mapAdapterError(firstError);
        }
    }
    resourcesFor(namespace) {
        const state = this.lifecycle;
        if (state.kind !== "open") {
            if (state.kind === "idle" || state.kind === "opening") {
                throw new BrowserPersistenceError("persistence_error", "persistence namespace is not open");
            }
            throw new BrowserPersistenceError("closed", "persistence adapter is closed");
        }
        if (state.resources.namespace !== namespace) {
            throw new BrowserPersistenceError("persistence_error", "persistence namespace does not match");
        }
        return state.resources;
    }
}
function globalIndexedDb() {
    const runtime = globalThis;
    return runtime.indexedDB;
}
function globalLockManager() {
    const runtime = globalThis;
    return runtime.navigator?.locks;
}
async function acquireOwnership(ownershipKey, lockManager) {
    if (activeNamespaces.has(ownershipKey)) {
        throw new BrowserPersistenceError("already_open", "persistence namespace is already open");
    }
    activeNamespaces.add(ownershipKey);
    if (lockManager === undefined) {
        return {
            release: async () => {
                activeNamespaces.delete(ownershipKey);
            },
        };
    }
    let releaseLock;
    const held = new Promise((resolve) => {
        releaseLock = resolve;
    });
    let acquired = false;
    let rejectAcquired;
    let requestPromise;
    const acquiredPromise = new Promise((resolve, reject) => {
        rejectAcquired = reject;
        requestPromise = lockManager.request(ownershipKey, { mode: "exclusive", ifAvailable: true }, async (lock) => {
            if (lock === null) {
                rejectAcquired?.(new BrowserPersistenceError("already_open", "persistence namespace is already open"));
                return;
            }
            acquired = true;
            resolve();
            await held;
        });
        requestPromise.catch((error) => {
            if (!acquired) {
                activeNamespaces.delete(ownershipKey);
                rejectAcquired?.(error);
            }
        });
    });
    try {
        await acquiredPromise;
    }
    catch (error) {
        releaseLock?.();
        activeNamespaces.delete(ownershipKey);
        if (error instanceof BrowserPersistenceError) {
            throw error;
        }
        throw new BrowserPersistenceError("persistence_denied", `could not acquire persistence ownership: ${errorMessage(error)}`);
    }
    return {
        release: async () => {
            releaseLock?.();
            try {
                await requestPromise;
            }
            finally {
                activeNamespaces.delete(ownershipKey);
            }
        },
    };
}
function validateGeneration(generation) {
    if (!Number.isSafeInteger(generation) || generation < 0) {
        throw new BrowserPersistenceError("persistence_error", "persistence generation must be a non-negative integer");
    }
}
function mapAdapterError(error) {
    if (error instanceof BrowserPersistenceError) {
        return error;
    }
    if (isRecord(error) && error.name === "QuotaExceededError") {
        return new BrowserPersistenceError("quota_exceeded", "IndexedDB quota exceeded");
    }
    if (isRecord(error) && error.name === "SecurityError") {
        return new BrowserPersistenceError("persistence_denied", "IndexedDB access was denied");
    }
    return new BrowserPersistenceError("persistence_error", `IndexedDB persistence failed: ${errorMessage(error)}`);
}
function isRecord(value) {
    return typeof value === "object" && value !== null;
}
function errorMessage(error) {
    return error instanceof Error ? error.message : String(error);
}
//# sourceMappingURL=indexeddb-persistence.js.map