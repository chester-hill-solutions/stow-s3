import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { createServer } from "node:http";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { arch, cpus, platform, release, totalmem } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { promisify } from "node:util";

const execute = promisify(execFile);
const [baselineArgument, candidateArgument, outputArgument] = process.argv.slice(2);
if (!baselineArgument || !candidateArgument || !outputArgument) {
  throw new Error("usage: node qualify.mjs BASELINE_ROOT CANDIDATE_ROOT OUTPUT_DIRECTORY");
}
const roots = { baseline: resolve(baselineArgument), candidate: resolve(candidateArgument) };
const out = resolve(outputArgument);
await mkdir(out, { recursive: true });
const hash = (bytes) => createHash("sha256").update(bytes).digest("hex");
const identity = async (file) => {
  const bytes = await readFile(file);
  return { bytes: bytes.length, sha256: hash(bytes) };
};
const metadata = {
  version: 1, startedAt: new Date().toISOString(), complete: false,
  host: { platform: platform(), arch: arch(), release: release(), cpu: cpus()[0]?.model, logicalCpus: cpus().length, memoryBytes: totalmem(), node: process.version },
  compiler: (await execute("go", ["version"])).stdout.trim(),
  buildArgs: ["-buildvcs=false", "-trimpath", "-ldflags", "-s -w"],
  baselineRevision: "29897c891d39766d856fd3ff9b0e4e80036eb8e3",
  candidateRevision: (await execute("git", ["rev-parse", "HEAD"], { cwd: roots.candidate })).stdout.trim(),
  qualificationHarnessSha256: hash(await readFile(new URL(import.meta.url))),
  order: ["baseline", "candidate", "candidate", "baseline"],
  artifacts: {}, harnesses: {},
  notes: [
    "Runs execute serially on one host. No test/build jobs run beside the measurements.",
    "Native startup creates fresh processes with warm OS caches; it is not a cold-disk measurement.",
    "Browser trials create fresh contexts and persistent workers in a warmed Chromium process; warm-up trials are excluded.",
    "Raw per-block summaries are preserved. Do not infer a combined percentile from block percentiles.",
    "RSS is sampled by the existing native harnesses; browser heap and production capacity are not measured.",
    "The general workspace lifecycle harness failed during baseline parallel cleanup. Capture comparisons use the serial portable harness; the workspace memory-only harness has no parallel stage.",
  ],
};
for (const [label, root] of Object.entries(roots)) {
  metadata.artifacts[label] = {
    binary: await identity(join(root, "bin/stow-s3")),
    wasm: await identity(join(root, "packages/stow-s3/dist/stow-runtime.wasm")),
    worker: await identity(join(root, "packages/stow-s3/dist/worker-host.js")),
    streams: await identity(join(root, "packages/stow-s3/dist/async-streams.js")),
  };
}
for (const name of ["session", "concurrency", "workspace"]) {
  const relative = `packages/stow-s3/scripts/benchmark-${name}.mjs`;
  const before = await identity(join(roots.baseline, relative));
  const after = await identity(join(roots.candidate, relative));
  if (before.sha256 !== after.sha256) throw new Error(`benchmark harness changed: ${relative}`);
  metadata.harnesses[relative] = before;
}
metadata.harnesses["scripts/benchmark-portable.mjs"] = await identity(join(roots.candidate, "scripts/benchmark-portable.mjs"));
await writeFile(join(out, "metadata.json"), JSON.stringify(metadata, null, 2) + "\n");
for (const [block, label] of metadata.order.entries()) {
  const root = roots[label];
  const env = { ...process.env, STOW_BIN: join(root, "bin/stow-s3") };
  const prefix = `${block + 1}-${label}`;
  const jobs = [
    ["session", ["packages/stow-s3/scripts/benchmark-session.mjs", "--sessions", "20", "--json", join(out, `${prefix}-session.json`)]],
    ["concurrency", ["packages/stow-s3/scripts/benchmark-concurrency.mjs", "--runs", "3", "--json", join(out, `${prefix}-concurrency.json`)]],
    ["workspace-memory", ["packages/stow-s3/scripts/benchmark-workspace.mjs", "--memory-only", "--json", join(out, `${prefix}-workspace-memory.json`)]],
    ["portable", ["scripts/benchmark-portable.mjs", "256", "8388608", "5", "0"]],
  ];
  for (const [name, args] of jobs) {
    console.log(`${prefix}: ${name}`);
    const result = await execute(process.execPath, args, { cwd: root, env, maxBuffer: 16 * 1024 * 1024 });
    await writeFile(join(out, `${prefix}-${name}.log`), result.stdout + result.stderr);
    if (name === "portable") await writeFile(join(out, `${prefix}-${name}.json`), result.stdout);
  }
}

