import test from"node:test";
import assert from"node:assert/strict";
import{pairedEffect}from"../analyze-owned.mjs";
test("paired effect reports a constant ratio without spurious variance",()=>{const effect=pairedEffect(Array.from({length:6},()=>[100,150]));assert.equal(effect.geometricMeanPercentChange,50);assert.deepEqual(effect.conditional95PercentInterval,[50,50]);assert.throws(()=>pairedEffect([[1,2]]));assert.throws(()=>pairedEffect(Array.from({length:6},()=>[0,1])));});
