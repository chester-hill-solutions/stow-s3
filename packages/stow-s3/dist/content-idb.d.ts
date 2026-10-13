import { BrowserPersistenceError } from "./browser-types.js";
import type { ContentRecord } from "./content-types.js";
export declare const CONTENT_STORES: string[];
export declare function contentDatabase(factory: IDBFactory, name: string): Promise<IDBDatabase>;
export declare function requestValue<T>(request: IDBRequest<T>): Promise<T>;
export declare function contentTransaction<T>(database: IDBDatabase, mode: IDBTransactionMode, run: (transaction: IDBTransaction) => Promise<T>): Promise<T>;
export declare function namespaceRange(namespace: string, after?: string): IDBKeyRange;
export declare function rows<T>(store: IDBObjectStore, range: IDBKeyRange, decode: (value: unknown) => T, limit?: number): Promise<{
    key: string;
    value: T;
}[]>;
export declare function decodeRecord(value: unknown): ContentRecord;
export declare function decodeStrings(value: unknown): string[];
export declare function decodeBytes(value: unknown): Uint8Array;
export declare function isObject(value: unknown): value is Record<string, unknown>;
export declare function corrupt(message: string): BrowserPersistenceError;
export declare function mapContentError(error: unknown): BrowserPersistenceError;
//# sourceMappingURL=content-idb.d.ts.map