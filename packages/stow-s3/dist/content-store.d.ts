import type { ContentAdapter, ContentReference, ContentStageOptions, ContentStoreOptions, ContentUsage, StagedContent } from "./content-types.js";
export declare function contentHash(bytes: Uint8Array): Promise<string>;
export declare class ContentStore {
    readonly adapter: ContentAdapter;
    private readonly chunkBytes;
    private readonly cacheLimit;
    private readonly cache;
    private cacheBytes;
    constructor(adapter: ContentAdapter, options?: ContentStoreOptions);
    stage(input: Uint8Array | AsyncIterable<Uint8Array>, options?: ContentStageOptions): Promise<StagedContent>;
    stream(content: ContentReference, options?: {
        start?: number;
        end?: number;
        signal?: AbortSignal;
    }): AsyncIterable<Uint8Array>;
    readRange(content: ContentReference, start?: number, end?: number): Promise<Uint8Array>;
    usage(): Promise<ContentUsage>;
    clearCache(): void;
    private loadChunk;
    private remember;
}
export declare function validateContent(content: ContentReference): void;
//# sourceMappingURL=content-store.d.ts.map