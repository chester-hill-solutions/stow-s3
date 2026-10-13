import { type ObjectSyncRemote } from "./file-history-types.js";
import { type ProjectStorage } from "./project-storage.js";
import { type ProjectStatus } from "./project-types.js";
type Exclusive = <T>(operation: () => Promise<T>) => Promise<T>;
export declare function syncProject(storage: ProjectStorage, remote: ObjectSyncRemote, bucket: string, exclusive: Exclusive): Promise<ProjectStatus>;
export {};
//# sourceMappingURL=project-sync.d.ts.map