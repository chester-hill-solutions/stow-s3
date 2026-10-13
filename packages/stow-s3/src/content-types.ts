export const MAX_CONTENT_RECORD_BYTES = 1024 * 1024;
import type { LockManagerLike } from "./browser-types.js";

export interface ContentChunk { hash: string; size: number }
export interface ContentReference { size: number; chunks: ContentChunk[] }
export interface ContentRecord {
  id: string;
  metadata: Uint8Array;
  contents: ContentReference[];
  references: string[];
}
export interface ContentHead { name: string; generation: number; recordId: string | null }
export interface ContentPage { records: ContentRecord[]; next?: string }
export interface ContentUsage { physicalBytes: number; chunks: number; records: number; bookkeepingBytes?: number; cacheBytes?: number }
export interface ContentProtection { name: string; recordIds: string[]; lifetime: "handle" | "durable" }
export interface ContentPublication {
  head: string;
  expectedGeneration: number;
  record: ContentRecord;
  additionalRecords?: ContentRecord[];
  stageId: string;
}
export interface ContentAdapter {
  open(namespace: string): Promise<void>;
  getHead(name: string): Promise<ContentHead>;
  getRecord(id: string): Promise<ContentRecord | null>;
  listRecords(options?: { after?: string; limit?: number }): Promise<ContentPage>;
  getChunk(hash: string): Promise<Uint8Array | null>;
  stageChunk(stageId: string, hash: string, data: Uint8Array): Promise<void>;
  releaseStage(stageId: string): Promise<void>;
  listStages(): Promise<string[]>;
  publish(publication: ContentPublication): Promise<ContentHead>;
  protect(name: string, recordIds: string[], options?: { lifetime?: "handle" | "durable" }): Promise<void>;
  listProtections(): Promise<ContentProtection[]>;
  collect(options?: { retainRecordIds?: string[] }): Promise<ContentUsage>;
  usage(): Promise<ContentUsage>;
  close(): Promise<void>;
}
export interface IndexedDbContentOptions {
  indexedDB?: IDBFactory;
  databaseName?: string;
  lockManager?: LockManagerLike;
  quotaBytes?: number;
}
export interface ContentStoreOptions { chunkBytes?: number; cacheBytes?: number }
export interface ContentStageOptions { stageId?: string; signal?: AbortSignal }
export interface StagedContent { stageId: string; content: ContentReference }
