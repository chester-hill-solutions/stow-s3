import type { ContentHead, ContentRecord } from "./content-types.js";
import { type ContentStore } from "./content-store.js";
import { type BrowserProjectOptions, type ProjectManifest, type ProjectState, type ProjectStatus } from "./project-types.js";
export interface LoadedProject {
    head: ContentHead;
    state: ProjectState;
}
export declare class ProjectStorage {
    readonly store: ContentStore;
    readonly options: BrowserProjectOptions;
    readonly headName: string;
    readonly maxVersions: number;
    readonly maxReadBytes: number;
    readonly maxManifestBytes: number;
    constructor(store: ContentStore, options: BrowserProjectOptions);
    load(): Promise<LoadedProject>;
    requireRecord(id: string): Promise<ContentRecord>;
    manifest(id: string | null): Promise<ProjectManifest>;
    current(state: ProjectState): string | null;
    status(loaded: LoadedProject): ProjectStatus;
    revisionRecord(id: string, manifest: ProjectManifest): ContentRecord;
    publish(loaded: LoadedProject, state: ProjectState, stageId?: string, records?: ContentRecord[]): Promise<LoadedProject>;
    private empty;
}
//# sourceMappingURL=project-storage.d.ts.map