const server = createServer(async (request, response) => {
  try {
    const path = new URL(request.url, "http://localhost").pathname;
    if (path === "/") { response.setHeader("content-type", "text/html"); response.end("<!doctype html><title>Stow patch qualification</title>"); return; }
    const match = /^\/(baseline|candidate)\/([a-zA-Z0-9._-]+)$/.exec(path);
    if (!match || [".", ".."].includes(match[2])) { response.writeHead(404); response.end(); return; }
    response.setHeader("content-type", match[2].endsWith(".wasm") ? "application/wasm" : "text/javascript");
    response.setHeader("cache-control", "no-store");
    response.end(await readFile(join(roots[match[1]], "packages/stow-s3/dist", match[2])));
  } catch { response.writeHead(500); response.end(); }
});
await new Promise((resolveListen) => server.listen(0, "127.0.0.1", resolveListen));
const address = server.address();
const origin = `http://127.0.0.1:${address.port}`;
const { chromium } = await import(pathToFileURL(join(roots.candidate, "packages/stow-s3/node_modules/playwright/index.mjs")).href);
const browser = await chromium.launch({ headless: true });
const browserTrials = [];
try {
  metadata.chromium = browser.version();
  for (let trial = -1; trial < 10; trial++) {
    const order = trial % 2 === 0 ? ["candidate", "baseline"] : ["baseline", "candidate"];
    for (const label of order) {
      const context = await browser.newContext();
      try {
        const page = await context.newPage();
        await page.goto(origin);
        const result = await page.evaluate(async ({ label }) => {
          const started = performance.now();
          const { loadBrowserPersistentWasmHost } = await import(`/${label}/browser-wasm-host.js`);
          const { AsyncEmbeddedStow } = await import(`/${label}/async-embedded.js`);
          const host = loadBrowserPersistentWasmHost();
          const runtime = await AsyncEmbeddedStow.openPersistent(host, {
            namespace: crypto.randomUUID(), databaseName: "stow-patch-benchmark", maxBytes: 32 * 1024 * 1024,
          });
          const readyMs = performance.now() - started;
          try {
            await runtime.createBucket("assets");
            const payload = new Uint8Array(4 * 1024 * 1024).fill(90);
            const writeStarted = performance.now();
            await runtime.putObjectStream("assets", "file", payload, { size: payload.length });
            const writeMs = performance.now() - writeStarted;
            const readStarted = performance.now();
            const reader = await runtime.getObjectStream("assets", "file");
            const chunks = [];
            let bytes = 0;
            for await (const chunk of reader.chunks) { chunks.push(chunk); bytes += chunk.length; }
            const readMs = performance.now() - readStarted;
            if (bytes !== payload.length) throw new Error("stream length mismatch");
            const received = new Uint8Array(bytes);
            let offset = 0;
            for (const chunk of chunks) { received.set(chunk, offset); offset += chunk.length; }
            const digest = (data) => crypto.subtle.digest("SHA-256", data).then((value) => Array.from(new Uint8Array(value)).join(","));
            if (await digest(payload) !== await digest(received)) throw new Error("stream digest mismatch");
            const readers = [];
            for (let index = 0; index < 16; index++) readers.push(await runtime.getObjectStream("assets", "file"));
            await Promise.all(readers.map((item) => item.close()));
            const lingeringPins = (await runtime.inspectRecovery()).protections.length;
            return { readyMs, payloadBytes: bytes, writeMs, readMs, writeMiBPerSecond: 4 / (writeMs / 1000), readMiBPerSecond: 4 / (readMs / 1000), lingeringPins, digestVerified: true };
          } finally { await runtime.close(); host.close(); }
        }, { label });
        if (label === "candidate" && result.lingeringPins !== 0) throw new Error("candidate leaked stream pins");
        if (trial >= 0) browserTrials.push({ label, trial, ...result });
        console.log(`browser ${label}: ${trial < 0 ? "warmup" : trial + 1}`);
      } finally { await context.close(); }
    }
  }
} finally {
  await browser.close();
  await new Promise((resolveClose) => server.close(resolveClose));
}
await writeFile(join(out, "browser.json"), JSON.stringify(browserTrials, null, 2) + "\n");
metadata.complete = true;
metadata.finishedAt = new Date().toISOString();
await writeFile(join(out, "metadata.json"), JSON.stringify(metadata, null, 2) + "\n");
console.log(`Completed qualification: ${out}`);
