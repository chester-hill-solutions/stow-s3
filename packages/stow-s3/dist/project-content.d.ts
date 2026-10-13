import { type ContentStore } from "./content-store.js";
import type { ContentReference } from "./content-types.js";
/** Fixed-length replacement hashes and stages only the overlapping chunks. */
export declare function replaceContentRange(store: ContentStore, content: ContentReference, replacement: {
    offset: number;
    data: Uint8Array;
    stageId: string;
    signal?: AbortSignal;
}): Promise<ContentReference>;
//# sourceMappingURL=project-content.d.ts.map