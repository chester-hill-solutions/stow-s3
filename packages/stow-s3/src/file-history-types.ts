import type { EmbeddedPutOptions } from "./embedded.js";

export type FileHistoryErrorCode =
  | "not_found" | "conflict" | "history_boundary" | "remote_unavailable"
  | "history_corrupt" | "invalid_options" | "uncertain_write";

export class FileHistoryError extends Error {
  constructor(readonly code: FileHistoryErrorCode, message: string) {
    super(message);
    this.name = "FileHistoryError";
  }
}

/** The remote must enforce this condition atomically with its write. */
export type RemoteObjectBasis = { etag: string } | { absent: true };

export interface SyncRemoteObject {
  data: Uint8Array;
  etag: string;
  contentType?: string;
  metadata?: Record<string, string>;
  saveId?: string;
}

export interface SyncRemoteWrite {
  basis: RemoteObjectBasis;
  saveId: string;
  contentType?: string;
  metadata?: Record<string, string>;
  /** Full-object SHA-256 in hexadecimal, verified by the remote storage provider. */
  sha256?: string;
}

/** Host-owned credentials, networking and conditional object writes. */
export interface ObjectSyncRemote {
  headObject?(bucket: string, key: string): Promise<{ etag: string; size: number; metadata?: Record<string, string>; sha256?: string } | null>;
  getObject(bucket: string, key: string): Promise<SyncRemoteObject | null>;
  putObject(bucket: string, key: string, data: Uint8Array, options: SyncRemoteWrite): Promise<{ etag: string }>;
}

export interface BrowserFileHistoryOptions {
  remoteBucket: string;
  remote?: ObjectSyncRemote;
  /** Dedicated local bucket; do not mutate it outside this facade. */
  historyBucket?: string;
  /** Full object snapshots retained per file. Defaults to 50; minimum 2. */
  maxVersions?: number;
}

export interface FileVersion {
  id: string;
  createdAt: string;
  label?: string;
  contentType?: string;
  metadata?: Record<string, string>;
}

export interface FileSaveOptions extends Pick<EmbeddedPutOptions, "contentType" | "metadata"> {
  label?: string;
}

export interface FileHistoryStatus {
  key: string;
  versions: FileVersion[];
  currentVersionId: string;
  pending: boolean;
  canUndo: boolean;
  canRedo: boolean;
  remoteETag?: string;
  hasUncertainWrite: boolean;
}

export interface FileReadResult {
  data: Uint8Array;
  version: FileVersion;
  status: FileHistoryStatus;
}

/** A committed save without materializing the saved file again. */
export type FileSaveReceipt = Omit<FileReadResult, "data">;

export interface FileHistoryExport {
  key: string;
  remoteBucket: string;
  versions: FileVersion[];
  currentVersionId: string;
}

export interface FileSyncAttempt {
  id: string;
  version: FileVersion;
  basis: RemoteObjectBasis;
}

export interface FileHistoryRecord {
  formatVersion: 1;
  remoteBucket: string;
  key: string;
  versions: FileVersion[];
  cursor: number;
  syncedVersionId: string | null;
  basis: RemoteObjectBasis;
  attempt: FileSyncAttempt | null;
}
