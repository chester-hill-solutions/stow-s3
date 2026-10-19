// Continuation of the marketing refresh: the chart step only.
//
// The measurement completed and published its dataset, and the chart step then
// failed because the chart source archive was missing four example includes
// that the rendered guides reference. The archive was rebuilt with them and the
// chart step is re-run against the same comparison results, so the dataset and
// the chart describe one run.
import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir,cp,rm,symlink} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
import {join} from 'node:path';
import {createHash} from 'node:crypto';

const root='/results/marketing-refresh-20261019';
const comparison='/results/403b5fff-5648-49f7-b478-899eecf31182';
const chartSource='/results/marketing-chart-source.tar.gz';
const status=JSON.parse(await readFile(join(root,'status.json'),'utf8'));

const run=(command,args,options={})=>execFileSync(command,args,{stdio:'inherit',timeout:600000,...options});

try{
  status.phase='charts';
  await writeFile(join(root,'status.json'),JSON.stringify(status,null,2)+'\n');

  const archiveHash=createHash('sha256').update(await readFile(chartSource)).digest('hex');
  await mkdir('/src',{recursive:true});
  run('tar',['-xzf',chartSource,'-C','/src']);
  await cp('/src/site/benchmark-data.json',join(root,'previous-agent-storage.json'));
  await cp(join(root,'agent-storage.json'),'/src/site/benchmark-data.json');
  await mkdir('/src/packages/stow-s3',{recursive:true});
  // The step is re-runnable, so the link from an earlier attempt is replaced
  // rather than refused.
  await rm('/src/packages/stow-s3/node_modules',{recursive:true,force:true});
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
  delete status.error;
  status.chartRetry='The chart step was re-run after the chart source archive was rebuilt with the four example includes the rendered guides reference. The measurement and the published dataset are the same run.';
  status.finishedAt=new Date().toISOString();
  status.phase='complete';
  await writeFile(join(root,'status.json'),JSON.stringify(status,null,2)+'\n');
}catch(error){
  status.error=error.message;
  status.phase='failed';
  await writeFile(join(root,'status.json'),JSON.stringify(status,null,2)+'\n');
  process.exitCode=1;
}
