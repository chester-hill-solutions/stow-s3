// What an agent-shaped workload costs on the wasm runtime.
//
// The deploy-anywhere plan ranked a binary transport as P0 on the strength of two
// 1 MiB-object numbers: 4.5x the payload to store, 7.5x more to read back. An
// agent's turn is not that shape, so scripts/wasm-agent-workload.mjs measures the
// shape that exists and these tests pin the properties that would change the
// transport decision. If one of them stops being true, the plan is wrong and
// should be rewritten rather than the test relaxed.
//
// The three properties, in the order they matter:
//
//   - Reads add no linear memory. An agent's compounding cost is many small
//     reads, so if reads were growing the footprint a binary transport would be
//     urgent for them. They are not: the pages are already committed, and a
//     transport change would not help.
//   - The fixed post-boot floor is the largest single term in total footprint, so
//     shrinking the runtime beats optimizing the bridge for a small-file workload.
//   - Writes do pay a multiple of their payload, which is the one place a
//     transport change would help, and the reason Phase 2 stays on the list rather
//     than being dropped.
//
// The latency numbers are reported, not asserted. A per-call threshold would fail
// on a loaded CI runner and say nothing about the design.

import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import test from "node:test";

const run = promisify(execFile);
const here = dirname(fileURLToPath(import.meta.url));

async function measure() {
  const { stdout } = await run(process.execPath, [join(here, "wasm-agent-workload.mjs")], {
    cwd: join(here, ".."),
    maxBuffer: 8 * 1024 * 1024,
  });
  return JSON.parse(stdout);
}

test("an agent working set reads without growing the footprint", async () => {
  const result = await measure();

  // The property that decides the transport question for reads. Linear memory
  // never shrinks, so a reading of zero after a full pass means the reads did not
  // commit a single new page.
  assert.equal(
    result.memory.readOverheadMiB,
    0,
    `reading the working set committed ${result.memory.readOverheadMiB} MiB; a transport change would now matter for reads`,
  );

  // The harness must still be an isolate. A measurement from a realm that had
  // Node available would not be about the thing the plan is deciding.
  assert.deepEqual(result.nodeApisVisible, [], "the workload ran with Node APIs visible");
  assert.equal(result.hostFunctionCount, 22, "the host surface changed");

  // The workload has to be big enough for the ratio to mean something.
  assert.ok(result.workload.files >= 20, "the working set is too small to conclude anything");
  assert.ok(
    result.workload.totalBytes > 400_000,
    `the working set is only ${result.workload.totalBytes} bytes`,
  );
});

test("the fixed post-boot floor dominates a small-file workload", async () => {
  const result = await measure();

  // This is the finding that reorders Phase 2. For a working set of ordinary
  // source files the floor is the largest single term, so shrinking the runtime
  // buys more than optimizing the bridge does.
  assert.ok(
    result.memory.floorShare > 0.5,
    `floor is only ${result.memory.floorShare} of the footprint; the transport, not the runtime size, is now the dominant term`,
  );
  assert.equal(result.dominantTerm, "fixed boot floor");
});

test("writes pay a multiple of their payload, which is why the transport stays on the list", async () => {
  const result = await measure();

  // The one place a binary transport would help. Seeded here means `workspace
  // prepare` copying a repository in, and the multiple is the base64-in-JSON cost.
  //
  // Note what this number is: linear memory is a high-water mark and does not
  // shrink, so it measures the peak the write path reached, not a steady-state
  // cost per stored byte. The read phase above is what bounds the steady state,
  // and it commits nothing.
  assert.ok(
    result.seed.miBPerMiBStored > 2,
    `seeding cost only ${result.seed.miBPerMiBStored}x its payload; the write path is no longer paying the transport, so Phase 2 should be re-examined`,
  );

  // Reported for a human, since a latency threshold is machine-dependent.
  console.log(
    `agent workload: ${result.workload.files} files / ${result.workload.totalBytes} bytes; ` +
      `read p50 ${result.readWorkingSet.p50Ms}ms p95 ${result.readWorkingSet.p95Ms}ms; ` +
      `seed ${result.seed.miBPerMiBStored}x payload; floor ${result.afterBootMiB} MiB ` +
      `(${result.memory.floorShare} of footprint)`,
  );
});
