import { type ContentStore } from "./content-store.js";
import type { ContentHead, ContentRecord, ContentReference } from "./content-types.js";
import type { EmbeddedBucket, EmbeddedObject } from "./embedded.js";
export interface CatalogEntry {
    id: string;
    object: EmbeddedObject;
}
export interface ObjectCatalog {
    formatVersion: 1;
    buckets: EmbeddedBucket[];
    objects: CatalogEntry[];
}
export interface LoadedCatalog {
    head: ContentHead;
    catalog: ObjectCatalog;
}
export declare class ContentObjectCatalog {
    readonly store: ContentStore;
    private readonly headName;
    constructor(store: ContentStore, headName: string);
    load(): Promise<LoadedCatalog>;
    requireBucket(catalog: ObjectCatalog, bucket: string): void;
    entry(catalog: ObjectCatalog, bucket: string, key: string): CatalogEntry;
    content(entry: CatalogEntry): Promise<ContentReference>;
    publish(loaded: LoadedCatalog, catalog: ObjectCatalog, stageId?: string, records?: ContentRecord[]): Promise<void>;
    record(object: EmbeddedObject, content: ContentReference): ContentRecord;
}
export declare function hostContent(value: unknown): ContentReference;
//# sourceMappingURL=content-object-catalog.d.ts.map