import { bookkeepingSize, deleteHandlePins, deleteStagePins, putPins } from "./content-accounting.js";
import { BrowserPersistenceError, validatePersistenceNamespace } from "./browser-types.js";
import { MAX_CONTENT_RECORD_BYTES } from "./content-types.js";
import { contentHash } from "./content-store.js";
import type { ContentAdapter, ContentHead, ContentPage, ContentProtection, ContentPublication, ContentRecord, ContentUsage, IndexedDbContentOptions } from "./content-types.js";
import { contentDatabase, contentTransaction, corrupt, decodeBytes, decodeRecord, decodeStrings, namespaceRange, requestValue, rows } from "./content-idb.js";
import { collectContent, decodeHead, decodeSize, decodeUsage, recordBytes, writeUsage } from "./content-gc.js";
import { ownContentNamespace } from "./content-ownership.js";

export class IndexedDbContentAdapter implements ContentAdapter {
  private database?: IDBDatabase;
  private namespace?: string;
  private release?: () => Promise<void>;
  private closed = false;
  private readonly quota: number;

  constructor(private readonly options: IndexedDbContentOptions = {}) {
    this.quota = options.quotaBytes ?? 1024 * 1024 * 1024;
    if (!Number.isSafeInteger(this.quota) || this.quota < 0) throw new RangeError("quotaBytes must be a non-negative integer");
  }

  async open(namespace: string): Promise<void> {
    validatePersistenceNamespace(namespace);
    if (this.closed) throw new BrowserPersistenceError("closed", "content adapter closed");
    if (this.release !== undefined) throw new BrowserPersistenceError("already_open", "content adapter already open");
    const factory = this.options.indexedDB ?? globalThis.indexedDB;
    if (factory === undefined) throw new BrowserPersistenceError("persistence_unsupported", "IndexedDB is unavailable");
    const name = this.options.databaseName ?? "chs-stow-content";
    this.release = await ownContentNamespace(this.options.lockManager ?? globalThis.navigator?.locks, `stow-content:${name}:${namespace}`);
    try {
      this.database = await contentDatabase(factory, name);
      this.namespace = namespace;
      await this.run("readwrite", async (transaction, namespace) => {
        const usage = decodeUsage(await requestValue<unknown>(transaction.objectStore("namespace").get(namespace)));
        await deleteHandlePins(transaction, namespace, usage);
        writeUsage(transaction, namespace, usage);
      });
    } catch (error) {
      await this.close();
      throw error;
    }
  }

  getHead(name: string): Promise<ContentHead> {
    return this.run("readonly", async (transaction, namespace) => {
      const value: unknown = await requestValue(transaction.objectStore("heads").get([namespace, name]));
      return value === undefined ? { name, generation: 0, recordId: null } : decodeHead(value);
    });
  }

  getRecord(id: string): Promise<ContentRecord | null> {
    return this.run("readonly", async (transaction, namespace) => {
      const value: unknown = await requestValue(transaction.objectStore("records").get([namespace, id]));
      return value === undefined ? null : decodeRecord(value);
    });
  }

  listRecords(options: { after?: string; limit?: number } = {}): Promise<ContentPage> {
    const limit = options.limit ?? 100;
    if (!Number.isSafeInteger(limit) || limit < 1 || limit > 1000) throw new RangeError("page limit must be between 1 and 1000");
    return this.run("readonly", async (transaction, namespace) => {
      const values = await rows(transaction.objectStore("records"), namespaceRange(namespace, options.after), decodeRecord, limit + 1);
      return { records: values.slice(0, limit).map(({ value }) => value), next: values.length > limit ? values[limit - 1]?.key : undefined };
    });
  }

  getChunk(hash: string): Promise<Uint8Array | null> {
    validateHash(hash);
    return this.run("readonly", async (transaction, namespace) => {
      const value: unknown = await requestValue(transaction.objectStore("payloads").get([namespace, hash]));
      return value === undefined ? null : decodeBytes(value);
    });
  }

