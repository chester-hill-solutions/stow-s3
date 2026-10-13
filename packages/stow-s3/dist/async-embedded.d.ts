import { type EmbeddedObjectStream, type EmbeddedStreamPutOptions } from "./async-streams.js";
import type { ContentProtection, ContentUsage } from "./content-types.js";
import { type EmbeddedBucket, type EmbeddedCapabilities, type EmbeddedListOptions, type EmbeddedObject, type EmbeddedObjectPage, type EmbeddedPutOptions, type EmbeddedStowOptions, type EmbeddedUsage } from "./embedded.js";
import type { AsyncEmbeddedHost, WorkerCallOptions } from "./worker-host.js";
export interface PersistentAsyncEmbeddedOptions extends EmbeddedStowOptions {
    namespace: string;
    databaseName?: string;
    cacheBytes?: number;
    physicalQuotaBytes?: number;
    maxStreams?: number;
}
export declare class AsyncEmbeddedStow {
    private readonly host;
    readonly handle: number;
    private readonly runtimeCapabilities;
    private closed;
    private constructor();
    static open(host: AsyncEmbeddedHost, options?: EmbeddedStowOptions): Promise<AsyncEmbeddedStow>;
    static openPersistent(host: AsyncEmbeddedHost, options: PersistentAsyncEmbeddedOptions): Promise<AsyncEmbeddedStow>;
    /** Attach the receipt from a host-backed open without opening a second runtime. */
    static attach(host: AsyncEmbeddedHost, handle: number, capabilities: EmbeddedCapabilities): AsyncEmbeddedStow;
    capabilities(): EmbeddedCapabilities;
    collectGarbage(): Promise<ContentUsage>;
    inspectRecovery(): Promise<{
        stages: string[];
        protections: ContentProtection[];
    }>;
    discardAbandonedStage(stageId: string): Promise<void>;
    private maintain;
    usage(options?: WorkerCallOptions): Promise<EmbeddedUsage>;
    createBucket(bucket: string): Promise<void>;
    deleteBucket(bucket: string): Promise<void>;
    deleteObject(bucket: string, key: string): Promise<void>;
    reset(): Promise<void>;
    listBuckets(): Promise<EmbeddedBucket[]>;
    putObject(bucket: string, key: string, data: Uint8Array, options?: EmbeddedPutOptions, call?: WorkerCallOptions): Promise<EmbeddedObject>;
    getObject(bucket: string, key: string, options?: WorkerCallOptions): Promise<EmbeddedObject>;
    getObjectRange(bucket: string, key: string, range: {
        offset: number;
        length: number;
    }, options?: WorkerCallOptions): Promise<EmbeddedObject>;
    putObjectStream(bucket: string, key: string, source: Uint8Array | AsyncIterable<Uint8Array>, options: EmbeddedStreamPutOptions): Promise<EmbeddedObject>;
    getObjectStream(bucket: string, key: string, options?: {
        signal?: AbortSignal;
    }): Promise<EmbeddedObjectStream>;
    private streams;
    headObject(bucket: string, key: string): Promise<EmbeddedObject>;
    listObjects(bucket: string, list?: EmbeddedListOptions): Promise<EmbeddedObjectPage>;
    copyObject(sourceBucket: string, sourceKey: string, destinationBucket: string, destinationKey: string): Promise<EmbeddedObject>;
    close(): Promise<void>;
    private invoke;
    private ensureOpen;
}
//# sourceMappingURL=async-embedded.d.ts.map