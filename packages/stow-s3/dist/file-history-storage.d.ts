import type { BrowserObjectMutation } from "./browser-types.js";
import type { BrowserEmbeddedStow } from "./persistent-embedded.js";
import { type FileHistoryRecord, type FileSaveOptions, type FileVersion } from "./file-history-types.js";
export declare class FileHistoryStorage {
    private readonly store;
    private readonly bucket;
    readonly remoteBucket: string;
    private readonly maxVersions;
    constructor(store: BrowserEmbeddedStow, bucket: string, remoteBucket: string, maxVersions: number);
    initialize(): Promise<void>;
    load(key: string): Promise<FileHistoryRecord | null>;
    bytes(record: FileHistoryRecord, version?: FileVersion): Promise<Uint8Array>;
    write(record: FileHistoryRecord, additional?: BrowserObjectMutation[]): Promise<FileHistoryRecord>;
    append(key: string, previous: FileHistoryRecord | null, data: Uint8Array, options: FileSaveOptions, remoteETag?: string): Promise<FileHistoryRecord>;
    acknowledge(record: FileHistoryRecord, etag: string): Promise<FileHistoryRecord>;
    clearAttempt(record: FileHistoryRecord): Promise<FileHistoryRecord>;
    private obsoleteAttempt;
    private removeRevision;
    private recordKey;
}
//# sourceMappingURL=file-history-storage.d.ts.map