import { type ContentStore } from "./content-store.js";
import { type EmbeddedBinaryResponse } from "./embedded.js";
import type { HostObjectStoreBridge } from "./wasm-bridge.js";
/** Host persistence beneath the shared Go object runtime; bytes are never replayed at open. */
export declare class ContentObjectHost implements HostObjectStoreBridge {
    readonly store: ContentStore;
    /** Installed by the Go host-backed runtime to evaluate its shared atomic write predicates. */
    checkConditions?: (metadata: string) => string;
    private readonly catalog;
    private readonly puts;
    private readonly reads;
    private closed;
    constructor(store: ContentStore, options?: {
        name?: string;
        maxStreams?: number;
    });
    private readonly maxStreams;
    hasActiveStage(stageId: string): boolean;
    call(metadata: string, data: Uint8Array): Promise<EmbeddedBinaryResponse>;
    private dispatch;
    private catalogOperation;
    private listObjects;
    private startPut;
    private writePut;
    private commitPut;
    private openObject;
    private readObject;
    private closeRead;
    private checkStreamLimit;
    private close;
}
//# sourceMappingURL=content-object-host.d.ts.map