import {test} from 'node:test';
import assert from 'node:assert/strict';
import {verifyReceipts} from './check-live-receipts.mjs';
const receipts=()=>['aws-s3','cloudflare-r2','custom'].map(profile=>({profile,provider:profile,status:'verified',commit:'abc'}));
test('three actual provider outcomes satisfy release gate',()=>verifyReceipts(receipts(),'abc'));
test('configuration, dry run, duplicate or stale receipts cannot claim verification',()=>{
 for(const alteration of [r=>r.pop(),r=>r[1].status='unverified',r=>r[1].provider='custom',r=>r[1].commit='old',r=>r[1].profile='aws-s3']) {
  const input=receipts();alteration(input);assert.throws(()=>verifyReceipts(input,'abc'));
 }
});
