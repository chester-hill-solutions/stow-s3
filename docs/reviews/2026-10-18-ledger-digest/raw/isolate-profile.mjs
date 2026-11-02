import {spawnSync} from 'node:child_process';
import {mkdir,writeFile} from 'node:fs/promises';
const root=process.env.STOW_RESULT_ROOT;
if(!root?.startsWith('/results/')||process.getuid()!==10001)throw Error('Railway job required');
const fixtures=root+'/profile-fixtures';await mkdir(fixtures);const commands=[];
for(const variant of ['legacy','bounded']){
 const binary='benchmark/'+(variant==='legacy'?'legacy-reader':'bounded-reader')+'.test',profile=root+'/'+variant+'.pprof';
 for(const [stage,cmd,args]of [
  ['profile',binary,['-test.run=^$','-test.bench=^BenchmarkLedgerIsolation$/^resources=4096$/^stage=snapshot$','-test.benchtime=20x','-test.memprofile='+profile,'-test.memprofilerate=1']],
  ['alloc-top','go',['tool','pprof','-top','-sample_index=alloc_space','-unit=bytes',binary,profile]],
  ['alloc-lines','go',['tool','pprof','-top','-lines','-sample_index=alloc_space','-unit=bytes',binary,profile]]
 ]){
  const start=Date.now(),q=spawnSync(cmd,args,{cwd:'/app',env:{...process.env,STOW_BENCH_ROOT:fixtures,TMPDIR:fixtures},encoding:'utf8',timeout:90000,maxBuffer:4000000});
  await writeFile(root+'/'+variant+'-'+stage+'.log',q.stdout+q.stderr);commands.push({variant,stage,cmd,args,exitCode:q.status,signal:q.signal,elapsedMs:Date.now()-start});if(q.status!==0)throw Error(q.stdout+q.stderr);
 }
}
await writeFile(root+'/profile-receipt.json',JSON.stringify({commands,allPassed:true},null,2));
