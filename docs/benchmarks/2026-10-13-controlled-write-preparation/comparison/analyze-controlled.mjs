import { readFile, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { resolve, join } from "node:path";
import assert from "node:assert/strict";

const directory = resolve(process.argv[2]);
if (!directory.startsWith("/results/") || process.platform !== "linux") throw new Error("controlled analysis runs on the Railway results volume");
const json = async name => JSON.parse(await readFile(join(directory, name), "utf8"));
const run = await json("run.json"), profile = await json("profile.json"), suite = await json("s3/suite.json");
assert.equal(run.complete, true); assert.equal(run.embedded.complete, true); assert.equal(run.s3.acceptable, true); assert.equal(suite.complete, true);
assert.equal(profile.pairedVariants, true); assert.equal(profile.repetitions, 6);
assert.equal(suite.results.length, 72); assert.equal(new Set(suite.results.map(r=>r.rawFile)).size, 72);
assert.deepEqual(suite.metadata.generator.build, suite.metadata.target.build);
assert.equal(suite.metadata.generator.cgroup.cpuMax, "400000 100000");
assert.equal(suite.metadata.generator.cgroup.memoryMax, "4294967296");
assert.equal(suite.metadata.target.cgroup.cpuMax, "200000 100000");
assert.equal(suite.metadata.target.cgroup.memoryMax, "2147483648");
const build = suite.metadata.target.build;
assert.equal(build.variants.variants.after.raceTestsPassed, true);
assert.equal(build.variants.variants.before.raceTestsPassed, true);
assert.equal(build.variants.variants.before.expectedRetryRegressionsReproduced, true);
for (const [variant, suffix] of [["after", ""], ["before", "-before"]]) {
  assert.equal(build.binaries[`stow-s3${suffix}`], build.variants.variants[variant].serverSha256);
  assert.equal(build.binaries[`embedded${suffix}.test`], build.variants.variants[variant].embeddedSha256);
}
const median = values => { const sorted=values.toSorted((a,b)=>a-b), i=Math.floor(sorted.length/2); return sorted.length%2?sorted[i]:(sorted[i-1]+sorted[i])/2; };
const round = value => Math.round(value*1000)/1000;
function pairedEffect(pairs) {
  assert.equal(pairs.length, 6);
  const ratios=pairs.map(({before,after})=>{assert.ok(before>0&&after>0);return Math.log(after/before);});
  const mean=ratios.reduce((a,b)=>a+b,0)/6, variance=ratios.reduce((sum,value)=>sum+(value-mean)**2,0)/5;
  const radius=2.57058183661474*Math.sqrt(variance/6);
  return {pairedPercentChanges:pairs.map(({before,after})=>round((after/before-1)*100)), medianPairedPercentChange:round(median(pairs.map(({before,after})=>(after/before-1)*100))),
    geometricMeanPercentChange:round((Math.exp(mean)-1)*100), conditional95PercentInterval:[round((Math.exp(mean-radius)-1)*100),round((Math.exp(mean+radius)-1)*100)]};
}
const stat = text => Object.fromEntries(text.split("\n").map(line=>{const [key,value]=line.split(/\s+/);return [key,Number(value)];}));
const throttling = (first,last) => {const a=stat(first.cpuStat),b=stat(last.cpuStat),periods=b.nr_periods-a.nr_periods;return {periods,throttledPeriods:b.nr_throttled-a.nr_throttled,throttledPeriodPercent:periods?round((b.nr_throttled-a.nr_throttled)/periods*100):0,cumulativeThrottledSeconds:round((b.throttled_usec-a.throttled_usec)/1e6)};};
const batches = new Map(), resources = [];
for (const indexed of suite.results) {
  const raw = await json("s3/"+indexed.rawFile);
  assert.deepEqual(raw.scenario,indexed.scenario);assert.equal(raw.repetition,indexed.repetition);assert.equal(raw.orderWithinRepetition,indexed.orderWithinRepetition);
  for (const stage of [raw.warmup,raw.load]) {assert.equal(stage.failed,0);assert.equal(stage.dropped,0);}
  assert.equal(raw.verification.complete,true);assert.equal(raw.leftovers.count,0);assert.equal(raw.resources.diskHeadroomMaintained,true);
  assert.equal(raw.firstWriteVerified,true);assert.equal(raw.firstWriteBytes,4096);assert.ok(raw.firstWriteMs>=raw.startupMs&&raw.startupMs>0);
  assert.ok(raw.resources.samples.length>=2);assert.ok(raw.load.successful>=100,"too few measured files for a throughput window");
  const before=raw.scenario.implementation.endsWith("-before"), variant=before?"before":"after";
  assert.equal(raw.scenario.backend,raw.scenario.implementation.includes("-memory")?"memory":"filesystem");
  const key=JSON.stringify([raw.scenario.backend,raw.scenario.workload]), rows=batches.get(key)??[];
  rows.push({variant,raw,rawFile:indexed.rawFile});batches.set(key,rows);
  const first=raw.resources.samples[0],last=raw.resources.samples.at(-1);
  resources.push({rawFile:indexed.rawFile,variant,backend:raw.scenario.backend,workload:raw.scenario.workload,repetition:raw.repetition,
    generatorAverageCpuPercent:round(raw.resources.generatorCpuSeconds/raw.resources.intervalSeconds*100),serverAverageCpuPercent:round(raw.resources.serverAverageCpuPercent),
    targetThrottling:throttling(first.cgroup,last.cgroup),generatorThrottling:throttling(first.generatorCgroup,last.generatorCgroup),
    generatorEventLoopMaxMs:round(raw.resources.generatorEventLoopDelayMs.max),minimumDiskAvailableMiB:round(raw.resources.minimumSampledDiskAvailableBytes/1048576),
    peakRssMiB:round(raw.resources.serverPeakSampledRssBytes/1048576),highWaterRssMiB:round(raw.resources.serverHighWaterRssBytes/1048576)});
}
const s3=[];
for (const [key,rows] of batches) {
  const [backend,workload]=JSON.parse(key), pairs=[];
  const summarize=variant=>{
    const samples=rows.filter(r=>r.variant===variant);assert.equal(samples.length,6);
    assert.deepEqual(samples.map(r=>r.raw.repetition).toSorted((a,b)=>a-b),[1,2,3,4,5,6]);
    const rates=samples.map(r=>r.raw.load.filesPerSecond);
    return {filesPerSecond:round(median(rates)),minFilesPerSecond:round(Math.min(...rates)),maxFilesPerSecond:round(Math.max(...rates)),
      payloadMiBPerSecond:round(median(rates)*profile.sizes[0]/1048576),medianRunP99Ms:round(median(samples.map(r=>r.raw.load.responseLatencyMs.p99))),
      peakSampledServerRssMiB:round(Math.max(...samples.map(r=>r.raw.resources.serverPeakSampledRssBytes))/1048576),
      medianServerCpuPercent:round(median(samples.map(r=>r.raw.resources.serverAverageCpuPercent))),minimumCompletedSamples:Math.min(...samples.map(r=>r.raw.load.successful)),p99HasAtLeast1000FilesInEveryWindow:samples.every(r=>r.raw.load.successful>=1000)};
  };
  for(let repetition=1;repetition<=6;repetition++) {
    const a=rows.find(r=>r.variant==="after"&&r.raw.repetition===repetition),b=rows.find(r=>r.variant==="before"&&r.raw.repetition===repetition);
    assert.equal(Math.abs(a.raw.orderWithinRepetition-b.raw.orderWithinRepetition),1);
    assert.equal(b.raw.orderWithinRepetition<a.raw.orderWithinRepetition,repetition%2===1);
    pairs.push({before:b.raw.load.filesPerSecond,after:a.raw.load.filesPerSecond,beforeFile:b.rawFile,afterFile:a.rawFile});
  }
  s3.push({backend,workload,before:summarize("before"),after:summarize("after"),effect:pairedEffect(pairs),pairs});
}
assert.equal(s3.length,6);
const native = new Map(), managed = {before:[],after:[]};
const pattern=/^BenchmarkComparison\/([^/]+)\/([^/]+)\/(\d+)B-(\d+)\s+(\d+)\s+([\d.]+) ns\/op\s+([\d.]+) MB\/s\s+(\d+) B\/op\s+(\d+) allocs\/op/;
for(let repetition=1;repetition<=6;repetition++) for(const variant of ["before","after"]) {
  const text=await readFile(join(directory,`embedded-${variant}-${repetition}.txt`),"utf8");assert.ok(text.trim().endsWith("PASS"));let count=0;
  for(const line of text.split("\n").filter(line=>line.startsWith("Benchmark"))) {
    const match=line.match(pattern);assert.ok(match,line);const [,implementation,workload,bytes,cpu,iterations,ns,mb,allocated,allocs]=match;
    assert.equal(Number(bytes),1048576);assert.equal(Number(cpu),4);count++;
    const key=JSON.stringify([implementation,workload]),samples=native.get(key)??[];samples.push({variant,repetition,ns:Number(ns),mib:Number(mb)*1e6/1048576,allocated:Number(allocated),allocs:Number(allocs),iterations:Number(iterations)});native.set(key,samples);
  }
  assert.equal(count,6);
  const save=await readFile(join(directory,`managed-${variant}-${repetition}.txt`),"utf8");assert.ok(save.trim().endsWith("PASS"));
  const match=save.match(/^BenchmarkManagedSave\/1048576B-4\s+(\d+)\s+([\d.]+) ns\/op\s+([\d.]+) MB\/s\s+(\d+) B\/op\s+(\d+) allocs\/op/m);assert.ok(match);assert.equal(Number(match[1]),64);
  managed[variant].push({repetition,ns:Number(match[2]),mib:Number(match[3])*1e6/1048576,allocated:Number(match[4]),allocs:Number(match[5])});
}
const summarizeNative=values=>({payloadMiBPerSecond:round(median(values.map(v=>v.mib))),medianNsPerOp:round(median(values.map(v=>v.ns))),medianAllocatedBytesPerOp:median(values.map(v=>v.allocated)),medianAllocationsPerOp:median(values.map(v=>v.allocs))});
const embedded=[...native].map(([key,values])=>{
  const [implementation,workload]=JSON.parse(key),before=values.filter(v=>v.variant==="before"),after=values.filter(v=>v.variant==="after");assert.equal(before.length,6);assert.equal(after.length,6);
  return {implementation,workload,before:summarizeNative(before),after:summarizeNative(after),effect:pairedEffect(before.map((b,i)=>({before:b.mib,after:after[i].mib}))),samples:values};
});
assert.equal(embedded.length,6);
const managedSave={before:summarizeNative(managed.before),after:summarizeNative(managed.after),effect:pairedEffect(managed.before.map((b,i)=>({before:b.mib,after:managed.after[i].mib}))),samples:managed};
const startup=[];
for(const backend of ["memory","filesystem"]) for(const variant of ["before","after"]) {
  const rows=[...batches.values()].flat().filter(r=>r.raw.scenario.backend===backend&&r.variant===variant);assert.equal(rows.length,18);
  startup.push({backend,variant,samples:18,medianReadyMs:round(median(rows.map(r=>r.raw.startupMs))),medianFirstWriteMs:round(median(rows.map(r=>r.raw.firstWriteMs)))});
}
const analysisSource=await readFile(new URL(import.meta.url));
const output={complete:true,checkedAt:new Date().toISOString(),sameImageAndWorkers:true,source:build.source,variants:build.variants,s3Runs:72,nativeSamples:72,managedSaveSamples:12,
  failures:0,drops:0,analysisSourceSha256:createHash("sha256").update(analysisSource).digest("hex"),s3,embedded,managedSave,startup,resources,
  statisticalScope:"Six paired rounds on one target worker. Student-t interval for mean log throughput ratio, conditional on these rounds; shared-host noise and client limits remain. No sustained-capacity qualification.",
  baselineScope:build.variants.specification.scope};
await writeFile(join(directory,"analysis.json"),JSON.stringify(output,null,2)+"\n");
await writeFile(join(directory,"paired-report.md"),["# Paired write-preparation throughput comparison","",output.statisticalScope,"",output.baselineScope,"",
  "| Backend | Workload | Before | After | Median paired change | Conditional 95% interval |","|---|---|---:|---:|---:|---:|",
  ...s3.map(r=>`| ${r.backend} | ${r.workload} | ${r.before.filesPerSecond} | ${r.after.filesPerSecond} | ${r.effect.medianPairedPercentChange}% | ${r.effect.conditional95PercentInterval.join(" to ")}% |`),"",
  "PUT and GET rates are MiB/s for 1 MiB objects; quarantine rates are complete files/s. Reads are SHA-256 verified. Quarantine uses a 2 ms simulated scanner.",
  "Private HTTP, no TLS, four clients, six paired 20 s windows, 5 s warm-up, bounded fixtures. Native measurements use ephemeral runner storage; S3 disk uses the target volume.","",
  `Managed filesystem saves: ${managedSave.before.payloadMiBPerSecond} to ${managedSave.after.payloadMiBPerSecond} MiB/s; allocations ${managedSave.before.medianAllocatedBytesPerOp} to ${managedSave.after.medianAllocatedBytesPerOp} B/op.`,
  "Managed saves use 64 acknowledged keyed replacements per sample, fresh stores and retained receipts. Every sample verifies the final payload.","",
  "Competitors were not remeasured. This compares the write-preparation implementation only; it does not attribute the earlier streaming-build HTTP PUT regression.",""].join("\n"));
console.log(JSON.stringify({complete:true,s3Runs:72,nativeSamples:72,managedSaveSamples:12,s3:s3.map(({pairs,...row})=>row),managedSave:{before:managedSave.before,after:managedSave.after,effect:managedSave.effect},startup},null,2));
