import {readFile,writeFile} from "node:fs/promises";
import {join,resolve} from "node:path";
import assert from "node:assert/strict";

const directory=resolve(process.argv[2]), destination=resolve(process.argv[3]);
const read=async name=>JSON.parse(await readFile(join(directory,name),"utf8"));
const [run,analysis,suite,summary,profile]=await Promise.all([read("run.json"),read("analysis.json"),read("s3/suite.json"),read("s3/summary.json"),read("profile.json")]);
assert.equal(run.complete,true);assert.equal(run.s3.acceptable,true);assert.equal(analysis.complete,true);assert.equal(analysis.sameBuild,true);
assert.equal(profile.label.includes("smoke"),false,"smoke results cannot be published as a comparison");
assert.equal(analysis.nativeSamples,profile.embedded.expectedSamples);
assert.equal(analysis.agent.length,18);assert.equal(analysis.nodeAgent.length,6);assert.equal(analysis.serverAgent.length,profile.targets.length);
assert.equal(analysis.s3Runs,profile.targets.length*profile.workloads.length*profile.repetitions);
for(const row of summary.rows) {
  assert.equal(row.repetitions,3);assert.equal(row.failedFiles,0);assert.equal(row.droppedFiles,0);
  assert.equal(row.verified,true);assert.equal(row.diskHeadroomMaintained,true);
}
const names={"stow-memory":"Stow","stow-filesystem":"Stow","s3proxy-memory":"S3Proxy",
  "s3proxy-filesystem":"S3Proxy","minio-filesystem":"MinIO","rustfs-filesystem":"RustFS"};
assert.deepEqual([...new Set(summary.rows.map(row=>row.implementation))].sort(),Object.keys(names).sort());
assert.deepEqual(analysis.serverAgent.map(row=>row.implementation).sort(),Object.keys(names).sort());
const peerEntries=object=>Object.fromEntries(Object.entries(object).filter(([key])=>!/afero|sqlite|embedded/i.test(key)));
const cgroups=suite.metadata.target.cgroup;
const result={version:2,complete:true,comparisonScope:"S3-shaped agent storage",qualification:"Exploratory development snapshot; fixed workloads, not a sustained-capacity qualification",
  measuredAt:run.finishedAt,runId:run.runId,source:run.metadata.build.source,
  binaryHashes:peerEntries(run.metadata.build.binaries),wasmSha256:run.metadata.build.wasmSha256,versions:peerEntries(suite.versions),names,
  hardware:{cpu:suite.metadata.target.cpu,targetCpuMax:cgroups.cpuMax,targetMemoryMax:cgroups.memoryMax,
    runnerCpuMax:run.metadata.cgroup.cpuMax,runnerMemoryMax:run.metadata.cgroup.memoryMax},
  settings:{sizes:profile.sizes,concurrency:profile.concurrency,repetitions:profile.repetitions,warmupSeconds:profile.warmupSeconds,
    durationSeconds:profile.durationSeconds,retainedObjects:profile.retainedObjects,scannerDelayMs:suite.config.scannerDelayMs,
    putPattern:"Overwrite one key per worker",s3GetPattern:"Cycle retained fixtures",
    serverStartup:{samplesPerProduct:9,bytes:4096,readinessPollMs:10},transport:"Private HTTP, no TLS",storage:"Filesystem: same persistent target volume; memory: RAM"},
  validation:{failed:0,dropped:0,verified:true,sameBuild:true,s3Runs:analysis.s3Runs,
    generatorThrottledPeriods:analysis.resources.reduce((n,r)=>n+r.generatorThrottling.throttledPeriods,0),
    maxGeneratorCpuPercent:Math.max(...analysis.resources.map(r=>r.generatorAverageCpuPercent)),
    maxTargetThrottledPeriodPercent:Math.max(...analysis.resources.map(r=>r.targetThrottling.throttledPeriodPercent))},
  s3:summary.rows,serverAgent:analysis.serverAgent};
await writeFile(destination,JSON.stringify(result,null,2)+"\n");
console.log(JSON.stringify({destination,complete:true,s3Rows:result.s3.length,scope:result.comparisonScope}));
