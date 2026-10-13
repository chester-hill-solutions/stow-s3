import assert from "node:assert/strict";
import { access } from "node:fs/promises";
import { test } from "node:test";

import { EmbeddedStow, EmbeddedStowError } from "../dist/index.js";
import { loadNodeWasmHost } from "../dist/node-wasm-host.js";
import { AsyncEmbeddedStow } from "../dist/async-embedded.js";

test("loadNodeWasmHost starts the packaged memory runtime", async (t) => {
  try {
    await access(new URL("../dist/stow-runtime.wasm", import.meta.url));
  } catch {
    if (process.env.CI) {
      throw new Error("packaged WASM asset is missing in CI");
    }
    t.skip("WASM asset is built by the WASM test target");
    return;
  }

  const host = await loadNodeWasmHost();
  const runtime = EmbeddedStow.open(host, { maxBytes: 8, maxObjects: 2 });
  try {
    runtime.createBucket("package");
    runtime.putObject("package", "hello.txt", new TextEncoder().encode("hello"));
    assert.deepEqual(
      runtime.getObject("package", "hello.txt").data,
      new TextEncoder().encode("hello"),
    );
    assert.equal(typeof host.callBinary, "function");
    const metadata = JSON.stringify({ version: 2, op: "getObject", handle: runtime.handle, bucket: "package", key: "hello.txt" });
    const binary = host.callBinary?.(metadata);
    assert.ok(binary);
    assert.equal(JSON.parse(binary.metadata).result.data, undefined);
    assert.deepEqual(binary.data, new TextEncoder().encode("hello"));
    binary.data?.fill(0);
    assert.deepEqual(runtime.getObject("package", "hello.txt").data, new TextEncoder().encode("hello"));
    runtime.putObject("package", "empty", new Uint8Array());
    assert.deepEqual(runtime.getObject("package", "empty").data, new Uint8Array());
    const asynchronous = await AsyncEmbeddedStow.open({ callBinary: (request, data) => host.callBinaryAsync(request, data) }, { maxBytes: 8 });
    try {
      await asynchronous.createBucket("async");
      const bytes = new Uint8Array([0, 128, 255]);
      await asynchronous.putObject("async", "binary", bytes, { ifNoneMatch: "*" });
      bytes.fill(9);
      assert.deepEqual((await asynchronous.getObject("async", "binary")).data, new Uint8Array([0, 128, 255]));
      assert.deepEqual((await asynchronous.getObjectRange("async", "binary", { offset: 1, length: 2 })).data, new Uint8Array([128, 255]));
      await assert.rejects(asynchronous.putObject("async", "binary", bytes, { ifNoneMatch: "*" }), EmbeddedStowError);
    } finally {
      await asynchronous.close();
    }
    runtime.close();
    assert.throws(() => runtime.usage(), EmbeddedStowError);
  } finally {
    try {
      runtime.close();
    } catch {
      // The public close assertion may have completed already.
    }
    await host.close();
  }
});
