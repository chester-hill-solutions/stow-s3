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
export interface WorkspaceDeltaOptions extends WorkspaceArchiveOptions {
    readonly from: string;
    readonly to: string;
    readonly output: string;
}
/**
 * The base a delta applies to, and the document that says what changes.
 *
 * Both are named rather than defaulted: a delta states "this path was A and is
 * now B", so the point it is applied to is part of its meaning and inferring one
 * would let a change be applied to a state it was never measured against.
 */
export interface WorkspaceApplyOptions extends WorkspaceArchiveOptions {
    readonly delta: string;
    readonly base: string;
}
export interface WorkspaceAdoptOptions extends WorkspaceArchiveOptions {
    readonly handoffPath: string;
    readonly root: string;
}
/** Run the native workspace command contract without invoking a shell. */
export declare function decodeWorkspaceJSON(raw: string): WorkspaceJSON;
export declare function runWorkspaceCommand(args: readonly string[]): Promise<WorkspaceJSON>;
export declare function prepareWorkspace(manifestPath: string): Promise<WorkspaceJSON>;
export declare function resumeWorkspace(options: ResumeWorkspaceOptions): Promise<WorkspaceJSON>;
export declare function destroyWorkspace(id: string, registryDir?: string): Promise<WorkspaceJSON>;
export declare function collectWorkspaces(registryDir?: string): Promise<WorkspaceJSON>;
export declare function handoffWorkspace(id: string, options?: {
    readonly registryDir?: string;
    readonly checkpointId?: string;
    /** Write the checkpoint to this path so another machine can adopt the handoff. */
    readonly archive?: string;
    readonly output?: string;
}): Promise<WorkspaceJSON>;
/**
 * Adopt a portable handoff on the machine that received it.
 *
 * The document and the archive it names are verified before anything is written,
 * so a handoff that arrived over a channel is checked rather than trusted.
 */
export declare function adoptWorkspaceHandoff(options: WorkspaceAdoptOptions): Promise<WorkspaceJSON>;
/** Write the difference between two checkpoints to a document the other side can apply. */
export declare function createWorkspaceDelta(options: WorkspaceDeltaOptions): Promise<WorkspaceJSON>;
/** Bring a base checkpoint to the state a delta describes, publishing a new checkpoint. */
export declare function applyWorkspaceDelta(options: WorkspaceApplyOptions): Promise<WorkspaceJSON>;
export declare function checkpointWorkspace(options: WorkspaceCheckpointOptions): Promise<WorkspaceJSON>;
export declare function diffWorkspaces(from: string, to: string, registryDir?: string): Promise<WorkspaceJSON>;
export declare function restoreWorkspaceCheckpoint(checkpointId: string, root: string, registryDir?: string): Promise<WorkspaceJSON>;
export declare function exportWorkspaceCheckpoint(checkpointId: string, output: string, options?: WorkspaceArchiveOptions): Promise<WorkspaceJSON>;
export declare function importWorkspaceCheckpoint(archive: string, options?: WorkspaceArchiveOptions): Promise<WorkspaceJSON>;
//# sourceMappingURL=workspace.d.ts.map