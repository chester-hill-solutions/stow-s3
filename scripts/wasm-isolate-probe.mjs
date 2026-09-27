// Can stow's wasm runtime boot and work inside a V8 isolate with no Node APIs?
//
// This is the proof step for an edge/embedded target. A Worker isolate is V8
// plus Web APIs: no require, no process, no Buffer, no fs, no net, no
// setImmediate. Node's vm.createContext produces exactly that shape — a fresh
// realm containing only what this file puts in it. If stow needs a Node API to
// boot or to serve an operation, this fails, and that is the finding.
//
// It measures three things the "does it work" question hides:
//
//   - the host surface actually required, read out of the binary's own import
//     section rather than assumed;
//   - linear memory after a real workload, which is the number that decides
//     whether this fits a device with hundreds of megabytes or hundreds of
//     kilobytes;
//   - growth per object, so a memory budget can be stated rather than guessed.

import vm from "node:vm";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const dist = join(here, "..", "packages", "stow-s3", "dist");

const PROTOCOL_VERSION = 1;

/** Every import the binary declares, so the host surface is measured not guessed. */
function importSurface(bytes) {
  let pos = 8;
  const uleb = (d, i) => {
    let r = 0;
    let s = 0;
    for (;;) {
      const b = d[i++];
      r |= (b & 0x7f) << s;
      if (!(b & 0x80)) return [r, i];
      s += 7;
    }
  };
  while (pos < bytes.length) {
    const id = bytes[pos++];
    const [size, next] = uleb(bytes, pos);
    const end = next + size;
    if (id === 2) {
      let [count, p] = uleb(bytes, next);
      const out = {};
      for (let i = 0; i < count; i++) {
        let l;
        [l, p] = uleb(bytes, p);
        const mod = bytes.subarray(p, p + l).toString();
        p += l;
        [l, p] = uleb(bytes, p);
        const name = bytes.subarray(p, p + l).toString();
        p += l;
        const kind = bytes[p++];
        if (kind === 0) [, p] = uleb(bytes, p);
        else if (kind === 1) { p++; const [f, q] = uleb(bytes, p); p = q; [, p] = uleb(bytes, p); if (f & 1) [, p] = uleb(bytes, p); }
        else if (kind === 2) { const [f, q] = uleb(bytes, p); p = q; [, p] = uleb(bytes, p); if (f & 1) [, p] = uleb(bytes, p); }
        else if (kind === 3) p += 2;
        (out[mod] ??= []).push(name);
      }
      return out;
    }
    pos = end;
  }
  return {};
}

// Deliberately Web-standard only. If this list is missing something stow needs,
// the boot fails and the failure names it.
const sandbox = {
  crypto: globalThis.crypto,
  performance: globalThis.performance,
  Date,
  TextEncoder,
  TextDecoder,
  setTimeout,
  clearTimeout,
  queueMicrotask,
  console: { log() {}, error() {}, warn() {} },
  WebAssembly,
  // Absent on purpose, as an isolate has none of them: require, process,
  // Buffer, module, __dirname, global, setImmediate, fs, net.
};
sandbox.globalThis = sandbox;
sandbox.self = sandbox;

const context = vm.createContext(sandbox);
vm.runInContext(await readFile(join(dist, "wasm_exec.js"), "utf8"), context, {
  filename: "wasm_exec.js",
});

const wasmBytes = await readFile(join(dist, "stow-runtime.wasm"));
const surface = importSurface(wasmBytes);

const go = new context.Go();
const { instance } = await WebAssembly.instantiate(wasmBytes, go.importObject);

// main() parks on a channel until exit() is called, so go.run() is started and
// deliberately not awaited: it settles only after the workload is done.
const runSettled = go.run(instance).then((code) => ({ settled: true, code }));
runSettled.catch(() => ({ settled: true, code: null }));

const t0 = performance.now();
let published = false;
for (let i = 0; i < 2000; i++) {
  if (typeof sandbox.stow === "object" && sandbox.stow !== null) {
    published = true;
    break;
  }
  await new Promise((r) => setTimeout(r, 5));
}
const bootMs = performance.now() - t0;

const memBytes = () => instance.exports.mem.buffer.byteLength;
const afterBootMiB = +(memBytes() / 1048576).toFixed(2);

const call = (payload) => JSON.parse(sandbox.stow.call(JSON.stringify({ version: PROTOCOL_VERSION, ...payload })));

