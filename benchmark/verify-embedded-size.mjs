import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { gzipSync, brotliCompressSync, constants } from "node:zlib";

assert.equal(process.cwd(), "/src");
assert.equal(process.env.BENCH_CLOUD_VERIFY, "1");
const out = "/out/verification/embedded-size";
const beforeRoot = "/tmp/embedded-api-before";
await mkdir(out, { recursive: true });
await mkdir(beforeRoot, { recursive: true });
execFileSync("tar", ["-xzf", "benchmark/variants/before-embedded-api.tar.gz", "-C", beforeRoot]);
const env = { ...process.env, GOOS: "js", GOARCH: "wasm" };
const flags = ["build", "-buildvcs=false", "-trimpath", "-ldflags", "-s -w"];
const digest = bytes => createHash("sha256").update(bytes).digest("hex");
const info = async name => {
  const bytes = await readFile(`${out}/${name}.wasm`);
  assert.ok(WebAssembly.validate(bytes));
  return { bytes: bytes.length, sha256: digest(bytes), gzipBytes: gzipSync(bytes, { level: 9 }).length,
    brotliBytes: brotliCompressSync(bytes, { params: { [constants.BROTLI_PARAM_QUALITY]: 11 } }).length };
};
for (const [name, cwd] of [["before", beforeRoot], ["after", "/src"]]) {
  execFileSync("go", [...flags, "-o", `${out}/${name}.wasm`, "./cmd/stow-wasm"], { cwd, env, stdio: "inherit" });
  await writeFile(`${out}/${name}-dependencies.txt`, execFileSync("go", ["list", "-deps", "./cmd/stow-wasm"], { cwd, env, encoding: "utf8" }));
}
const before = await info("before"), after = await info("after");
assert.equal(before.sha256, digest(await readFile("packages/stow-s3/dist/stow-runtime.wasm")), "Baseline must reproduce the validated SDK");
assert.ok(after.bytes < before.bytes);
execFileSync("go", ["test", "./pkg/stow", "-run", "^TestWasmExcludesNativeDependencies$", "-count=1", "-v"], { stdio: "inherit" });
execFileSync("go", ["build", "./pkg/stow"], { env: { ...process.env, GOOS: "windows", GOARCH: "amd64" }, stdio: "inherit" });
const samples = [];
for (let repetition = 0; repetition < 6; repetition++) {
  for (const variant of repetition % 2 ? ["after", "before"] : ["before", "after"]) {
    samples.push({ variant, repetition, ...JSON.parse(execFileSync(process.execPath, ["benchmark/embedded-startup.mjs", `${out}/${variant}.wasm`], { encoding: "utf8" })) });
  }
}
const median = values => { const sorted = values.toSorted((a, b) => a - b); return (sorted[2] + sorted[3]) / 2; };
const performanceSummary = ["before", "after"].map(variant => {
  const selected = samples.filter(sample => sample.variant === variant);
  return { variant, medianReadyMs: median(selected.map(sample => sample.readyMs)), medianFirstWriteMs: median(selected.map(sample => sample.firstWriteMs)), medianPutGetPairsPerSecond: median(selected.map(sample => sample.putGetPairsPerSecond)) };
});
await writeFile(`${out}/performance.json`, JSON.stringify({ complete: true, samples, summary: performanceSummary, method: "Six alternating fresh Node processes per variant. Read, compile, instantiate, publish bridge, open bucket and first 4 KiB write. Forty verified 1 MiB PUT/GET pairs after five warmup pairs. Binary bridge without sockets, SDK facade, host persistence, or actual browser." }, null, 2) + "\n");
const memorySamples = [];
for (let repetition = 0; repetition < 3; repetition++) {
  for (const variant of repetition % 2 ? ["after", "before"] : ["before", "after"]) {
    const result = JSON.parse(execFileSync(process.execPath, ["scripts/wasm-agent-workload.mjs", `${out}/${variant}.wasm`], { encoding: "utf8" }));
    memorySamples.push({ variant, repetition, ...result });
    console.log(JSON.stringify({ memoryVariant: variant, memory: result.memory, finalMiB: result.finalMiB }));
  }
}
await writeFile(`${out}/memory.json`, JSON.stringify({ complete: true, samples: memorySamples }, null, 2) + "\n");
await writeFile(`${out}/result.json`, JSON.stringify({ complete: true, before, after, savedBytes: before.bytes - after.bytes,
  savedPercent: 100 * (before.bytes - after.bytes) / before.bytes,
  gzipSavedPercent: 100 * (before.gzipBytes - after.gzipBytes) / before.gzipBytes,
  brotliSavedPercent: 100 * (before.brotliBytes - after.brotliBytes) / before.brotliBytes,
  toolchain: execFileSync("go", ["version"], { encoding: "utf8" }).trim(), method: "Same pinned Go compiler and release flags; exact pre-refactor SDK reproduced; canonical embedded API extracted without changing native workspace behavior." }, null, 2) + "\n");
console.log(JSON.stringify({ embeddedSize: { before: before.bytes, after: after.bytes } }));
