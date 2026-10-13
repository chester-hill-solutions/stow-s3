import { type ContentStore } from "./content-store.js";
import { type BrowserProjectOptions, type ProjectChange, type ProjectCommitOptions, type ProjectFile, type ProjectStatus } from "./project-types.js";
/** Durable project revisions over lazy immutable content. The caller owns rendering and edit semantics. */
export declare class BrowserProject {
    private readonly storage;
    private readonly queue;
    private closed;
    get retentionLimit(): number;
    private constructor();
    static open(store: ContentStore, options: BrowserProjectOptions): Promise<BrowserProject>;
    status(): Promise<ProjectStatus>;
    listFiles(): Promise<ProjectFile[]>;
    commit(changes: ProjectChange[], options?: ProjectCommitOptions): Promise<ProjectStatus>;
    read(key: string): Promise<Uint8Array>;
    readRange(key: string, start: number, end: number): Promise<Uint8Array>;
    stream(key: string, options?: {
        signal?: AbortSignal;
    }): AsyncIterable<Uint8Array>;
    undo(): Promise<ProjectStatus>;
    redo(): Promise<ProjectStatus>;
    checkout(revisionId: string): Promise<ProjectStatus>;
    migrationProgress(token: string): Promise<string | undefined>;
    sync(): Promise<ProjectStatus>;
    close(): Promise<void>;
    private move;
    private file;
    private apply;
    private enqueue;
    private schedule;
}
//# sourceMappingURL=project.d.ts.map