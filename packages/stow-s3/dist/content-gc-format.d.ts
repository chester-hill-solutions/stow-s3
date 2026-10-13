import type { ContentHead, ContentRecord, ContentUsage } from "./content-types.js";
export declare function decodeHead(value: unknown): ContentHead;
export declare function decodeSize(value: unknown): number;
export declare function decodeUsage(value: unknown): ContentUsage;
export declare function writeUsage(transaction: IDBTransaction, namespace: string, usage: ContentUsage): void;
export declare function recordBytes(record: ContentRecord): number;
//# sourceMappingURL=content-gc-format.d.ts.map