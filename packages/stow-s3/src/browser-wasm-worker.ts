import { loadBrowserWasmBridge } from "./browser-wasm-host.js";
import { serveEmbeddedWorker } from "./worker-server.js";
import type { WorkerEndpoint } from "./worker-protocol.js";

const ready = loadBrowserWasmBridge();
// The first request reports initialization failures through the transport.
void ready.catch(() => undefined);
serveEmbeddedWorker(globalThis as typeof globalThis & WorkerEndpoint, ready.then((bridge) => ({
  callBinary: (metadata: string, data?: Uint8Array) => bridge.callBinaryAsync(metadata, data),
})));
