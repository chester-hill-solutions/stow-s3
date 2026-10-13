export { decodeHead, decodeSize, decodeUsage, recordBytes, writeUsage } from "./content-gc-format.js";
import type { ContentUsage } from "./content-types.js";
export declare function collectContent(transaction: IDBTransaction, namespace: string, retained: string[]): Promise<ContentUsage>;
//# sourceMappingURL=content-gc.d.ts.map