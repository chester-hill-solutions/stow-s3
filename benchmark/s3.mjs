import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { defaults, validateConfig, scenarios } from "../packages/stow-s3/scripts/performance/options.mjs";
import { Measurements } from "../packages/stow-s3/scripts/performance/metrics.mjs";
import { runLoad } from "../packages/stow-s3/scripts/performance/load.mjs";
import { prepare, workload, verifyFinal, leftovers } from "../packages/stow-s3/scripts/performance/workload.mjs";
import { summarizeRuns } from "../packages/stow-s3/scripts/performance/report.mjs";
import { targets } from "./implementations.mjs";
import { systemMetadata } from "./resources.mjs";
import { benchmarkClient } from "./client.mjs";
import { sampleRemote } from "./sample.mjs";
import { remote } from "./http.mjs";

export function comparisonConfig(profile) {
  const { targets: selected, embedded, agent, nodeAgent, pairedVariants = false, ...settings } = profile;
  if (!Array.isArray(selected) || !selected.length || selected.some(t=>!targets.includes(t))) throw new Error("invalid comparison targets");
  for (const key of Object.keys(settings)) if (!(key in defaults)) throw new Error(`unknown profile field ${key}`);
  const config = validateConfig({ ...defaults, ...settings, backends: ["memory"], soakSeconds: 0 });
  if (typeof pairedVariants !== "boolean") throw new Error("pairedVariants must be boolean");
  if (pairedVariants) comparisonOrder(selected, 1, true);
  return { config, selected, embedded, agent, nodeAgent, pairedVariants };
}

export function comparisonOrder(selected, repetition, pairedVariants = false) {
  if (pairedVariants) {
    const expected = ["stow-memory", "stow-memory-before", "stow-filesystem", "stow-filesystem-before"];
    if (selected.length !== 4 || new Set(selected).size !== 4 || expected.some(target => !selected.includes(target))) throw new Error("paired comparison requires both Stow variants for both backends");
    const pairs = [["stow-memory-before", "stow-memory"], ["stow-filesystem-before", "stow-filesystem"]];
    if (repetition % 2 === 0) pairs.forEach(pair => pair.reverse());
    if (Math.floor((repetition - 1) / 2) % 2) pairs.reverse();
    return pairs.flat();
  }
  const offset = (repetition - 1) % selected.length;
  return selected.slice(offset).concat(selected.slice(0, offset));
}

export async function runS3(profile, out, host) {
  const { config, selected, pairedVariants } = comparisonConfig(profile);
  const base = `http://${host}:${process.env.BENCH_TARGET_CONTROL_PORT ?? 8081}`;
  await mkdir(out, { recursive: true });
  const suite = { version: 1, config, targets: selected, metadata: { generator: await systemMetadata(), target: await remote(base, "/metadata") }, results: [], complete: false };
  const versions = JSON.parse(await readFile("benchmark/versions.json", "utf8"));
  suite.versions = versions;
  async function save() {
    await writeFile(join(out, "suite.json"), JSON.stringify(suite, null, 2) + "\n");
    const rows = summarizeRuns(suite.results);
    await writeFile(join(out, "summary.json"), JSON.stringify({ protocolComplete: false, rows }, null, 2) + "\n");
    await writeFile(join(out, "report.md"), comparisonReport(suite, rows));
  }
  await save();
  for (const scenario of scenarios(config)) {
    for (let repetition = 1; repetition <= config.repetitions; repetition++) {
      // Rotate order across repetitions while keeping every target on the same worker.
      const order = comparisonOrder(selected, repetition, pairedVariants);
      for (const implementation of order) {
        const current = { ...scenario, backend: implementation.includes("-memory") ? "memory" : "filesystem", implementation };
        const name = `${implementation}-${current.workload}-${current.bytes}-c${current.concurrency}-r${current.rate}-${repetition}`;
        console.log(`Measuring ${name}`);
        const result = await measure(base, host, current, config);
        result.repetition = repetition;
        result.orderWithinRepetition = order.indexOf(implementation);
        await writeFile(join(out, `${name}.json`), JSON.stringify(result, null, 2) + "\n");
        const resources = { ...result.resources }; delete resources.samples;
        const load = { ...result.load }; delete load.perSecond;
        suite.results.push({ ...result, resources, load, rawFile: `${name}.json` });
        await save();
      }
    }
  }
  suite.complete = true;
  await save();
  return { complete: true, runs: suite.results.length, acceptable: suite.results.every(r=>!r.load.failed && !r.load.dropped && r.verification.complete && !r.leftovers.count && r.resources.diskHeadroomMaintained) };
}

async function measure(base, host, scenario, config) {
  const ready = await remote(base, "/start", { target: scenario.implementation, bytes: scenario.bytes,
    maxBytes: config.maxBytes, maxObjects: config.maxObjects, serverConcurrency: config.serverConcurrency });
  const connection = benchmarkClient(`http://${host}:${ready.port}`, ready, scenario.concurrency, config.timeoutMs);
  try {
    const state = await prepare(connection.client, scenario, config);
    const options = { concurrency: scenario.concurrency, rate: scenario.rate, maxQueue: config.maxQueue, timeoutMs: config.timeoutMs };
    const warmup = config.warmupSeconds ? await runLoad({ ...options, durationMs: config.warmupSeconds * 1000,
      task: workload(connection.client, scenario, config, state, new Measurements()) }) : null;
    const sampler = await sampleRemote(base, config.sampleMs);
    const measurements = new Measurements();
    let load, resources;
    try { load = await runLoad({ ...options, durationMs: config.durationSeconds * 1000, task: workload(connection.client, scenario, config, state, measurements) }); }
    finally { resources = await sampler.stop(); }
    return { scenario, startupMs: ready.startupMs, firstWriteMs: ready.firstWriteMs,
      firstWriteBytes: ready.firstWriteBytes, firstWriteVerified: ready.firstWriteVerified,
      warmup, load, resources, measurements: measurements.summary(load.measuredSeconds),
      verification: await verifyFinal(connection.client, state, config.timeoutMs), leftovers: await leftovers(connection.client, config.timeoutMs) };
  } finally { connection.close(); await remote(base, "/stop", {}); }
}

function comparisonReport(suite, rows) {
  return ["# Railway S3 comparison", "", `Complete: **${suite.complete}**. Exploratory measurements; qualification protocol incomplete.`, "",
    `${suite.config.repetitions} repetitions, ${suite.config.durationSeconds} s windows, ${suite.config.warmupSeconds} s warm-up. One target at a time, separate generator, private HTTP, no TLS.`, "",
    "| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |",
    "|---|---|---:|---:|---:|---:|---:|---:|---|",
    ...rows.map(r=>`| ${r.implementation} | ${r.workload} | ${r.filesPerSecond} | ${r.minPayloadMiBPerSecond}–${r.maxPayloadMiBPerSecond} | ${r.successfulRequestsPerSecond} | ${r.medianRunP99ResponseMs} | ${r.peakSampledServerRssMiB} | ${r.failedFiles} / ${r.droppedFiles} | ${r.verified} |`), "",
    "All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.",
    "Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.",
    "Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.",
    "Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.",
    "S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.",
    "Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.", ""].join("\n");
}
