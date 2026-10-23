import type { EmbeddedSaveObservation, EmbeddedSaveOptions, EmbeddedSaveResult } from "./embedded-saves.js";
export declare const EMBEDDED_PROTOCOL_VERSION = 1;
export declare const EMBEDDED_BINARY_PROTOCOL_VERSION = 2;
export interface EmbeddedBinaryResponse {
    metadata: string;
    data?: Uint8Array;
}
export interface EmbeddedHost {
    call(request: string): string;
    /** Binary calls preserve caller ownership; returned bytes belong to the caller. */
    callBinary?(metadata: string, data?: Uint8Array): EmbeddedBinaryResponse;
}
export interface EmbeddedStowOptions {
    maxBytes?: number;
    maxObjects?: number;
}
export interface EmbeddedCapabilities {
    backend: "memory" | "indexeddb" | "host";
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
    /** Absent on older bridges, which cannot safely accept write conditions. */
    conditionalWrites?: boolean;
    /**
     * Whether this runtime can observe an object and publish against that
     * observation, so a save can be retried without writing twice.
     *
     * Absent on older bridges, and `false` on a host whose store cannot compare at
     * publication. Treat absent as `false`: it is the same answer for the caller,
     * which then reads before writing and reconciles by key instead.
     */
    guardedSaves?: boolean;
    /** Whether a save request key is retained, so a lost reply can be resolved. */
    durableSaveRequests?: boolean;
    /** Whether an observed object's bytes can be protected from mutation until release. */
    recoveryHolds?: boolean;
    /** Caller-owned maintenance operations offered by the ready-made persistent worker. */
    hostMaintenance?: boolean;
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
    /** Save only while the object's content ETag matches; empty means unset. */
    ifMatch?: string;
    /** Use "*" to create only when absent; empty means unset. */
    ifNoneMatch?: string;
}
export interface EmbeddedListOptions {
    prefix?: string;
    /** Groups keys that share it into `commonPrefixes`; empty lists every key. */
    delimiter?: string;
    cursor?: string;
    limit?: number;
}
export interface EmbeddedObjectPage {
    objects: EmbeddedObject[];
    truncated: boolean;
    nextCursor?: string;
    /** The delimiter groups this page collapsed, in row order. */
    commonPrefixes?: string[];
}
export interface BridgeObject extends Omit<EmbeddedObject, "data"> {
    data?: string | Uint8Array;
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
    /**
     * Observe an object, or its absence, before changing it.
     *
     * The bytes come back with the observation, so a caller cannot read one version
     * and save against another.
     */
    readForSave(bucket: string, key: string): EmbeddedSaveObservation;
    /**
     * Publish a save that compares at publication, or replaces deliberately.
     *
     * A replacement is still a save with a request key: consent to replace is not
     * consent to write twice.
     */
    saveObject(bucket: string, key: string, data: Uint8Array, options?: EmbeddedSaveOptions): EmbeddedSaveResult;
    /** Ask what a request key did, instead of writing again. */
    resolveSave(bucket: string, key: string, requestKey: string): EmbeddedSaveResult;
    /** Drop a token this caller will not use, rather than waiting for its expiry. */
    releaseObservation(observationToken: string): void;
    private requireGuardedSaves;
    copyObject(sourceBucket: string, sourceKey: string, destinationBucket: string, destinationKey: string): EmbeddedObject;
    reset(): void;
    close(): void;
    private invoke;
    private ensureOpen;
}
export declare function decodeEmbeddedResponse<T>(parsed: unknown, version: number, payload?: Uint8Array): T;
export declare function fromBridgeObject(object: BridgeObject): EmbeddedObject;
//# sourceMappingURL=embedded.d.ts.map