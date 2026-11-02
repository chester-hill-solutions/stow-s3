import {readFile,writeFile,mkdir,statfs} from 'node:fs/promises';
import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const root=process.env.STOW_RESULT_ROOT;
if(!root?.startsWith('/results/')||process.getuid()!==10001)throw Error('Railway unprivileged result root required');
const fixture=root+'/fixture-tmp';await mkdir(fixture);const disk=await statfs(fixture);if(disk.type!==61267)throw Error('ext4 volume required');
const hash=async n=>createHash('sha256').update(await readFile(n)).digest('hex');
const binaries={before:{path:'/app/benchmark/scan-before.test'},after:{path:'/app/benchmark/scan-after.test'}};for(const b of Object.values(binaries))b.sha256=await hash(b.path);
const observations=[],commands=[],order=['A','B','B','A','B','A','A','B','C','D','D','C','D','C','C','D'];
const cases=[...['object','part'].flatMap(kind=>[1048576,67108864].flatMap(size=>['scan','bind','delete'].map(stage=>`^BenchmarkCapacityPayloadScan$/^kind=${kind}$/^size=${size}$/^stage=${stage}$`))),... [1048576,67108864].map(size=>`^BenchmarkCapacityPayloadScan$/^header=${size}$`),'^BenchmarkCapacityPayloadScan$/^empty$'];
for(let i=0;i<order.length;i++){
 const label=order[i],variant=label==='B'?'after':'before';
 for(const cell of cases){
  const binary=binaries[variant],name=cell.replace(/[^a-zA-Z0-9=-]/g,'_');
  const args=['-test.run=^$','-test.bench='+cell,'-test.benchmem','-test.benchtime=3x','-test.count=1'];
  const start=Date.now(),q=spawnSync('python3',['benchmark/measure-child.py',binary.path,...args],{cwd:'/app',encoding:'utf8',env:{...process.env,STOW_BENCH_ROOT:fixture,GOMAXPROCS:'4'},timeout:90000,maxBuffer:2000000});
  await writeFile(root+`/sample-${i}-${name}-${label}.log`,q.stdout+q.stderr);const resourceLine=q.stdout.split('\n').find(line=>line.startsWith('STOW_CHILD_RESOURCE:'));if(!resourceLine)throw Error(q.stdout+q.stderr);const usage=JSON.parse(resourceLine.slice('STOW_CHILD_RESOURCE:'.length));
  commands.push({i,label,variant,cell,args,exitCode:q.status,error:q.error?.code??null,signal:q.signal,elapsedMs:Date.now()-start,binary,usage});if(q.status!==0)throw Error(q.stdout+q.stderr);
  let count=0;for(const line of q.stdout.split('\n')){const m=line.match(/^(BenchmarkCapacityPayloadScan\/\S+)\s+(\d+)\s+([\d.]+) ns\/op\s+([\d.]+) B\/op\s+([\d.]+) allocs\/op$/);if(!m)continue;count++;observations.push({i,label,variant,name:m[1],iterations:Number(m[2]),ns:Number(m[3]),bytes:Number(m[4]),allocs:Number(m[5]),maxRSSBytes:usage.maxRSSBytes});}if(count!==1)throw Error('Expected one isolated benchmark cell; got '+count);
 }
 await writeFile(root+'/measurement-progress.json',JSON.stringify({complete:i+1,total:order.length,label}));
 await writeFile(root+'/measurements.json',JSON.stringify({binaries,order,diskType:disk.type,observations,commands},null,2));
}
console.log(JSON.stringify({observations:observations.length,binaries,diskType:disk.type}));
