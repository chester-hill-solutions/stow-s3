// Run only on the Railway generator. No measurements or site builds run locally.
//
// It refreshes the marketing comparison against the published v0.3.3 revision:
// the smoke profile, the comparison profile, the analysis, the published
// dataset, the site build with the new dataset, and the Chromium chart
// verification. The results land on the runner's volume for collection.
import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir,cp,symlink} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
import {join} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
import {createHash} from 'node:crypto';
import {remote} from '/app/benchmark/http.mjs';

assert.equal(process.cwd(),'/app');
assert.equal(process.env.RAILWAY_SERVICE_ID,'83497842-c51d-420e-8ba1-181589730ad4');

const root='/results/marketing-refresh-20261019';
const sourceHash='b34705cc5c076a13f47753a79c3a8a31d9e8de50fdd5f4cd4a00fdc092b6b277';
const chartSource='/app/benchmark/chart-source.tar.gz';
const status={complete:false,published:false,startedAt:new Date().toISOString()};

await mkdir(root,{recursive:false});
const save=async phase=>{status.phase=phase;status.updatedAt=new Date().toISOString();await writeFile(join(root,'status.json'),JSON.stringify(status,null,2)+'\n');};
const run=(command,args,options={})=>execFileSync(command,args,{stdio:'inherit',timeout:600000,...options});

async function job(profile){
  const launched=await remote('http://127.0.0.1:8082','/run',{profile});
  status[profile+'RunId']=launched.id;await save(profile);
  const began=Date.now();
  while(Date.now()-began<3700000){
    await delay(10000);
    const state=await remote('http://127.0.0.1:8082','/status');
    assert.equal(state.id,launched.id);
    if(!state.running){
      assert.equal(state.exitCode,0);
      const receipt=JSON.parse(await readFile('/results/'+launched.id+'/run.json','utf8'));
      assert.equal(receipt.complete,true);
      assert.equal(receipt.s3.acceptable,true);
      return '/results/'+launched.id;
    }
  }
  throw Error(profile+' exceeded the job deadline');
}

try{
  await save('identity');

  // The runner and the target must be the same build, and that build must be
  // the source this refresh was planned against.
  const own=JSON.parse(await readFile('/app/benchmark/build-identity.json','utf8'));
  const target=await remote('http://'+process.env.BENCH_TARGET_HOST+':8081','/metadata');
  assert.equal(own.source.payloadSha256,sourceHash);
  assert.deepEqual(own,target.build);
  assert.equal(target.cgroup.cpuMax,'200000 100000');
  assert.equal(target.cgroup.memoryMax,'2147483648');
  await writeFile(join(root,'build-identity.json'),JSON.stringify(own,null,2)+'\n');

  const smoke=await job('smoke');
  assert.equal(JSON.parse(await readFile(smoke+'/s3/suite.json','utf8')).results.length,18);
  await cp(smoke,join(root,'smoke'),{recursive:true});

  const comparison=await job('comparison');
  await save('analysis');
  run('node',['benchmark/analyze.mjs',comparison]);
  run('node',['benchmark/publish.mjs',comparison,join(root,'agent-storage.json')]);
  await cp(comparison,join(root,'comparison'),{recursive:true});

  await save('charts');
  const archiveHash=createHash('sha256').update(await readFile(chartSource)).digest('hex');
  await mkdir('/src',{recursive:true});
  run('tar',['-xzf',chartSource,'-C','/src']);
  await cp('/src/site/benchmark-data.json',join(root,'previous-agent-storage.json'));
  await cp(join(root,'agent-storage.json'),'/src/site/benchmark-data.json');
  await mkdir('/src/packages/stow-s3',{recursive:true});
  await symlink('/app/packages/stow-s3/node_modules','/src/packages/stow-s3/node_modules');
  run('npm',['ci','--ignore-scripts','--prefix','site'],{cwd:'/src'});
  run('npm',['run','build','--prefix','site'],{cwd:'/src'});
  run('npm',['test','--prefix','site'],{cwd:'/src'});
  run('/app/packages/stow-s3/node_modules/.bin/playwright',['install','--with-deps','chromium']);
  run('node',['benchmark/verify-site.mjs'],{cwd:'/src',env:{...process.env,BENCH_CLOUD_VERIFY:'1'}});
  await cp('/out/verification/preview',join(root,'preview'),{recursive:true});
  status.chartSourceArchiveSha256=archiveHash;

  // The two batches are separate, so the difference is an observation and not
  // a controlled paired causal estimate.
  const previous=JSON.parse(await readFile(join(root,'previous-agent-storage.json'),'utf8'));
  const current=JSON.parse(await readFile(join(root,'agent-storage.json'),'utf8'));
  const effects=current.s3.map(row=>{
    const old=previous.s3.find(r=>r.implementation===row.implementation&&r.workload===row.workload&&r.bytes===row.bytes&&r.concurrency===row.concurrency);
    assert.ok(old);
    return {
      implementation:row.implementation,
      workload:row.workload,
      beforePayloadMiBPerSecond:old.payloadMiBPerSecond,
      afterPayloadMiBPerSecond:row.payloadMiBPerSecond,
      observedPercentChange:(row.payloadMiBPerSecond/old.payloadMiBPerSecond-1)*100,
      beforeRssMiB:old.peakSampledServerRssMiB,
      afterRssMiB:row.peakSampledServerRssMiB,
    };
  });
  await writeFile(join(root,'previous-comparison.json'),JSON.stringify({
    beforeRunId:previous.runId,
    afterRunId:current.runId,
    note:'Separate batches on Railway; changes are observations, not a controlled paired causal estimate.',
    effects,
  },null,2)+'\n');

  status.complete=true;
  status.finishedAt=new Date().toISOString();
  await save('complete');
}catch(error){
  status.error=error.message;
  await save('failed');
  process.exitCode=1;
}
