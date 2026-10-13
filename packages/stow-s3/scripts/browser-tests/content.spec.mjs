import { test, expect } from "@playwright/test";

const modules = async (page) => {
  await page.goto("/test/harness.html");
  await page.evaluate(async () => {
    const { IndexedDbContentAdapter } = await import("/runtime/indexeddb-content.js");
    const { ContentStore } = await import("/runtime/content-store.js");
    window.contentTest = { IndexedDbContentAdapter, ContentStore };
  });
};

test("content persistence shares chunks, opens without payload reads, and reads ranges", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore } = window.contentTest;
    const databaseName = `content-${crypto.randomUUID()}`;
    let adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("project");
    let store = new ContentStore(adapter, { chunkBytes: 4, cacheBytes: 4 });
    const first = await store.stage(new Uint8Array([1, 2, 3, 4, 1, 2, 3, 4]));
    await adapter.publish({ head: "main", expectedGeneration: 0, stageId: first.stageId, record: { id: "first", metadata: new Uint8Array([1]), contents: [first.content], references: [] } });
    const second = await store.stage(new Uint8Array([1, 2, 3, 4, 9, 8, 7, 6]));
    await adapter.publish({ head: "main", expectedGeneration: 1, stageId: second.stageId, record: { id: "second", metadata: new Uint8Array([2]), contents: [second.content], references: ["first"] } });
    const usage = await adapter.usage();
    await adapter.close();
    adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("project");
    let reads = 0;
    const getChunk = adapter.getChunk.bind(adapter);
    adapter.getChunk = async (hash) => { reads++; return getChunk(hash); };
    store = new ContentStore(adapter, { cacheBytes: 4 });
    const head = await adapter.getHead("main");
    const record = await adapter.getRecord(head.recordId);
    const page = await adapter.listRecords({ limit: 1 });
    const before = reads;
    const bytes = await store.readRange(record.contents[0], 5, 7);
    const after = reads;
    await adapter.close();
    return { usage, before, after, bytes: [...bytes], pageLength: page.records.length, next: page.next, generation: head.generation };
  });
  expect(result.usage.chunks).toBe(2);
  expect(result.before).toBe(0);
  expect(result.after).toBe(1);
  expect(result.bytes).toEqual([8, 7]);
  expect(result.generation).toBe(2);
  expect(result.pageLength).toBe(1);
  expect(result.next).toBe("first");
});

test("publication rolls back the complete record batch on missing references or CAS conflict", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter } = window.contentTest;
    const adapter = new IndexedDbContentAdapter({ databaseName: `content-${crypto.randomUUID()}` });
    await adapter.open("project");
    const record = (id, references = []) => ({ id, metadata: new Uint8Array(), contents: [], references });
    await adapter.publish({ head: "main", expectedGeneration: 0, stageId: "empty", record: record("first") });
    let missing, conflict;
    try { await adapter.publish({ head: "main", expectedGeneration: 1, stageId: "empty", additionalRecords: [record("extra")], record: record("bad", ["missing"]) }); } catch (error) { missing = error.code; }
    try { await adapter.publish({ head: "main", expectedGeneration: 0, stageId: "empty", record: record("stale") }); } catch (error) { conflict = error.code; }
    const head = await adapter.getHead("main");
    const extra = await adapter.getRecord("extra");
    const stale = await adapter.getRecord("stale");
    await adapter.close();
    return { missing, conflict, head, extra, stale };
  });
  expect(result.missing).toBe("persistence_corrupt");
  expect(result.conflict).toBe("persistence_error");
  expect(result.head).toEqual({ name: "main", generation: 1, recordId: "first" });
  expect(result.extra).toBeNull();
  expect(result.stale).toBeNull();
});

test("staging survives page restart and collection protects and releases staging pins", async ({ page }) => {
  await modules(page);
  const databaseName = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore } = window.contentTest;
    const databaseName = `content-${crypto.randomUUID()}`;
    const adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("project");
    await new ContentStore(adapter).stage(new Uint8Array([1, 2, 3]), { stageId: "interrupted" });
    return databaseName;
  });
  // Navigation destroys the writer without asking it to close, releasing its Web Lock.
  await modules(page);
  const result = await page.evaluate(async (databaseName) => {
    const { IndexedDbContentAdapter } = window.contentTest;
    const adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("project");
    const stages = await adapter.listStages();
    const protectedUsage = await adapter.collect();
    await adapter.releaseStage("interrupted");
    const releasedUsage = await adapter.collect();
    await adapter.close();
    return { stages, protectedUsage, releasedUsage };
  }, databaseName);
  expect(result.stages).toEqual(["interrupted"]);
  expect(result.protectedUsage.chunks).toBe(1);
  expect(result.protectedUsage.physicalBytes).toBe(3);
  expect(result.releasedUsage.chunks).toBe(0);
  expect(result.releasedUsage.physicalBytes).toBe(0);
});

