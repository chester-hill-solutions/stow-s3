import {openStow,resolveStowBinary} from '@chester-hill-solutions/stow-s3';
import {PutObjectCommand,GetObjectCommand} from '@aws-sdk/client-s3';
import {existsSync,writeFileSync} from 'node:fs';
import {performance} from 'node:perf_hooks';
for(const key of Object.keys(process.env))if(/^(STOW_|AWS_|S3_)/.test(key))delete process.env[key];
console.log('binary',resolveStowBinary());
const samples=[];
for(let i=0;i<20;i++){
 const begin=performance.now();const s=await openStow();const ready=performance.now();
 try{
  await s.s3.send(new PutObjectCommand({Bucket:s.bucket,Key:'input.bin',Body:Buffer.alloc(1024*1024,97)}));
  const uploaded=performance.now();
  const r=await s.s3.send(new GetObjectCommand({Bucket:s.bucket,Key:'input.bin'}));
  const bytes=await r.Body.transformToByteArray();if(bytes.length!==1024*1024||bytes[0]!==97)throw new Error('payload mismatch');
  samples.push({ready_ms:ready-begin,first_put_ms:uploaded-begin});
 }finally{await s.close();}
 if(existsSync(s.dataDir))throw new Error('temporary data retained');
}
function percentile(key,p){const values=samples.map(s=>s[key]).sort((a,b)=>a-b);return +values[Math.ceil(p*values.length)-1].toFixed(2);}
const result={samples:20,payload_bytes:1024*1024,ready_p50_ms:percentile('ready_ms',.5),ready_p95_ms:percentile('ready_ms',.95),ready_and_first_put_p50_ms:percentile('first_put_ms',.5),ready_and_first_put_p95_ms:percentile('first_put_ms',.95),roundtrips:'20/20',directory_cleanup:'20/20'};
console.log(JSON.stringify(result,null,2));writeFileSync('/private/tmp/stow-assessment-node-results.json',JSON.stringify({summary:result,samples},null,2));
