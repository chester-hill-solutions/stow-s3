import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdir,copyFile,writeFile,readFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {gzipSync,brotliCompressSync,constants} from 'node:zlib';
assert.equal(process.cwd(),'/src');assert.equal(process.env.BENCH_CLOUD_VERIFY,'1');
const out='/out/verification/hot-paths',baseline='/tmp/hot-paths-before',startedAt=new Date().toISOString();
await mkdir(out,{recursive:true});await mkdir(baseline,{recursive:true});
execFileSync('tar',['-xzf','benchmark/variants/before-read-performance.tar.gz','-C',baseline],{stdio:'ignore'});
for(const file of ['pkg/stow/read_performance_test.go','internal/storage/fs/disk_performance_test.go','internal/storage/workspace/index_performance_test.go'])await copyFile(file,`${baseline}/${file}`);
const binaries={},wasm={};
for(const [variant,cwd]of [['before',baseline],['after','/src']]){
 for(const [kind,pkg]of [['get','./pkg/stow'],['disk','./internal/storage/fs'],['index','./internal/storage/workspace']])execFileSync('go',['test','-c','-trimpath','-o',`${out}/${kind}-${variant}.test`,pkg],{cwd,stdio:'inherit'});
 execFileSync('go',['build','-buildvcs=false','-trimpath','-ldflags','-s -w','-o',`${out}/${variant}.wasm`,'./cmd/stow-wasm'],{cwd,env:{...process.env,GOOS:'js',GOARCH:'wasm'},stdio:'inherit'});
 const bytes=await readFile(`${out}/${variant}.wasm`);
 wasm[variant]={bytes:bytes.length,sha256:createHash('sha256').update(bytes).digest('hex'),gzipBytes:gzipSync(bytes,{level:9}).length,brotliBytes:brotliCompressSync(bytes,{params:{[constants.BROTLI_PARAM_QUALITY]:11}}).length};
}
assert.equal(wasm.before.sha256,createHash('sha256').update(await readFile('packages/stow-s3/dist/stow-runtime.wasm')).digest('hex'));
execFileSync('go',['test','./pkg/stow','-run','^TestWasmExcludesNativeDependencies$','-count=1'],{stdio:'inherit'});
execFileSync('go',['build','./pkg/stow'],{env:{...process.env,GOOS:'windows',GOARCH:'amd64'},stdio:'inherit'});
const samples=[],startup=[];
for(let repetition=0;repetition<6;repetition++)for(const variant of repetition%2?['after','before']:['before','after']){
 for(const kind of ['get','disk','index']){
  const benchmark={get:'EmbeddedGet',disk:'DiskObject',index:'WorkspaceIndex'}[kind];
  const output=execFileSync(`${out}/${kind}-${variant}.test`,['-test.run=^$',`-test.bench=^Benchmark${benchmark}$`,'-test.benchtime=1s','-test.benchmem','-test.cpu=4'],{encoding:'utf8'});
  await writeFile(`${out}/${kind}-${variant}-${repetition}.txt`,output);
  for(const line of output.split('\n')){
   const match=line.match(/^(Benchmark\S+)\s+(\d+)\s+([\d.]+) ns\/op\s+([\d.]+) MB\/s\s+(\d+) B\/op\s+(\d+) allocs\/op/);
   if(match)samples.push({kind,variant,repetition,name:match[1],iterations:Number(match[2]),nsPerOp:Number(match[3]),MBps:Number(match[4]),bytesPerOp:Number(match[5]),allocsPerOp:Number(match[6])});
   const index=line.match(/^(BenchmarkWorkspaceIndex\S+)\s+(\d+)\s+([\d.]+) ns\/op\s+([\d.]+) file-bytes\s+(\d+) B\/op\s+(\d+) allocs\/op/);
   if(index)samples.push({kind,variant,repetition,name:index[1],iterations:Number(index[2]),nsPerOp:Number(index[3]),MBps:0,bytesPerOp:Number(index[5]),allocsPerOp:Number(index[6]),fileBytes:Number(index[4])});
  }
 }
 startup.push({variant,repetition,...JSON.parse(execFileSync(process.execPath,['benchmark/embedded-startup.mjs',`${out}/${variant}.wasm`],{encoding:'utf8'}))});
}
const median=values=>{const sorted=values.toSorted((a,b)=>a-b);return (sorted[2]+sorted[3])/2;};
const summary=[...new Set(samples.map(x=>x.name))].map(name=>{
 const result={name};for(const variant of ['before','after']){
  const selected=samples.filter(x=>x.name===name&&x.variant===variant);assert.equal(selected.length,6);
  const metrics=['nsPerOp','MBps','bytesPerOp','allocsPerOp',...(selected[0].kind==='index'?['fileBytes']:[])];
  result[variant]=Object.fromEntries(metrics.map(metric=>[metric,median(selected.map(x=>x[metric]))]));
 }
 result.allocatedBytesReductionPercent=100*(1-result.after.bytesPerOp/result.before.bytesPerOp);
 result.throughputChangePercent=100*(result.before.nsPerOp/result.after.nsPerOp-1);return result;
});
const startupSummary=['before','after'].map(variant=>{const selected=startup.filter(x=>x.variant===variant);return {variant,...Object.fromEntries(['readyMs','firstWriteMs','putGetPairsPerSecond'].map(metric=>[metric,median(selected.map(x=>x[metric]))]))};});
const executor={cpuMax:(await readFile('/sys/fs/cgroup/cpu.max','utf8')).trim(),memoryMax:(await readFile('/sys/fs/cgroup/memory.max','utf8')).trim(),cpuModel:(await readFile('/proc/cpuinfo','utf8')).match(/model name\s*:\s*(.+)/)?.[1],startedAt,finishedAt:new Date().toISOString()};
await writeFile(`${out}/analysis.json`,JSON.stringify({complete:true,wasm,samples,summary,startup,startupSummary,executor,source:JSON.parse(await readFile('benchmark/source.json','utf8')),method:'Six alternating fresh processes per variant, pinned Go release compiler. Native GET, generated-body filesystem PUT/COPY and index replacement on the Railway benchmark runner with recorded resource limits; ephemeral temporary storage. Binary WASM bridge without SDK, sockets or host persistence. Exploratory medians, not HTTP, actual browser startup or mounted-volume capacity guarantees.'},null,2)+'\n');
console.log(JSON.stringify({hotPaths:{summary,wasm,startupSummary}}));
