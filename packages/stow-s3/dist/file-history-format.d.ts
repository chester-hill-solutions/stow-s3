import { type FileHistoryRecord, type FileHistoryStatus, type FileVersion } from "./file-history-types.js";
export declare function filePrefix(bucket: string, key: string): string;
export declare function revisionKey(record: Pick<FileHistoryRecord, "remoteBucket" | "key">, id: string): string;
export declare function currentVersion(record: FileHistoryRecord): FileVersion;
export declare function fileStatus(record: FileHistoryRecord): FileHistoryStatus;
export declare function decodeHistory(data: Uint8Array, bucket: string, key: string): FileHistoryRecord;
//# sourceMappingURL=file-history-format.d.ts.map