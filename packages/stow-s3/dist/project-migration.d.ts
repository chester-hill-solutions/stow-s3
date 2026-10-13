import type { BrowserFileHistory } from "./file-history.js";
import type { BrowserProject } from "./project.js";
import { type ProjectStatus } from "./project-types.js";
/** Copies a legacy file's retained stages without deleting or advancing its source. */
export declare function migrateFileHistory(source: Pick<BrowserFileHistory, "exportRetained">, key: string, target: BrowserProject): Promise<ProjectStatus>;
//# sourceMappingURL=project-migration.d.ts.map