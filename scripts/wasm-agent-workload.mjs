// What does an agent-shaped workload cost on this runtime?
//
// The deploy-anywhere plan ranked a binary transport as P0 on the strength of two
// numbers measured with 1 MiB objects: 4.5x the payload to store and 7.5x more to
// read back. An agent's turn is not that. It is many small reads across a working
// set and few writes, so the cost that compounds is per-call, and the fixed 8 MiB
// post-boot floor may dominate everything else. Optimizing the bridge on the
// large-object numbers alone would be the wrong fix for the workload that exists.
//
// This measures the agent shape and reports both, so the transport decision
// follows the number that matters. It answers three questions:
//
//   1. What does a single small read cost, including the bridge round trip?
//   2. How does linear memory scale with the size of the working set?
//   3. Is the fixed floor or the per-object transport the dominant term?
//
// It reports; it does not assert. A threshold here would fail on a Go patch
// release that grows linear memory by a page, and the numbers are for a human to
// read and a plan to be written against.

import { bootIsolate, b64, unb64 } from "./wasm-isolate-host.mjs";

// A working set shaped like a small source repository: mostly small files, a
// long tail of larger ones, and a handful of generated artefacts. Sizes are the
// thing that matters, because the bridge's cost is proportional to payload and the
// question is whether that proportionality is what an agent pays.
const WORKING_SET = [
  { name: "src/parser/tokenizer.go", size: 4_200 },
  { name: "src/parser/grammar.go", size: 11_800 },
  { name: "src/parser/ast.go", size: 7_300 },
  { name: "src/parser/parser.go", size: 18_600 },
  { name: "src/parser/errors.go", size: 2_100 },
  { name: "src/parser/lexer.go", size: 9_400 },
  { name: "src/store/store.go", size: 6_700 },
  { name: "src/store/memory.go", size: 5_200 },
  { name: "src/store/fs.go", size: 12_900 },
  { name: "src/store/workspace.go", size: 15_300 },
  { name: "src/s3api/server.go", size: 21_400 },
  { name: "src/s3api/handlers.go", size: 34_100 },
  { name: "src/s3api/router.go", size: 8_800 },
  { name: "src/s3api/errors.go", size: 3_300 },
  { name: "src/auth/sigv4.go", size: 16_700 },
  { name: "src/auth/presign.go", size: 5_900 },
  { name: "go.mod", size: 880 },
  { name: "go.sum", size: 2_570 },
  { name: "Makefile", size: 6_481 },
  { name: "README.md", size: 19_783 },
  { name: "CHANGELOG.md", size: 74_215 },
  { name: "docs/workspace-contract.md", size: 18_920 },
  { name: "docs/agent-workspace-plan.md", size: 28_314 },
  { name: "testdata/fixture-large.bin", size: 262_144 },
];

// Roughly what an agent does in a turn: touch a small fraction of the files.
const EDITS = [
  "src/parser/parser.go",
  "src/parser/grammar.go",
  "src/store/fs.go",
  "src/s3api/handlers.go",
  "README.md",
  "go.mod",
];

const percentile = (sorted, p) =>
  sorted.length === 0 ? 0 : sorted[Math.min(sorted.length - 1, Math.floor((p / 100) * sorted.length))];

const iso = await bootIsolate();

const report = {
  workload: { files: WORKING_SET.length, totalBytes: WORKING_SET.reduce((n, f) => n + f.size, 0) },
  bootMs: +iso.bootMs.toFixed(1),
  afterBootMiB: +(iso.memoryBytes() / 1048576).toFixed(2),
  wasmMiB: +(iso.wasmBytes.length / 1048576).toFixed(2),
  hostFunctionCount: Object.values(iso.surface).reduce((n, f) => n + f.length, 0),
};

const opened = iso.call({ op: "open", options: { backend: "memory" } });
if (!opened.ok) {
  console.error(JSON.stringify({ error: "open failed", detail: opened.error }, null, 2));
  process.exit(1);
}
const handle = opened.result.handle;
report.capabilities = opened.result.capabilities;

const content = (size) => b64(Buffer.alloc(size, 0x61));
iso.call({ op: "createBucket", handle, bucket: "repo" });

