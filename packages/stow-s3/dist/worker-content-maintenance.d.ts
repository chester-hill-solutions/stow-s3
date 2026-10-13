import type { ContentObjectHost } from "./content-object-host.js";
import type { ContentStore } from "./content-store.js";
import { type EmbeddedBinaryResponse } from "./embedded.js";
import type { IndexedDbContentAdapter } from "./indexeddb-content.js";
export interface WorkerContentSession {
    adapter: IndexedDbContentAdapter;
    host: ContentObjectHost;
    store: ContentStore;
}
export declare function isContentMaintenance(op: unknown): boolean;
export declare function maintainWorkerContent(sessions: Map<number, WorkerContentSession>, request: Record<string, unknown>): Promise<EmbeddedBinaryResponse>;
//# sourceMappingURL=worker-content-maintenance.d.ts.map