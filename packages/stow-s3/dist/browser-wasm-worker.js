import { loadBrowserWasmBridge } from "./browser-wasm-host.js";
import { serveEmbeddedWorker } from "./worker-server.js";
const ready = loadBrowserWasmBridge();
// The first request reports initialization failures through the transport.
void ready.catch(() => undefined);
serveEmbeddedWorker(globalThis, ready.then((bridge) => ({
    callBinary: (metadata, data) => bridge.callBinaryAsync(metadata, data),
})));
//# sourceMappingURL=browser-wasm-worker.js.map