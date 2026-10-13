import { type EmbeddedObject, type EmbeddedPutOptions } from "./embedded.js";
import type { WorkerCallOptions } from "./worker-host.js";
export declare const EMBEDDED_STREAM_CHUNK_BYTES: number;
export interface EmbeddedStreamPutOptions extends EmbeddedPutOptions {
    size: number;
    signal?: AbortSignal;
}
export interface EmbeddedObjectStream {
    object: EmbeddedObject;
    chunks: AsyncIterable<Uint8Array>;
    close(): Promise<void>;
}
export interface AsyncOperationInvoker {
    <T>(request: Record<string, unknown>, data?: Uint8Array, options?: WorkerCallOptions): Promise<T>;
}
export declare class EmbeddedStreamOperations {
    private readonly invoke;
    constructor(invoke: AsyncOperationInvoker);
    put(bucket: string, key: string, source: Uint8Array | AsyncIterable<Uint8Array>, options: EmbeddedStreamPutOptions): Promise<EmbeddedObject>;
    openRead(bucket: string, key: string, options?: {
        signal?: AbortSignal;
    }): Promise<EmbeddedObjectStream>;
    private cleanup;
}
//# sourceMappingURL=async-streams.d.ts.map