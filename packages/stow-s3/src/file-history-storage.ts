import { EmbeddedStowError } from "./embedded.js";
import type { BrowserObjectMutation } from "./browser-types.js";
import type { BrowserEmbeddedStow } from "./persistent-embedded.js";
import { currentVersion, decodeHistory, filePrefix, revisionKey } from "./file-history-format.js";
import {
  FileHistoryError,
  type FileHistoryRecord,
  type FileSaveOptions,
  type FileVersion,
} from "./file-history-types.js";

export class FileHistoryStorage {
  constructor(
    private readonly store: BrowserEmbeddedStow,
    private readonly bucket: string,
    readonly remoteBucket: string,
    private readonly maxVersions: number,
  ) {}

  async initialize(): Promise<void> {
    const buckets = await this.store.listBuckets();
    if (!buckets.some((bucket) => bucket.name === this.bucket)) await this.store.createBucket(this.bucket);
  }

  async load(key: string): Promise<FileHistoryRecord | null> {
    try {
      const object = await this.store.getObject(this.bucket, this.recordKey(key));
      if (!object.data) throw new FileHistoryError("history_corrupt", "file history has no bytes");
      return decodeHistory(object.data, this.remoteBucket, key);
    } catch (error) {
      if (error instanceof EmbeddedStowError && error.code === "object_not_found") return null;
      throw error;
    }
  }

  async bytes(record: FileHistoryRecord, version = currentVersion(record)): Promise<Uint8Array> {
    try {
      const object = await this.store.getObject(this.bucket, revisionKey(record, version.id));
      if (object.data) return object.data;
    } catch (error) {
      if (!(error instanceof EmbeddedStowError && error.code === "object_not_found")) throw error;
    }
    throw new FileHistoryError("history_corrupt", "a retained file revision is missing");
  }

  async write(record: FileHistoryRecord, additional: BrowserObjectMutation[] = []): Promise<FileHistoryRecord> {
    await this.store.writeBatch([...additional, {
      type: "put", bucket: this.bucket, key: this.recordKey(record.key),
      data: new TextEncoder().encode(JSON.stringify(record)),
      options: { contentType: "application/json" },
    }]);
    return record;
  }

  async append(
    key: string,
    previous: FileHistoryRecord | null,
    data: Uint8Array,
    options: FileSaveOptions,
    remoteETag?: string,
  ): Promise<FileHistoryRecord> {
    if (Object.keys(options.metadata ?? {}).some((key) => key.toLowerCase() === "stow-save-id")) {
      throw new FileHistoryError("invalid_options", "stow-save-id metadata is reserved for sync receipts");
    }
    const current = previous && remoteETag === undefined ? currentVersion(previous) : undefined;
    const version = newVersion(options, current);
    const record = appendedRecord(previous, version, { remoteBucket: this.remoteBucket, key }, { maxVersions: this.maxVersions, remoteETag });
    const retained = new Set(record.versions.map((item) => item.id));
    if (record.attempt) retained.add(record.attempt.version.id);
    const removed = previous?.versions.filter((item) => !retained.has(item.id)) ?? [];
    const mutations: BrowserObjectMutation[] = removed.map((item) => this.removeRevision(record, item.id));
    mutations.push({
      type: "put", bucket: this.bucket, key: revisionKey(record, version.id), data,
      options: { contentType: version.contentType, metadata: version.metadata, ifNoneMatch: "*" },
    });
    return this.write(record, mutations);
  }

  async acknowledge(record: FileHistoryRecord, etag: string): Promise<FileHistoryRecord> {
    if (!record.attempt || !etag) throw new FileHistoryError("uncertain_write", "remote write has no confirmed identity");
    const versionId = record.attempt.version.id;
    const next = { ...record, basis: { etag }, syncedVersionId: versionId, attempt: null };
    return this.write(next, this.obsoleteAttempt(record));
  }

  clearAttempt(record: FileHistoryRecord): Promise<FileHistoryRecord> {
    return this.write({ ...record, attempt: null }, this.obsoleteAttempt(record));
  }

  private obsoleteAttempt(record: FileHistoryRecord): BrowserObjectMutation[] {
    const id = record.attempt?.version.id;
    return id && !record.versions.some((version) => version.id === id)
      ? [this.removeRevision(record, id)] : [];
  }

  private removeRevision(record: FileHistoryRecord, id: string): BrowserObjectMutation {
    return { type: "delete", bucket: this.bucket, key: revisionKey(record, id) };
  }

  private recordKey(key: string): string {
    return `${filePrefix(this.remoteBucket, key)}state.json`;
  }
}

function newVersion(options: FileSaveOptions, previous?: FileVersion): FileVersion {
  return {
    id: crypto.randomUUID(), createdAt: new Date().toISOString(), label: options.label,
    contentType: options.contentType ?? previous?.contentType,
    metadata: options.metadata ?? previous?.metadata,
  };
}

function appendedRecord(
  previous: FileHistoryRecord | null,
  version: FileVersion,
  destination: { remoteBucket: string; key: string },
  options: { maxVersions: number; remoteETag?: string },
): FileHistoryRecord {
  const kept = previous?.versions.slice(0, previous.cursor + 1) ?? [];
  const versions = [...kept, version].slice(-options.maxVersions);
  return {
    formatVersion: 1, ...destination, versions, cursor: versions.length - 1,
    syncedVersionId: options.remoteETag ? version.id : previous?.syncedVersionId ?? null,
    basis: options.remoteETag ? { etag: options.remoteETag } : previous?.basis ?? { absent: true },
    attempt: previous?.attempt ?? null,
  };
}
