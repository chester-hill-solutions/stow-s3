import {readFile,writeFile,mkdir,statfs} from 'node:fs/promises';
import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const root=process.env.STOW_RESULT_ROOT;
if(!root?.startsWith('/results/')||process.getuid()!==10001)throw Error('Railway unprivileged result root required');
const fixture=root+'/fixture-tmp';await mkdir(fixture);const disk=await statfs(fixture);if(disk.type!==61267)throw Error('ext4 volume required');
const hash=async n=>createHash('sha256').update(await readFile(n)).digest('hex');
const binaries={before:{path:'/app/benchmark/count-before.test'},after:{path:'/app/benchmark/count-after.test'}};
for(const v of Object.values(binaries))v.sha256=await hash(v.path);
const observations=[],commands=[],order=['A','B','B','A','B','A','A','B','C','D','D','C','D','C','C','D'];
for(let i=0;i<order.length;i++){
 const label=order[i],variant=label==='B'?'after':'before';
 for(const family of ['Admission','Charge']){
  const args=['-test.run=^$','-test.bench=^BenchmarkResourceCount'+family+'$','-test.benchmem','-test.benchtime='+(family==='Admission'?'10x':'200ms'),'-test.count=1'];
  const start=Date.now(),q=spawnSync(binaries[variant].path,args,{cwd:'/app',encoding:'utf8',env:{...process.env,STOW_BENCH_ROOT:fixture,STOW_BENCH_EXPECT_COUNT:variant==='after'?'1':'0',GOMAXPROCS:'4'},timeout:90000,maxBuffer:2000000});
  await writeFile(root+`/sample-${i}-${family}-${label}.log`,q.stdout+q.stderr);commands.push({i,label,variant,family,args,exitCode:q.status,error:q.error?.code??null,signal:q.signal,elapsedMs:Date.now()-start,binary:binaries[variant]});if(q.status!==0)throw Error(q.stdout+q.stderr);
  let count=0;for(const line of q.stdout.split('\n')){const m=line.match(/^(BenchmarkResourceCount(?:Admission|Charge)\/\S+)\s+(\d+)\s+([\d.]+) ns\/op\s+([\d.]+) B\/op\s+([\d.]+) allocs\/op$/);if(!m)continue;count++;observations.push({i,label,variant,family,name:m[1],iterations:Number(m[2]),ns:Number(m[3]),bytes:Number(m[4]),allocs:Number(m[5])});}if(count!==(family==='Admission'?2:3))throw Error('Unexpected benchmark cell count: '+count);
 }
 await writeFile(root+'/measurement-progress.json',JSON.stringify({complete:i+1,total:order.length,label}));
 await writeFile(root+'/measurements.json',JSON.stringify({binaries,order,diskType:disk.type,observations,commands},null,2));
}
console.log(JSON.stringify({observations:observations.length,binaries,diskType:disk.type}));
