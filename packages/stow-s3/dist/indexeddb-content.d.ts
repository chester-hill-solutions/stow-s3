import type { ContentAdapter, ContentHead, ContentPage, ContentProtection, ContentPublication, ContentRecord, ContentUsage, IndexedDbContentOptions } from "./content-types.js";
export declare class IndexedDbContentAdapter implements ContentAdapter {
    private readonly options;
    private database?;
    private namespace?;
    private release?;
    private closed;
    private readonly quota;
    constructor(options?: IndexedDbContentOptions);
    open(namespace: string): Promise<void>;
    getHead(name: string): Promise<ContentHead>;
    getRecord(id: string): Promise<ContentRecord | null>;
    listRecords(options?: {
        after?: string;
        limit?: number;
    }): Promise<ContentPage>;
    getChunk(hash: string): Promise<Uint8Array | null>;
    stageChunk(stageId: string, hash: string, bytes: Uint8Array): Promise<void>;
    listStages(): Promise<string[]>;
    releaseStage(stageId: string): Promise<void>;
    publish(publication: ContentPublication): Promise<ContentHead>;
    listProtections(): Promise<ContentProtection[]>;
    protect(name: string, recordIds: string[], options?: {
        lifetime?: "handle" | "durable";
    }): Promise<void>;
    collect(options?: {
        retainRecordIds?: string[];
    }): Promise<ContentUsage>;
    usage(): Promise<ContentUsage>;
    close(): Promise<void>;
    private run;
    private checkQuota;
}
//# sourceMappingURL=indexeddb-content.d.ts.map