import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { after, test } from "node:test";
import vm from "node:vm";

const goroot = execFileSync("go", ["env", "GOROOT"], { encoding: "utf8" }).trim();
vm.runInThisContext(await readFile(join(goroot, "lib/wasm/wasm_exec.js"), "utf8"));

const { EmbeddedStow, EmbeddedStowError } = await import(
  new URL("../packages/stow-s3/dist/embedded.js", import.meta.url)
);

const go = new Go();
const wasmBytes = await readFile(new URL("../bin/stow-runtime.wasm", import.meta.url));
const { instance } = await WebAssembly.instantiate(wasmBytes, go.importObject);
const runPromise = go.run(instance);
for (let attempt = 0; attempt < 100 && !globalThis.stow; attempt += 1) {
  await delay(1);
}
assert.ok(globalThis.stow, "WASM runtime did not initialize");

test("EmbeddedStow drives the real memory runtime", async () => {
  const host = { call: (request) => globalThis.stow.call(request) };
  let embedded;
  try {
    embedded = EmbeddedStow.open(host, { maxBytes: 10, maxObjects: 2 });
    const capabilities = embedded.capabilities();
    assert.equal(capabilities.conditionalWrites, true);
    // The save-semantics flags are reported rather than omitted. A caller cannot
    // ask for a guarantee it has no way to see, and a missing field is
    // indistinguishable from false in a language where undefined is falsy.
    for (const name of ["guardedSaves", "durableSaveRequests", "recoveryHolds"]) {
      assert.equal(typeof capabilities[name], "boolean", `${name} must be reported`);
    }
    embedded.createBucket("assets");
    const put = embedded.putObject(
      "assets",
      "hello.txt",
      new TextEncoder().encode("hello"),
      { ifNoneMatch: "*" },
    );
    assert.equal(put.size, 5);

    const got = embedded.getObject("assets", "hello.txt");
    assert.deepEqual(got.data, new TextEncoder().encode("hello"));
    const otherRead = embedded.getObject("assets", "hello.txt");
    embedded.putObject("assets", "hello.txt", new TextEncoder().encode("world"), {
      ifMatch: `"${got.etag}"`,
    });
    for (const [key, options] of [
      ["hello.txt", { ifMatch: otherRead.etag }],
      ["hello.txt", { ifNoneMatch: "*" }],
      ["missing.txt", { ifMatch: otherRead.etag }],
    ]) {
      assert.throws(
        () => embedded.putObject("assets", key, new TextEncoder().encode("stale"), options),
        (error) => error instanceof EmbeddedStowError && error.code === "precondition_failed",
      );
    }
    assert.deepEqual(embedded.getObject("assets", "hello.txt").data, new TextEncoder().encode("world"));
    assert.deepEqual(embedded.usage(), { bytes: 5, objects: 1 });
    embedded.putObject("assets", "hello.txt", new TextEncoder().encode("hello"));
    const listed = embedded.listObjects("assets", { limit: 1 });
    assert.equal(listed.truncated, false);
    assert.deepEqual(listed.objects.map((object) => object.key), ["hello.txt"]);
    assert.deepEqual(
      embedded.listBuckets().map((bucket) => bucket.name),
      ["assets"],
    );
    embedded.copyObject("assets", "hello.txt", "assets", "copy.txt");

    assert.throws(
      () => embedded.putObject("assets", "too-large", new TextEncoder().encode("123456")),
      /quota/i,
    );
    assert.deepEqual(embedded.usage(), { bytes: 10, objects: 2 });
    embedded.reset();
    assert.deepEqual(embedded.usage(), { bytes: 0, objects: 0 });
    embedded.close();
    assert.throws(() => embedded.usage(), EmbeddedStowError);
  } finally {
    try {
      embedded?.close();
    } catch {
      // The test may fail before the public close operation is reached.
    }
  }
});

// A refusal is identified by its code, not by prose: the message is for a human
// and the code is what a caller branches on.
const refusedWith = (code) => (error) => error?.code === code;

// Guarded saves over the real runtime: observe, publish against the observation,
// refuse a stale one without touching the bytes, and replace only on request.
test("guarded saves observe, publish, refuse a stale observation, and replace", async () => {
  const host = { call: (request) => globalThis.stow.call(request) };
  const text = new TextEncoder();
  let embedded;
  try {
    embedded = EmbeddedStow.open(host, { maxBytes: 1 << 20, maxObjects: 100 });
    embedded.createBucket("saves");

    const absent = embedded.readForSave("saves", "doc.txt");
    assert.equal(absent.absent, true);
    assert.equal(typeof absent.observationToken, "string");
    const created = embedded.saveObject("saves", "doc.txt", text.encode("first"), {
      observationToken: absent.observationToken,
    });
    assert.equal(created.outcome, "committed");

    const observed = embedded.readForSave("saves", "doc.txt");
    assert.deepEqual(observed.object.data, text.encode("first"));
    embedded.putObject("saves", "doc.txt", text.encode("interloper"));
    assert.throws(
      () => embedded.saveObject("saves", "doc.txt", text.encode("second"), {
        observationToken: observed.observationToken,
      }),
      refusedWith("save_conflict"),
    );
    // The refused save did not overwrite what the other writer published.
    assert.deepEqual(embedded.getObject("saves", "doc.txt").data, text.encode("interloper"));

    const fresh = embedded.readForSave("saves", "doc.txt");
    assert.equal(
      embedded.saveObject("saves", "doc.txt", text.encode("third"), {
        observationToken: fresh.observationToken,
      }).outcome,
      "committed",
    );

    assert.throws(
      () => embedded.saveObject("saves", "doc.txt", text.encode("unobserved"), {}),
      refusedWith("invalid_save_condition"),
    );
    assert.throws(
      () => embedded.saveObject("saves", "doc.txt", text.encode("keyed"), { replace: true, requestKey: "rk-1" }),
      refusedWith("save_requests_unsupported"),
    );

    const released = embedded.readForSave("saves", "doc.txt");
    embedded.releaseObservation(released.observationToken);
    assert.throws(
      () => embedded.saveObject("saves", "doc.txt", text.encode("after release"), {
        observationToken: released.observationToken,
      }),
      refusedWith("invalid_observation"),
    );

    assert.equal(embedded.saveObject("saves", "doc.txt", text.encode("replaced"), { replace: true }).outcome, "committed");
    assert.deepEqual(embedded.getObject("saves", "doc.txt").data, text.encode("replaced"));
  } finally {
    try {
      embedded?.close();
    } catch {
      // The test may fail before the public close operation is reached.
    }
  }
});

// One shutdown for the whole file: the runtime is a single Go program, so a test
// that ended it would leave the next one with nothing to talk to.
after(async () => {
  globalThis.stow.exit();
  await runPromise;
});
