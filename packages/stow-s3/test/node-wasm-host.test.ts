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

for (const surface of ["sync", "async"] as const) {
  test(`${surface} binary managed saves retain observation bytes and commit metadata`, async () => {
    const host = await loadNodeWasmHost();
    const runtime = surface === "sync"
      ? EmbeddedStow.open(host, { maxBytes: 16 })
      : await AsyncEmbeddedStow.open({ callBinary: (request, data) => host.callBinaryAsync(request, data) }, { maxBytes: 16 });
    try {
      await runtime.createBucket("managed");
      const original = new Uint8Array([0, 128, 255]);
      await runtime.putObject("managed", "doc", original);
      const observed = await runtime.readForSave("managed", "doc");
      assert.deepEqual(observed.object.data, original);
      const replacement = new Uint8Array([255, 0, 129]);
      const saved = await runtime.saveObject("managed", "doc", replacement, { observationToken: observed.observationToken });
      assert.equal(saved.outcome, "committed");
      assert.equal(saved.object.size, replacement.length);
      assert.equal(saved.object.etag, (await runtime.headObject("managed", "doc")).etag);
      assert.deepEqual((await runtime.getObject("managed", "doc")).data, replacement);
    } finally {
      try { await runtime.close(); } finally { await host.close(); }
    }
  });
}

test("the raw save bridge distinguishes missing conditions from unknown tokens", async () => {
  const host = await loadNodeWasmHost();
  const runtime = EmbeddedStow.open(host);
  try {
    runtime.createBucket("raw-save");
    for (const [token, code] of [["", "invalid_save_condition"], ["unretained", "invalid_observation"]]) {
      const response = host.callBinary?.(JSON.stringify({ version: 2, op: "saveObject", handle: runtime.handle, bucket: "raw-save", key: "doc", observationToken: token }), new Uint8Array([1]));
      assert.ok(response);
      assert.equal(JSON.parse(response.metadata).error.code, code);
    }
  } finally {
    runtime.close();
    await host.close();
  }
});

test("save observations stay bound to their runtime and resource", async () => {
  const host = await loadNodeWasmHost();
  const first = EmbeddedStow.open(host);
  const second = EmbeddedStow.open(host);
  try {
    for (const runtime of [first, second]) runtime.createBucket("bindings");
    first.putObject("bindings", "doc", new Uint8Array([1]));
    second.putObject("bindings", "doc", new Uint8Array([2]));
    const observed = first.readForSave("bindings", "doc");
    assert.throws(() => second.saveObject("bindings", "doc", new Uint8Array([9]), { observationToken: observed.observationToken }), (error: unknown) => error instanceof EmbeddedStowError && error.code === "invalid_observation");
    assert.throws(() => first.saveObject("bindings", "other", new Uint8Array([9]), { observationToken: observed.observationToken }), (error: unknown) => error instanceof EmbeddedStowError && error.code === "invalid_save_condition");
    second.releaseObservation(observed.observationToken);
    assert.equal(first.saveObject("bindings", "doc", new Uint8Array([3]), { observationToken: observed.observationToken }).outcome, "committed");
    assert.deepEqual(second.getObject("bindings", "doc").data, new Uint8Array([2]));
    assert.throws(() => first.getObject("bindings", "other"), (error: unknown) => error instanceof EmbeddedStowError && error.code === "object_not_found");
  } finally {
    try { first.close(); second.close(); } finally { await host.close(); }
  }
});
