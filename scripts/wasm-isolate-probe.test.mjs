// The isolate probe is the evidence for running stow's runtime where there is no
// filesystem, no listener, and no Node. A measurement nobody checks is a
// measurement that rots, so this runs the probe and asserts the properties that
// would change the design if they stopped being true.
//
// What is asserted, and why each one matters:
//
//   - the host surface stays small. A binary that starts importing `fs` or
//     `net` cannot run in an isolate, and the failure would be silent until
//     someone tried to deploy it.
//   - the round trip stays byte-identical. This is the assertion that catches a
//     transport bug rather than a boot bug, and it is the one an earlier version
//     of the probe got wrong by sending raw bytes where the bridge wants base64.
//   - the published global keeps its shape. Everything in the TypeScript client
//     calls through it, so a rename is a breaking change to every consumer.
//
// The memory figures are printed rather than asserted. A hard ceiling here would
// fail on a Go patch release that grows linear memory by a page, and the number
// that matters for a device budget is reported for a human to read.

import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import test from "node:test";

const run = promisify(execFile);
const here = dirname(fileURLToPath(import.meta.url));
const probe = join(here, "wasm-isolate-probe.mjs");

async function probeOutput() {
  const { stdout } = await run(process.execPath, [probe], {
    cwd: join(here, ".."),
    maxBuffer: 8 * 1024 * 1024,
  });
  return JSON.parse(stdout);
}

test("the wasm runtime boots and serves operations with no Node APIs present", async () => {
  const result = await probeOutput();

  assert.equal(result.booted, true, "the runtime must publish itself in an isolate-like realm");

  // Only Web-standard host functions. A new import module would mean the
  // runtime had started needing something an isolate does not provide.
  assert.deepEqual(
    Object.keys(result.hostSurface),
    ["gojs"],
    `host surface changed to ${JSON.stringify(result.hostSurface)}`,
  );
  assert.ok(
    result.hostFunctionCount <= 32,
    `host surface grew to ${result.hostFunctionCount} functions; re-check isolate portability`,
  );

  // require, Buffer, module and setImmediate must not be reachable. `process` is
  // expected: wasm_exec.js installs a minimal shim of its own when the host has
  // none, which is a self-contained polyfill rather than a Node dependency.
  for (const api of ["require", "Buffer", "module", "setImmediate"]) {
    assert.ok(
      !result.nodeApisVisible.includes(api),
      `${api} was visible in the isolate; the probe is not hermetic`,
    );
  }

  // Every operation is checked, because the probe reports what it attempted
  // alongside what succeeded, and a silent partial failure is the thing this
  // exists to catch. `smallPuts` is a count and `roundTripMatches` is a boolean,
  // so neither is an "ok" string.
  for (const [operation, outcome] of Object.entries(result.operations)) {
    if (operation === "smallPuts") {
      assert.equal(outcome, "200/200 succeeded", `small puts reported ${outcome}`);
      continue;
    }
    if (operation === "roundTripMatches") {
      assert.equal(outcome, true, "a 1 MiB object did not round-trip byte-identically");
      continue;
    }
    assert.equal(outcome, "ok", `operation ${operation} reported ${JSON.stringify(outcome)}`);
  }

  // The object accounting has to match what was written, or the transport is
  // losing or inflating bytes somewhere the round trip would not show it.
  assert.equal(result.usage.objects, 201, "expected one large object plus 200 small ones");
  assert.equal(result.usage.bytes, 1024 * 1024 + 200 * 5, "stored bytes do not match what was written");

  assert.deepEqual(result.stowKeys, ["call", "exit"], "the published global changed shape");
  assert.equal(result.goRunSettled.settled, true, "the runtime did not shut down when exit() was called");
});

test("the bundle fits a current Cloudflare Worker with room to spare", async () => {
  const result = await probeOutput();
  // Cloudflare removed its compressed-bundle limits on 2026-09-04 and now gates
  // only uncompressed size, at 64 MiB on both free and paid plans. Before that
  // change the compressed limit was 3 MB free / 10 MB paid and this binary did
  // not fit either. The assertion encodes the current limit, so a future change
  // to it is a deliberate edit rather than a silent drift.
  const WORKER_UNCOMPRESSED_LIMIT_MIB = 64;
  assert.ok(
    result.wasmMiB < WORKER_UNCOMPRESSED_LIMIT_MIB / 4,
    `wasm is ${result.wasmMiB} MiB, a quarter of the ${WORKER_UNCOMPRESSED_LIMIT_MIB} MiB Worker limit`,
  );
});

test("the base64 JSON bridge costs several times the payload, which bounds object size", async () => {
  const result = await probeOutput();

  // Small objects are effectively free: 200 of them moved no measurable memory,
  // because linear memory is already-committed pages. This is the good news for
  // a device with many small files.
  assert.equal(
    result.miBPer200SmallObjects,
    0,
    "small objects now cost measurable memory; the per-object overhead changed",
  );

  // A single 1 MiB object costs several times its size to move, because the
  // bridge base64-encodes into a JSON string, crosses the JS boundary, and
  // decodes again — and a read pays it twice. This is the number that decides
  // whether a memory-constrained target is viable, so it is asserted as a floor
  // rather than an exact value: the claim is that the overhead is large, and a
  // future streaming transport would legitimately fail this.
  assert.ok(
    result.miBPer1MiBObject >= 1,
    `a 1 MiB object cost only ${result.miBPer1MiBObject} MiB; if a binary transport landed, update this assertion`,
  );
});
