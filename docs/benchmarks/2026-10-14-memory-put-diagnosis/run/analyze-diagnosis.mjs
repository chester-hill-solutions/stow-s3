import assert from "node:assert/strict";
import { readFile, writeFile, readdir } from "node:fs/promises";
import { createHash } from "node:crypto";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const round = value => Math.round(value * 1000) / 1000;
const median = values => { const sorted = values.toSorted((a,b) => a-b), i = Math.floor(sorted.length/2); return sorted.length % 2 ? sorted[i] : (sorted[i-1]+sorted[i])/2; };
export function summarizeCpuProfile(profile) {
  assert.equal(profile.samples.length, profile.timeDeltas.length);
  const nodes = new Map(profile.nodes.map(node => [node.id, node]));
  const parents = new Map();
  for (const node of profile.nodes) for (const child of node.children ?? []) parents.set(child, node.id);
  const flat = new Map(), cumulative = new Map();
  let totalUs = 0;
  const key = id => { const frame = nodes.get(id).callFrame; return `${frame.functionName || "(anonymous)"} @ ${frame.url}:${frame.lineNumber+1}`; };
  for (let index = 0; index < profile.samples.length; index++) {
    const id = profile.samples[index], duration = profile.timeDeltas[index];
    assert.ok(nodes.has(id)); totalUs += duration;
    flat.set(key(id), (flat.get(key(id)) ?? 0) + duration);
    const visited = new Set();
    for (let ancestor = id; ancestor !== undefined; ancestor = parents.get(ancestor)) {
      const frame = key(ancestor);
      if (!visited.has(frame)) { cumulative.set(frame, (cumulative.get(frame) ?? 0) + duration); visited.add(frame); }
    }
  }
  const rank = data => [...data].sort((a,b) => b[1]-a[1]).slice(0,30).map(([frame, duration]) => ({ frame, sampledMs: round(duration/1000), percentOfWallSamples: round(duration/totalUs*100) }));
  return { sampledSeconds: totalUs/1e6, flat: rank(flat), cumulative: rank(cumulative),
    note: "Inspector wall-time samples include idle and GC; percentages are not process CPU accounting. Cumulative entries overlap." };
}
function stat(text) { return Object.fromEntries(text.split("\n").map(line => { const [key,value] = line.split(/\s+/); return [key,Number(value)]; })); }
function throttling(rows, field) {
  const first = stat(rows[0][field].cpuStat), last = stat(rows.at(-1)[field].cpuStat);
  return { throttledPeriods: last.nr_throttled-first.nr_throttled, cumulativeThrottledSeconds: round((last.throttled_usec-first.throttled_usec)/1e6) };
}
async function main() {
  const out = resolve(process.argv[2]);
  if (process.platform !== "linux" || !out.startsWith("/results/")) throw new Error("analysis requires Railway /results");
  const run = JSON.parse(await readFile(join(out,"run.json"),"utf8"));
  assert.equal(run.complete,true); assert.equal(run.results.length,12); assert.equal(run.profiles.length,2); assert.equal(run.smoke.length,2);
  assert.equal(run.metadata.cgroup.cpuMax,"400000 100000"); assert.equal(run.targetMetadata.cgroup.cpuMax,"200000 100000");
  assert.deepEqual(run.metadata.build.binaries,run.targetMetadata.build.binaries);
  assert.equal(run.metadata.build.binaries["stow-s3"],"7c80ec1b79aa0d457e2c4f7e7eb680ae2a8f13bf060fef2961a86d170dca70a6");
  const resources=[];
  for (const result of [...run.smoke,...run.results,...run.profiles]) {
    assert.equal(result.clients,4); assert.ok(result.startSkewMs<=50); assert.equal(result.firstWriteVerified,true);
    assert.equal(result.verification.complete,true); assert.equal(result.verification.verifiedObjects,16); assert.equal(result.resources.diskHeadroomMaintained,true);
    assert.equal(result.measured.children.reduce((sum,child)=>sum+child.verification.verifiedObjects,0),4);
    for (const child of result.measured.children) { assert.equal(child.load.failed,0); assert.equal(child.load.dropped,0); assert.equal(child.verification.complete,true); }
    resources.push({ processes:result.processes, profiled:result.profiled, repetition:result.repetition,
      clientCpuPercent:result.clientCpuPercent, serverCpuPercent:result.resources.serverAverageCpuPercent,
      serverThrottle:throttling(result.resources.samples,"cgroup"), generatorThrottle:throttling(result.resources.samples,"generatorCgroup"),
      peakSumClientRssBytes:result.measured.children.reduce((sum,child)=>sum+child.peakRssBytes,0),
      serverHighWaterRssBytes:result.resources.serverHighWaterRssBytes });
  }
  const pairs=[];
  for(let repetition=1;repetition<=6;repetition++) {
    const rows=run.results.filter(row=>row.repetition===repetition);
    assert.deepEqual(rows.map(row=>row.processes),repetition%2?[1,4]:[4,1]);
    const before=rows.find(row=>row.processes===1).mibPerSecond,after=rows.find(row=>row.processes===4).mibPerSecond;
    assert.ok(before>0&&after>0); pairs.push({repetition,one:before,four:after,percentChange:round((after/before-1)*100)});
  }
  const ratios=pairs.map(pair=>Math.log(pair.four/pair.one)),mean=ratios.reduce((a,b)=>a+b,0)/6;
  const variance=ratios.reduce((sum,value)=>sum+(value-mean)**2,0)/5,radius=2.57058183661474*Math.sqrt(variance/6);
  const summary=[1,4].map(processes=>{const rows=run.results.filter(row=>row.processes===processes);return {processes,
    medianMiBPerSecond:round(median(rows.map(row=>row.mibPerSecond))),medianClientCpuPercent:round(median(rows.map(row=>row.clientCpuPercent))),
    medianServerCpuPercent:round(median(rows.map(row=>row.resources.serverAverageCpuPercent))),maximumStartSkewMs:Math.max(...rows.map(row=>row.startSkewMs))};});
  const profiles={};
  for (const name of await readdir(out)) if(name.endsWith(".cpuprofile")) profiles[name]=summarizeCpuProfile(JSON.parse(await readFile(join(out,name),"utf8")));
  const analysis={complete:true,runId:run.runId,analysisSourceSha256:createHash("sha256").update(await readFile(fileURLToPath(import.meta.url))).digest("hex"),
    summary,pairs,effect:{geometricMeanPercentChange:round((Math.exp(mean)-1)*100),conditional95PercentInterval:[round((Math.exp(mean-radius)-1)*100),round((Math.exp(mean+radius)-1)*100)]},resources,clientProfiles:profiles,
    limitations:["Shared Railway hosts; six paired windows, conditional interval excludes broader host-to-host variation.","No TLS; SDK default checksum/signing and verification hashing preserved.","Profiler runs excluded from throughput comparison; mutex/block/heap profiles include startup and warmup.","Worker starts may differ by up to 50 ms over 20-second windows; actual skew recorded.","Server CPU sampling includes barrier and final verification; client CPU counts measured traffic plus drain.","Sum of per-process RSS peaks is an upper bound; shared pages may be counted repeatedly."]};
  await writeFile(join(out,"analysis.json"),JSON.stringify(analysis,null,2)+"\n");
  await writeFile(join(out,"report.md"),["# Memory HTTP PUT generator diagnosis","","All workloads executed on Railway. Original server binary SHA matches the retained controlled comparison.","",
    "| Generator processes | Total clients | Median MiB/s | Client CPU % | Server CPU % |","|---|---|---|---|---|",
    ...summary.map(row=>`| ${row.processes} | 4 | ${row.medianMiBPerSecond} | ${row.medianClientCpuPercent} | ${row.medianServerCpuPercent} |`),"",
    `Paired geometric effect: ${analysis.effect.geometricMeanPercentChange}%; conditional 95% interval ${analysis.effect.conditional95PercentInterval.join(" to ")}%.`,"",
    ...analysis.limitations.map(item=>`- ${item}`),""].join("\n"));
  console.log(JSON.stringify({complete:analysis.complete,summary,effect:analysis.effect,pairs}));
}
if(process.argv[1]===fileURLToPath(import.meta.url)) await main();
