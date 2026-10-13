import assert from "node:assert/strict";
import test from "node:test";
import { ContentStore } from "../src/content-store.js";
import type { ContentAdapter, ContentUsage } from "../src/content-types.js";

function fixture() {
  const chunks = new Map<string, Uint8Array>();
  let reads = 0;
  const usage = (): ContentUsage => ({ physicalBytes: [...chunks.values()].reduce((sum, bytes) => sum + bytes.length, 0), records: 0, chunks: chunks.size });
  const adapter: ContentAdapter = {
    async open() {}, async close() {}, async releaseStage() {}, async protect() {},
    async listStages() { return []; }, async listProtections() { return []; },
    async getHead(name) { return { name, generation: 0, recordId: null }; },
    async getRecord() { return null; }, async listRecords() { return { records: [] }; },
    async publish() { throw new Error("not used"); }, async collect() { return usage(); },
    async usage() { return usage(); },
    async stageChunk(_stage, hash, bytes) { if (!chunks.has(hash)) chunks.set(hash, bytes.slice()); },
    async getChunk(hash) { reads++; return chunks.get(hash)?.slice() ?? null; },
  };
  return { adapter, chunks, get reads() { return reads; } };
}

test("streaming stage rechunks input and deduplicates unchanged chunks", async () => {
  const { adapter, chunks } = fixture();
  const store = new ContentStore(adapter, { chunkBytes: 4 });
  async function* inputs() { yield new Uint8Array([1]); yield new Uint8Array([2, 3, 4, 1, 2]); yield new Uint8Array([3, 4, 9]); }
  const staged = await store.stage(inputs());
  assert.deepEqual(staged.content.chunks.map((chunk) => chunk.size), [4, 4, 1]);
  assert.equal(staged.content.chunks[0]?.hash, staged.content.chunks[1]?.hash);
  assert.equal(chunks.size, 2);
  assert.deepEqual(await store.readRange(staged.content, 2, 7), new Uint8Array([3, 4, 1, 2, 3]));
});

test("ranges read only intersecting chunks and LRU remains bounded", async () => {
  const backing = fixture();
  const store = new ContentStore(backing.adapter, { chunkBytes: 4, cacheBytes: 4 });
  const staged = await store.stage(new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8]));
  assert.equal(backing.reads, 0);
  await store.readRange(staged.content, 0, 1);
  assert.equal(backing.reads, 1);
  await store.readRange(staged.content, 0, 1);
  assert.equal(backing.reads, 1);
  await store.readRange(staged.content, 4, 5);
  assert.equal((await store.usage()).cacheBytes, 4);
  await store.readRange(staged.content, 0, 1);
  assert.equal(backing.reads, 3);
});

test("content reads fail closed for missing, corrupt or conflicting descriptors", async () => {
  const backing = fixture();
  const store = new ContentStore(backing.adapter, { chunkBytes: 4, cacheBytes: 0 });
  const staged = await store.stage(new Uint8Array([1, 2, 3, 4]));
  const hash = staged.content.chunks[0]?.hash;
  assert.ok(hash);
  backing.chunks.set(hash, new Uint8Array([4, 3, 2, 1]));
  await assert.rejects(store.readRange(staged.content), { code: "persistence_corrupt" });
  backing.chunks.delete(hash);
  await assert.rejects(store.readRange(staged.content), { code: "persistence_corrupt" });
  await assert.rejects(store.readRange({ ...staged.content, size: 3 }), { code: "persistence_corrupt" });
});

test("empty content and cancellation do not write payloads", async () => {
  const backing = fixture();
  const store = new ContentStore(backing.adapter);
  const staged = await store.stage(new Uint8Array());
  assert.deepEqual(staged.content, { size: 0, chunks: [] });
  assert.deepEqual(await store.readRange(staged.content), new Uint8Array());
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(store.stage(new Uint8Array([1]), { signal: controller.signal }), { name: "AbortError" });
  assert.equal(backing.chunks.size, 0);
});

test("streaming returns detached copies of retained cache bytes", async () => {
  const backing = fixture();
  const store = new ContentStore(backing.adapter);
  const staged = await store.stage(new Uint8Array([1, 2]));
  const first = await store.readRange(staged.content);
  first[0] = 99;
  assert.deepEqual(await store.readRange(staged.content), new Uint8Array([1, 2]));
  await assert.rejects(store.readRange(staged.content, -1, 1), RangeError);
});
