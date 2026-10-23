import assert from "node:assert/strict";
import { test } from "node:test";
import { ContentStore } from "../src/content-store.js";
import { ContentObjectHost } from "../src/content-object-host.js";
import { MemoryContentAdapter } from "./content-fixtures.js";

async function fixture() {
  const adapter = new MemoryContentAdapter();
  await adapter.open("objects");
  const store = new ContentStore(adapter, { chunkBytes: 4, cacheBytes: 8 });
  return { adapter, store, host: new ContentObjectHost(store) };
}
async function call(host: ContentObjectHost, request: Record<string, unknown>, data = new Uint8Array()) {
  const reply = await host.call(JSON.stringify({ version: 2, ...request }), data);
  const envelope: { ok: boolean; result?: unknown; error?: { code: string } } = JSON.parse(reply.metadata);
  return { ...envelope, data: reply.data };
}
async function put(host: ContentObjectHost, key: string, text: string, options = {}) {
  const start = await call(host, { op: "startPut", bucket: "objects", key, ...options });
  assert.ok(start.ok);
  const token = start.result as { token: string };
  const bytes = new TextEncoder().encode(text);
  const write = await call(host, { op: "writePut", token: token.token, offset: 0 }, bytes);
  assert.ok(write.ok);
  return call(host, { op: "commitPut", token: token.token, size: bytes.length, etag: `"${text}"`, lastModified: new Date().toISOString() });
}

test("persistent object host stages without visibility, reads pinned ranges, and opens from metadata", async () => {
  const { adapter, store, host } = await fixture();
  assert.ok((await call(host, { op: "createBucket", bucket: "objects" })).ok);
  assert.ok((await put(host, "file", "abcdefghijkl")).ok);
  const open = await call(host, { op: "openObject", bucket: "objects", key: "file" });
  const token = open.result as { token: string };
  await put(host, "file", "replacement");
  await adapter.collect();
  const range = await call(host, { op: "readObject", token: token.token, offset: 4, length: 4 });
  assert.equal(new TextDecoder().decode(range.data), "efgh");
  await call(host, { op: "closeObject", token: token.token });
  await adapter.collect();
  const chunkReads = adapter.chunkReads;
  const reopened = new ContentObjectHost(new ContentStore(adapter));
  assert.ok((await call(reopened, { op: "listBuckets" })).ok);
  assert.ok((await call(reopened, { op: "listObjects", bucket: "objects", list: { limit: 1 } })).ok);
  assert.equal(adapter.chunkReads, chunkReads);
  assert.ok(((await store.usage()).cacheBytes ?? 0) <= 8);
});

test("conditional host commit is atomic and aborted stages can be reclaimed", async () => {
  const { adapter, host } = await fixture();
  await call(host, { op: "createBucket", bucket: "objects" });
  await put(host, "file", "first");
  host.checkConditions = () => JSON.stringify({ version: 2, ok: false, error: { code: "precondition_failed", message: "shared predicate refused" } });
  const collision = await put(host, "file", "second", { ifNoneMatch: "*" });
  assert.equal(collision.error?.code, "precondition_failed");
  const head = await call(host, { op: "headObject", bucket: "objects", key: "file" });
  assert.equal((head.result as { etag: string }).etag, '"first"');
  for (const token of await adapter.listStages()) await call(host, { op: "abortPut", token });
  await adapter.collect();
  assert.equal((await adapter.usage()).chunks, 2);
});

test("copy shares payloads and bucket deletion refuses nonempty data", async () => {
  const { adapter, host } = await fixture();
  await call(host, { op: "createBucket", bucket: "objects" });
  await put(host, "file", "abcdefgh");
  const count = (await adapter.usage()).chunks;
  const copy = await call(host, { op: "copyObject", sourceBucket: "objects", sourceKey: "file", destinationBucket: "objects", destinationKey: "copy" });
  assert.ok(copy.ok);
  assert.equal((await adapter.usage()).chunks, count);
  assert.equal((await call(host, { op: "deleteBucket", bucket: "objects" })).error?.code, "bucket_not_empty");
});

test("a delimiter groups keys into directory rows the host can page over", async () => {
  const { host } = await fixture();
  assert.ok((await call(host, { op: "createBucket", bucket: "objects" })).ok);
  for (const key of ["root.txt", "docs/a.txt", "docs/b.txt", "docs/deep/c.txt", "img/logo.png"]) {
    assert.ok((await put(host, key, key)).ok);
  }

  const flat = await call(host, { op: "listObjects", bucket: "objects", list: {} });
  const flatPage = flat.result as { objects: { key: string }[] };
  assert.equal(flatPage.objects.length, 5);

  const top = await call(host, { op: "listObjects", bucket: "objects", list: { delimiter: "/" } });
  const topPage = top.result as { objects: { key: string }[]; commonPrefixes: string[] };
  assert.deepEqual(topPage.objects.map((object) => object.key), ["root.txt"]);
  assert.deepEqual(topPage.commonPrefixes, ["docs/", "img/"]);

  const docs = await call(host, { op: "listObjects", bucket: "objects", list: { prefix: "docs/", delimiter: "/" } });
  const docsPage = docs.result as { objects: { key: string }[]; commonPrefixes: string[] };
  assert.deepEqual(docsPage.objects.map((object) => object.key), ["docs/a.txt", "docs/b.txt"]);
  assert.deepEqual(docsPage.commonPrefixes, ["docs/deep/"]);

  // A page ends on a group, and the cursor names that group rather than a key in it.
  const first = await call(host, { op: "listObjects", bucket: "objects", list: { delimiter: "/", limit: 1 } });
  const firstPage = first.result as { commonPrefixes: string[]; truncated: boolean; nextCursor: string };
  assert.deepEqual(firstPage.commonPrefixes, ["docs/"]);
  assert.equal(firstPage.truncated, true);
  assert.equal(firstPage.nextCursor, "docs/");
  const second = await call(host, {
    op: "listObjects",
    bucket: "objects",
    list: { delimiter: "/", limit: 1, cursor: firstPage.nextCursor },
  });
  const secondPage = second.result as { commonPrefixes: string[]; objects: { key: string }[]; truncated: boolean; nextCursor?: string };
  assert.deepEqual(secondPage.commonPrefixes, ["img/"]);
  const third = await call(host, {
    op: "listObjects",
    bucket: "objects",
    list: { delimiter: "/", limit: 1, cursor: secondPage.nextCursor },
  });
  const thirdPage = third.result as { objects: { key: string }[]; truncated: boolean };
  assert.deepEqual(thirdPage.objects.map((object) => object.key), ["root.txt"]);
  assert.equal(thirdPage.truncated, false);
});
