export type IndexedDbAdapterErrorCode = "not_found" | "precondition_failed" | "invalid" | "closed" | "unsupported";
export declare class IndexedDbAdapterError extends Error {
    readonly code: IndexedDbAdapterErrorCode;
    constructor(code: IndexedDbAdapterErrorCode, message: string);
}
export interface IndexedDbAdapterOptions {
    databaseName?: string;
    indexedDB?: IDBFactory;
    now?: () => number;
}
export interface AdapterObjectMetadata {
    key: string;
    size: number;
    etag: string;
    modified: string;
    contentType: string;
    metadata: Record<string, string>;
}
export interface AdapterObject {
    metadata: AdapterObjectMetadata;
    data: Uint8Array;
}
export interface AdapterPutOptions {
    contentType?: string;
    metadata?: Record<string, string>;
}
export interface AdapterCondition {
    ifMatch?: string;
    ifAbsent?: boolean;
}
export interface AdapterConditionalPutOptions extends AdapterPutOptions {
    condition: AdapterCondition;
}
export interface AdapterListOptions {
    prefix?: string;
    delimiter?: string;
    continuationToken?: string;
    startAfter?: string;
    maxKeys?: number;
}
export interface AdapterListResult {
    objects: AdapterObjectMetadata[];
    commonPrefixes: string[];
    isTruncated: boolean;
    nextContinuationToken: string;
}
export declare class IndexedDbAdapter {
    private readonly database;
    private readonly now;
    private closed;
    private constructor();
    static open(options?: IndexedDbAdapterOptions): Promise<IndexedDbAdapter>;
    get(bucket: string, key: string): Promise<AdapterObject>;
    put(bucket: string, key: string, data: Uint8Array, options?: AdapterPutOptions): Promise<AdapterObjectMetadata>;
    conditionalPut(bucket: string, key: string, data: Uint8Array, options: AdapterConditionalPutOptions): Promise<AdapterObjectMetadata>;
    delete(bucket: string, key: string): Promise<void>;
    list(bucket: string, options?: AdapterListOptions): Promise<AdapterListResult>;
    close(): void;
    private read;
    private write;
    private assertOpen;
}
//# sourceMappingURL=indexeddb-adapter.d.ts.map