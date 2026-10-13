import type { ContentRecord, ContentReference } from "./content-types.js";
import { type ProjectManifest, type ProjectState } from "./project-types.js";
export declare function encodeProject(value: ProjectState | ProjectManifest, maxBytes: number): Uint8Array;
export declare function decodeManifest(record: ContentRecord, projectId: string): ProjectManifest;
export declare function decodeContent(value: unknown): ContentReference;
export declare function decodeState(record: ContentRecord, projectId: string): ProjectState;
export declare function isRecord(value: unknown): value is Record<string, unknown>;
//# sourceMappingURL=project-format.d.ts.map