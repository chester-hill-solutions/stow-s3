import { expect, test } from "@playwright/test";

test("dedicated worker loads real WASM and carries binary objects without detaching ordinary saves", async ({ page }) => {
  await page.goto("/");
  const result = await page.evaluate(async () => {
    const { loadBrowserWasmHost } = await import("/runtime/browser-wasm-host.js");
    const { AsyncEmbeddedStow } = await import("/runtime/async-embedded.js");
    const host = loadBrowserWasmHost({ maxInFlightBytes: 2_000_000 });
    const runtime = await AsyncEmbeddedStow.open(host, { maxBytes: 2_000_000 });
    try {
      await runtime.createBucket("worker");
      const input = new Uint8Array(1_000_000);
      input[0] = 128;
      input[input.length - 1] = 255;
      await runtime.putObject("worker", "large", input);
      const preservedLength = input.byteLength;
      input[0] = 9;
      const object = await runtime.getObject("worker", "large");
      const transferred = new Uint8Array([7, 128, 255]);
      await runtime.putObject("worker", "transferred", transferred, {}, { transfer: true });
      const read = await runtime.getObjectRange("worker", "large", { offset: 999_999, length: 1 });
      await runtime.putObject("worker", "empty", new Uint8Array());
      const empty = await runtime.getObject("worker", "empty");
      return {
        preservedLength, transferredLength: transferred.byteLength,
        first: object.data[0], last: object.data[object.data.length - 1],
        range: Array.from(read.data), emptyLength: empty.data.byteLength,
      };
    } finally {
      await runtime.close();
      host.close();
    }
  });
  expect(result).toEqual({ preservedLength: 1_000_000, transferredLength: 0, first: 128, last: 255, range: [255], emptyLength: 0 });
});

test("persistent worker streams above transport limits, pins reads, and reopens without replay", async ({ page }) => {
  await page.goto("/");
  const result = await page.evaluate(async () => {
    const { loadBrowserPersistentWasmHost } = await import("/runtime/browser-wasm-host.js");
    const { AsyncEmbeddedStow } = await import("/runtime/async-embedded.js");
    const namespace = crypto.randomUUID();
    const settings = { namespace, databaseName: "stow-worker-tests", cacheBytes: 262_144, physicalQuotaBytes: 12_000_000, maxBytes: 10_000_000 };
    let host = loadBrowserPersistentWasmHost({ maxInFlightBytes: 1_000_000, maxResponseBytes: 1_000_000 });
    let runtime = await AsyncEmbeddedStow.openPersistent(host, settings);
    try {
      await runtime.createBucket("assets");
      await runtime.putObject("assets", "base", new Uint8Array([1, 2, 3, 4]));
      const size = 20 * 262_144;
      async function* source() {
        for (let index = 0; index < 20; index++) {
          if (index === 1) await runtime.headObject("assets", "base");
          yield new Uint8Array(262_144).fill(7);
        }
      }
      const saved = await runtime.putObjectStream("assets", "large", source(), { size });
      const pinned = await runtime.getObjectStream("assets", "large");
      await runtime.putObject("assets", "large", new Uint8Array([9]));
      let readBytes = 0;
      let first = null;
      for await (const chunk of pinned.chunks) { first ??= chunk[0]; readBytes += chunk.byteLength; }
      const controller = new AbortController();
      async function* interrupted() { yield new Uint8Array([5]); controller.abort(); yield new Uint8Array([6]); }
      let cancelled = false;
      try { await runtime.putObjectStream("assets", "interrupted", interrupted(), { size: 2, signal: controller.signal }); }
      catch { cancelled = true; }
      let absent = false;
      try { await runtime.headObject("assets", "interrupted"); } catch (error) { absent = error.code === "object_not_found"; }
      await runtime.close();
      host.close();
      host = loadBrowserPersistentWasmHost({ maxInFlightBytes: 1_000_000, maxResponseBytes: 1_000_000 });
      runtime = await AsyncEmbeddedStow.openPersistent(host, settings);
      return {
        backend: runtime.capabilities().backend, persistent: runtime.capabilities().persistent,
        savedSize: saved.size, pinnedSize: pinned.object.size, readBytes, first,
        latest: Array.from((await runtime.getObject("assets", "large")).data),
        cancelled, absent,
      };
    } finally {
      await runtime.close();
      host.close();
    }
  });
  expect(result).toEqual({ backend: "host", persistent: true, savedSize: 5_242_880, pinnedSize: 5_242_880, readBytes: 5_242_880, first: 7, latest: [9], cancelled: true, absent: true });
});

test("persistent worker releases concurrently closed readers and their content pins", async ({ page }) => {
  await page.goto("/");
  const protections = await page.evaluate(async () => {
    const { loadBrowserPersistentWasmHost } = await import("/runtime/browser-wasm-host.js");
    const { AsyncEmbeddedStow } = await import("/runtime/async-embedded.js");
    const host = loadBrowserPersistentWasmHost();
    const runtime = await AsyncEmbeddedStow.openPersistent(host, {
      namespace: crypto.randomUUID(), databaseName: "stow-worker-close-tests", maxBytes: 1_000_000,
    });
    try {
      await runtime.createBucket("assets");
      await runtime.putObject("assets", "file", new Uint8Array([1, 2, 3]));
      const counts = [];
      for (let round = 0; round < 2; round++) {
        const readers = [];
        for (let index = 0; index < 16; index++) readers.push(await runtime.getObjectStream("assets", "file"));
        counts.push((await runtime.inspectRecovery()).protections.length);
        await Promise.all(readers.flatMap((reader) => [reader.close(), reader.close()]));
        counts.push((await runtime.inspectRecovery()).protections.length);
      }
      return counts;
    } finally {
      await runtime.close();
      host.close();
    }
  });
  expect(protections).toEqual([16, 0, 16, 0]);
});

