export declare const EMBEDDED_PROTOCOL_VERSION = 1;
export interface EmbeddedHost {
    call(request: string): string;
}
export interface EmbeddedStowOptions {
    maxBytes?: number;
    maxObjects?: number;
}
export interface EmbeddedCapabilities {
    backend: "memory" | "indexeddb";
    maxBytes: number;
    maxObjects: number;
    /**
     * Whether objects outlive this instance. This is a property of the backend and
     * the host, not a setting: it is `false` on a memory backend because the host
     * has nowhere to persist, which is the same `false` a caller would get from
     * forgetting to ask.
     *
     * Branch on `backend` to tell the cases apart. `"indexeddb"` means a durable
     * store with generation-checked commits; `"memory"` means everything is gone when
     * the instance closes.
     *
     * It is also `false` on a host that cannot persist at all — a Worker or any
     * other isolate, where the object store is not the same thing as a workspace
     * and a durable workspace is not available. See "Where each surface can run" in
     * the README.
     */
    persistent: boolean;
    multipart: boolean;
    upstream: boolean;
}
export interface EmbeddedUsage {
    bytes: number;
    objects: number;
}
export interface EmbeddedBucket {
    name: string;
    creationDate?: string;
}
export interface EmbeddedObject {
    bucket: string;
    key: string;
    data?: Uint8Array;
    size: number;
    etag: string;
    contentType?: string;
    metadata?: Record<string, string>;
    lastModified?: string;
}
export interface EmbeddedPutOptions {
    contentType?: string;
    metadata?: Record<string, string>;
}
export interface EmbeddedListOptions {
    prefix?: string;
    cursor?: string;
    limit?: number;
}
export interface EmbeddedObjectPage {
    objects: EmbeddedObject[];
    truncated: boolean;
    nextCursor?: string;
}
export declare class EmbeddedStowError extends Error {
    readonly code: string;
    constructor(code: string, message: string);
}
export declare class EmbeddedStow {
    private readonly host;
    private readonly runtimeHandle;
    private readonly runtimeCapabilities;
    private closed;
    private constructor();
    static open(host: EmbeddedHost, options?: EmbeddedStowOptions): EmbeddedStow;
    get handle(): number;
    capabilities(): EmbeddedCapabilities;
    usage(): EmbeddedUsage;
    createBucket(bucket: string): void;
    deleteBucket(bucket: string): void;
    listBuckets(): EmbeddedBucket[];
    putObject(bucket: string, key: string, data: Uint8Array, options?: EmbeddedPutOptions): EmbeddedObject;
    getObject(bucket: string, key: string): EmbeddedObject;
    headObject(bucket: string, key: string): EmbeddedObject;
    listObjects(bucket: string, options?: EmbeddedListOptions): EmbeddedObjectPage;
    deleteObject(bucket: string, key: string): void;
    copyObject(sourceBucket: string, sourceKey: string, destinationBucket: string, destinationKey: string): EmbeddedObject;
    reset(): void;
    close(): void;
    private invoke;
    private ensureOpen;
}
//# sourceMappingURL=embedded.d.ts.map