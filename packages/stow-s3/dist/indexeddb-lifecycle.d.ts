interface DatabaseOpenOptions {
    name: string;
    version: number;
    upgrade(database: IDBDatabase): void;
    failedMessage: string;
    blockedMessage: string;
}
export declare function openIndexedDatabase(factory: IDBFactory, options: DatabaseOpenOptions): Promise<IDBDatabase>;
export {};
//# sourceMappingURL=indexeddb-lifecycle.d.ts.map