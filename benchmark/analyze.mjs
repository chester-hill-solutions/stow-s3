import { readFile, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import assert from "node:assert/strict";

// Post-process exported evidence; this does not run a workload or contact Railway.
const directory = resolve(process.argv[2]);
const json = async path => JSON.parse(await readFile(join(directory, path), "utf8"));
const run = await json("run.json");
const suite = await json("s3/suite.json");
const profile = await json("profile.json");
assert.equal(run.complete, true, "job must be complete");
assert.equal(run.embedded.complete, true);
assert.equal(suite.complete, true);
assert.equal(run.s3.acceptable, true, "failed or unverified runs require investigation");
assert.equal(suite.results.length, profile.targets.length * profile.workloads.length * profile.sizes.length * profile.concurrency.length * profile.rates.length * profile.repetitions);
assert.equal(new Set(suite.results.map(r => r.rawFile)).size, suite.results.length, "case filenames must be unique");
assert.deepEqual(suite.metadata.generator.build, suite.metadata.target.build, "both roles must use the same build");
const median = values => {
  const sorted = values.toSorted((a, b) => a - b);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
};
const round = x => Math.round(x * 1000) / 1000;
const latency = values => {
  const sorted = values.toSorted((a,b)=>a-b);
  return { samples: sorted.length, medianMs: median(sorted), minMs: sorted[0], maxMs: sorted.at(-1),
    p95Ms: sorted[Math.ceil(sorted.length * .95) - 1] };
};
let agent = [], nodeAgent = [];
if (profile.nodeAgent) {
  assert.equal(run.nodeAgent.complete, true);
  const evidence = await json("node-agent.json");
  assert.equal(evidence.complete, true);
  assert.equal(evidence.results.length, 3*2*profile.nodeAgent.samples);
  const batches = new Map();
  for (const record of evidence.results) {
    assert.equal(record.verified,true);
    assert.ok(Number.isFinite(record.startupMs)&&record.startupMs>0);
    assert.ok(Number.isFinite(record.firstWriteMs)&&record.firstWriteMs>=record.startupMs);
    assert.ok(Number.isFinite(record.peakRssBytes)&&record.peakRssBytes>0);
    const key=JSON.stringify([record.implementation,record.bytes]), values=batches.get(key)??[];
    values.push(record);batches.set(key,values);
  }
  assert.equal(batches.size,6);
  nodeAgent=[...batches].map(([key,records])=>{
    assert.deepEqual(records.map(r=>r.sample).toSorted((a,b)=>a-b),Array.from({length:profile.nodeAgent.samples},(_,i)=>i));
    const [implementation,bytes]=JSON.parse(key);
    const versions=[...new Set(records.map(r=>r.sqliteVersion).filter(Boolean))];
    assert.ok(versions.length<=1);
    return {implementation,bytes,startup:latency(records.map(r=>r.startupMs)),firstWrite:latency(records.map(r=>r.firstWriteMs)),
      maxPeakRssBytes:Math.max(...records.map(r=>r.peakRssBytes)),verified:true,...(versions[0]?{sqliteVersion:versions[0]}:{})};
  });
}
if (profile.agent) {
  assert.equal(run.agent.complete, true);
  const agentText = await readFile(join(directory, "agent.txt"), "utf8");
  assert.ok(agentText.trim().endsWith("PASS"));
  const records = agentText.split("\n").filter(line=>line.startsWith("AGENT_SAMPLE ")).map(line=>JSON.parse(line.slice(13)));
  assert.equal(records.length, profile.agent.implementationCount * profile.agent.sizes.length * profile.agent.samples);
  const batches = new Map();
  for (const record of records) {
    assert.equal(record.verified, true);
    assert.ok(Number.isFinite(record.startupMs) && record.startupMs > 0);
    assert.ok(Number.isFinite(record.firstWriteMs) && record.firstWriteMs >= record.startupMs);
    assert.ok(profile.agent.sizes.includes(record.bytes));
    const key = JSON.stringify([record.implementation, record.bytes]);
    const values = batches.get(key) ?? [];
    values.push(record); batches.set(key, values);
  }
  assert.equal(batches.size, profile.agent.implementationCount * profile.agent.sizes.length);
  agent = [...batches].map(([key, records])=>{
    assert.deepEqual(records.map(r=>r.sample).toSorted((a,b)=>a-b), Array.from({length:profile.agent.samples},(_,i)=>i));
    const [implementation, bytes] = JSON.parse(key);
    const versions = [...new Set(records.map(r=>r.sqliteVersion).filter(Boolean))];
    assert.ok(versions.length <= 1);
    return { implementation, bytes, startup: latency(records.map(r=>r.startupMs)),
      firstWrite: latency(records.map(r=>r.firstWriteMs)), verified: true, ...(versions[0] ? {sqliteVersion:versions[0]} : {}) };
  });
}
const groups = new Map();
const text = await readFile(join(directory, "embedded.txt"), "utf8");
const pattern = /^BenchmarkComparison\/([^/]+)\/([^/]+)\/(\d+)B(?:-(\d+))?\s+(\d+)\s+([\d.]+) ns\/op\s+([\d.]+) MB\/s\s+(\d+) B\/op\s+(\d+) allocs\/op/;
let nativeSamples = 0;
for (const line of text.split("\n")) {
  if (!line.startsWith("Benchmark")) continue;
  const match = line.match(pattern);
  assert.ok(match, `unrecognized benchmark row: ${line}`);
  const [, implementation, workload, bytes, cpu, iterations, ns, mb, allocated, allocs] = match;
  const key = JSON.stringify([implementation, workload, Number(bytes), Number(cpu ?? 1)]);
  const values = groups.get(key) ?? [];
  values.push({ iterations: Number(iterations), ns: Number(ns), mib: Number(mb) * 1e6 / 1048576, allocated: Number(allocated), allocs: Number(allocs) });
  groups.set(key, values); nativeSamples++;
}
assert.equal(nativeSamples, profile.embedded.expectedSamples ?? 7 * 3 * 3 * profile.embedded.cpu.split(",").length * profile.embedded.count);
assert.ok(text.trim().endsWith("PASS"));
const embedded = [...groups].map(([key, values]) => {
  assert.equal(values.length, profile.embedded.count);
  const [implementation, workload, bytes, cpu] = JSON.parse(key);
  return { implementation, workload, bytes, cpu, repetitions: values.length,
    medianNsPerOp: round(median(values.map(v => v.ns))),
    payloadMiBPerSecond: round(median(values.map(v => v.mib))),
    minPayloadMiBPerSecond: round(Math.min(...values.map(v => v.mib))),
    maxPayloadMiBPerSecond: round(Math.max(...values.map(v => v.mib))),
    medianAllocatedBytesPerOp: median(values.map(v => v.allocated)),
    medianAllocationsPerOp: median(values.map(v => v.allocs)) };
});
const stat = text => Object.fromEntries(text.split("\n").map(line => { const [key, value] = line.split(/\s+/); return [key, Number(value)]; }));
const resources = [];
for (const indexed of suite.results) {
  const raw = await json("s3/" + indexed.rawFile);
  assert.deepEqual(raw.scenario, indexed.scenario);
  assert.equal(raw.load.failed, 0);
  assert.equal(raw.load.dropped, 0);
  assert.equal(raw.warmup?.failed ?? 0, 0);
  assert.equal(raw.warmup?.dropped ?? 0, 0);
  assert.equal(raw.verification.complete, true);
  assert.equal(raw.leftovers.count, 0);
  assert.equal(raw.resources.diskHeadroomMaintained, true);
  if (profile.agent) {
    assert.equal(raw.firstWriteVerified, true);
    assert.equal(raw.firstWriteBytes, 4096);
    assert.ok(Number.isFinite(raw.firstWriteMs) && raw.firstWriteMs >= raw.startupMs);
  }
  assert.ok(raw.resources.samples.length >= 2);
  const first = raw.resources.samples[0], last = raw.resources.samples.at(-1);
  const throttling = key => {
    const a = stat(first[key].cpuStat), b = stat(last[key].cpuStat);
    const periods = b.nr_periods - a.nr_periods;
    return { periods, throttledPeriods: b.nr_throttled - a.nr_throttled,
      throttledPeriodPercent: periods ? round((b.nr_throttled - a.nr_throttled) / periods * 100) : 0,
      cumulativeThrottledSeconds: round((b.throttled_usec - a.throttled_usec) / 1e6) };
  };
  resources.push({ rawFile: indexed.rawFile, ...raw.scenario,
    serverAverageCpuPercent: round(raw.resources.serverAverageCpuPercent),
    generatorAverageCpuPercent: round(raw.resources.generatorCpuSeconds / raw.resources.intervalSeconds * 100),
    generatorEventLoopMaxMs: round(raw.resources.generatorEventLoopDelayMs.max),
    targetThrottling: throttling("cgroup"), generatorThrottling: throttling("generatorCgroup"),
    minimumDiskAvailableMiB: round(raw.resources.minimumSampledDiskAvailableBytes / 1048576),
    serverHighWaterRssMiB: round(raw.resources.serverHighWaterRssBytes / 1048576),
    startupMs: round(raw.startupMs), ...(profile.agent ? { firstWriteMs: raw.firstWriteMs, firstWriteVerified: true, firstWriteBytes: raw.firstWriteBytes } : {}) });
}
const serverAgent = profile.agent ? suite.targets.map(implementation=>{
  const records = suite.results.filter(r=>r.scenario.implementation === implementation);
  return { implementation, bytes: 4096, startup: latency(records.map(r=>r.startupMs)),
    firstWrite: latency(records.map(r=>r.firstWriteMs)), verified: true };
}) : [];
const output = { checkedAt: new Date().toISOString(), complete: true, nativeSamples, s3Runs: resources.length,
  sameBuild: true, embedded, resources, agent, serverAgent, nodeAgent };
await writeFile(join(directory, "analysis.json"), JSON.stringify(output, null, 2) + "\n");
await writeFile(join(directory, "embedded-report.md"), [
  "# Embedded storage comparison", "",
  `Exploratory: ${profile.embedded.count} repetitions, ${profile.embedded.benchtime} per Go benchmark, CPU settings ${profile.embedded.cpu}.`, "",
  "Rates below convert Go's decimal MB/s to binary MiB/s. Reads include SHA-256 verification. Quarantine counts input payload once per completed workflow.",
  "Filesystem rows use ephemeral runner storage. Temporary files and Afero omit the sync guarantees of the atomic-file baseline; Stow also maintains object metadata and admission state.", "",
  "| Implementation | Workload | Bytes/file | CPU | Payload MiB/s (min–max) | Allocated B/op | Allocations/op |",
  "|---|---|---:|---:|---:|---:|---:|",
  ...embedded.map(r => `| ${r.implementation} | ${r.workload} | ${r.bytes} | ${r.cpu} | ${r.payloadMiBPerSecond} (${r.minPayloadMiBPerSecond}–${r.maxPayloadMiBPerSecond}) | ${r.medianAllocatedBytesPerOp} | ${r.medianAllocationsPerOp} |`), "",
  "Allocation counts describe Go heap allocation per operation; they do not measure retained memory, process RSS or a memory leak.", "",
].join("\n"));
console.log(JSON.stringify({ complete: true, nativeSamples, s3Runs: resources.length, sameBuild: true }));
