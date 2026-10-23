import type { EmbeddedObject } from "./embedded.js";
/**
 * What a read-for-save observed: the bytes, whether the key was absent, and the
 * token a save compares against.
 *
 * The token is a handle to a condition the runtime keeps, not the condition
 * itself: a condition is bound to one runtime and one exact resource, and an ETag
 * alone would lose the case where a key is written, changed, and changed back.
 * It expires after fifteen minutes and a runtime retains at most 256 of them.
 */
export interface EmbeddedSaveObservation {
    object: EmbeddedObject;
    absent: boolean;
    observationToken: string;
}
/** The outcome of a save or of resolving one. Only `committed` proves the write. */
export interface EmbeddedSaveResult {
    object: EmbeddedObject;
    outcome: "committed" | "not_committed" | "unknown";
    /** True when a retained request answered instead of the write being attempted. */
    replayed: boolean;
}
export interface EmbeddedSaveOptions {
    /** The token from readForSave. Required unless `replace` is true. */
    observationToken?: string;
    /** Publish without comparing, deliberately replacing whatever is stored. */
    replace?: boolean;
    contentType?: string;
    metadata?: Record<string, string>;
    /** Reuse on every retry, so a lost reply can be resolved instead of repeated. */
    requestKey?: string;
}
export interface BridgeObject extends Omit<EmbeddedObject, "data"> {
    data?: string | Uint8Array;
}
export interface BridgeSaveObservation extends Omit<EmbeddedSaveObservation, "object"> {
    object: BridgeObject;
}
export interface BridgeSaveResult extends Omit<EmbeddedSaveResult, "object"> {
    object: BridgeObject;
}
//# sourceMappingURL=embedded-saves.d.ts.map