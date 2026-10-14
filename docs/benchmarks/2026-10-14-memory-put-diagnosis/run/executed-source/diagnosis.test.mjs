import test from "node:test";
import assert from "node:assert/strict";
import { workload } from "../../packages/stow-s3/scripts/performance/workload.mjs";
import { Measurements } from "../../packages/stow-s3/scripts/performance/metrics.mjs";

test("split generators preserve four distinct writer keys and final expectations", async () => {
  const writes = new Map();
  for (const processes of [1, 4]) {
    writes.clear();
    for (let index = 0; index < processes; index++) {
      const count = 4 / processes;
      const state = { payloads: Array.from({ length: count }, () => Buffer.alloc(1024)), latest: new Map(), unknown: new Set(), workerOffset: index * count };
      const client = { async send(command) { assert.equal(command.input.Bucket, "stow-performance"); assert.equal(command.input.ContentType, "application/octet-stream"); writes.set(command.input.Key, Buffer.from(command.input.Body)); } };
      const task = workload(client, { workload: "put" }, {}, state, new Measurements());
      for (let worker = 0; worker < count; worker++) await task({ worker, sequence: worker + index * count, deadline: Infinity, signal: AbortSignal.timeout(1000) });
      assert.equal(state.latest.size, count); assert.equal(state.unknown.size, 0);
    }
    assert.deepEqual([...writes.keys()].sort(), ["writer-0", "writer-1", "writer-2", "writer-3"]);
    for (let worker = 0; worker < 4; worker++) assert.equal(writes.get(`writer-${worker}`).readBigUInt64LE(0), BigInt(worker));
  }
});
