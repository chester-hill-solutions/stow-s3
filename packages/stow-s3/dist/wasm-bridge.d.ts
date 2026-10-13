import type { EmbeddedBinaryResponse, EmbeddedHost } from "./embedded.js";
export interface HostObjectStoreBridge {
    call(metadata: string, data: Uint8Array): Promise<EmbeddedBinaryResponse>;
}
export interface AsyncWasmBridge extends EmbeddedHost {
    callBinaryAsync(metadata: string, data?: Uint8Array): Promise<EmbeddedBinaryResponse>;
    openHost(metadata: string, store: HostObjectStoreBridge): Promise<EmbeddedBinaryResponse>;
    exit(): void;
}
//# sourceMappingURL=wasm-bridge.d.ts.map