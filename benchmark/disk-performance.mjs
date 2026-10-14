import {execFileSync} from 'node:child_process';
import {mkdir,copyFile,writeFile,readFile} from 'node:fs/promises';
if(process.cwd()!=='/src'||process.env.BENCH_CLOUD_VERIFY!=='1')throw Error('Railway verification only');
const out='/out/verification/disk-performance',baseline='/tmp/disk-before';
await mkdir(out,{recursive:true});await mkdir(baseline,{recursive:true});
execFileSync('tar',['-xzf','benchmark/variants/before-read-performance.tar.gz','-C',baseline],{stdio:'ignore'});
await copyFile('internal/storage/fs/disk_performance_test.go',`${baseline}/internal/storage/fs/disk_performance_test.go`);
execFileSync('go',['test','-race','./internal/atomicfile','./internal/runtime','./internal/storage/...','./pkg/stow'],{stdio:'inherit'});
for(const [name,cwd]of [['before',baseline],['after','/src']])execFileSync('go',['test','-c','-o',`${out}/${name}.test`,'./internal/storage/fs'],{cwd,stdio:'inherit'});
const samples=[];
for(let repetition=0;repetition<6;repetition++)for(const variant of repetition%2?['after','before']:['before','after']){
 const output=execFileSync(`${out}/${variant}.test`,['-test.run=^$','-test.bench=^BenchmarkDiskObject$','-test.benchtime=1s','-test.benchmem','-test.cpu=4'],{encoding:'utf8'});
 await writeFile(`${out}/${variant}-${repetition}.txt`,output);samples.push({variant,repetition,output});
}
await writeFile(`${out}/samples.json`,JSON.stringify({complete:true,samples,source:JSON.parse(await readFile('benchmark/source.json','utf8')),method:'Six alternating fresh processes per variant. Direct managed filesystem PUT/COPY, generated body, native benchmark temporary directory on Railway build host. Not mounted-volume or network capacity measurements.'},null,2)+'\n');
