import type { AsyncWasmBridge } from "./wasm-bridge.js";
import { WorkerEmbeddedHost, type WorkerHostOptions } from "./worker-host.js";
export interface BrowserWasmOptions {
    wasmUrl?: string | URL;
    runtimeUrl?: string | URL;
}
export interface BrowserWasmBridge extends AsyncWasmBridge {
    close(): Promise<void>;
}
/** Loads Go in the current realm; use within a custom worker to supply host storage. */
export declare function loadBrowserWasmBridge(options?: BrowserWasmOptions): Promise<BrowserWasmBridge>;
export interface BrowserWorkerHostOptions extends WorkerHostOptions {
    /** Custom module worker entry may instantiate its own persistence adapter. */
    workerUrl?: string | URL;
}
export declare class BrowserWorkerEmbeddedHost extends WorkerEmbeddedHost {
    private readonly worker;
    private readonly failure;
    constructor(worker: Worker, options?: WorkerHostOptions);
    close(): void;
}
export declare function loadBrowserWasmHost(options?: BrowserWorkerHostOptions): BrowserWorkerEmbeddedHost;
export declare function loadBrowserPersistentWasmHost(options?: BrowserWorkerHostOptions): BrowserWorkerEmbeddedHost;
//# sourceMappingURL=browser-wasm-host.d.ts.map