import { type EmbeddedBinaryResponse } from "./embedded.js";
import { type WorkerEndpoint } from "./worker-protocol.js";
export interface WorkerCallOptions {
    signal?: AbortSignal;
    /** Detach the entire input ArrayBuffer. Defaults to copying; views must span it. */
    transfer?: boolean;
}
export interface WorkerHostOptions {
    maxInFlight?: number;
    maxInFlightBytes?: number;
    maxResponseBytes?: number;
}
export interface AsyncEmbeddedHost {
    callBinary(metadata: string, data?: Uint8Array, options?: WorkerCallOptions): Promise<EmbeddedBinaryResponse>;
}
/** Bounds admitted calls instead of retaining an unbounded queue of copied payloads. */
export declare class WorkerEmbeddedHost implements AsyncEmbeddedHost {
    private readonly endpoint;
    private readonly pending;
    private nextId;
    private pendingBytes;
    private controls;
    private controlBytes;
    private activeControl;
    private readonly controlQueue;
    private closed;
    private readonly maxInFlight;
    private readonly maxInFlightBytes;
    private readonly maxResponseBytes;
    constructor(endpoint: WorkerEndpoint, options?: WorkerHostOptions);
    callBinary(metadata: string, data?: Uint8Array, options?: WorkerCallOptions): Promise<EmbeddedBinaryResponse>;
    close(): void;
    private readonly receive;
    private release;
    private dispatchControl;
}
//# sourceMappingURL=worker-host.d.ts.map