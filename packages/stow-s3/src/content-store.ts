import { BrowserPersistenceError } from "./browser-types.js";
import type {
  ContentAdapter, ContentReference, ContentStageOptions, ContentStoreOptions,
  ContentUsage, StagedContent,
} from "./content-types.js";

export async function contentHash(bytes: Uint8Array): Promise<string> {
  const digest = await globalThis.crypto.subtle.digest("SHA-256", new Uint8Array(bytes));
  return Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, "0")).join("");
}

export class ContentStore {
  private readonly chunkBytes: number;
  private readonly cacheLimit: number;
  private readonly cache = new Map<string, Uint8Array>();
  private cacheBytes = 0;

  constructor(readonly adapter: ContentAdapter, options: ContentStoreOptions = {}) {
    this.chunkBytes = options.chunkBytes ?? 1024 * 1024;
    this.cacheLimit = options.cacheBytes ?? 64 * 1024 * 1024;
    if (!Number.isSafeInteger(this.chunkBytes) || this.chunkBytes < 1 ||
        !Number.isSafeInteger(this.cacheLimit) || this.cacheLimit < 0) {
      throw new RangeError("chunkBytes must be positive and cacheBytes must be non-negative integers");
    }
  }

  async stage(input: Uint8Array | AsyncIterable<Uint8Array>, options: ContentStageOptions = {}): Promise<StagedContent> {
    const settings = { ...options };
    const source = input instanceof Uint8Array ? new Uint8Array(input) : input;
    const stageId = settings.stageId ?? globalThis.crypto.randomUUID();
    const content: ContentReference = { size: 0, chunks: [] };
    try {
      for await (const bytes of fixedChunks(source, this.chunkBytes, settings.signal)) {
        const hash = await contentHash(bytes);
        settings.signal?.throwIfAborted();
        await this.adapter.stageChunk(stageId, hash, bytes);
        content.chunks.push({ hash, size: bytes.byteLength });
        content.size += bytes.byteLength;
      }
      return { stageId, content };
    } catch (error) {
      if (settings.stageId === undefined) await this.adapter.releaseStage(stageId);
      throw error;
    }
  }

  async *stream(content: ContentReference, options: { start?: number; end?: number; signal?: AbortSignal } = {}): AsyncIterable<Uint8Array> {
    validateContent(content);
    const start = options.start ?? 0;
    const end = options.end ?? content.size;
    if (!Number.isSafeInteger(start) || !Number.isSafeInteger(end) || start < 0 || end < start || end > content.size) {
      throw new RangeError("invalid content range");
    }
    let offset = 0;
    for (const chunk of content.chunks) {
      options.signal?.throwIfAborted();
      const next = offset + chunk.size;
      if (next > start && offset < end) {
        const bytes = await this.loadChunk(chunk.hash, chunk.size);
        yield bytes.slice(Math.max(0, start - offset), Math.min(chunk.size, end - offset));
      }
      offset = next;
      if (offset >= end) break;
    }
  }

  async readRange(content: ContentReference, start = 0, end = content.size): Promise<Uint8Array> {
    validateContent(content);
    if (!Number.isSafeInteger(start) || !Number.isSafeInteger(end) || start < 0 || end < start || end > content.size) {
      throw new RangeError("invalid content range");
    }
    const result = new Uint8Array(end - start);
    let offset = 0;
    for await (const part of this.stream(content, { start, end })) {
      result.set(part, offset);
      offset += part.byteLength;
    }
    return result;
  }

  async usage(): Promise<ContentUsage> {
    return { ...await this.adapter.usage(), cacheBytes: this.cacheBytes };
  }

  clearCache(): void { this.cache.clear(); this.cacheBytes = 0; }

  private async loadChunk(hash: string, size: number): Promise<Uint8Array> {
    const cached = this.cache.get(hash);
    if (cached !== undefined) {
      if (cached.byteLength !== size) throw corrupt("content chunk length differs");
      this.cache.delete(hash);
      this.cache.set(hash, cached);
      return cached;
    }
    const loaded = await this.adapter.getChunk(hash);
    const bytes = loaded === null ? null : new Uint8Array(loaded);
    if (bytes === null || bytes.byteLength !== size || await contentHash(bytes) !== hash) {
      throw corrupt("content chunk is missing or damaged");
    }
    this.remember(hash, bytes);
    return bytes;
  }

  private remember(hash: string, bytes: Uint8Array): void {
    if (bytes.byteLength > this.cacheLimit) return;
    while (this.cacheBytes + bytes.byteLength > this.cacheLimit) {
      const first = this.cache.entries().next().value;
      if (first === undefined) break;
      this.cache.delete(first[0]);
      this.cacheBytes -= first[1].byteLength;
    }
    this.cache.set(hash, bytes);
    this.cacheBytes += bytes.byteLength;
  }
}

export function validateContent(content: ContentReference): void {
  if (!Number.isSafeInteger(content.size) || content.size < 0 || !Array.isArray(content.chunks)) throw corrupt("invalid content descriptor");
  let total = 0;
  for (const chunk of content.chunks) {
    if (!/^[a-f0-9]{64}$/.test(chunk.hash) || !Number.isSafeInteger(chunk.size) || chunk.size < 1) throw corrupt("invalid content chunk descriptor");
    total += chunk.size;
  }
  if (!Number.isSafeInteger(total) || total !== content.size) throw corrupt("invalid content size");
}

async function* fixedChunks(input: Uint8Array | AsyncIterable<Uint8Array>, size: number, signal?: AbortSignal): AsyncIterable<Uint8Array> {
  const source = input instanceof Uint8Array ? single(input) : input;
  let buffer = new Uint8Array(size);
  let filled = 0;
  for await (const part of source) {
    signal?.throwIfAborted();
    let offset = 0;
    while (offset < part.byteLength) {
      const length = Math.min(size - filled, part.byteLength - offset);
      buffer.set(part.subarray(offset, offset + length), filled);
      offset += length;
      filled += length;
      if (filled === size) {
        yield buffer;
        buffer = new Uint8Array(size);
        filled = 0;
      }
    }
  }
  signal?.throwIfAborted();
  if (filled > 0) yield buffer.slice(0, filled);
}

async function* single(bytes: Uint8Array): AsyncIterable<Uint8Array> { yield bytes; }
function corrupt(message: string): BrowserPersistenceError { return new BrowserPersistenceError("persistence_corrupt", message); }
