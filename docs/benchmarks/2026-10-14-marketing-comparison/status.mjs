import {readFile} from 'node:fs/promises';
const root='/results/marketing-refresh-20261014';
const read=async path=>readFile(path,'utf8').then(JSON.parse).catch(()=>null);
const status=await read(root+'/status.json');
const id=status?.comparisonRunId??status?.smokeRunId;
const suite=id?await read('/results/'+id+'/s3/suite.json'):null;
const run=id?await read('/results/'+id+'/run.json'):null;
const rows=suite?.results??[];
console.log(JSON.stringify({status,run:{complete:run?.complete,embedded:run?.embedded?.complete,error:run?.error},
  cases:rows.length,last:rows.at(-1)?.scenario,failed:rows.reduce((n,r)=>n+r.load.failed,0),
  dropped:rows.reduce((n,r)=>n+r.load.dropped,0),verified:rows.every(r=>r.verification.complete),
  summaries:suite?.complete?await read('/results/'+id+'/s3/summary.json'):undefined}));
console.log((await readFile('/results/marketing-refresh.log','utf8')).split('\n').slice(-4).join('\n'));
