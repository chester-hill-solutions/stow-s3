import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";

assert.equal(process.cwd(), "/src");
assert.equal(process.env.BENCH_CLOUD_VERIFY, "1");
vm.runInThisContext(await readFile("/usr/local/go/lib/wasm/wasm_exec.js", "utf8"));
const start = performance.now();
const bytes = await readFile(process.argv[2]);
const go = new Go();
const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
void go.run(instance);
assert.ok(globalThis.stow);
const readyMs = performance.now() - start;
const call = (request, data = new Uint8Array()) => {
  const result = globalThis.stow.callBinary(JSON.stringify({ version: 2, ...request }), data);
  const metadata = JSON.parse(result.metadata);
  assert.equal(metadata.ok, true, JSON.stringify(metadata.error));
  return { ...metadata.result, data: result.data };
};
const { handle } = call({ op: "open" });
call({ op: "createBucket", handle, bucket: "startup" });
call({ op: "putObject", handle, bucket: "startup", key: "first" }, new Uint8Array(4096));
const firstWriteMs = performance.now() - start;
const payload = new Uint8Array(1 << 20).fill(37);
const operation = () => {
  call({ op: "putObject", handle, bucket: "startup", key: "payload" }, payload);
  const result = call({ op: "getObject", handle, bucket: "startup", key: "payload" });
  assert.equal(result.size, payload.length);
  assert.equal(Buffer.compare(Buffer.from(result.data), Buffer.from(payload)), 0);
};
for (let index = 0; index < 5; index++) operation();
const steadyStart = performance.now();
for (let index = 0; index < 40; index++) operation();
const steadyMs = performance.now() - steadyStart;
call({ op: "close", handle });
console.log(JSON.stringify({ readyMs, firstWriteMs, putGetPairsPerSecond: 40000 / steadyMs, pairs: 40, payloadBytes: payload.length }));
process.exit(0);
