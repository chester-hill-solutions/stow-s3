import {readFile,writeFile} from "node:fs/promises";
import {execFileSync} from "node:child_process";
import assert from "node:assert/strict";
const old="/results/59d62a35-6293-4ddb-a06f-ce041bd9b9c9",current=process.argv[2];
execFileSync(process.execPath,["benchmark/analyze.mjs",current],{stdio:"inherit"});
const read=async(root,name)=>JSON.parse(await readFile(root+"/"+name,"utf8"));
const before=await read(old,"s3/summary.json"), after=await read(current,"s3/summary.json");
const oldSuite=await read(old,"s3/suite.json"),newSuite=await read(current,"s3/suite.json");
for(const k of ["warmupSeconds","durationSeconds","repetitions","sizes","concurrency","rates","retainedObjects","scannerDelayMs","maxQueue","timeoutMs","maxBytes","maxObjects","serverConcurrency"]) assert.deepEqual(newSuite.config[k],oldSuite.config[k],k);
for(const role of ["generator","target"]) {
 assert.equal(newSuite.metadata[role].cpu,oldSuite.metadata[role].cpu);
 assert.equal(newSuite.metadata[role].cgroup.cpuMax,oldSuite.metadata[role].cgroup.cpuMax);
 assert.equal(newSuite.metadata[role].cgroup.memoryMax,oldSuite.metadata[role].cgroup.memoryMax);
}
const round=x=>Math.round(x*1000)/1000;
const fields=["payloadMiBPerSecond","filesPerSecond","medianRunP99ResponseMs","peakSampledServerRssMiB"];
const s3=after.rows.map(a=>{
 const b=before.rows.find(b=>b.implementation===a.implementation&&b.workload===a.workload&&b.bytes===a.bytes&&b.concurrency===a.concurrency&&b.rate===a.rate);
 assert.ok(b);assert.equal(a.repetitions,b.repetitions);
 return {implementation:a.implementation,workload:a.workload,bytes:a.bytes,concurrency:a.concurrency,before:Object.fromEntries(fields.map(k=>[k,b[k]])),after:Object.fromEntries(fields.map(k=>[k,a[k]])),throughputRatio:round(a.filesPerSecond/b.filesPerSecond),throughputChangePercent:round((a.filesPerSecond/b.filesPerSecond-1)*100),beforeRange:[b.minPayloadMiBPerSecond,b.maxPayloadMiBPerSecond],afterRange:[a.minPayloadMiBPerSecond,a.maxPayloadMiBPerSecond]};
});
const oldAnalysis=await read(old,"analysis.json"),newAnalysis=await read(current,"analysis.json");
const native=newAnalysis.embedded.map(a=>{
 const b=oldAnalysis.embedded.find(b=>b.implementation===a.implementation&&b.workload===a.workload&&b.bytes===a.bytes&&b.cpu===a.cpu);assert.ok(b);
 return {implementation:a.implementation,workload:a.workload,bytes:a.bytes,cpu:a.cpu,before:b,after:a,allocationReductionPercent:round((1-a.medianAllocatedBytesPerOp/b.medianAllocatedBytesPerOp)*100)};
});
const resourceSummary={generatorMaxAverageCpuPercent:Math.max(...newAnalysis.resources.map(r=>r.generatorAverageCpuPercent)),generatorMaxThrottledPeriodPercent:Math.max(...newAnalysis.resources.map(r=>r.generatorThrottling.throttledPeriodPercent)),targetMaxThrottledPeriodPercent:Math.max(...newAnalysis.resources.map(r=>r.targetThrottling.throttledPeriodPercent)),targetMaxCumulativeThrottledSeconds:Math.max(...newAnalysis.resources.map(r=>r.targetThrottling.cumulativeThrottledSeconds)),minimumDiskAvailableMiB:Math.min(...newAnalysis.resources.map(r=>r.minimumDiskAvailableMiB))};
const result={complete:true,checkedAt:new Date().toISOString(),beforeRunId:old.split("/").at(-1),afterRunId:current.split("/").at(-1),sameS3ConfigurationAndCpuLimits:true,competitorsRemeasured:false,s3,native,resourceSummary,limits:"Exploratory cross-run comparison, shared Railway hardware, warm bounded working set; no scanner, DPI, TLS, power-loss or sustained capacity qualification. Native baseline uses 300ms windows and new native cases use 500ms."};
await writeFile(current+"/comparison.json",JSON.stringify(result,null,2)+"\n");
await writeFile(current+"/comparison.md",["# Streaming and quarantine performance regression","","All workloads and analysis ran on Railway. Same S3 configuration and CPU limits as the earlier comparison. Competitors were not remeasured.","","| Backend / workflow | Before rate | After rate | Ratio | p99 ms before → after | Sampled RSS MiB before → after |","|---|---:|---:|---:|---:|---:|",...s3.map(r=>`| ${r.implementation} / ${r.workload} | ${r.before.filesPerSecond} | ${r.after.filesPerSecond} | ${r.throughputRatio}× | ${r.before.medianRunP99ResponseMs} → ${r.after.medianRunP99ResponseMs} | ${r.before.peakSampledServerRssMiB} → ${r.after.peakSampledServerRssMiB} |`),"","Rates are files/s; at 1 MiB each, PUT/GET rates also equal payload MiB/s. Quarantine counts incoming payload once.","",result.limits,""].join("\n"));
console.log(JSON.stringify(result));
