import { type WorkspaceJSON } from "./workspace.js";
export interface ServeWorkspaceOptions {
    readonly maxRequestBytes?: number;
    maxConcurrentRequests?: number;
    readonly id: string;
    readonly registryDir?: string;
    readonly team?: string;
    readonly maxBytes?: number;
    readonly maxObjects?: number;
    readonly timeoutMs?: number;
    readonly signal?: AbortSignal;
}
export interface ServedWorkspace {
    readonly ready: WorkspaceJSON;
    /** Stop serving and release the workspace claim; preserve all workspace data. */
    close(): Promise<void>;
}
export declare function serveWorkspace(options: ServeWorkspaceOptions): Promise<ServedWorkspace>;
//# sourceMappingURL=workspace-serve.d.ts.map