  async stageChunk(stageId: string, hash: string, bytes: Uint8Array): Promise<void> {
    validateHash(hash);
    validateId(stageId);
    const data = new Uint8Array(bytes);
    if (await contentHash(data) !== hash) throw corrupt("staged chunk hash differs");
    await this.run("readwrite", async (transaction, namespace) => {
      const chunks = transaction.objectStore("chunks");
      const [old, usageValue] = await Promise.all([
        requestValue<unknown>(chunks.get([namespace, hash])),
        requestValue<unknown>(transaction.objectStore("namespace").get(namespace)),
      ]);
      const usage = decodeUsage(usageValue);
      if (old === undefined) {
        usage.physicalBytes += data.byteLength;
        usage.chunks++;
        chunks.put(data.byteLength, [namespace, hash]);
        transaction.objectStore("payloads").put(data, [namespace, hash]);
      } else if (decodeSize(old) !== data.byteLength) throw corrupt("immutable chunk collision");
      await putPins(transaction, namespace, `stage:${stageId}:${hash}`, [hash], usage);
      this.checkQuota(usage);
      writeUsage(transaction, namespace, usage);
    });
  }

  listStages(): Promise<string[]> {
    return this.run("readonly", async (transaction, namespace) => {
      const pins = await rows(transaction.objectStore("pins"), namespaceRange(namespace), decodeStrings);
      const stages = new Set<string>();
      for (const { key } of pins) if (key.startsWith("stage:")) stages.add(key.slice(6, key.lastIndexOf(":")));
      return [...stages].sort();
    });
  }

  releaseStage(stageId: string): Promise<void> {
    validateId(stageId);
    return this.run("readwrite", async (transaction, namespace) => {
      const usage = decodeUsage(await requestValue<unknown>(transaction.objectStore("namespace").get(namespace)));
      await deleteStagePins(transaction, namespace, stageId, usage);
      writeUsage(transaction, namespace, usage);
    });
  }

  publish(publication: ContentPublication): Promise<ContentHead> {
    const settings = { ...publication };
    const targetId = publication.record.id;
    const records = [...settings.additionalRecords ?? [], settings.record].map(decodeRecord);
    for (const record of records) {
      validateId(record.id);
      if (recordBytes(record) > MAX_CONTENT_RECORD_BYTES) throw new RangeError("record metadata and references exceed 1 MiB");
    }
    if (!Number.isSafeInteger(settings.expectedGeneration) || settings.expectedGeneration < 0) throw new RangeError("invalid generation");
    return this.run("readwrite", async (transaction, namespace) => {
      const value: unknown = await requestValue(transaction.objectStore("heads").get([namespace, settings.head]));
      const current = value === undefined ? 0 : decodeHead(value).generation;
      if (current !== settings.expectedGeneration) throw new BrowserPersistenceError("persistence_error", "content head generation conflict");
      const usage = decodeUsage(await requestValue<unknown>(transaction.objectStore("namespace").get(namespace)));
      await publishRecords(transaction, namespace, records, usage);
      const head = { name: settings.head, generation: current + 1, recordId: targetId };
      if (bookkeepingSize(settings.head, head) > MAX_CONTENT_RECORD_BYTES) throw new RangeError("content head metadata exceeds 1 MiB");
      usage.bookkeepingBytes = (usage.bookkeepingBytes ?? 0) + bookkeepingSize(settings.head, head) -
        (value === undefined ? 0 : bookkeepingSize(settings.head, decodeHead(value)));
      await deleteStagePins(transaction, namespace, settings.stageId, usage);
      this.checkQuota(usage);
      transaction.objectStore("heads").put(head, [namespace, settings.head]);
      writeUsage(transaction, namespace, usage);
      return head;
    });
  }

