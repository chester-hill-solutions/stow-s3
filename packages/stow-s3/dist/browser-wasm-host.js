import { WorkerEmbeddedHost } from "./worker-host.js";
/** Loads Go in the current realm; use within a custom worker to supply host storage. */
export async function loadBrowserWasmBridge(options = {}) {
    const runtimeGlobal = globalThis;
    await import(String(options.runtimeUrl ?? new URL("./wasm_exec.js", import.meta.url)));
    const Go = runtimeGlobal.Go;
    if (Go === undefined)
        throw new Error("Go WASM runtime support is unavailable");
    const response = await fetch(options.wasmUrl ?? new URL("./stow-runtime.wasm", import.meta.url));
    if (!response.ok)
        throw new Error(`WASM fetch failed with status ${response.status}`);
    const go = new Go();
    const { instance } = await WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
    Reflect.deleteProperty(runtimeGlobal, "stow");
    const running = go.run(instance);
    const bridge = runtimeGlobal.stow;
    if (bridge === undefined)
        throw new Error("Stow WASM runtime did not initialize");
    let closed = false;
    return {
        call: bridge.call.bind(bridge),
        callBinary: bridge.callBinary?.bind(bridge),
        callBinaryAsync: (metadata, data = new Uint8Array()) => bridge.callBinaryAsync(metadata, data),
        openHost: bridge.openHost.bind(bridge),
        exit: bridge.exit.bind(bridge),
        close: async () => {
            if (closed)
                return;
            closed = true;
            bridge.exit();
            await running;
            for (const timeout of go._scheduledTimeouts.values())
                clearTimeout(timeout);
            go._scheduledTimeouts.clear();
        },
    };
}
export class BrowserWorkerEmbeddedHost extends WorkerEmbeddedHost {
    worker;
    failure = () => this.close();
    constructor(worker, options = {}) {
        super(worker, options);
        this.worker = worker;
        worker.addEventListener("error", this.failure);
        worker.addEventListener("messageerror", this.failure);
    }
    close() {
        super.close();
        this.worker.removeEventListener("error", this.failure);
        this.worker.removeEventListener("messageerror", this.failure);
        this.worker.terminate();
    }
}
export function loadBrowserWasmHost(options = {}) {
    const worker = new Worker(options.workerUrl ?? new URL("./browser-wasm-worker.js", import.meta.url), { type: "module" });
    return new BrowserWorkerEmbeddedHost(worker, options);
}
export function loadBrowserPersistentWasmHost(options = {}) {
    return loadBrowserWasmHost({ ...options, workerUrl: options.workerUrl ?? new URL("./browser-content-worker.js", import.meta.url) });
}
//# sourceMappingURL=browser-wasm-host.js.map