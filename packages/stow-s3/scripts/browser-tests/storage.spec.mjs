import { test, expect } from "@playwright/test";
import { openHarness, invoke, reloadHarness, testKey } from "./helpers.mjs";

test("real IndexedDB retains bytes, metadata, stages and the selected cursor after reload", async ({ page }) => {
  expect(await openHarness(page)).toBeNull(); const key = testKey();
  await invoke(page, "save", key, "first", { metadata: { purpose: "stage" } });
  await invoke(page, "save", key, "second", { label: "Before formatting" });
  expect((await invoke(page, "undo", key)).text).toBe("first");
  await reloadHarness(page);
  expect((await invoke(page, "read", key)).text).toBe("first");
  const redone = await invoke(page, "redo", key);
  expect(redone.text).toBe("second");
  expect(redone.version.label).toBe("Before formatting");
  expect(redone.version.metadata).toEqual({ purpose: "stage" });
});

test("an actual IndexedDB transaction abort leaves no partial edit or stage", async ({ page }) => {
  await openHarness(page); const key = testKey();
  await invoke(page, "save", key, "committed");
  const before = await page.evaluate(() => window.qa.usage());
  await page.evaluate(() => window.qa.abortNextCommit());
  expect((await invoke(page, "save", key, "must roll back")).error).toBe("persistence_error");
  expect((await invoke(page, "read", key)).text).toBe("committed");
  await reloadHarness(page);
  expect((await invoke(page, "status", key)).versions).toHaveLength(1);
  expect(await page.evaluate(() => window.qa.usage())).toEqual(before);
});

test("WASM quota rejection rolls back with the real persistence adapter", async ({ page }) => {
  await openHarness(page, { maxBytes: 4096 }); const key = testKey();
  await invoke(page, "save", key, "small");
  expect((await invoke(page, "save", key, "x".repeat(5000))).error).toBe("quota_exceeded");
  await reloadHarness(page);
  expect((await invoke(page, "read", key)).text).toBe("small");
  expect((await invoke(page, "status", key)).versions).toHaveLength(1);
});

test("Web Locks excludes a competing tab and releases ownership when its page exits", async ({ page, context }) => {
  const namespace = testKey(); await openHarness(page, { namespace });
  const second = await context.newPage();
  expect(await openHarness(second, { namespace })).toBe("already_open");
  await page.close();
  await second.evaluate(() => window.qa.open());
  expect(await second.evaluate(() => window.qa.ready)).toBe(true);
});

test("retention and an edit after undo remove abandoned redo objects", async ({ page }) => {
  await openHarness(page); const key = testKey();
  for (const text of ["one", "two", "three", "four"]) await invoke(page, "save", key, text);
  await invoke(page, "undo", key);
  await invoke(page, "save", key, "branch");
  expect((await invoke(page, "redo", key)).error).toBe("history_boundary");
  expect((await invoke(page, "status", key)).versions).toHaveLength(3);
  expect((await page.evaluate(() => window.qa.usage())).objects).toBe(4);
});

test("malformed persisted history fails instead of replacing local work from upstream", async ({ page }) => {
  await openHarness(page); const key = testKey();
  await invoke(page, "save", key, "local work");
  await page.evaluate((key) => window.qa.corrupt(key), key);
  await reloadHarness(page);
  expect((await invoke(page, "read", key)).error).toBe("history_corrupt");
});

test("namespace reads and deletes never retrieve unrelated payloads", async ({ page }) => {
  await openHarness(page);
  const result = await page.evaluate(async () => {
    const { IndexedDbPersistenceAdapter } = await import("/runtime/browser.js");
    const databaseName = `namespace-ranges-${crypto.randomUUID()}`;
    const first = new IndexedDbPersistenceAdapter({ databaseName });
    const second = new IndexedDbPersistenceAdapter({ databaseName });
    await first.open("first");
    await second.open("second");
    const put = (bucket, key, data) => ({
      type: "putObject", object: { bucket, key, data, size: data.length, etag: "" },
    });
    let generation = await first.commit("first", 0, [
      { type: "putBucket", bucket: { name: "remove" } },
      { type: "putBucket", bucket: { name: "keep" } },
      put("remove", "one", new Uint8Array([1])),
      put("keep", "two", new Uint8Array([2])),
    ]);
    await second.commit("second", 0, [
      { type: "putBucket", bucket: { name: "remove" } },
      put("remove", "large", new Uint8Array(1024 * 1024)),
    ]);
    const queries = [];
    const getAll = IDBObjectStore.prototype.getAll;
    IDBObjectStore.prototype.getAll = function (query, ...args) {
      queries.push({ store: this.name, lower: query?.lower, upper: query?.upper });
      return getAll.call(this, query, ...args);
    };
    try {
      const opened = await first.load("first");
      const openQueries = queries.splice(0);
      generation = await first.commit("first", generation, [{ type: "deleteBucket", bucket: "remove" }]);
      const deleteReads = queries.splice(0).length;
      const remaining = await first.load("first");
      queries.length = 0;
      await first.clear("first", generation);
      const clearReads = queries.splice(0).length;
      const cleared = await first.load("first");
      const neighbor = await second.load("second");
      return {
        openQueries, opened: opened.objects.length, deleteReads, clearReads,
        remaining: remaining.objects.map((object) => object.bucket),
        cleared: cleared.objects.length, neighbor: neighbor.objects[0].size,
      };
    } finally {
      IDBObjectStore.prototype.getAll = getAll;
      await first.close();
      await second.close();
    }
  });
  expect(result.openQueries).toEqual([
    { store: "buckets", lower: ["first"], upper: ["first", []] },
    { store: "objects", lower: ["first"], upper: ["first", []] },
  ]);
  expect(result.opened).toBe(2);
  expect(result.deleteReads).toBe(0);
  expect(result.clearReads).toBe(0);
  expect(result.remaining).toEqual(["keep"]);
  expect(result.cleared).toBe(0);
  expect(result.neighbor).toBe(1024 * 1024);
});
