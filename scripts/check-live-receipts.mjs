#!/usr/bin/env node
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
export function verifyReceipts(receipts,commit) {
 const expected=['aws-s3','cloudflare-r2','custom'];
 if (receipts.length!==3) throw new Error('three provider receipts are required');
 for(const profile of expected) {
  const matches=receipts.filter(r=>r.profile===profile);
  if(matches.length!==1 || matches[0].provider!==profile || matches[0].status!=='verified' || matches[0].commit!==commit) throw new Error(`missing verified ${profile} receipt for this commit`);
 }
}
if(process.argv[1]?.endsWith('check-live-receipts.mjs')) {
 const [directory,commit]=process.argv.slice(2);
 if(!directory || !commit) throw new Error('usage: check-live-receipts.mjs <directory> <commit>');
 verifyReceipts(['aws-s3','cloudflare-r2','custom'].map(name=>JSON.parse(readFileSync(join(directory,`${name}.json`),'utf8'))),commit);
 console.log('Verified AWS S3, Cloudflare R2, and custom S3 receipts for this commit');
}
