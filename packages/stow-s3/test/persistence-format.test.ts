import assert from "node:assert/strict";
import { it } from "node:test";
import { applyChanges, normalizeSnapshot } from "../src/persistence-format.js";

it("reuses unchanged owned buffers and isolates new changes from their caller", () => {
  const original = new Uint8Array(1024 * 1024);
  original[0] = 7;
  const snapshot = normalizeSnapshot({
    formatVersion: 1,
    generation: 1,
    buckets: [{ name: "assets" }],
    objects: [{ bucket: "assets", key: "large", data: original, size: original.length }],
  });
  original[0] = 9;
  assert.equal(snapshot.objects[0]?.data?.[0], 7);
  const small = new Uint8Array([1]);
  const metadata = { label: "original" };
  const next = applyChanges(snapshot, [{
    type: "putObject",
    object: { bucket: "assets", key: "small", data: small, size: 1, etag: "", metadata },
  }]);
  assert.strictEqual(next.objects[0]?.data, snapshot.objects[0]?.data);
  small[0] = 5;
  metadata.label = "changed";
  assert.equal(next.objects[1]?.data?.[0], 1);
  assert.deepEqual(next.objects[1]?.metadata, { label: "original" });
  const removed = applyChanges(next, [{ type: "deleteObject", bucket: "assets", key: "small" }]);
  assert.strictEqual(removed.objects[0]?.data, snapshot.objects[0]?.data);
});