test("quota includes staged content and namespaces have independent usage", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore } = window.contentTest;
    const databaseName = `content-${crypto.randomUUID()}`;
    const a = new IndexedDbContentAdapter({ databaseName, quotaBytes: 512 });
    const b = new IndexedDbContentAdapter({ databaseName, quotaBytes: 512 });
    await a.open("a"); await b.open("b");
    await new ContentStore(a).stage(new Uint8Array(128).fill(1), { stageId: "first" });
    let code;
    try { await new ContentStore(a).stage(new Uint8Array(512).fill(4), { stageId: "second" }); } catch (error) { code = error.code; }
    const aUsage = await a.usage(); const bUsage = await b.usage();
    await a.close(); await b.close();
    return { code, aUsage, bUsage };
  });
  expect(result.code).toBe("quota_exceeded");
  expect(result.aUsage.physicalBytes).toBe(128);
  expect(result.bUsage.physicalBytes).toBe(0);
});

test("exclusive Web Locks reject a competing tab and release on close", async ({ page, context }) => {
  await modules(page);
  const databaseName = `content-${crypto.randomUUID()}`;
  await page.evaluate(async (databaseName) => {
    const { IndexedDbContentAdapter } = window.contentTest;
    window.heldContentAdapter = new IndexedDbContentAdapter({ databaseName });
    await window.heldContentAdapter.open("project");
  }, databaseName);
  const second = await context.newPage();
  await modules(second);
  const code = await second.evaluate(async (databaseName) => {
    const { IndexedDbContentAdapter } = window.contentTest;
    try { await new IndexedDbContentAdapter({ databaseName }).open("project"); } catch (error) { return error.code; }
  }, databaseName);
  expect(code).toBe("already_open");
  await page.evaluate(() => window.heldContentAdapter.close());
  await second.evaluate(async (databaseName) => {
    const { IndexedDbContentAdapter } = window.contentTest;
    const adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("project"); await adapter.close();
  }, databaseName);
});

test("collection follows retained record references and releases unused assets", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore } = window.contentTest;
    const adapter = new IndexedDbContentAdapter({ databaseName: `content-${crypto.randomUUID()}` });
    await adapter.open("project");
    const store = new ContentStore(adapter);
    const first = await store.stage(new Uint8Array([1]));
    await adapter.publish({ head: "main", expectedGeneration: 0, stageId: first.stageId, record: { id: "first", metadata: new Uint8Array(), contents: [first.content], references: [] } });
    const next = await store.stage(new Uint8Array([2]));
    await adapter.publish({ head: "main", expectedGeneration: 1, stageId: next.stageId, additionalRecords: [{ id: "asset", metadata: new Uint8Array(), contents: [next.content], references: [] }], record: { id: "second", metadata: new Uint8Array(), contents: [], references: ["asset"] } });
    await adapter.protect("upload-attempt", ["first"]);
    const protectedUsage = await adapter.collect();
    await adapter.protect("upload-attempt", []);
    const releasedUsage = await adapter.collect();
    const missing = await adapter.getRecord("first");
    const asset = await adapter.getRecord("asset");
    await adapter.close();
    return { protectedUsage, releasedUsage, missing, assetId: asset.id };
  });
  expect(result.protectedUsage.records).toBe(3);
  expect(result.protectedUsage.chunks).toBe(2);
  expect(result.releasedUsage.records).toBe(2);
  expect(result.releasedUsage.chunks).toBe(1);
  expect(result.missing).toBeNull();
  expect(result.assetId).toBe("asset");
});

