import { type IndexedDbPersistenceAdapterOptions, type PersistenceAdapter, type PersistenceChange, type PersistenceSnapshot } from "./browser-types.js";
export declare class IndexedDbPersistenceAdapter implements PersistenceAdapter {
    private readonly factory;
    private readonly databaseName;
    private readonly manifestStoreName;
    private readonly bucketStoreName;
    private readonly objectStoreName;
    private readonly lockManager;
    private readonly lockName;
    private lifecycle;
    constructor(options?: IndexedDbPersistenceAdapterOptions);
    open(namespace: string): Promise<void>;
    private acquireResources;
    private get stores();
    load(namespace: string): Promise<PersistenceSnapshot | null>;
    commit(namespace: string, expectedGeneration: number, changes: PersistenceChange[]): Promise<number>;
    clear(namespace: string, expectedGeneration: number): Promise<number>;
    close(): Promise<void>;
    private closeInternal;
    private resourcesFor;
}
//# sourceMappingURL=indexeddb-persistence.d.ts.map