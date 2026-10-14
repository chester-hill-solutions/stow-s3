import {execFileSync} from 'node:child_process';
import {mkdir,copyFile,writeFile,readFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
if(process.cwd()!=='/src'||process.env.BENCH_CLOUD_VERIFY!=='1')throw Error('Railway verification only');
const out='/out/verification/read-performance', baseline='/tmp/read-before';
await mkdir(out,{recursive:true});await mkdir(baseline,{recursive:true});
execFileSync('tar',['-xzf','benchmark/variants/before-read-performance.tar.gz','-C',baseline]);
for(const file of ['pkg/stow/read_performance_test.go','internal/runtime/read_concurrency_test.go'])await copyFile(file,`${baseline}/${file}`);
const red=execFileSync('sh',['-c',`go test ./pkg/stow ./internal/runtime -run '^(TestEmbeddedGetAllocationBounded|TestWholeObjectReadsOverlapAndCloseWaits)$' -count=1 > '${out}/red.log' 2>&1; code=$?; cat '${out}/red.log'; test "$code" -eq 1`],{cwd:baseline,encoding:'utf8'});
if(!red.includes('whole-object reads serialized')||!red.includes('GET allocated'))throw Error('Expected both performance regressions on baseline');
execFileSync('go',['test','-race','./internal/runtime','./internal/storage/...','./pkg/stow'],{stdio:'inherit'});
for(const [name,cwd]of [['before',baseline],['after','/src']]){
 execFileSync('go',['test','-c','-o',`${out}/${name}.test`,'./pkg/stow'],{cwd,stdio:'inherit'});
 execFileSync('go',['build','-buildvcs=false','-trimpath','-ldflags','-s -w','-o',`${out}/${name}.wasm`,'./cmd/stow-wasm'],{cwd,env:{...process.env,GOOS:'js',GOARCH:'wasm'},stdio:'inherit'});
}
const hash=bytes=>createHash('sha256').update(bytes).digest('hex');
if(hash(await readFile(`${out}/before.wasm`))!==hash(await readFile('packages/stow-s3/dist/stow-runtime.wasm')))throw Error('Baseline SDK mismatch');
const native=[],wasm=[];
for(let repetition=0;repetition<6;repetition++)for(const variant of repetition%2?['after','before']:['before','after']){
 const output=execFileSync(`${out}/${variant}.test`,['-test.run=^$','-test.bench=^BenchmarkEmbeddedGet$','-test.benchtime=1s','-test.benchmem','-test.cpu=4'],{encoding:'utf8'});
 await writeFile(`${out}/${variant}-${repetition}.txt`,output);native.push({variant,repetition,output});
 wasm.push({variant,repetition,...JSON.parse(execFileSync(process.execPath,['benchmark/embedded-startup.mjs',`${out}/${variant}.wasm`],{encoding:'utf8'}))});
}
await writeFile(`${out}/samples.json`,JSON.stringify({complete:true,native,wasm,source:JSON.parse(await readFile('benchmark/source.json','utf8')),sizes:await Promise.all(['before','after'].map(async variant=>{const bytes=await readFile(`${out}/${variant}.wasm`);return {variant,bytes:bytes.length,sha256:hash(bytes)}})),method:'Six alternating fresh processes per variant; in-process native GET and binary WASM bridge. Shared Railway build host, no sockets or deployed CPU quota guarantees.'},null,2)+'\n');
