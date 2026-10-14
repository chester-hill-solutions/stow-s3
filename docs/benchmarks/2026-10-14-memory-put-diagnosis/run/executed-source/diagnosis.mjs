import { fork } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve, join, basename } from "node:path";
import { remote } from "./http.mjs";
import { benchmarkClient } from "./client.mjs";
import { sampleRemote } from "./sample.mjs";
import { systemMetadata } from "./resources.mjs";
import { prepare, verifyFinal } from "../packages/stow-s3/scripts/performance/workload.mjs";

const out = resolve(process.argv[2]);
if (process.platform !== "linux" || !out.startsWith("/results/")) throw new Error("Railway /results required");
const host = process.env.BENCH_TARGET_HOST;
if (!host || !/^[a-z0-9.-]+$/i.test(host)) throw new Error("invalid target host");
const base = `http://${host}:8081`;
const config = { bytes: 1048576, concurrency: 4, retainedObjects: 16, maxQueue: 8, timeoutMs: 30000,
  maxBytes: 268435456, maxObjects: 4096, serverConcurrency: 128, sampleMs: 250, warmupMs: 5000, durationMs: 20000, repetitions: 6 };
await mkdir(out, { recursive: true });
const receipt = { runId: basename(out), startedAt: new Date().toISOString(), config, complete: false,
  metadata: await systemMetadata(), targetMetadata: await remote(base, "/metadata"), smoke: [], results: [], profiles: [] };
const save = () => writeFile(join(out, "run.json"), JSON.stringify(receipt, null, 2) + "\n");
await save();
try {
  for (const processes of [1, 4]) { receipt.smoke.push(await measure(processes, 3000, false)); await save(); }
  for (let repetition = 1; repetition <= config.repetitions; repetition++) {
    for (const processes of repetition % 2 ? [1, 4] : [4, 1]) {
      const result = await measure(processes, config.durationMs, false);
      receipt.results.push({ repetition, ...result }); await save();
      console.log(JSON.stringify({ repetition, processes, mibPerSecond: result.mibPerSecond, clientCpuPercent: result.clientCpuPercent,
        serverCpuPercent: result.resources.serverAverageCpuPercent }));
    }
  }
  for (const processes of [1, 4]) { receipt.profiles.push(await measure(processes, config.durationMs, true)); await save(); }
  receipt.complete = true; receipt.completedAt = new Date().toISOString();
} catch (error) { receipt.error = error.message; process.exitCode = 1; }
await save();

function waitFor(child, expected) {
  return new Promise((resolveMessage, reject) => {
    const timer = setTimeout(() => done(new Error(`worker timed out waiting for ${expected}`)), 90000);
    function done(error, value) { clearTimeout(timer); child.off("message", message); child.off("exit", exit); error ? reject(error) : resolveMessage(value); }
    function message(value) { if (value.type === "error") done(new Error(value.error)); else if (value.type === expected) done(null, value); }
    function exit(code) { done(new Error(`worker exited ${code} waiting for ${expected}`)); }
    child.on("message", message); child.once("exit", exit);
  });
}
async function phase(children, type, durationMs, profile) {
  const pending = children.map(child => waitFor(child, type));
  const startAt = Date.now() + 1200;
  for (const child of children) child.send({ type, durationMs, startAt, profile });
  const results = await Promise.all(pending);
  const startSkewMs = Math.max(...results.map(r => r.startedAt)) - Math.min(...results.map(r => r.startedAt));
  if (startSkewMs > 50 || results.some(r => Math.abs(r.startedAt - startAt) > 50)) throw new Error("worker measurement barrier exceeded 50 ms");
  if (results.some(r => r.load.failed || r.load.dropped || r.verification?.complete === false)) throw new Error("worker errors or incomplete verification");
  return { startAt, startSkewMs, children: results };
}
async function measure(processes, durationMs, profile) {
  const ready = await remote(base, "/start", { target: profile ? "stow-memory-profile" : "stow-memory", bytes: config.bytes,
    maxBytes: config.maxBytes, maxObjects: config.maxObjects, serverConcurrency: config.serverConcurrency });
  const endpoint = `http://${host}:${ready.port}`, children = [];
  const connection = benchmarkClient(endpoint, ready, config.concurrency, config.timeoutMs);
  try {
    const fixtures = await prepare(connection.client, { bytes: config.bytes, concurrency: config.concurrency }, config);
    for (let index = 0; index < processes; index++) {
      const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !/^(BENCH_|STOW_|AWS_|S3_)/.test(key)));
      const child = fork("benchmark/diagnosis-worker.mjs", [], { env, stdio: ["ignore", "inherit", "inherit", "ipc"] });
      children.push(child);
      const pending = waitFor(child, "ready");
      child.send({ type: "init", endpoint, ready, config, scenario: { bytes: config.bytes, concurrency: config.concurrency / processes, workload: "put" },
        workerOffset: index * config.concurrency / processes });
      await pending;
    }
    const warmup = await phase(children, "warmup", config.warmupMs, false);
    const sampler = await sampleRemote(base, config.sampleMs);
    // Profile interval includes the barrier and measured traffic; timed comparisons use the original binary with no profiler.
    const serverProfiles = profile ? remote(base, "/profiles", {}).then(value => ({ value }), error => ({ error })) : null;
    let measured, resources;
    try { measured = await phase(children, "measure", durationMs, profile); }
    finally { resources = await sampler.stop(); }
    if (!resources.diskHeadroomMaintained) throw new Error("insufficient disk headroom");
    const verification = await verifyFinal(connection.client, fixtures, config.timeoutMs);
    if (verification.verifiedObjects !== 16 || measured.children.reduce((sum, child) => sum + child.verification.verifiedObjects, 0) !== 4) throw new Error("expected sixteen fixtures and four distinct writer objects");
    if (profile) {
      const collected = await serverProfiles;
      if (collected.error) throw collected.error;
      const profiles = collected.value;
      for (const [kind, value] of Object.entries(profiles)) {
        await writeFile(join(out, `server-${processes}-${kind}.pprof`), Buffer.from(value.data, "base64"));
        await writeFile(join(out, `server-${processes}-${kind}-top.txt`), value.top);
        await writeFile(join(out, `server-${processes}-${kind}-cum.txt`), value.cumulative);
      }
      for (const child of measured.children) {
        await writeFile(join(out, `client-${processes}-${child.workerOffset}.cpuprofile`), JSON.stringify(child.profile));
        delete child.profile;
      }
    }
    for (const child of warmup.children) delete child.profile;
    for (const child of measured.children) delete child.profile;
    const completed = measured.children.reduce((sum, r) => sum + r.load.windowSuccessful, 0);
    return { processes, clients: config.concurrency, profiled: profile, durationMs, startSkewMs: measured.startSkewMs,
      startupMs: ready.startupMs, firstWriteMs: ready.firstWriteMs, firstWriteVerified: ready.firstWriteVerified,
      mibPerSecond: completed * config.bytes / 1048576 / (durationMs / 1000),
      clientCpuPercent: measured.children.reduce((sum, r) => sum + r.cpuSeconds, 0) / (durationMs / 1000) * 100,
      warmup, measured, resources, verification };
  } finally {
    for (const child of children) if (child.exitCode === null) child.kill("SIGTERM");
    connection.close(); await remote(base, "/stop", {});
  }
}
