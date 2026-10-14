import assert from "node:assert/strict";
import { test } from "node:test";
import { PassThrough } from "node:stream";
import { setTimeout as delay } from "node:timers/promises";
import { performance } from "node:perf_hooks";
import { digest, verifiedRead, workload } from "./workload.mjs";
import { Measurements } from "./metrics.mjs";

test("download verification rejects corrupted content of the correct length", async () => {
  const original = Buffer.from("payload");
  const expected = { bytes: original.length, sha256: digest(original) };
  const client = { async send() { return { Body: [Buffer.from("payloae")] }; } };
  await assert.rejects(verifiedRead(client, "object", expected), { name: "IntegrityFailure" });
});

test("an aborted download destroys its stream and releases the reader", async () => {
  const body = new PassThrough();
  const client = { async send() { return { Body: body }; } };
  await Promise.all([
    assert.rejects(verifiedRead(client, "object", { bytes: 7, sha256: digest("payload") }, AbortSignal.timeout(20)), { name: "TimeoutError" }),
    delay(50),
  ]);
  assert.equal(body.destroyed, true);
});

test("unacknowledged overwrites are marked unknown rather than verified against old bytes", async () => {
  const state = { fixtures: [], payloads: [Buffer.alloc(16)], latest: new Map([["writer-0", { bytes: 16, sha256: "previous" }]]), unknown: new Set() };
  const client = { async send() { const error = new Error("timeout"); error.name = "TimeoutError"; throw error; } };
  const task = workload(client, { workload: "put" }, {}, state, new Measurements());
  await assert.rejects(task({ worker: 0, sequence: 1, deadline: performance.now() + 1000, signal: AbortSignal.timeout(1000) }), { name: "TimeoutError" });
  assert.equal(state.latest.has("writer-0"), false);
  assert.equal(state.unknown.has("writer-0"), true);
});

test("download verification consumes all chunks and checks total size", async () => {
  let chunks = 0;
  const client = { async send() { return { Body: (async function* () { chunks++; yield Buffer.from("pay"); chunks++; yield Buffer.from("load"); })() }; } };
  await verifiedRead(client, "object", { bytes: 7, sha256: digest("payload") });
  assert.equal(chunks, 2);
  await assert.rejects(verifiedRead(client, "object", { bytes: 8, sha256: digest("payload") }), { name: "IntegrityFailure" });
});
