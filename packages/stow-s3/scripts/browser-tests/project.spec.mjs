import { test, expect } from "@playwright/test";

async function modules(page) {
  await page.goto("/test/harness.html");
  await page.evaluate(async () => {
    const { IndexedDbContentAdapter } = await import("/runtime/indexeddb-content.js");
    const { ContentStore } = await import("/runtime/content-store.js");
    const { BrowserProject } = await import("/runtime/project.js");
    const { ContentObjectHost } = await import("/runtime/content-object-host.js");
    window.projectTest = { IndexedDbContentAdapter, ContentStore, BrowserProject, ContentObjectHost };
  });
}

test("project revisions share assets, replace only overlapping chunks, and reopen with undo", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore, BrowserProject } = window.projectTest;
    const databaseName = `project-${crypto.randomUUID()}`;
    let adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("projects");
    let store = new ContentStore(adapter, { chunkBytes: 4, cacheBytes: 4 });
    let project = await BrowserProject.open(store, { projectId: "design" });
    await project.commit([{ type: "put", key: "asset", data: new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8]) }, { type: "put", key: "document", data: new Uint8Array([1]) }]);
    const initial = await project.listFiles();
    await project.commit([{ type: "replaceRange", key: "asset", offset: 5, data: new Uint8Array([99]) }]);
    const changed = await project.listFiles();
    await project.commit([{ type: "put", key: "document", data: new Uint8Array([2]) }]);
    const usage = await store.usage();
    await project.close(); await adapter.close();
    adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("projects");
    let reads = 0;
    const load = adapter.getChunk.bind(adapter);
    adapter.getChunk = async (hash) => { reads++; return load(hash); };
    store = new ContentStore(adapter, { cacheBytes: 4 });
    project = await BrowserProject.open(store, { projectId: "design" });
    const status = await project.status(); const listed = await project.listFiles();
    const beforeReads = reads;
    await project.undo();
    const undone = await project.read("document");
    await project.undo();
    const original = await project.read("asset");
    await project.close(); await adapter.close();
    return { initial, changed, usage, status, beforeReads, listed: listed.length, undone: [...undone], original: [...original] };
  });
  const initial = result.initial.find((file) => file.key === "asset");
  const changed = result.changed.find((file) => file.key === "asset");
  expect(initial.content.chunks[0].hash).toBe(changed.content.chunks[0].hash);
  expect(initial.content.chunks[1].hash).not.toBe(changed.content.chunks[1].hash);
  expect(result.usage.chunks).toBe(5);
  expect(result.status.versions).toHaveLength(3);
  expect(result.beforeReads).toBe(0);
  expect(result.undone).toEqual([1]);
  expect(result.original).toEqual([1, 2, 3, 4, 5, 6, 7, 8]);
});

test("project commit failure preserves its complete prior revision", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore, BrowserProject } = window.projectTest;
    const adapter = new IndexedDbContentAdapter({ databaseName: `project-${crypto.randomUUID()}` });
    await adapter.open("projects");
    const store = new ContentStore(adapter);
    const project = await BrowserProject.open(store, { projectId: "design" });
    await project.commit([{ type: "put", key: "a", data: new Uint8Array([1]) }, { type: "put", key: "b", data: new Uint8Array([2]) }]);
    let failed = false;
    try { await project.commit([{ type: "put", key: "a", data: new Uint8Array([3]) }, { type: "replaceRange", key: "missing", offset: 0, data: new Uint8Array([4]) }]); } catch { failed = true; }
    const a = await project.read("a"); const b = await project.read("b");
    const status = await project.status(); const stages = await adapter.listStages();
    await adapter.collect();
    const usage = await adapter.usage();
    await project.close(); await adapter.close();
    return { failed, a: [...a], b: [...b], status, stages, usage };
  });
  expect(result.failed).toBe(true);
  expect(result.a).toEqual([1]); expect(result.b).toEqual([2]);
  expect(result.status.versions).toHaveLength(1);
  expect(result.stages).toEqual([]);
  expect(result.usage.chunks).toBe(2);
});

