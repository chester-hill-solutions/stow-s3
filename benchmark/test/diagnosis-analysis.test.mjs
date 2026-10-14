import test from "node:test";
import assert from "node:assert/strict";
import { summarizeCpuProfile } from "../analyze-diagnosis.mjs";
test("CPU profile analysis attributes self time and deduplicates recursive ancestors", () => {
  const frame = functionName => ({ functionName, url: "fixture.js", lineNumber: 0 });
  const result = summarizeCpuProfile({ nodes: [{id:1,callFrame:frame("root"),children:[2]},
    {id:2,callFrame:frame("hash"),children:[3]},{id:3,callFrame:frame("hash")}],samples:[2,3],timeDeltas:[1000,2000] });
  assert.equal(result.sampledSeconds,0.003);
  assert.equal(result.flat.find(row=>row.frame.startsWith("hash")).sampledMs,3);
  assert.equal(result.cumulative.find(row=>row.frame.startsWith("hash")).sampledMs,3);
  assert.equal(result.cumulative.find(row=>row.frame.startsWith("root")).sampledMs,3);
  assert.throws(()=>summarizeCpuProfile({nodes:[],samples:[1],timeDeltas:[]}));
});
