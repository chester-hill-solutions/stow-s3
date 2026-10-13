import type { FileHistoryStorage } from "./file-history-storage.js";
import { type FileHistoryRecord, type ObjectSyncRemote } from "./file-history-types.js";
type Exclusive = <T>(operation: () => Promise<T>) => Promise<T>;
/** Network requests hold a retained revision, rather than the local editing queue. */
export declare function syncFile(storage: FileHistoryStorage, key: string, remote: ObjectSyncRemote, exclusive: Exclusive): Promise<FileHistoryRecord>;
export {};
//# sourceMappingURL=file-history-sync.d.ts.map