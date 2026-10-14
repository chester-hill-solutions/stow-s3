import test from"node:test";
import assert from"node:assert/strict";
import{ownedOrder,mergeHistograms}from"../owned-load.mjs";
import{Histogram}from"../../packages/stow-s3/scripts/performance/metrics.mjs";
test("three variants occupy each ordinal position twice",()=>{const counts={baseline:[0,0,0],staging:[0,0,0],after:[0,0,0]};for(let round=1;round<=6;round++){const order=ownedOrder(round);assert.equal(new Set(order).size,3);order.forEach((variant,index)=>counts[variant][index]++);}for(const count of Object.values(counts))assert.deepEqual(count,[2,2,2]);assert.throws(()=>ownedOrder(7));});
test("workflow histograms merge observations rather than averaging percentiles",()=>{const first=new Histogram(),second=new Histogram();for(const value of[1,2,3])first.add(value);second.add(100);const raw=h=>({bins:[...h.bins],count:h.count,sum:h.sum,max:h.max});const summary=mergeHistograms([raw(first),raw(second)]);assert.equal(summary.samples,4);assert.equal(summary.mean,26.5);assert.equal(summary.p99,100);assert.ok(summary.p50<3);});
