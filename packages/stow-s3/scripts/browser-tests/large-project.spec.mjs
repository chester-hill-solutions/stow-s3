import { test, expect, chromium } from "@playwright/test";
import { writeFile, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const enabled = process.env.STOW_LARGE_PROJECT === "1";
test("qualifies 1 GiB streaming assets and 100 retained document edits", async ({ page }, testInfo) => {
  test.skip(!enabled, "Set STOW_LARGE_PROJECT=1 to run the 1 GiB storage workload");
  test.setTimeout(600_000);
  let persistentContext;
  let profile;
  if (process.env.STOW_LARGE_PROJECT_EPHEMERAL !== "1") {
    profile = await mkdtemp(join(tmpdir(), "stow-large-project-"));
    persistentContext = await chromium.launchPersistentContext(profile, { headless: true });
    page = await persistentContext.newPage();
  }
  await page.route("**/test/harness.html", (route) => route.fulfill({
    status: 200, contentType: "text/html",
    body: "<!doctype html><html lang=en><meta charset=utf-8><title>BrowserProject JavaScript qualification</title></html>",
  }));
  await page.goto(new URL("/test/harness.html", testInfo.project.use.baseURL).href);
  const metrics = await page.evaluate(async () => {
    const measurementScope = "BrowserProject JavaScript facade, ContentStore and IndexedDB on the browser main thread; no WASM runtime is instantiated or measured";
    const longTasks = {
      supported: typeof PerformanceObserver !== "undefined" && PerformanceObserver.supportedEntryTypes.includes("longtask"),
      count: 0, totalDurationMs: 0, maximumDurationMs: 0,
    };
    const recordLongTasks = (entries) => {
      for (const entry of entries) {
        longTasks.count++;
        longTasks.totalDurationMs += entry.duration;
        longTasks.maximumDurationMs = Math.max(longTasks.maximumDurationMs, entry.duration);
      }
    };
    let observer;
    if (longTasks.supported) {
      observer = new PerformanceObserver((list) => recordLongTasks(list.getEntries()));
      observer.observe({ type: "longtask" });
    }
    const finishLongTasks = async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
      if (observer) { recordLongTasks(observer.takeRecords()); observer.disconnect(); }
    };
    const { IndexedDbContentAdapter } = await import("/runtime/indexeddb-content.js");
    const { ContentStore } = await import("/runtime/content-store.js");
    const { BrowserProject } = await import("/runtime/project.js");
    const MiB = 1024 * 1024;
    const assetBytes = 1024 * MiB;
    const databaseName = `qualification-${crypto.randomUUID()}`;
    let adapter = new IndexedDbContentAdapter({ databaseName, quotaBytes: 2 * assetBytes });
    await adapter.open("large-project");
    let store = new ContentStore(adapter, { chunkBytes: MiB, cacheBytes: 8 * MiB });
    let project = await BrowserProject.open(store, { projectId: "large-design", maxVersions: 101 });
    let stagedBytes = 0;
    let maximumInputChunkBytes = 0;
    let peakSampledJSHeapBytes = 0;
    let heapSamples = 0;
    const sampleHeap = () => {
      const bytes = performance.memory?.usedJSHeapSize;
      if (typeof bytes === "number") { heapSamples++; peakSampledJSHeapBytes = Math.max(peakSampledJSHeapBytes, bytes); }
    };
    const stageChunk = adapter.stageChunk.bind(adapter);
    adapter.stageChunk = async (stageId, hash, bytes) => {
      stagedBytes += bytes.byteLength;
      maximumInputChunkBytes = Math.max(maximumInputChunkBytes, bytes.byteLength);
      await stageChunk(stageId, hash, bytes);
      sampleHeap();
    };
    const start = performance.now();
    async function* assets() {
      for (let index = 0; index < 1024; index++) {
        const bytes = new Uint8Array(MiB);
        const words = new Uint32Array(bytes.buffer);
        let value = Math.imul(index + 1, 0x9e3779b9) >>> 0;
        for (let word = 0; word < words.length; word++) {
          value ^= value << 13; value ^= value >>> 17; value ^= value << 5;
          words[word] = value >>> 0;
        }
        new DataView(bytes.buffer).setUint32(0, index, true);
        yield bytes;
      }
    }
    const encoder = new TextEncoder();
    try {
      await project.commit([{ type: "put", key: "asset.bin", data: assets() }, { type: "put", key: "document.json", data: encoder.encode(JSON.stringify({ layer: 0 })) }], { label: "initial" });
    } catch (error) {
      const usage = await store.usage();
      const storageEstimate = await navigator.storage.estimate();
      await project.close(); await adapter.close();
      await finishLongTasks();
      return { failed: true, code: error.code, message: error.message, stagedBytes, usage, originStorageEstimate: storageEstimate, measurementScope, wasmRuntimeInstantiated: false, longTasks };
    }
    const initialUsage = await store.usage();
    const initialStagedBytes = stagedBytes;
    const assetDurationMs = performance.now() - start;
    const editStart = performance.now();
    for (let index = 1; index <= 100; index++) {
      await project.commit([{ type: "put", key: "document.json", data: encoder.encode(JSON.stringify({ layer: index })) }], { label: `edit ${index}` });
    }
    const editDurationMs = performance.now() - editStart;
    const retained = await project.status();
    const afterEdits = await store.usage();
    const editStagedBytes = stagedBytes - initialStagedBytes;
    await project.close(); store.clearCache(); await adapter.close();
    const reopenStart = performance.now();
    adapter = new IndexedDbContentAdapter({ databaseName, quotaBytes: 2 * assetBytes });
    await adapter.open("large-project");
    let payloadReads = 0;
    const getChunk = adapter.getChunk.bind(adapter);
    adapter.getChunk = async (hash) => { payloadReads++; return getChunk(hash); };
    store = new ContentStore(adapter, { cacheBytes: 8 * MiB });
    project = await BrowserProject.open(store, { projectId: "large-design", maxVersions: 101 });
    const status = await project.status();
    const files = await project.listFiles();
    const reopenDurationMs = performance.now() - reopenStart;
    const reopenPayloadReads = payloadReads;
    const range = await project.readRange("asset.bin", 100 * MiB, 100 * MiB + 4);
    const readUsage = await store.usage();
    const originalGet = IDBObjectStore.prototype.get;
    const originalGetAll = IDBObjectStore.prototype.getAll;
    const originalOpenCursor = IDBObjectStore.prototype.openCursor;
    let gcPayloadReads = 0;
    let gcGetAllCalls = 0;
    let outstandingMetadataReads = 0;
    let maximumOutstandingMetadataReads = 0;
    let maximumRecordMetadataBytes = 0;
    IDBObjectStore.prototype.get = function (...args) {
      if (this.name === "payloads") gcPayloadReads++;
      const request = originalGet.apply(this, args);
      if (this.name === "records") {
        outstandingMetadataReads++;
        maximumOutstandingMetadataReads = Math.max(maximumOutstandingMetadataReads, outstandingMetadataReads);
        request.addEventListener("success", () => {
          outstandingMetadataReads--;
          maximumRecordMetadataBytes = Math.max(maximumRecordMetadataBytes, request.result?.metadata?.byteLength ?? 0);
        });
        request.addEventListener("error", () => { outstandingMetadataReads--; });
      }
      return request;
    };
    IDBObjectStore.prototype.getAll = function (...args) {
      gcGetAllCalls++;
      if (this.name === "payloads") gcPayloadReads++;
      return originalGetAll.apply(this, args);
    };
    IDBObjectStore.prototype.openCursor = function (...args) {
      if (this.name === "payloads") gcPayloadReads++;
      return originalOpenCursor.apply(this, args);
    };
    const gcStart = performance.now();
    let afterCollection;
    try { afterCollection = await adapter.collect(); }
    finally {
      IDBObjectStore.prototype.get = originalGet;
      IDBObjectStore.prototype.getAll = originalGetAll;
      IDBObjectStore.prototype.openCursor = originalOpenCursor;
    }
    const gcDurationMs = performance.now() - gcStart;
    const integrityAfterCollection = await project.readRange("asset.bin", 100 * MiB, 100 * MiB + 4);
    const storageEstimate = await navigator.storage.estimate();
    await project.close(); await adapter.close();
    await finishLongTasks();
    return {
      measurementScope, wasmRuntimeInstantiated: false, longTasks,
      assetBytes, dataset: "deterministic xorshift32 generated 1 MiB chunks", edits: 100, retainedVersions: retained.versions.length,
      initialUsage, afterEdits, initialStagedBytes, editStagedBytes,
      maximumInputChunkBytes, configuredCacheBytes: 8 * MiB, readCacheBytes: readUsage.cacheBytes,
      reopenPayloadReads, payloadReadsAfterRange: payloadReads, range: [...range],
      reopenedVersions: status.versions.length, fileCount: files.length,
      assetDurationMs, editDurationMs, reopenDurationMs,
      collection: { afterCollection, gcDurationMs, gcPayloadReads, gcGetAllCalls, maximumOutstandingMetadataReads, maximumRecordMetadataBytes, integrityAfterCollection: [...integrityAfterCollection] },
      heapMeasurement: heapSamples > 0 ? { kind: "coarse sampled Chrome JavaScript heap only; excludes native and WASM allocations", samples: heapSamples, peakSampledJSHeapBytes } : null,
      originStorageEstimate: storageEstimate,
    };
  });
  metrics.context = persistentContext ? "persistent Chromium profile" : "ephemeral Chromium context";
  const receipt = JSON.stringify(metrics, null, 2);
  const path = testInfo.outputPath("large-project-receipt.json");
  await writeFile(path, receipt);
  await writeFile(fileURLToPath(new URL("../../../../.cache/large-project-receipt.json", import.meta.url)), receipt);
  await testInfo.attach("large-project qualification", { path, contentType: "application/json" });
  await persistentContext?.close();
  if (profile) await rm(profile, { recursive: true, force: true });
  expect(metrics.failed ?? false).toBe(false);
  expect(metrics.assetBytes).toBe(1024 ** 3);
  expect(metrics.retainedVersions).toBe(101);
  expect(metrics.reopenedVersions).toBe(101);
  expect(metrics.fileCount).toBe(2);
  expect(metrics.initialUsage.chunks).toBe(1025);
  expect(metrics.afterEdits.chunks).toBe(1125);
  expect(metrics.editStagedBytes).toBeLessThan(4096);
  expect(metrics.afterEdits.physicalBytes).toBeLessThan(metrics.assetBytes * 1.1);
  expect(metrics.maximumInputChunkBytes).toBe(1024 ** 2);
  expect(metrics.reopenPayloadReads).toBe(0);
  expect(metrics.payloadReadsAfterRange).toBe(1);
  expect(metrics.readCacheBytes).toBeLessThanOrEqual(metrics.configuredCacheBytes);
  expect(metrics.range).toEqual([100, 0, 0, 0]);
  expect(metrics.collection.afterCollection.records).toBe(102);
  expect(metrics.collection.afterCollection.chunks).toBe(1125);
  expect(metrics.collection.gcPayloadReads).toBe(0);
  expect(metrics.collection.gcGetAllCalls).toBe(0);
  expect(metrics.collection.maximumOutstandingMetadataReads).toBe(1);
  expect(metrics.collection.maximumRecordMetadataBytes).toBeLessThanOrEqual(1024 ** 2);
  expect(metrics.collection.integrityAfterCollection).toEqual([100, 0, 0, 0]);
});