// Phase 1 — seed, which is what `workspace prepare` does. Timed separately from
// the reads because an agent does not pay it every turn; a host does, once.
let t = performance.now();
for (const file of WORKING_SET) {
  const r = iso.call({ op: "putObject", handle, bucket: "repo", key: file.name, data: content(file.size) });
  if (!r.ok) {
    console.error(JSON.stringify({ error: "seed failed", key: file.name, detail: r.error }, null, 2));
    process.exit(1);
  }
}
const seedMs = performance.now() - t;
const afterSeedMiB = iso.memoryBytes() / 1048576;
report.seed = {
  totalMs: +seedMs.toFixed(1),
  perFileMs: +(seedMs / WORKING_SET.length).toFixed(2),
  afterSeedMiB: +afterSeedMiB.toFixed(2),
  miBPerMiBStored: +((afterSeedMiB - report.afterBootMiB) / (report.workload.totalBytes / 1048576)).toFixed(2),
};

// Phase 2 — the agent reads its working set. This is the cost that compounds:
// one bridge round trip per file, each base64-encoding the object on the way out.
const readTimes = [];
t = performance.now();
for (const file of WORKING_SET) {
  const started = performance.now();
  const r = iso.call({ op: "getObject", handle, bucket: "repo", key: file.name });
  readTimes.push(performance.now() - started);
  if (!r.ok) {
    console.error(JSON.stringify({ error: "read failed", key: file.name, detail: r.error }, null, 2));
    process.exit(1);
  }
  // Verify the bytes, so a fast wrong answer cannot look like a fast right one.
  if (unb64(r.result.data).length !== file.size) {
    console.error(JSON.stringify({ error: "short read", key: file.name, want: file.size }, null, 2));
    process.exit(1);
  }
}
const readMs = performance.now() - t;
readTimes.sort((a, b) => a - b);
report.readWorkingSet = {
  totalMs: +readMs.toFixed(1),
  perFileMs: +(readMs / WORKING_SET.length).toFixed(2),
  p50Ms: +percentile(readTimes, 50).toFixed(2),
  p95Ms: +percentile(readTimes, 95).toFixed(2),
  maxMs: +readTimes[readTimes.length - 1].toFixed(2),
};
const afterReadsMiB = iso.memoryBytes() / 1048576;
report.memory = {
  afterSeedMiB: +afterSeedMiB.toFixed(2),
  afterReadsMiB: +afterReadsMiB.toFixed(2),
  readOverheadMiB: +(afterReadsMiB - afterSeedMiB).toFixed(2),
  floorShare: +(report.afterBootMiB / afterReadsMiB).toFixed(2),
};

// Phase 3 — the edits. A read-modify-write per file, which is the shape of an
// agent changing code rather than replacing it wholesale.
const editTimes = [];
for (const name of EDITS) {
  const file = WORKING_SET.find((f) => f.name === name);
  const got = iso.call({ op: "getObject", handle, bucket: "repo", key: name });
  if (!got.ok) {
    console.error(JSON.stringify({ error: "edit read failed", key: name, detail: got.error }, null, 2));
    process.exit(1);
  }
  const started = performance.now();
  const put = iso.call({
    op: "putObject",
    handle,
    bucket: "repo",
    key: name,
    data: b64(Buffer.concat([unb64(got.result.data), Buffer.from("// edited\n")])),
  });
  editTimes.push(performance.now() - started);
  if (!put.ok) {
    console.error(JSON.stringify({ error: "edit write failed", key: name, detail: put.error }, null, 2));
    process.exit(1);
  }
  if (file) file.size += 9;
}
editTimes.sort((a, b) => a - b);
report.edits = {
  count: EDITS.length,
  totalMs: +editTimes.reduce((n, d) => n + d, 0).toFixed(1),
  p50Ms: +percentile(editTimes, 50).toFixed(2),
  p95Ms: +percentile(editTimes, 95).toFixed(2),
};

report.finalMiB = +(iso.memoryBytes() / 1048576).toFixed(2);
report.nodeApisVisible = ["require", "Buffer", "module", "setImmediate"].filter((k) => k in iso.sandbox);

// A one-line reading of the two competing costs, so the number that should drive
// the transport decision is not left to be inferred.
report.dominantTerm = report.memory.floorShare > 0.5 ? "fixed boot floor" : "per-object transport";

iso.exit();
process.on("uncaughtException", () => {});
process.on("unhandledRejection", () => {});
await Promise.race([iso.settled(), new Promise((r) => setTimeout(r, 5000))]);

console.log(JSON.stringify(report, null, 2));