// The bridge carries object bytes as base64, not as a raw string. An earlier
// version of this probe sent raw bytes, and the round-trip assertion passed
// because the same wrong encoding went in and came out — the stored object was
// three quarters of the size that was sent and nothing noticed. Encoded
// properly, and every operation's result is inspected rather than assumed.
const b64 = (text) => Buffer.from(text, "utf8").toString("base64");
const unb64 = (text) => Buffer.from(text, "base64").toString("utf8");

const results = { booted: published, bootMs: +bootMs.toFixed(1), afterBootMiB, operations: {} };

if (published) {
  // Everything the TypeScript client calls through, so a rename shows up here
  // rather than at a consumer.
  results.stowKeys = Object.keys(sandbox.stow).sort();

  const opened = call({ op: "open", options: { backend: "memory" } });
  results.operations.open = opened.ok ? "ok" : opened.error;
  if (opened.ok) {
    const handle = opened.result.handle;
    results.capabilities = opened.result.capabilities;

    const created = call({ op: "createBucket", handle, bucket: "edge" });
    results.operations.createBucket = created.ok ? "ok" : created.error;

    // Small objects, to separate per-object overhead from payload. Each result
    // is checked: an unchecked loop reports the attempt count, which is not the
    // same as the success count. Measured before the large object so the two
    // costs are not conflated — base64 through a JSON bridge is the expensive
    // part, and a device budget needs them apart.
    const small = "hello";
    let smallOk = 0;
    let firstSmallError = null;
    for (let i = 0; i < 200; i++) {
      const r = call({ op: "putObject", handle, bucket: "edge", key: `small/${i}.txt`, data: b64(small) });
      if (r.ok) smallOk++;
      else if (!firstSmallError) firstSmallError = r.error;
    }
    results.operations.smallPuts = `${smallOk}/200 succeeded`;
    if (firstSmallError) results.operations.firstSmallError = firstSmallError;

    const afterSmallMiB = memBytes() / 1048576;
    results.after200SmallMiB = +afterSmallMiB.toFixed(2);
    results.miBPer200SmallObjects = +(afterSmallMiB - afterBootMiB).toFixed(3);
    results.peakOverheadPerSmallObjectBytes = Math.round(
      ((afterSmallMiB - afterBootMiB) * 1048576) / 200,
    );

    // 1 MiB of real payload. The gap between what was stored and what memory
    // moved is the transport's cost, and it is the number that decides whether
    // this bridge is usable on a device at all.
    const payload = "x".repeat(1024 * 1024);
    const beforeLargeMiB = memBytes() / 1048576;
    const put = call({ op: "putObject", handle, bucket: "edge", key: "big.bin", data: b64(payload), contentType: "application/octet-stream" });
    results.operations.putObject = put.ok ? "ok" : put.error;

    const afterLargeMiB = memBytes() / 1048576;
    results.afterLargeObjectMiB = +afterLargeMiB.toFixed(2);
    results.miBPer1MiBObject = +(afterLargeMiB - beforeLargeMiB).toFixed(2);

    const got = call({ op: "getObject", handle, bucket: "edge", key: "big.bin" });
    results.operations.getObject = got.ok ? "ok" : got.error;
    results.operations.roundTripMatches = got.ok ? unb64(got.result.data) === payload : false;
    results.afterReadMiB = +(memBytes() / 1048576).toFixed(2);

    const usage = call({ op: "usage", handle });
    results.usage = usage.ok ? usage.result : usage.error;
  }
  sandbox.stow.exit();
}

// A timer armed inside the Go runtime can fire after exit and throw from
// wasm_exec's own callback, which no promise handler can catch. Reported as a
// flag rather than allowed to decide the exit code.
let teardownThrew = false;
process.on("uncaughtException", () => {
  teardownThrew = true;
});
process.on("unhandledRejection", () => {});

const settled = await Promise.race([runSettled, new Promise((r) => setTimeout(() => r({ settled: false }), 5000))]);

console.log(
  JSON.stringify(
    {
      ...results,
      hostSurface: Object.fromEntries(
        Object.entries(surface).map(([m, fns]) => [m, fns.length]),
      ),
      hostFunctionCount: Object.values(surface).reduce((n, f) => n + f.length, 0),
      wasmMiB: +(wasmBytes.length / 1048576).toFixed(2),
      nodeApisVisible: ["require", "process", "Buffer", "module", "setImmediate"].filter((k) => k in sandbox),
      goRunSettled: settled,
    },
    null,
    2,
  ),
);

if (!published) {
  console.error("FAIL: the runtime never published globalThis.stow");
  process.exit(1);
}
if (results.operations.roundTripMatches !== true) {
  console.error("FAIL: the round trip did not return identical bytes");
  process.exit(1);
}
