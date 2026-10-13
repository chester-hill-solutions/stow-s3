import type { EmbeddedBinaryResponse, EmbeddedHost } from "./embedded.js";
import type { HostObjectStoreBridge } from "./wasm-bridge.js";
export interface NodeWasmHost extends EmbeddedHost {
    callBinaryAsync(metadata: string, data?: Uint8Array): Promise<EmbeddedBinaryResponse>;
    openHost(metadata: string, store: HostObjectStoreBridge): Promise<EmbeddedBinaryResponse>;
    close(): Promise<void>;
}
export declare function loadNodeWasmHost(): Promise<NodeWasmHost>;
//# sourceMappingURL=node-wasm-host.d.ts.map