test("damaged stored payloads fail verified reads", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore } = window.contentTest;
    const databaseName = `content-${crypto.randomUUID()}`;
    const adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("project");
    const store = new ContentStore(adapter, { cacheBytes: 0 });
    const staged = await store.stage(new Uint8Array([1, 2, 3]));
    const database = await new Promise((resolve, reject) => {
      const request = indexedDB.open(databaseName);
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    await new Promise((resolve, reject) => {
      const transaction = database.transaction("payloads", "readwrite");
      transaction.objectStore("payloads").put(new Uint8Array([3, 2, 1]), ["project", staged.content.chunks[0].hash]);
      transaction.oncomplete = resolve;
      transaction.onabort = () => reject(transaction.error);
    });
    database.close();
    let code;
    try { await store.readRange(staged.content); } catch (error) { code = error.code; }
    await adapter.close();
    return code;
  });
  expect(result).toBe("persistence_corrupt");
});

test("metadata-only publication cannot bypass namespace capacity", async ({ page }) => {
  await modules(page);
  const code = await page.evaluate(async () => {
    const { IndexedDbContentAdapter } = window.contentTest;
    const adapter = new IndexedDbContentAdapter({ databaseName: `content-${crypto.randomUUID()}`, quotaBytes: 0 });
    await adapter.open("project");
    let code;
    try { await adapter.publish({ head: "main", expectedGeneration: 0, stageId: "empty", record: { id: "empty", metadata: new Uint8Array(), contents: [], references: [] } }); } catch (error) { code = error.code; }
    const head = await adapter.getHead("main");
    const usage = await adapter.usage();
    await adapter.close();
    return { code, head, usage };
  });
  expect(code.code).toBe("quota_exceeded");
  expect(code.head.generation).toBe(0);
  expect(code.usage.records).toBe(0);
});

test("owned reopen releases crashed read handles and preserves durable protections", async ({ page }) => {
  await modules(page);
  const databaseName = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore } = window.contentTest;
    const databaseName = `content-${crypto.randomUUID()}`;
    const adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("project");
    const store = new ContentStore(adapter);
    const first = await store.stage(new Uint8Array([1]));
    await adapter.publish({ head: "main", expectedGeneration: 0, stageId: first.stageId, record: { id: "first", metadata: new Uint8Array(), contents: [first.content], references: [] } });
    await adapter.protect("interrupted-read", ["first"], { lifetime: "handle" });
    const second = await store.stage(new Uint8Array([2]));
    await adapter.publish({ head: "main", expectedGeneration: 1, stageId: second.stageId, record: { id: "second", metadata: new Uint8Array(), contents: [second.content], references: [] } });
    await adapter.protect("durable-upload", ["second"]);
    return databaseName;
  });
  await modules(page);
  const result = await page.evaluate(async (databaseName) => {
    const { IndexedDbContentAdapter } = window.contentTest;
    const adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("project");
    const protections = await adapter.listProtections();
    const usage = await adapter.collect();
    const stale = await adapter.getRecord("first");
    await adapter.close();
    return { protections, usage, stale };
  }, databaseName);
  expect(result.protections).toEqual([{ name: "durable-upload", recordIds: ["second"], lifetime: "durable" }]);
  expect(result.usage.chunks).toBe(1);
  expect(result.stale).toBeNull();
});

test("publication captures caller-owned metadata and descriptors before awaiting storage", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter } = window.contentTest;
    const adapter = new IndexedDbContentAdapter({ databaseName: `content-${crypto.randomUUID()}` });
    await adapter.open("project");
    const publication = { head: "main", expectedGeneration: 0, stageId: "empty", record: { id: "first", metadata: new Uint8Array([1]), contents: [], references: [] } };
    const pending = adapter.publish(publication);
    publication.head = "mutated";
    publication.record.id = "mutated";
    publication.record.metadata[0] = 99;
    publication.record.references.push("missing");
    await pending;
    const head = await adapter.getHead("main");
    const record = await adapter.getRecord("first");
    await adapter.close();
    return { head, metadata: [...record.metadata], references: record.references };
  });
  expect(result.head.recordId).toBe("first");
  expect(result.metadata).toEqual([1]);
  expect(result.references).toEqual([]);
});

