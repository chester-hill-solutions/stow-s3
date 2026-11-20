import { expect, test } from "@playwright/test";

test("persistence adapter implements the five operations over IndexedDB", async ({ page }) => {
  await page.goto("/");
  const outcome = await page.evaluate(async () => {
    const { IndexedDbAdapter } = await import("/runtime/browser.js");
    const adapter = await IndexedDbAdapter.open({ databaseName: `adapter-${crypto.randomUUID()}` });
    const encoder = new TextEncoder();
    const summary = {
      body: "", contentType: "", metadata: "", etag: "", changedEtag: "", createConflict: "",
      staleConflict: "", objects: [], prefixes: [], pages: [],
      deleted: "", deleteMissing: "",
    };
    try {
      const first = await adapter.put("alpha", "a/one.txt", encoder.encode("one"), {
        contentType: "text/plain", metadata: { owner: "spec" },
      });
      const got = await adapter.get("alpha", "a/one.txt");
      summary.body = new TextDecoder().decode(got.data);
      summary.contentType = got.metadata.contentType;
      summary.metadata = got.metadata.metadata.owner ?? "";
      summary.etag = first.etag;
      const second = await adapter.put("alpha", "a/one.txt", encoder.encode("two"));
      summary.changedEtag = second.etag === first.etag ? "unchanged" : "changed";
      try {
        await adapter.conditionalPut("alpha", "a/one.txt", encoder.encode("x"), { condition: { ifAbsent: true } });
      } catch (error) {
        summary.createConflict = error.code;
      }
      try {
        await adapter.conditionalPut("alpha", "a/one.txt", encoder.encode("x"), { condition: { ifMatch: first.etag } });
      } catch (error) {
        summary.staleConflict = error.code;
      }
      await adapter.conditionalPut("alpha", "a/one.txt", encoder.encode("three"), { condition: { ifMatch: second.etag } });
      const final = await adapter.get("alpha", "a/one.txt");
      summary.body = new TextDecoder().decode(final.data);
      await adapter.put("alpha", "b/two.txt", encoder.encode("two"));
      await adapter.put("alpha", "top.txt", encoder.encode("top"));
      const prefixed = await adapter.list("alpha", { prefix: "a/" });
      summary.objects = prefixed.objects.map((object) => object.key);
      const grouped = await adapter.list("alpha", { delimiter: "/" });
      summary.prefixes = grouped.commonPrefixes;
      let token = "";
      for (let page = 0; page < 4; page += 1) {
        const listing = await adapter.list("alpha", { maxKeys: 1, continuationToken: token });
        summary.pages.push(...listing.objects.map((object) => object.key));
        if (!listing.isTruncated) break;
        token = listing.nextContinuationToken;
      }
      await adapter.delete("alpha", "top.txt");
      try {
        await adapter.get("alpha", "top.txt");
      } catch (error) {
        summary.deleted = error.code;
      }
      try {
        await adapter.delete("alpha", "top.txt");
      } catch (error) {
        summary.deleteMissing = error.code;
      }
    } finally {
      adapter.close();
    }
    return summary;
  });
  const { etag, ...rest } = outcome;
  expect(etag).toMatch(/^[0-9a-f]{64}$/);
  expect(rest).toEqual({
    body: "three",
    contentType: "text/plain",
    metadata: "spec",
    changedEtag: "changed",
    createConflict: "precondition_failed",
    staleConflict: "precondition_failed",
    objects: ["a/one.txt"],
    prefixes: ["a/", "b/"],
    pages: ["a/one.txt", "b/two.txt", "top.txt"],
    deleted: "not_found",
    deleteMissing: "not_found",
  });
});

test("persistence adapter paginates within a delimiter group without repeating it", async ({ page }) => {
  await page.goto("/");
  const outcome = await page.evaluate(async () => {
    const { IndexedDbAdapter } = await import("/runtime/browser.js");
    const adapter = await IndexedDbAdapter.open({ databaseName: `adapter-groups-${crypto.randomUUID()}` });
    const encoder = new TextEncoder();
    try {
      for (const key of ["a/1.txt", "a/2.txt", "b/1.txt", "root.txt"]) {
        await adapter.put("alpha", key, encoder.encode(key));
      }
      const first = await adapter.list("alpha", { delimiter: "/", maxKeys: 1 });
      const second = await adapter.list("alpha", {
        delimiter: "/", maxKeys: 1, continuationToken: first.nextContinuationToken,
      });
      const third = await adapter.list("alpha", {
        delimiter: "/", maxKeys: 1, continuationToken: second.nextContinuationToken,
      });
      return {
        first: first.commonPrefixes,
        second: second.commonPrefixes,
        third: third.commonPrefixes,
        thirdObjects: third.objects.map((object) => object.key),
        truncated: third.isTruncated,
      };
    } finally {
      adapter.close();
    }
  });
  expect(outcome).toEqual({ first: ["a/"], second: ["b/"], third: [], thirdObjects: ["root.txt"], truncated: false });
});

test("persistence adapter keeps the prefix on the second page", async ({ page }) => {
  await page.goto("/");
  const outcome = await page.evaluate(async () => {
    const { IndexedDbAdapter } = await import("/runtime/browser.js");
    const adapter = await IndexedDbAdapter.open({ databaseName: `adapter-prefix-pages-${crypto.randomUUID()}` });
    const encoder = new TextEncoder();
    try {
      for (const key of ["a/1.txt", "a/2.txt", "b/1.txt"]) {
        await adapter.put("alpha", key, encoder.encode(key));
      }
      const first = await adapter.list("alpha", { prefix: "a/", maxKeys: 1 });
      const second = await adapter.list("alpha", {
        prefix: "a/", maxKeys: 1, continuationToken: first.nextContinuationToken,
      });
      return {
        first: first.objects.map((object) => object.key),
        second: second.objects.map((object) => object.key),
        truncated: second.isTruncated,
      };
    } finally {
      adapter.close();
    }
  });
  expect(outcome).toEqual({ first: ["a/1.txt"], second: ["a/2.txt"], truncated: false });
});
