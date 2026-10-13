import { readFile } from "node:fs/promises";
import { setTimeout as delay } from "node:timers/promises";
import vm from "node:vm";

import type { EmbeddedBinaryResponse, EmbeddedHost } from "./embedded.js";
import type { HostObjectStoreBridge } from "./wasm-bridge.js";

interface GoRuntime {
  importObject: unknown;
  run(instance: unknown): Promise<unknown>;
  /** Timers owned by the pinned wasm_exec.js runtime can outlive Go's exit. */
  _scheduledTimeouts: Map<number, ReturnType<typeof setTimeout>>;
}

interface GoConstructor {
  new (): GoRuntime;
}

interface WasmBridge {
  call(request: string): string;
  callBinary?(metadata: string, data: Uint8Array): EmbeddedBinaryResponse;
  callBinaryAsync?(metadata: string, data: Uint8Array): Promise<EmbeddedBinaryResponse>;
  openHost?(metadata: string, store: HostObjectStoreBridge): Promise<EmbeddedBinaryResponse>;
  exit(): void;
}

interface WasmGlobal {
  Go?: GoConstructor;
  stow?: WasmBridge;
  WebAssembly?: {
    instantiate(bytes: Uint8Array, imports: unknown): Promise<{ instance: unknown }>;
  };
}

export interface NodeWasmHost extends EmbeddedHost {
  callBinaryAsync(metadata: string, data?: Uint8Array): Promise<EmbeddedBinaryResponse>;
  openHost(metadata: string, store: HostObjectStoreBridge): Promise<EmbeddedBinaryResponse>;
  close(): Promise<void>;
}

export async function loadNodeWasmHost(): Promise<NodeWasmHost> {
  const runtimeGlobal = globalThis as typeof globalThis & WasmGlobal;
  const wasmExec = await readFile(new URL("./wasm_exec.js", import.meta.url), "utf8");
  vm.runInThisContext(wasmExec, { filename: "wasm_exec.js" });
  const GoConstructor = runtimeGlobal.Go;
  if (GoConstructor === undefined) {
    throw new Error("Go WASM runtime support is unavailable");
  }

  const webAssembly = runtimeGlobal.WebAssembly;
  if (webAssembly === undefined) {
    throw new Error("WebAssembly is unavailable in this Node runtime");
  }
  const go = new GoConstructor();
  const wasmBytes = await readFile(new URL("./stow-runtime.wasm", import.meta.url));
  const { instance } = await webAssembly.instantiate(wasmBytes, go.importObject);
  Reflect.deleteProperty(runtimeGlobal, "stow");
  const runPromise = go.run(instance);
  for (let attempt = 0; attempt < 100 && runtimeGlobal.stow === undefined; attempt += 1) {
    await delay(1);
  }
  const bridge = runtimeGlobal.stow;
  if (bridge === undefined) {
    throw new Error("Stow WASM runtime did not initialize");
  }

  const binaryCall = bridge.callBinary?.bind(bridge);
  let closed = false;
  return {
    call: (request) => bridge.call(request),
    callBinary: binaryCall === undefined ? undefined :
      (metadata, data = new Uint8Array()) => binaryCall(metadata, data),
    callBinaryAsync: async (metadata, data = new Uint8Array()) => {
      if (bridge.callBinaryAsync === undefined) throw new Error("host does not support asynchronous binary calls");
      return bridge.callBinaryAsync(metadata, data);
    },
    openHost: async (metadata, store) => {
      if (bridge.openHost === undefined) throw new Error("host does not support host-backed storage");
      return bridge.openHost(metadata, store);
    },
    close: async () => {
      if (closed) {
        return;
      }
      closed = true;
      bridge.exit();
      await runPromise;
      for (const timeout of go._scheduledTimeouts.values()) clearTimeout(timeout);
      go._scheduledTimeouts.clear();
    },
  };
}
