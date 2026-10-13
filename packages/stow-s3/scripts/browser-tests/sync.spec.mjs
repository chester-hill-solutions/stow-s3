import { test, expect } from "@playwright/test";
import { openHarness, invoke, reloadHarness, remoteStats, testKey } from "./helpers.mjs";

test("selected stages publish through the host transport and cached reads stay offline", async ({ page, context, request }) => {
  await openHarness(page); const key = testKey();
  for (const value of ["first", "second", "third"]) await invoke(page, "save", key, value);
  await invoke(page, "undo", key);
  expect((await invoke(page, "sync", key)).pending).toBe(false);
  const object = await request.get(`/api/object?key=${encodeURIComponent(key)}`);
  expect(await object.text()).toBe("second");
  await context.setOffline(true);
  expect((await invoke(page, "read", key)).text).toBe("second");
  expect((await invoke(page, "redo", key)).text).toBe("third");
  await context.setOffline(false);
  expect((await remoteStats(request, key)).writes).toBe(1);
});

test("a lost response after remote commit reconciles across page reload without another write", async ({ page, request }) => {
  await openHarness(page); const key = testKey();
  await invoke(page, "save", key, "committed remotely");
  await page.route("**/api/object?*", async (route) => {
    if (route.request().method() !== "PUT") return route.continue();
    const response = await route.fetch(); expect(response.ok()).toBe(true);
    await route.abort("connectionreset");
  });
  expect((await invoke(page, "sync", key)).error).toBe("transport_error");
  await page.unroute("**/api/object?*");
  await reloadHarness(page);
  expect((await invoke(page, "status", key)).hasUncertainWrite).toBe(true);
  expect((await invoke(page, "sync", key)).pending).toBe(false);
  expect((await remoteStats(request, key)).writes).toBe(1);
});

test("an aborted IndexedDB acknowledgement recovers the real remote receipt after reload", async ({ page, request }) => {
  await openHarness(page); const key = testKey();
  await invoke(page, "save", key, "remote success, local acknowledgement interrupted");
  await page.evaluate(() => window.qa.abortNextAcknowledgement());
  expect((await invoke(page, "sync", key)).error).toBe("persistence_error");
  await reloadHarness(page);
  expect((await invoke(page, "sync", key)).pending).toBe(false);
  expect((await remoteStats(request, key)).writes).toBe(1);
});

test("two isolated browser stores compete for one remote ETag and exactly one wins", async ({ page, context }) => {
  await openHarness(page); const key = testKey();
  await invoke(page, "save", key, "base"); await invoke(page, "sync", key);
  const second = await context.newPage(); await openHarness(second);
  expect((await invoke(second, "read", key)).text).toBe("base");
  await invoke(page, "save", key, "writer one"); await invoke(second, "save", key, "writer two");
  const results = await Promise.all([invoke(page, "sync", key), invoke(second, "sync", key)]);
  expect(results.filter((result) => result.pending === false)).toHaveLength(1);
  expect(results.filter((result) => result.error === "conflict")).toHaveLength(1);
  expect((await invoke(page, "read", key)).text).toBe("writer one");
  expect((await invoke(second, "read", key)).text).toBe("writer two");
});

test("an existing remote key conflicts with an offline-created file, and explicit adoption retains undo", async ({ page, request }) => {
  await openHarness(page); const key = testKey();
  expect((await request.post(`/api/other-edit?key=${encodeURIComponent(key)}`)).ok()).toBe(true);
  await invoke(page, "save", key, "offline-created edit");
  expect((await invoke(page, "sync", key)).error).toBe("conflict");
  expect((await invoke(page, "read", key)).text).toBe("offline-created edit");
  expect((await invoke(page, "adoptRemote", key)).status.pending).toBe(false);
  expect((await invoke(page, "undo", key)).text).toBe("offline-created edit");
  expect((await invoke(page, "sync", key)).pending).toBe(false);
});

test("HTTP 403 remains a failed read rather than a missing file", async ({ page }) => {
  await openHarness(page); const key = testKey();
  await page.route("**/api/object?*", (route) => route.fulfill({ status: 403 }));
  expect((await invoke(page, "read", key)).error).toBe("transport_error");
  await page.unroute("**/api/object?*");
  expect((await invoke(page, "status", key)).error).toBe("not_found");
});

test("direct Railway requests exercise CORS, conditional headers and exposed receipt metadata", async ({ page }) => {
  test.skip(process.env.STOW_BROWSER_ALLOW_LIVE !== "1", "Direct provider qualification requires the live Railway profile");
  page.on("console", (message) => {
    if (message.type() === "error") process.stdout.write(`Browser: ${message.text().replace(/https?:\/\/[^\s]+/g, "[URL]")}\n`);
  });
  await openHarness(page, { direct: true }); const key = testKey();
  await invoke(page, "save", key, "direct browser bytes", { contentType: "text/plain", metadata: { purpose: "direct-cors" } });
  expect(await invoke(page, "sync", key)).toMatchObject({ pending: false });
  const second = await page.context().newPage(); await openHarness(second, { direct: true });
  const fetched = await invoke(second, "read", key);
  expect(fetched.text).toBe("direct browser bytes"); expect(fetched.version.metadata).toEqual({ purpose: "direct-cors" });
  await invoke(second, "save", key, "remote writer"); expect((await invoke(second, "sync", key)).pending).toBe(false);
  await invoke(page, "save", key, "stale direct browser edit");
  expect((await invoke(page, "sync", key)).error).toBe("conflict");
  expect((await invoke(page, "read", key)).text).toBe("stale direct browser edit");
});
