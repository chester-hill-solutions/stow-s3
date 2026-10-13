import type { ContentUsage } from "./content-types.js";
export declare function bookkeepingSize(key: string, value: unknown): number;
export declare function putPins(transaction: IDBTransaction, namespace: string, key: string, ids: string[], usage: ContentUsage): Promise<void>;
export declare function deleteStagePins(transaction: IDBTransaction, namespace: string, stageId: string, usage: ContentUsage): Promise<void>;
export declare function deleteHandlePins(transaction: IDBTransaction, namespace: string, usage: ContentUsage): Promise<void>;
//# sourceMappingURL=content-accounting.d.ts.map