test("host object bridge persists streams lazily and protects active reads through overwrite", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore, ContentObjectHost } = window.projectTest;
    const databaseName = `host-${crypto.randomUUID()}`;
    let adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("objects");
    let store = new ContentStore(adapter, { chunkBytes: 4, cacheBytes: 0 });
    let host = new ContentObjectHost(store);
    const call = async (request, data = new Uint8Array()) => {
      const response = await host.call(JSON.stringify({ version: 2, ...request }), data);
      const metadata = JSON.parse(response.metadata);
      if (!metadata.ok) throw new Error(metadata.error.message);
      return { ...metadata, data: response.data };
    };
    await call({ op: "createBucket", bucket: "assets" });
    const write = async (bytes, etag) => {
      const start = await call({ op: "startPut", bucket: "assets", key: "image" });
      await call({ op: "writePut", token: start.result.token, offset: 0 }, bytes);
      await call({ op: "commitPut", token: start.result.token, size: bytes.length, etag, lastModified: new Date().toISOString() });
    };
    await write(new Uint8Array([1, 2, 3, 4]), "first");
    const read = await call({ op: "openObject", bucket: "assets", key: "image" });
    await write(new Uint8Array([5, 6, 7, 8]), "second");
    await adapter.collect();
    const pinned = await call({ op: "readObject", token: read.result.token, offset: 0, length: 4 });
    await call({ op: "closeObject", token: read.result.token });
    const collected = await adapter.collect();
    await call({ op: "close" }); await adapter.close();
    adapter = new IndexedDbContentAdapter({ databaseName });
    await adapter.open("objects");
    let reads = 0;
    const load = adapter.getChunk.bind(adapter);
    adapter.getChunk = async (hash) => { reads++; return load(hash); };
    store = new ContentStore(adapter, { cacheBytes: 0 });
    host = new ContentObjectHost(store);
    const head = await call({ op: "headObject", bucket: "assets", key: "image" });
    const beforeReads = reads;
    const next = await call({ op: "openObject", bucket: "assets", key: "image" });
    const current = await call({ op: "readObject", token: next.result.token, offset: 1, length: 2 });
    await call({ op: "close" }); await adapter.close();
    return { pinned: [...pinned.data], current: [...current.data], collected, beforeReads, etag: head.result.etag };
  });
  expect(result.pinned).toEqual([1, 2, 3, 4]);
  expect(result.current).toEqual([6, 7]);
  expect(result.collected.chunks).toBe(1);
  expect(result.beforeReads).toBe(0);
  expect(result.etag).toBe("second");
});

test("project sync permits local saves and reconciles a lost hosted reply", async ({ page }) => {
  await modules(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbContentAdapter, ContentStore, BrowserProject } = window.projectTest;
    const { editorRemote } = await import("/remote.mjs");
    const transport = editorRemote(() => false);
    let release;
    let reached;
    const gate = new Promise((resolve) => { release = resolve; });
    const waiting = new Promise((resolve) => { reached = resolve; });
    let block = true;
    let loseReply = false;
    let writes = 0;
    let manifests = 0;
    const remote = {
      getObject: (...args) => transport.getObject(...args),
      async putObject(bucket, key, data, options) {
        writes++;
        const manifest = key.endsWith("/manifest.json");
        if (manifest) {
          manifests++;
          if (block) { block = false; reached(); await gate; }
        }
        const result = await transport.putObject(bucket, key, data, options);
        if (manifest && loseReply) { loseReply = false; throw new Error("injected lost hosted reply"); }
        return result;
      },
    };
    const adapter = new IndexedDbContentAdapter({ databaseName: `project-${crypto.randomUUID()}` });
    await adapter.open("projects");
    const project = await BrowserProject.open(new ContentStore(adapter, { chunkBytes: 4 }), { projectId: crypto.randomUUID(), remote, remoteBucket: "editor" });
    await project.commit([{ type: "put", key: "asset", data: new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8]) }, { type: "put", key: "document", data: new Uint8Array([1]) }]);
    const syncing = project.sync();
    await waiting;
    const localSave = await Promise.race([
      project.commit([{ type: "put", key: "document", data: new Uint8Array([2]) }]),
      new Promise((_, reject) => setTimeout(() => reject(new Error("local save was blocked by remote publication")), 5000)),
    ]);
    release();
    const oldAcknowledgement = await syncing;
    const updated = await project.sync();
    await project.commit([{ type: "put", key: "document", data: new Uint8Array([3]) }]);
    loseReply = true;
    let lost = false;
    try { await project.sync(); } catch { lost = true; }
    const uncertain = await project.status();
    const writesBeforeReconcile = writes;
    const reconciled = await project.sync();
    const writesAfterReconcile = writes;
    const current = await project.read("document");
    await project.close(); await adapter.close();
    return { localSave, oldAcknowledgement, updated, lost, uncertain, reconciled, writesBeforeReconcile, writesAfterReconcile, manifests, current: [...current] };
  });
  expect(result.localSave.versions).toHaveLength(2);
  expect(result.oldAcknowledgement.pending).toBe(true);
  expect(result.updated.pending).toBe(false);
  expect(result.lost).toBe(true);
  expect(result.uncertain.hasUncertainWrite).toBe(true);
  expect(result.reconciled.hasUncertainWrite).toBe(false);
  expect(result.reconciled.pending).toBe(false);
  expect(result.writesAfterReconcile).toBe(result.writesBeforeReconcile);
  expect(result.manifests).toBe(3);
  expect(result.current).toEqual([3]);
});