test("persistent worker inspects crashed stages, refuses live discard, and reclaims abandoned bytes", async ({ page }) => {
  await page.goto("/");
  const result = await page.evaluate(async () => {
    const { loadBrowserPersistentWasmHost } = await import("/runtime/browser-wasm-host.js");
    const { AsyncEmbeddedStow } = await import("/runtime/async-embedded.js");
    const options = { namespace: crypto.randomUUID(), databaseName: "stow-worker-recovery", maxBytes: 1_000_000, physicalQuotaBytes: 2_000_000 };
    let host = loadBrowserPersistentWasmHost();
    let runtime = await AsyncEmbeddedStow.openPersistent(host, options);
    try {
      await runtime.createBucket("assets");
      await runtime.putObject("assets", "retained", new Uint8Array([1, 2, 3, 4]));
      let release;
      const gate = new Promise((resolve) => { release = resolve; });
      async function* partial() { yield new Uint8Array(262_144).fill(55); await gate; yield new Uint8Array([6]); }
      const writing = runtime.putObjectStream("assets", "partial", partial(), { size: 262_145 });
      const ended = writing.catch((error) => error.code);
      let inspection;
      for (let attempt = 0; attempt < 100; attempt++) {
        inspection = await runtime.inspectRecovery();
        if (inspection.stages.length > 0) break;
        await new Promise((resolve) => setTimeout(resolve, 10));
      }
      if (!inspection?.stages.length) throw new Error("the write never staged a chunk");
      const stage = inspection.stages[0];
      let refusedLive = false;
      try { await runtime.discardAbandonedStage(stage); } catch (error) { refusedLive = error.code === "stage_active"; }
      const before = await runtime.collectGarbage();
      host.close();
      release();
      await ended;
      host = loadBrowserPersistentWasmHost();
      for (let attempt = 0; attempt < 100; attempt++) {
        try { runtime = await AsyncEmbeddedStow.openPersistent(host, options); break; }
        catch (error) {
          if (error.code !== "already_open" || attempt === 99) throw error;
          await new Promise((resolve) => setTimeout(resolve, 10));
        }
      }
      const recovered = await runtime.inspectRecovery();
      const foundCrashStage = recovered.stages.includes(stage);
      await runtime.discardAbandonedStage(stage);
      const after = await runtime.collectGarbage();
      const final = await runtime.inspectRecovery();
      let partialAbsent = false;
      try { await runtime.headObject("assets", "partial"); } catch (error) { partialAbsent = error.code === "object_not_found"; }
      return {
        supported: runtime.capabilities().hostMaintenance, refusedLive, foundCrashStage,
        reclaimed: before.physicalBytes - after.physicalBytes,
        stages: final.stages.length, protections: final.protections.length, cacheBytes: after.cacheBytes,
        retained: Array.from((await runtime.getObject("assets", "retained")).data), partialAbsent,
      };
    } finally {
      await runtime.close();
      host.close();
    }
  });
  expect(result).toMatchObject({ supported: true, refusedLive: true, foundCrashStage: true, stages: 0, protections: 0, cacheBytes: 0, retained: [1, 2, 3, 4], partialAbsent: true });
  expect(result.reclaimed).toBeGreaterThanOrEqual(262_144);
});

test("browser WASM starts with streamed and fallback responses and keeps read chunks owned", async ({ page }) => {
  await page.goto("/");
  for (const streaming of [true, false]) {
    if (!streaming) {
      await page.route("**/stow-runtime.wasm", async route => {
        const response = await route.fetch();
        await route.fulfill({ response, headers: { ...response.headers(), "content-type": "application/octet-stream" } });
      });
    }
    const result = await page.evaluate(async () => {
      const { loadBrowserWasmBridge } = await import("/runtime/browser-wasm-host.js");
      const { AsyncEmbeddedStow } = await import("/runtime/async-embedded.js");
      const bridge = await loadBrowserWasmBridge();
      const runtime = await AsyncEmbeddedStow.open({ callBinary: (metadata, data) => bridge.callBinaryAsync(metadata, data) }, { maxBytes: 3_000_000 });
      try {
        await runtime.createBucket("assets");
        const data = new Uint8Array(2_000_000);
        for (let n = 0; n < data.length; n++) data[n] = (n >>> 18) + 1;
        await runtime.putObject("assets", "large", data);
        const read = await runtime.getObjectStream("assets", "large");
        const chunks = [];
        for await (const chunk of read.chunks) chunks.push(chunk);
        let offset = 0;
        for (const chunk of chunks) {
          for (let n = 0; n < chunk.length; n++) if (chunk[n] !== data[offset + n]) throw new Error("retained chunk was changed by buffer reuse");
          offset += chunk.length;
        }
        chunks[0].fill(0);
        const saved = await runtime.getObject("assets", "large");
        return { size: offset, first: saved.data[0], last: saved.data[saved.data.length - 1] };
      } finally {
        await runtime.close();
        await bridge.close();
      }
    });
    expect(result).toEqual({ size: 2_000_000, first: 1, last: 8 });
  }
});
