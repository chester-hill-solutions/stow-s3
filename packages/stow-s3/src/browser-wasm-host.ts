import type { AsyncWasmBridge } from "./wasm-bridge.js";
import { WorkerEmbeddedHost, type WorkerHostOptions } from "./worker-host.js";

export interface BrowserWasmOptions {
  wasmUrl?: string | URL;
  runtimeUrl?: string | URL;
}

interface GoRuntime {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
  _scheduledTimeouts: Map<number, ReturnType<typeof setTimeout>>;
}

interface BrowserWasmGlobal {
  Go?: new () => GoRuntime;
  stow?: AsyncWasmBridge;
}

export interface BrowserWasmBridge extends AsyncWasmBridge {
  close(): Promise<void>;
}

/** Loads Go in the current realm; use within a custom worker to supply host storage. */
export async function loadBrowserWasmBridge(options: BrowserWasmOptions = {}): Promise<BrowserWasmBridge> {
  const runtimeGlobal = globalThis as typeof globalThis & BrowserWasmGlobal;
  await import(String(options.runtimeUrl ?? new URL("./wasm_exec.js", import.meta.url)));
  const Go = runtimeGlobal.Go;
  if (Go === undefined) throw new Error("Go WASM runtime support is unavailable");
  const response = await fetch(options.wasmUrl ?? new URL("./stow-runtime.wasm", import.meta.url));
  if (!response.ok) throw new Error(`WASM fetch failed with status ${response.status}`);
  const go = new Go();
  const { instance } = await WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
  Reflect.deleteProperty(runtimeGlobal, "stow");
  const running = go.run(instance);
  const bridge = runtimeGlobal.stow;
  if (bridge === undefined) throw new Error("Stow WASM runtime did not initialize");
  let closed = false;
  return {
    call: bridge.call.bind(bridge),
    callBinary: bridge.callBinary?.bind(bridge),
    callBinaryAsync: (metadata, data = new Uint8Array()) => bridge.callBinaryAsync(metadata, data),
    openHost: bridge.openHost.bind(bridge),
    exit: bridge.exit.bind(bridge),
    close: async () => {
      if (closed) return;
      closed = true;
      bridge.exit();
      await running;
      for (const timeout of go._scheduledTimeouts.values()) clearTimeout(timeout);
      go._scheduledTimeouts.clear();
    },
  };
}

export interface BrowserWorkerHostOptions extends WorkerHostOptions {
  /** Custom module worker entry may instantiate its own persistence adapter. */
  workerUrl?: string | URL;
}

export class BrowserWorkerEmbeddedHost extends WorkerEmbeddedHost {
  private readonly failure = () => this.close();

  constructor(private readonly worker: Worker, options: WorkerHostOptions = {}) {
    super(worker, options);
    worker.addEventListener("error", this.failure);
    worker.addEventListener("messageerror", this.failure);
  }

  override close(): void {
    super.close();
    this.worker.removeEventListener("error", this.failure);
    this.worker.removeEventListener("messageerror", this.failure);
    this.worker.terminate();
  }
}

export function loadBrowserWasmHost(options: BrowserWorkerHostOptions = {}): BrowserWorkerEmbeddedHost {
  const worker = new Worker(options.workerUrl ?? new URL("./browser-wasm-worker.js", import.meta.url), { type: "module" });
  return new BrowserWorkerEmbeddedHost(worker, options);
}

export function loadBrowserPersistentWasmHost(options: BrowserWorkerHostOptions = {}): BrowserWorkerEmbeddedHost {
  return loadBrowserWasmHost({ ...options, workerUrl: options.workerUrl ?? new URL("./browser-content-worker.js", import.meta.url) });
}
