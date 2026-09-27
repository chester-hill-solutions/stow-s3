// Boots stow's wasm runtime inside a V8 realm shaped like an edge isolate.
//
// An isolate is V8 plus Web APIs: no require, no process, no Buffer, no fs, no
// net, no setImmediate. Node's vm.createContext produces exactly that shape — a
// fresh realm containing only what is put into it. If the runtime needed a Node
// API to boot or to serve an operation, this fails, and that is the finding.
//
// Shared by scripts/wasm-isolate-probe.mjs, which measures boot and the host
// surface, and scripts/wasm-agent-workload.mjs, which measures an agent-shaped
// workload. One implementation of "an isolate" matters here: two harnesses that
// each built their own sandbox would drift, and a measurement from the laxer one
// would be quietly wrong.

import vm from "node:vm";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

export const PROTOCOL_VERSION = 1;

const here = dirname(fileURLToPath(import.meta.url));
export const distDir = join(here, "..", "packages", "stow-s3", "dist");

/** Every import the binary declares, so the host surface is measured not guessed. */
export function importSurface(bytes) {
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
        else if (kind === 1) {
          p++;
          const [f, q] = uleb(bytes, p);
          p = q;
          [, p] = uleb(bytes, p);
          if (f & 1) [, p] = uleb(bytes, p);
        } else if (kind === 2) {
          const [f, q] = uleb(bytes, p);
          p = q;
          [, p] = uleb(bytes, p);
          if (f & 1) [, p] = uleb(bytes, p);
        } else if (kind === 3) p += 2;
        (out[mod] ??= []).push(name);
      }
      return out;
    }
    pos = end;
  }
  return {};
}

/**
 * Boot the runtime and return a handle.
 *
 * `waitForPublish` is how long to wait for the runtime to appear on the global.
 * A caller that measures a workload wants the boot time too, so this is a
 * parameter rather than a fixed internal wait.
 */
export async function bootIsolate({ waitForPublish = 2000 } = {}) {
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
  vm.runInContext(await readFile(join(distDir, "wasm_exec.js"), "utf8"), context, {
    filename: "wasm_exec.js",
  });

  const wasmBytes = await readFile(join(distDir, "stow-runtime.wasm"));
  const surface = importSurface(wasmBytes);

  const go = new context.Go();
  const { instance } = await WebAssembly.instantiate(wasmBytes, go.importObject);

  // main() parks on a channel until exit() is called, so go.run() is started and
  // deliberately not awaited: it settles only after the caller is finished.
  const runSettled = go.run(instance).then((code) => ({ settled: true, code }));
  runSettled.catch(() => ({ settled: true, code: null }));

  const started = performance.now();
  let published = false;
  for (let i = 0; i < waitForPublish; i++) {
    if (typeof sandbox.stow === "object" && sandbox.stow !== null) {
      published = true;
      break;
    }
    await new Promise((r) => setTimeout(r, 5));
  }
  const bootMs = performance.now() - started;

  if (!published) {
    throw new Error("the runtime never published globalThis.stow in an isolate-like realm");
  }

  const call = (payload) =>
    JSON.parse(sandbox.stow.call(JSON.stringify({ version: PROTOCOL_VERSION, ...payload })));

  return {
    call,
    bootMs,
    published,
    surface,
    wasmBytes,
    sandbox,
    memoryBytes: () => instance.exports.mem.buffer.byteLength,
    stowKeys: () => Object.keys(sandbox.stow).sort(),
    exit: () => sandbox.stow.exit(),
    settled: () => runSettled,
  };
}

// The bridge carries object bytes as base64 inside a JSON envelope. An earlier
// probe sent raw bytes and its round-trip assertion passed, because the same
// wrong encoding went in and came out and the stored object was three quarters of
// the size that was sent. These helpers are the encoding, used everywhere.
export const b64 = (bytes) => Buffer.from(bytes).toString("base64");
export const unb64 = (text) => Buffer.from(text, "base64");
