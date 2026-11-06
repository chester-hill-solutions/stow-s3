import { expect, test } from "@playwright/test";

test("close during ownership acquisition releases the eventual namespace and database", async ({ page }) => {
  await page.goto("/");
  const result = await page.evaluate(async () => {
    const { IndexedDbPersistenceAdapter } = await import("/runtime/browser.js");
    const options = { databaseName: `close-opening-${crypto.randomUUID()}` };
    const first = new IndexedDbPersistenceAdapter(options);
    const opening = first.open("profile").then(() => "opened", (error) => error.code);
    const closing = first.close();
    const sameClose = first.close() === closing;
    await closing;
    const outcome = await opening;
    await first.close();
    const second = new IndexedDbPersistenceAdapter(options);
    await second.open("profile");
    await second.close();
    await new Promise((resolve, reject) => {
      const request = indexedDB.deleteDatabase(options.databaseName);
      request.onsuccess = resolve;
      request.onerror = () => reject(request.error);
      request.onblocked = () => reject(new Error("database remained open"));
    });
    return { outcome, sameClose };
  });
  expect(result).toEqual({ outcome: "closed", sameClose: true });
});

test("close during database open waits for cleanup and rejects new work", async ({ page }) => {
  await page.goto("/");
  const result = await page.evaluate(async () => {
    const { IndexedDbPersistenceAdapter } = await import("/runtime/browser.js");
    let reached;
    const databaseStarted = new Promise((resolve) => { reached = resolve; });
    const databaseName = `close-database-${crypto.randomUUID()}`;
    const adapter = new IndexedDbPersistenceAdapter({
      databaseName,
      indexedDB: { open: (...args) => { const request = indexedDB.open(...args); reached(); return request; } },
    });
    const opening = adapter.open("profile").then(() => "opened", (error) => error.code);
    await databaseStarted;
    const closing = adapter.close();
    const load = await adapter.load("profile").then(() => "loaded", (error) => error.code);
    await closing;
    const outcome = await opening;
    const replacement = new IndexedDbPersistenceAdapter({ databaseName });
    await replacement.open("profile");
    await replacement.close();
    return { load, outcome };
  });
  expect(result).toEqual({ load: "closed", outcome: "closed" });
});

test("one adapter refuses a second opening namespace and can retry a failed open", async ({ page }) => {
  await page.goto("/");
  const result = await page.evaluate(async () => {
    const { IndexedDbPersistenceAdapter } = await import("/runtime/browser.js");
    const databaseName = `opening-guard-${crypto.randomUUID()}`;
    let calls = 0;
    const adapter = new IndexedDbPersistenceAdapter({
      databaseName,
      indexedDB: { open: (...args) => {
        if (calls++ === 0) throw new Error("injected database-open failure");
        return indexedDB.open(...args);
      } },
    });
    const first = adapter.open("first").then(() => "opened", (error) => error.code);
    const concurrent = await adapter.open("second").then(() => "opened", (error) => error.code);
    const failure = await first;
    await adapter.open("first");
    const empty = await adapter.load("first");
    await adapter.close();
    const replacement = new IndexedDbPersistenceAdapter({ databaseName });
    await replacement.open("first");
    await replacement.close();
    return { concurrent, failure, empty };
  });
  expect(result).toEqual({ concurrent: "already_open", failure: "persistence_error", empty: null });
});

test("abandoned blocked opens abort late upgrades and do not retain a database", async ({ page }) => {
  await page.goto("/");
  const result = await page.evaluate(async () => {
    const { openIndexedDatabase } = await import("/runtime/indexeddb-lifecycle.js");
    const name = `blocked-open-${crypto.randomUUID()}`;
    const blocker = await new Promise((resolve, reject) => {
      const request = indexedDB.open(name, 1);
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    let request;
    let upgrades = 0;
    const factory = { open: (...args) => { request = indexedDB.open(...args); return request; } };
    const failure = await openIndexedDatabase(factory, {
      name, version: 2, upgrade: () => { upgrades++; }, failedMessage: "failed", blockedMessage: "blocked",
    }).then(() => "opened", (error) => error.code);
    const settled = new Promise((resolve) => {
      request.addEventListener("success", resolve, { once: true });
      request.addEventListener("error", resolve, { once: true });
    });
    blocker.close();
    await settled;
    const version = await new Promise((resolve, reject) => {
      const check = indexedDB.open(name);
      check.onsuccess = () => { const version = check.result.version; check.result.close(); resolve(version); };
      check.onerror = () => reject(check.error);
    });
    return { failure, upgrades, version };
  });
  expect(result).toEqual({ failure: "persistence_denied", upgrades: 0, version: 1 });
});
