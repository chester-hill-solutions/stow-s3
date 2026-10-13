import {
  FileHistoryError,
  type FileHistoryRecord,
  type FileHistoryStatus,
  type FileSyncAttempt,
  type FileVersion,
  type RemoteObjectBasis,
} from "./file-history-types.js";

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function filePrefix(bucket: string, key: string): string {
  if (!key || !bucket) throw new FileHistoryError("invalid_options", "bucket and file key must be non-empty");
  return `files/${encodeURIComponent(bucket)}/${encodeURIComponent(key)}/`;
}

export function revisionKey(record: Pick<FileHistoryRecord, "remoteBucket" | "key">, id: string): string {
  return `${filePrefix(record.remoteBucket, record.key)}revisions/${id}`;
}

export function currentVersion(record: FileHistoryRecord): FileVersion {
  const version = record.versions[record.cursor];
  if (!version) throw corrupt();
  return version;
}

export function fileStatus(record: FileHistoryRecord): FileHistoryStatus {
  return {
    key: record.key,
    versions: structuredClone(record.versions),
    currentVersionId: currentVersion(record).id,
    pending: record.attempt !== null || currentVersion(record).id !== record.syncedVersionId,
    canUndo: record.cursor > 0,
    canRedo: record.cursor < record.versions.length - 1,
    remoteETag: "etag" in record.basis ? record.basis.etag : undefined,
    hasUncertainWrite: record.attempt !== null,
  };
}

export function decodeHistory(data: Uint8Array, bucket: string, key: string): FileHistoryRecord {
  try {
    const value: unknown = JSON.parse(new TextDecoder().decode(data));
    return validateRecord(value, bucket, key);
  } catch {
    throw corrupt();
  }
}

function validateRecord(value: unknown, bucket: string, key: string): FileHistoryRecord {
  if (!isRecord(value) || value.formatVersion !== 1 || value.remoteBucket !== bucket || value.key !== key) throw corrupt();
  if (!Array.isArray(value.versions) || value.versions.length === 0) throw corrupt();
  const versions = value.versions.map(validateVersion);
  if (new Set(versions.map((version) => version.id)).size !== versions.length) throw corrupt();
  const cursor = value.cursor;
  if (typeof cursor !== "number" || !Number.isInteger(cursor) || cursor < 0 || cursor >= versions.length) throw corrupt();
  return {
    formatVersion: 1, remoteBucket: bucket, key, versions, cursor, ...validateSyncState(value),
  };
}

function validateSyncState(value: Record<string, unknown>): Pick<FileHistoryRecord, "syncedVersionId" | "basis" | "attempt"> {
  const syncedVersionId = value.syncedVersionId;
  if (syncedVersionId !== null && (typeof syncedVersionId !== "string" || !uuid.test(syncedVersionId))) throw corrupt();
  const basis = validateBasis(value.basis);
  const attempt = validateAttempt(value.attempt);
  if ((syncedVersionId === null) !== ("absent" in basis)) throw corrupt();
  if (attempt && JSON.stringify(attempt.basis) !== JSON.stringify(basis)) throw corrupt();
  return { syncedVersionId, basis, attempt };
}

function validateVersion(value: unknown): FileVersion {
  if (!isRecord(value) || typeof value.id !== "string" || !uuid.test(value.id)) throw corrupt();
  if (typeof value.createdAt !== "string" || !Number.isFinite(Date.parse(value.createdAt))) throw corrupt();
  return {
    id: value.id,
    createdAt: value.createdAt,
    label: optionalString(value.label),
    contentType: optionalString(value.contentType),
    metadata: validateMetadata(value.metadata),
  };
}

function validateAttempt(value: unknown): FileSyncAttempt | null {
  if (value === null) return null;
  if (!isRecord(value) || typeof value.id !== "string" || !uuid.test(value.id)) throw corrupt();
  return { id: value.id, version: validateVersion(value.version), basis: validateBasis(value.basis) };
}

function validateBasis(value: unknown): RemoteObjectBasis {
  if (!isRecord(value)) throw corrupt();
  if (typeof value.etag === "string" && value.etag.length > 0 && value.absent === undefined) return { etag: value.etag };
  if (value.absent === true && value.etag === undefined) return { absent: true };
  throw corrupt();
}

function validateMetadata(value: unknown): Record<string, string> | undefined {
  if (value === undefined) return undefined;
  if (!isRecord(value)) throw corrupt();
  const result: Record<string, string> = {};
  for (const [key, item] of Object.entries(value)) {
    if (typeof item !== "string") throw corrupt();
    result[key] = item;
  }
  return result;
}

function optionalString(value: unknown): string | undefined {
  if (value === undefined || typeof value === "string") return value;
  throw corrupt();
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function corrupt(): FileHistoryError {
  return new FileHistoryError("history_corrupt", "file history is invalid or has an unsupported format");
}
