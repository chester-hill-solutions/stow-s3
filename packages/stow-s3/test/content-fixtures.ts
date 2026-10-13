import { BrowserPersistenceError } from "../src/browser-types.js";
import { bookkeepingSize } from "../src/content-accounting.js";
import { recordBytes } from "../src/content-gc.js";
import type { ContentAdapter, ContentHead, ContentProtection, ContentPublication, ContentRecord, ContentUsage } from "../src/content-types.js";

export interface MemoryContentNamespace {
  chunks: Map<string, Uint8Array>;
  records: Map<string, ContentRecord>;
  heads: Map<string, ContentHead>;
  stages: Map<string, Set<string>>;
  protections: Map<string, string[]>;
}
export type MemoryContentDatabase = Map<string, MemoryContentNamespace>;

export class MemoryContentAdapter implements ContentAdapter {
  private state?: MemoryContentNamespace;
  private closed = false;
  chunkReads = 0;
  readonly database: MemoryContentDatabase;
  failNextPublish = false;
  constructor(database: MemoryContentDatabase = new Map(), readonly quotaBytes = Number.MAX_SAFE_INTEGER) { this.database = database; }
  async open(namespace: string): Promise<void> {
    this.ensureLive();
    this.state = this.database.get(namespace) ?? { chunks: new Map(), records: new Map(), heads: new Map(), stages: new Map(), protections: new Map() };
    this.database.set(namespace, this.state);
    for (const name of this.state.protections.keys()) if (name.startsWith("handle:")) this.state.protections.delete(name);
  }
  async close(): Promise<void> { this.closed = true; }
  async getHead(name: string): Promise<ContentHead> { return structuredClone(this.current().heads.get(name) ?? { name, generation: 0, recordId: null }); }
  async getRecord(id: string): Promise<ContentRecord | null> { return structuredClone(this.current().records.get(id) ?? null); }
  async listRecords(options: { after?: string; limit?: number } = {}) {
    const ids = [...this.current().records.keys()].sort().filter((id) => options.after === undefined || id > options.after);
    const selected = ids.slice(0, options.limit ?? 100);
    const records: ContentRecord[] = [];
    for (const id of selected) { const record = await this.getRecord(id); if (record !== null) records.push(record); }
    return { records, next: ids.length > selected.length ? selected.at(-1) : undefined };
  }
  async getChunk(hash: string): Promise<Uint8Array | null> { this.chunkReads++; return this.current().chunks.get(hash)?.slice() ?? null; }
  async stageChunk(stageId: string, hash: string, bytes: Uint8Array): Promise<void> {
    const state = this.current();
    const current = await this.usage();
    const increment = (state.chunks.has(hash) ? 0 : bytes.byteLength) +
      (state.stages.get(stageId)?.has(hash) ? 0 : bookkeepingSize(`stage:${stageId}:${hash}`, [hash]));
    if (current.physicalBytes + (current.bookkeepingBytes ?? 0) + increment > this.quotaBytes) throw new BrowserPersistenceError("quota_exceeded", "memory quota exceeded");
    if (!state.chunks.has(hash)) state.chunks.set(hash, bytes.slice());
    const pins = state.stages.get(stageId) ?? new Set();
    pins.add(hash);
    state.stages.set(stageId, pins);
  }
  async releaseStage(stageId: string): Promise<void> { this.current().stages.delete(stageId); }
  async listStages(): Promise<string[]> { return [...this.current().stages.keys()].sort(); }
  async publish(publication: ContentPublication): Promise<ContentHead> {
    if (this.failNextPublish) { this.failNextPublish = false; throw new Error("injected publication failure"); }
    const state = this.current();
    const head = await this.getHead(publication.head);
    if (head.generation !== publication.expectedGeneration) throw new BrowserPersistenceError("persistence_error", "content generation conflict");
    const records = [...publication.additionalRecords ?? [], publication.record];
    const ids = new Set(records.map((record) => record.id));
    if (ids.size !== records.length) throw new Error("duplicate record");
    for (const record of records) {
      if (state.records.has(record.id)) throw new Error("immutable record collision");
      verifyRecordReferences(record, state, ids);
      verifyContentChunks(record, state);
    }
    const next = { name: publication.head, generation: head.generation + 1, recordId: publication.record.id };
    const currentUsage = await this.usage();
    let bytes = records.reduce((sum, record) => sum + recordBytes(record), currentUsage.physicalBytes + (currentUsage.bookkeepingBytes ?? 0));
    bytes += bookkeepingSize(publication.head, next) - (state.heads.has(publication.head) ? bookkeepingSize(publication.head, head) : 0);
    for (const hash of state.stages.get(publication.stageId) ?? []) bytes -= bookkeepingSize(`stage:${publication.stageId}:${hash}`, [hash]);
    if (bytes > this.quotaBytes) throw new BrowserPersistenceError("quota_exceeded", "memory quota exceeded");
    for (const record of records) state.records.set(record.id, structuredClone(record));
    state.heads.set(publication.head, next);
    state.stages.delete(publication.stageId);
    return structuredClone(next);
  }
  async listProtections(): Promise<ContentProtection[]> {
    return [...this.current().protections].map(([key, recordIds]) => ({ name: key.slice(key.indexOf(":") + 1), recordIds: [...recordIds], lifetime: key.startsWith("handle:") ? "handle" : "durable" }));
  }
  async protect(name: string, ids: string[], options: { lifetime?: "handle" | "durable" } = {}): Promise<void> {
    const key = `${options.lifetime === "handle" ? "handle" : "record"}:${name}`;
    const state = this.current();
    const current = await this.usage();
    const before = state.protections.get(key);
    const extra = (ids.length === 0 ? 0 : bookkeepingSize(key, ids)) - (before === undefined ? 0 : bookkeepingSize(key, before));
    if (current.physicalBytes + (current.bookkeepingBytes ?? 0) + extra > this.quotaBytes) throw new BrowserPersistenceError("quota_exceeded", "memory quota exceeded");
    if (ids.length === 0) state.protections.delete(key);
    else state.protections.set(key, [...ids]);
  }
  async collect(options: { retainRecordIds?: string[] } = {}): Promise<ContentUsage> {
    const state = this.current();
    const records = new Set(options.retainRecordIds);
    const chunks = new Set([...state.stages.values()].flatMap((pins) => [...pins]));
    for (const head of state.heads.values()) if (head.recordId !== null) records.add(head.recordId);
    for (const pins of state.protections.values()) for (const id of pins) records.add(id);
    const visit = (id: string): void => {
      const record = state.records.get(id);
      if (record === undefined) throw new Error("missing protected record");
      for (const content of record.contents) for (const chunk of content.chunks) chunks.add(chunk.hash);
      for (const ref of record.references) if (!records.has(ref)) { records.add(ref); visit(ref); }
    };
    for (const id of records) visit(id);
    for (const id of state.records.keys()) if (!records.has(id)) state.records.delete(id);
    for (const hash of state.chunks.keys()) if (!chunks.has(hash)) state.chunks.delete(hash);
    return this.usage();
  }
  async usage(): Promise<ContentUsage> {
    const state = this.current();
    const chunkBytes = [...state.chunks.values()].reduce((sum, bytes) => sum + bytes.byteLength, 0);
    let bookkeepingBytes = 0;
    for (const [stage, hashes] of state.stages) for (const hash of hashes) bookkeepingBytes += bookkeepingSize(`stage:${stage}:${hash}`, [hash]);
    for (const [name, ids] of state.protections) bookkeepingBytes += bookkeepingSize(name, ids);
    for (const [name, head] of state.heads) bookkeepingBytes += bookkeepingSize(name, head);
    return { physicalBytes: [...state.records.values()].reduce((sum, record) => sum + recordBytes(record), chunkBytes), bookkeepingBytes, records: state.records.size, chunks: state.chunks.size };
  }
  private current(): MemoryContentNamespace { this.ensureLive(); if (this.state === undefined) throw new Error("memory content not open"); return this.state; }
  private ensureLive(): void { if (this.closed) throw new BrowserPersistenceError("closed", "memory content closed"); }
}

function verifyContentChunks(record: ContentRecord, state: MemoryContentNamespace): void {
  for (const content of record.contents) {
    for (const chunk of content.chunks) if (!state.chunks.has(chunk.hash)) throw new Error("missing chunk");
  }
}

function verifyRecordReferences(record: ContentRecord, state: MemoryContentNamespace, ids: Set<string>): void {
  for (const id of record.references) if (!state.records.has(id) && !ids.has(id)) throw new Error("missing record");
}
