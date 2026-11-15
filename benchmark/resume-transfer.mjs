import {execFileSync} from 'node:child_process';
import {mkdir,readFile,writeFile} from 'node:fs/promises';
import {join} from 'node:path';
import {createHash} from 'node:crypto';

if (process.env.BENCH_CLOUD_VERIFY !== '1') throw new Error('Railway verification only');
const root='/app/verification/resume-transfer';
const binary='/app/resume-conformance.test';
const run=(cmd,args=[])=>execFileSync(cmd,args,{encoding:'utf8'}).trim();
await mkdir('/app/verification',{recursive:true});
await mkdir(root); // Refuse replacement of prior evidence.
const source=JSON.parse(await readFile('/app/benchmark/source.json','utf8'));
const binarySha256=createHash('sha256').update(await readFile(binary)).digest('hex');
const cpuModel=run('sh',['-c',"awk -F: '/model name/{gsub(/^ +/,\"\",$2);print $2;exit}' /proc/cpuinfo"]);
const host={uname:run('uname',['-a']),cpuModel,cpuQuota:run('cat',['/sys/fs/cgroup/cpu.max']),memoryLimit:run('cat',['/sys/fs/cgroup/memory.max']),filesystem:run('df',['-T','/tmp','/app']),binarySha256,source};
if(!cpuModel||!host.cpuQuota||!host.memoryLimit||!host.filesystem||!source.commit||!source.payloadSha256)throw Error('required host or source snapshot metadata is missing');
const batchStartedAt=new Date().toISOString();
const cgroupBefore={cpuStat:run('cat',['/sys/fs/cgroup/cpu.stat']),memoryEvents:run('cat',['/sys/fs/cgroup/memory.events'])};
await writeFile(join(root,'host.json'),JSON.stringify({host,batchStartedAt,cgroupBefore},null,2)+'\n');
const profiles=['memory','filesystem','runtimefs','workspace'];
const samples=[];
let failedSample='';
batch: for(const direction of ['download','upload']) for(const profile of profiles) for(let repetition=1;repetition<=3;repetition++) {
 const stem=`${direction}-${profile}-${repetition}`, output=join(root,`${stem}.json`), log=join(root,`${stem}.log`);
 let code=0, stdout='', stderr='';
 try { stdout=execFileSync(binary,[`-test.run=^TestResume${direction[0].toUpperCase()+direction.slice(1)}CostEvidence$`,'-test.count=1'],{encoding:'utf8',env:{...process.env,STOW_RESUME_COST_PROFILE:profile,STOW_RESUME_COST_OUT:output}}); }
 catch(error) { code=error.status??1; stdout=error.stdout?.toString()??''; stderr=error.stderr?.toString()??error.message; }
 await writeFile(log,stdout+stderr);
 if(code!==0) { failedSample=`${stem} failed; see ${log}`; break batch; }
 const record=JSON.parse(await readFile(output,'utf8'));
 samples.push({direction,profile,repetition,record});
}
const fields={download:['totalAllocDeltaBytes','mallocsDelta','maxRssBytes','procReadBytes','procWriteBytes','storageApparentBytes','storageAllocatedBytes','clientFilesApparentBytes','clientFilesAllocatedBytes','durationMs'],upload:['totalAllocDeltaBytes','mallocsDelta','maxRssBytes','procReadBytes','procWriteBytes','storageApparentBytes','storageAllocatedBytes','uploadStateBytes','durationMs']};
const batchFinishedAt=new Date().toISOString();
const cgroupAfter={cpuStat:run('cat',['/sys/fs/cgroup/cpu.stat']),memoryEvents:run('cat',['/sys/fs/cgroup/memory.events'])};
await writeFile(join(root,'batch-resources.json'),JSON.stringify({batchStartedAt,batchFinishedAt,cgroupBefore,cgroupAfter},null,2)+'\n');
if(failedSample)throw new Error(failedSample);
const summary={complete:true,batchStartedAt,batchFinishedAt,cgroupBefore,cgroupAfter,method:'Three repetitions per direction and profile; each sample is a fresh Go test process. Fixed 5 MiB + 317 byte payload; retained backends restart after interruption; memory remains in-process and is volatile.',host,fields:{},samples};
for(const direction of ['download','upload']) for(const profile of profiles) {
 const rows=samples.filter(s=>s.profile===profile&&s.direction===direction).map(s=>s.record);
 summary.fields[`${direction}-${profile}`]={};
 for(const field of fields[direction]) { const values=rows.map(r=>r[field]).sort((a,b)=>a-b); if(values.length!==3||values.some(v=>!Number.isFinite(v))) throw new Error(`Missing ${field} samples for ${direction}-${profile}`); summary.fields[`${direction}-${profile}`][field]={median:values[1],min:values[0],max:values.at(-1)}; }
}
await writeFile(join(root,'summary.json'),JSON.stringify(summary,null,2)+'\n');
console.log(`Resume transfer evidence complete: ${root}/summary.json`);
