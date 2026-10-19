import assert from "node:assert/strict";
import { access } from "node:fs/promises";
import { test } from "node:test";
import { AsyncEmbeddedStow } from "../dist/async-embedded.js";
import { decodeEmbeddedResponse, type EmbeddedCapabilities } from "../dist/embedded.js";
import { loadNodeWasmHost } from "../dist/node-wasm-host.js";
import { BrowserPersistenceError } from "../src/browser-types.js";
import { ContentObjectHost } from "../src/content-object-host.js";
import { ContentStore } from "../src/content-store.js";
import { MemoryContentAdapter } from "./content-fixtures.js";

async function openHostStreams(adapter: MemoryContentAdapter) {
  const wasm = await loadNodeWasmHost();
  await adapter.open("stream-test");
  const content = new ContentStore(adapter, { cacheBytes: 262_144, chunkBytes: 262_144 });
  const storage = new ContentObjectHost(content, { maxStreams: 64 });
  const receipt = await wasm.openHost(JSON.stringify({ version: 2, op: "open", options: { maxBytes: 10_000_000, maxObjects: 5 } }), storage);
  const opened = decodeEmbeddedResponse<{ handle: number; capabilities: EmbeddedCapabilities }>(JSON.parse(receipt.metadata), 2);
  const runtime = AsyncEmbeddedStow.attach({ callBinary: (request, data) => wasm.callBinaryAsync(request, data) }, opened.handle, opened.capabilities);
  return { wasm, content, runtime };
}

test("real Go host streams preserve snapshots, avoid lifetime locks, and free reservations on cancellation", async (t) => {
  try { await access(new URL("../dist/stow-runtime.wasm", import.meta.url)); } catch {
    if (process.env.CI) throw new Error("packaged WASM asset is missing in CI");
    t.skip("WASM asset is built by the WASM test target");
    return;
  }
  const adapter = new MemoryContentAdapter();
  const { wasm, content, runtime } = await openHostStreams(adapter);
  try {
    await runtime.createBucket("assets");
    await runtime.putObject("assets", "base", new Uint8Array([1, 2, 3, 4]));
    const base = await runtime.headObject("assets", "base");
    for (const conditions of [{ ifNoneMatch: base.etag }, { ifNoneMatch: `W/${base.etag}` }, { ifMatch: `W/${base.etag}` }]) {
      await assert.rejects(runtime.putObject("assets", "base", new Uint8Array([1, 2, 3, 4]), conditions), { code: "precondition_failed" });
    }
    for (const ifMatch of ["*", `"other", ${base.etag}`, base.etag.slice(1, -1).toUpperCase()]) {
      await runtime.putObject("assets", "base", new Uint8Array([1, 2, 3, 4]), { ifMatch });
    }
    async function* data() {
      for (let index = 0; index < 20; index++) {
        if (index === 1) await runtime.headObject("assets", "base");
        yield new Uint8Array(262_144).fill(7);
      }
    }
    const saved = await runtime.putObjectStream("assets", "large", data(), { size: 20 * 262_144 });
    assert.equal(saved.size, 20 * 262_144);
    const pinned = await runtime.getObjectStream("assets", "large");
    await runtime.putObject("assets", "large", new Uint8Array([9]));
    let read = 0;
    for await (const chunk of pinned.chunks) { assert.equal(chunk[0], 7); read += chunk.byteLength; }
    assert.equal(read, 20 * 262_144);
    const signal = new AbortController();
    async function* interrupted() { yield new Uint8Array([5]); signal.abort(); yield new Uint8Array([6]); }
    await assert.rejects(runtime.putObjectStream("assets", "interrupted", interrupted(), { size: 2, signal: signal.signal }));
    await assert.rejects(runtime.headObject("assets", "interrupted"), { code: "object_not_found" });
    assert.deepEqual(await runtime.usage(), { bytes: 5, objects: 2 });
    assert.equal((await adapter.listStages()).length, 0);
    assert.ok(((await content.usage()).cacheBytes ?? 0) <= 262_144);
    const attempts = await Promise.allSettled(Array.from({ length: 20 }, () => runtime.getObjectStream("assets", "base")));
    const active = attempts.flatMap((attempt) => attempt.status === "fulfilled" ? [attempt.value] : []);
    assert.equal(active.length, 16);
    await Promise.all(active.map((stream) => stream.close()));
    const reopened = await runtime.getObjectStream("assets", "base");
    await reopened.close();
    assert.equal(adapter.database.get("stream-test")?.protections.size, 0);
  } finally {
    await runtime.close();
    await adapter.close();
    await wasm.close();
  }
});

class FailingReleaseAdapter extends MemoryContentAdapter {
  releaseFailures = 0;
  releaseAttempts = 0;

  override async protect(name: string, ids: string[], options: { lifetime?: "handle" | "durable" } = {}): Promise<void> {
    if (options.lifetime === "handle" && ids.length === 0) {
      this.releaseAttempts++;
      if (this.releaseFailures > 0) {
        this.releaseFailures--;
        throw new BrowserPersistenceError("persistence_error", "injected protection release failure");
      }
    }
    await super.protect(name, ids, options);
  }
}

test("real Go host retains failed read releases until storage acknowledges cleanup", async () => {
  const adapter = new FailingReleaseAdapter();
  const { wasm, runtime } = await openHostStreams(adapter);
  try {
    await runtime.createBucket("assets");
    await runtime.putObject("assets", "base", new Uint8Array([1, 2, 3, 4]));
    const readers = await Promise.all(Array.from({ length: 16 }, () => runtime.getObjectStream("assets", "base")));
    const reader = readers[0];
    assert.ok(reader);
    adapter.releaseFailures = 1;
    await assert.rejects(reader.close(), { code: "runtime_error", message: /injected protection release failure/ });
    assert.equal((await adapter.listProtections()).length, 16);
    await assert.rejects(runtime.getObjectStream("assets", "base"), { code: "quota_exceeded" });
    await Promise.all([reader.close(), reader.close()]);
    assert.equal(adapter.releaseAttempts, 2);
    assert.equal((await adapter.listProtections()).length, 15);
    const reopened = await runtime.getObjectStream("assets", "base");
    await reopened.close();
    await Promise.all(readers.map((stream) => stream.close()));
    assert.equal((await adapter.listProtections()).length, 0);
  } finally {
    await runtime.close();
    await adapter.close();
    await wasm.close();
  }
});