  listProtections(): Promise<ContentProtection[]> {
    return this.run("readonly", async (transaction, namespace) => {
      const pins = await rows(transaction.objectStore("pins"), namespaceRange(namespace), decodeStrings);
      const protections: ContentProtection[] = [];
      for (const { key, value } of pins) {
        if (key.startsWith("stage:")) continue;
        protections.push({ name: key.slice(key.indexOf(":") + 1), recordIds: value,
          lifetime: key.startsWith("handle:") ? "handle" : "durable" });
      }
      return protections;
    });
  }

  protect(name: string, recordIds: string[], options: { lifetime?: "handle" | "durable" } = {}): Promise<void> {
    const ids = [...recordIds];
    const key = `${options.lifetime === "handle" ? "handle" : "record"}:${name}`;
    return this.run("readwrite", async (transaction, namespace) => {
      for (const id of ids) {
        if (await requestValue<unknown>(transaction.objectStore("records").get([namespace, id])) === undefined) throw corrupt("protected record missing");
      }
      const usage = decodeUsage(await requestValue<unknown>(transaction.objectStore("namespace").get(namespace)));
      await putPins(transaction, namespace, key, ids, usage);
      this.checkQuota(usage);
      writeUsage(transaction, namespace, usage);
    });
  }

  collect(options: { retainRecordIds?: string[] } = {}): Promise<ContentUsage> {
    return this.run("readwrite", (transaction, namespace) => collectContent(transaction, namespace, options.retainRecordIds ?? []));
  }

  usage(): Promise<ContentUsage> {
    return this.run("readonly", async (transaction, namespace) => decodeUsage(await requestValue<unknown>(transaction.objectStore("namespace").get(namespace))));
  }

  async close(): Promise<void> {
    this.closed = true;
    this.database?.close();
    this.database = undefined;
    const release = this.release;
    this.release = undefined;
    await release?.();
  }

  private run<T>(mode: IDBTransactionMode, operation: (transaction: IDBTransaction, namespace: string) => Promise<T>): Promise<T> {
    if (this.closed) throw new BrowserPersistenceError("closed", "content adapter closed");
    if (this.database === undefined || this.namespace === undefined) throw new BrowserPersistenceError("persistence_error", "content adapter not open");
    const namespace = this.namespace;
    return contentTransaction(this.database, mode, (transaction) => operation(transaction, namespace));
  }

  private checkQuota(usage: ContentUsage): void {
    if (usage.physicalBytes + (usage.bookkeepingBytes ?? 0) > this.quota) throw new BrowserPersistenceError("quota_exceeded", "namespace content quota exceeded");
  }
}

async function publishRecords(transaction: IDBTransaction, namespace: string, records: ContentRecord[], usage: ContentUsage): Promise<void> {
  const store = transaction.objectStore("records");
  const newIds = new Set(records.map((record) => record.id));
  if (newIds.size !== records.length) throw corrupt("duplicate publication record id");
  for (const record of records) {
    const previous: unknown = await requestValue(store.get([namespace, record.id]));
    if (previous !== undefined) throw corrupt("immutable content record id already exists");
    for (const reference of record.references) {
      if (!newIds.has(reference) && await requestValue<unknown>(store.get([namespace, reference])) === undefined) throw corrupt("referenced record missing");
    }
    await verifyChunks(transaction, namespace, record);
    store.put(record, [namespace, record.id]);
    usage.physicalBytes += recordBytes(record);
    usage.records++;
  }
}

function validateHash(hash: string): void { if (!/^[a-f0-9]{64}$/.test(hash)) throw new RangeError("invalid content hash"); }
function validateId(id: string): void { if (id.length === 0 || id.includes(":")) throw new RangeError("content id must be nonempty without colons"); }

async function verifyChunks(transaction: IDBTransaction, namespace: string, record: ContentRecord): Promise<void> {
  for (const content of record.contents) {
    for (const chunk of content.chunks) {
      const size: unknown = await requestValue(transaction.objectStore("chunks").get([namespace, chunk.hash]));
      if (size === undefined || decodeSize(size) !== chunk.size) throw corrupt("referenced chunk missing");
    }
  }
}
