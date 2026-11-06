import {
  BrowserPersistenceError,
  type IndexedDbPersistenceAdapterOptions,
  type LockManagerLike,
  type PersistenceAdapter,
  type PersistenceChange,
  type PersistenceSnapshot,
  validatePersistenceNamespace,
} from "./browser-types.js";
import {
  clearNamespace,
  commitChanges,
  openDatabase,
  readNamespace,
  type StoreNames,
} from "./indexeddb-store.js";
import { normalizeChange, normalizeSnapshot } from "./persistence-format.js";

const DEFAULT_DATABASE_NAME = "chs-stow";
const DEFAULT_MANIFEST_STORE = "manifest";
const DEFAULT_BUCKET_STORE = "buckets";
const DEFAULT_OBJECT_STORE = "objects";
const activeNamespaces = new Set<string>();

interface Ownership {
  release(): Promise<void>;
}

interface OpenPersistence {
  database: IDBDatabase;
  namespace: string;
  ownership: Ownership;
}

type ActiveLifecycle =
  | { kind: "idle" }
  | { kind: "opening"; completion: Promise<OpenPersistence> }
  | { kind: "open"; resources: OpenPersistence };

type AdapterLifecycle = ActiveLifecycle
  | { kind: "closing"; completion: Promise<void> }
  | { kind: "closed"; completion: Promise<void> }
  | { kind: "failed"; completion: Promise<void> };

export class IndexedDbPersistenceAdapter implements PersistenceAdapter {
  private readonly factory: IDBFactory | undefined;
  private readonly databaseName: string;
  private readonly manifestStoreName: string;
  private readonly bucketStoreName: string;
  private readonly objectStoreName: string;
  private readonly lockManager: LockManagerLike | undefined;
  private readonly lockName: (namespace: string) => string;
  private lifecycle: AdapterLifecycle = { kind: "idle" };

  constructor(options: IndexedDbPersistenceAdapterOptions = {}) {
    this.factory = options.indexedDB ?? globalIndexedDb();
    this.databaseName = options.databaseName ?? DEFAULT_DATABASE_NAME;
    this.manifestStoreName = options.manifestStoreName ?? DEFAULT_MANIFEST_STORE;
    this.bucketStoreName = options.bucketStoreName ?? DEFAULT_BUCKET_STORE;
    this.objectStoreName = options.objectStoreName ?? DEFAULT_OBJECT_STORE;
    this.lockManager = options.lockManager ?? globalLockManager();
    this.lockName = options.lockName ?? ((namespace) => `stow:${this.databaseName}:${namespace}`);
  }