test("schema two upgrades existing content without replaying payloads", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore } = window.contentTest;
    const { contentHash } = await import("/runtime/content-store.js");
    const { recordBytes } = await import("/runtime/content-gc.js");
    const { bookkeepingSize } = await import("/runtime/content-accounting.js");
    const databaseName = `upgrade-${crypto.randomUUID()}`;
    const bytes = new Uint8Array([1, 2, 3]);
    const hash = await contentHash(bytes);
    const record = { id: "first", metadata: new Uint8Array([9]), contents: [{ size: 3, chunks: [{ hash, size: 3 }] }], references: [] };
    const head = { name: "main", generation: 1, recordId: "first" };
    const database = await new Promise((resolve, reject) => {
      const request = indexedDB.open(databaseName, 1);
      request.onupgradeneeded = () => { for (const name of ["namespace", "chunks", "payloads", "records", "heads", "pins"]) request.result.createObjectStore(name); };
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    await new Promise((resolve, reject) => {
      const transaction = database.transaction(["namespace", "chunks", "payloads", "records", "heads"], "readwrite");
      transaction.objectStore("namespace").put({ version: 1, physicalBytes: 3 + recordBytes(record), bookkeepingBytes: bookkeepingSize("main", head), chunks: 1, records: 1 }, "project");
      transaction.objectStore("chunks").put(3, ["project", hash]);
      transaction.objectStore("payloads").put(bytes, ["project", hash]);
      transaction.objectStore("records").put(record, ["project", "first"]);
      transaction.objectStore("heads").put(head, ["project", "main"]);
      transaction.oncomplete = resolve;
      transaction.onabort = () => reject(transaction.error);
    });
    database.close();
    const adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("project");
    const loaded = await adapter.getRecord("first");
    const collected = await adapter.collect();
    const content = await new ContentStore(adapter).readRange(loaded.contents[0]);
    await adapter.close();
    return { content: [...content], metadata: [...loaded.metadata], collected };
  });
  expect(result.content).toEqual([1, 2, 3]);
  expect(result.metadata).toEqual([9]);
  expect(result.collected.records).toBe(1);
  expect(result.collected.chunks).toBe(1);
});

test("collection aborts preserve all original records, usage and payloads", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore } = window.contentTest;
    const adapter = new IndexedDbContentAdapter({ databaseName: `gc-abort-${crypto.randomUUID()}` });
    await adapter.open("project");
    const store = new ContentStore(adapter, { cacheBytes: 0 });
    const first = await store.stage(new Uint8Array([1]));
    await adapter.publish({ head: "main", expectedGeneration: 0, stageId: first.stageId, record: { id: "first", metadata: new Uint8Array(), contents: [first.content], references: [] } });
    const second = await store.stage(new Uint8Array([2]));
    await adapter.publish({ head: "main", expectedGeneration: 1, stageId: second.stageId, record: { id: "second", metadata: new Uint8Array(), contents: [second.content], references: [] } });
    const before = await adapter.usage();
    const originalPut = IDBObjectStore.prototype.put;
    let markFailure;
    IDBObjectStore.prototype.put = function (...args) {
      if (this.name === "gcChunks") throw new DOMException("injected temporary mark quota", "QuotaExceededError");
      return originalPut.apply(this, args);
    };
    try { await adapter.collect(); } catch (error) { markFailure = error.code; }
    finally { IDBObjectStore.prototype.put = originalPut; }
    const afterMarkFailure = await adapter.usage();
    const originalDelete = IDBCursor.prototype.delete;
    let sweepFailure;
    let failed = false;
    IDBCursor.prototype.delete = function (...args) {
      const request = originalDelete.apply(this, args);
      if (this.source.name === "records" && !failed) {
        failed = true;
        queueMicrotask(() => this.source.transaction.abort());
      }
      return request;
    };
    try { await adapter.collect(); } catch (error) { sweepFailure = error.code; }
    finally { IDBCursor.prototype.delete = originalDelete; }
    const afterSweepFailure = await adapter.usage();
    const firstBytes = await store.readRange(first.content);
    const secondBytes = await store.readRange(second.content);
    const collected = await adapter.collect();
    await adapter.close();
    return { before, markFailure, afterMarkFailure, sweepFailure, afterSweepFailure, firstBytes: [...firstBytes], secondBytes: [...secondBytes], collected };
  });
  expect(result.markFailure).toBe("quota_exceeded");
  expect(result.afterMarkFailure).toEqual(result.before);
  expect(result.sweepFailure).toBe("persistence_error");
  expect(result.afterSweepFailure).toEqual(result.before);
  expect(result.firstBytes).toEqual([1]); expect(result.secondBytes).toEqual([2]);
  expect(result.collected.records).toBe(1);
  expect(result.collected.chunks).toBe(1);
});
