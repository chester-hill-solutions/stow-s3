export type WorkspaceJSON = Readonly<Record<string, unknown>>;
export interface ResumeWorkspaceOptions {
    readonly id?: string;
    readonly handoffPath?: string;
    readonly registryDir?: string;
}
export interface WorkspaceCheckpointOptions {
    readonly id: string;
    readonly registryDir?: string;
    readonly parent?: string;
    readonly maxBytes?: number;
    readonly maxFiles?: number;
    readonly includeSensitive?: boolean;
}
export interface WorkspaceArchiveOptions {
    readonly registryDir?: string;
    readonly maxBytes?: number;
    readonly maxFiles?: number;
    readonly includeSensitive?: boolean;
}
/** Run the native workspace command contract without invoking a shell. */
export declare function runWorkspaceCommand(args: readonly string[]): Promise<WorkspaceJSON>;
export declare function prepareWorkspace(manifestPath: string): Promise<WorkspaceJSON>;
export declare function resumeWorkspace(options: ResumeWorkspaceOptions): Promise<WorkspaceJSON>;
export declare function handoffWorkspace(id: string, options?: {
    readonly registryDir?: string;
    readonly checkpointId?: string;
    readonly output?: string;
}): Promise<WorkspaceJSON>;
export declare function checkpointWorkspace(options: WorkspaceCheckpointOptions): Promise<WorkspaceJSON>;
export declare function diffWorkspaces(from: string, to: string, registryDir?: string): Promise<WorkspaceJSON>;
export declare function restoreWorkspaceCheckpoint(checkpointId: string, root: string, registryDir?: string): Promise<WorkspaceJSON>;
export declare function exportWorkspaceCheckpoint(checkpointId: string, output: string, options?: WorkspaceArchiveOptions): Promise<WorkspaceJSON>;
export declare function importWorkspaceCheckpoint(archive: string, options?: WorkspaceArchiveOptions): Promise<WorkspaceJSON>;
//# sourceMappingURL=workspace.d.ts.map