  async open(namespace: string): Promise<void> {
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
      if (this.lifecycle.kind !== "opening") throw new BrowserPersistenceError("closed", "persistence adapter is closed");
      this.lifecycle = { kind: "open", resources };
    } catch (error) {
      if (this.lifecycle.kind === "opening") this.lifecycle = { kind: "idle" };
      throw mapAdapterError(error);
    }
  }

  private async acquireResources(namespace: string): Promise<OpenPersistence> {
    const ownership = await acquireOwnership(this.lockName(namespace), this.lockManager);
    try {
      const database = await openDatabase(
        this.factory,
        this.databaseName,
        this.manifestStoreName,
        this.bucketStoreName,
        this.objectStoreName,
      );
      return { database, namespace, ownership };
    } catch (error) {
      try {
        await ownership.release();
      } catch {
        // Preserve the database-open failure.
      }
      throw mapAdapterError(error);
    }
  }

  private get stores(): StoreNames {
    return {
      manifest: this.manifestStoreName,
      buckets: this.bucketStoreName,
      objects: this.objectStoreName,
    };
  }

  async load(namespace: string): Promise<PersistenceSnapshot | null> {
    const { database } = this.resourcesFor(namespace);
    try {
      const raw = await readNamespace(database, namespace, this.stores);
      return raw === null ? null : normalizeSnapshot(raw);
    } catch (error) {
      throw mapAdapterError(error);
    }
  }

  async commit(
    namespace: string,
    expectedGeneration: number,
    changes: PersistenceChange[],
  ): Promise<number> {
    const { database } = this.resourcesFor(namespace);
    validateGeneration(expectedGeneration);
    const normalizedChanges = changes.map(normalizeChange);
    try {
      return await commitChanges(database, namespace, expectedGeneration, normalizedChanges, this.stores);
    } catch (error) {
      throw mapAdapterError(error);
    }
  }

  async clear(namespace: string, expectedGeneration: number): Promise<number> {
    const { database } = this.resourcesFor(namespace);
    validateGeneration(expectedGeneration);
    try {
      return await clearNamespace(database, namespace, expectedGeneration, this.stores);
    } catch (error) {
      throw mapAdapterError(error);
    }
  }

  close(): Promise<void> {
    const previous = this.lifecycle;
    if (previous.kind === "closing" || previous.kind === "closed" || previous.kind === "failed") return previous.completion;
    const completion: Promise<void> = Promise.resolve().then(() => this.closeInternal(previous)).then(
      () => { this.lifecycle = { kind: "closed", completion }; },
      (error: unknown) => { this.lifecycle = { kind: "failed", completion }; throw mapAdapterError(error); },
    );
    this.lifecycle = { kind: "closing", completion };
    return completion;
  }

  private async closeInternal(previous: ActiveLifecycle): Promise<void> {
    if (previous.kind === "idle") return;
    let resources: OpenPersistence;
    if (previous.kind === "opening") {
      try { resources = await previous.completion; }
      catch { return; } // Failed acquisition releases its own ownership.
    } else resources = previous.resources;
    let firstError: unknown;
    try {
      resources.database.close();
    } catch (error) {
      firstError = error;
    }
    try {
      await resources.ownership.release();
    } catch (error) {
      firstError ??= error;
    }
    if (firstError !== undefined) {
      throw mapAdapterError(firstError);
    }
  }

  private resourcesFor(namespace: string): OpenPersistence {
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

function globalIndexedDb(): IDBFactory | undefined {
  const runtime = globalThis as typeof globalThis & { indexedDB?: IDBFactory };
  return runtime.indexedDB;
}

function globalLockManager(): LockManagerLike | undefined {
  const runtime = globalThis as typeof globalThis & {
    navigator?: { locks?: LockManagerLike };
  };
  return runtime.navigator?.locks;
}

async function acquireOwnership(
  ownershipKey: string,
  lockManager: LockManagerLike | undefined,
): Promise<Ownership> {
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

  let releaseLock: (() => void) | undefined;
  const held = new Promise<void>((resolve) => {
    releaseLock = resolve;
  });
  let acquired = false;
  let rejectAcquired: ((error: unknown) => void) | undefined;
  let requestPromise: Promise<void> | undefined;
  const acquiredPromise = new Promise<void>((resolve, reject) => {
    rejectAcquired = reject;
    requestPromise = lockManager.request(
      ownershipKey,
      { mode: "exclusive", ifAvailable: true },
      async (lock) => {
        if (lock === null) {
          rejectAcquired?.(new BrowserPersistenceError("already_open", "persistence namespace is already open"));
          return;
        }
        acquired = true;
        resolve();
        await held;
      },
    );
    requestPromise.catch((error: unknown) => {
      if (!acquired) {
        activeNamespaces.delete(ownershipKey);
        rejectAcquired?.(error);
      }
    });
  });
  try {
    await acquiredPromise;
  } catch (error) {
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
      } finally {
        activeNamespaces.delete(ownershipKey);
      }
    },
  };
}

function validateGeneration(generation: number): void {
  if (!Number.isSafeInteger(generation) || generation < 0) {
    throw new BrowserPersistenceError("persistence_error", "persistence generation must be a non-negative integer");
  }
}

function mapAdapterError(error: unknown): BrowserPersistenceError {
  if (error instanceof BrowserPersistenceError) {
    return error;
  }
  if (isRecord(error) && error.name === "QuotaExceededError") {
    return new BrowserPersistenceError("quota_exceeded", "IndexedDB quota exceeded");
  }
  if (isRecord(error) && error.name === "SecurityError") {
    return new BrowserPersistenceError("persistence_denied", "IndexedDB access was denied");
  }
  return new BrowserPersistenceError(
    "persistence_error",
    `IndexedDB persistence failed: ${errorMessage(error)}`,
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
