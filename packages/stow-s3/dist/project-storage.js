import { MAX_CONTENT_RECORD_BYTES } from "./content-types.js";
import { decodeManifest, decodeState, encodeProject } from "./project-format.js";
import { ProjectError } from "./project-types.js";
export class ProjectStorage {
    store;
    options;
    headName;
    maxVersions;
    maxReadBytes;
    maxManifestBytes;
    constructor(store, options) {
        this.store = store;
        this.options = options;
        this.headName = `project:${options.projectId}`;
        this.maxVersions = options.maxVersions ?? 50;
        this.maxReadBytes = options.maxReadBytes ?? 16 * 1024 * 1024;
        this.maxManifestBytes = options.maxManifestBytes ?? MAX_CONTENT_RECORD_BYTES;
        if (!options.projectId || !Number.isSafeInteger(this.maxVersions) || this.maxVersions < 2 ||
            !Number.isSafeInteger(this.maxReadBytes) || this.maxReadBytes < 0 ||
            !Number.isSafeInteger(this.maxManifestBytes) || this.maxManifestBytes < 1) {
            throw new ProjectError("invalid_options", "invalid project ID, retention or byte limits");
        }
    }
    async load() {
        const head = await this.store.adapter.getHead(this.headName);
        if (head.recordId === null)
            return { head, state: this.empty() };
        return { head, state: decodeState(await this.requireRecord(head.recordId), this.options.projectId) };
    }
    async requireRecord(id) {
        const record = await this.store.adapter.getRecord(id);
        if (!record)
            throw new ProjectError("corrupt", "retained project record is missing");
        if (record.metadata.byteLength > this.maxManifestBytes)
            throw new ProjectError("corrupt", "project metadata exceeds the configured limit");
        return record;
    }
    async manifest(id) {
        return id === null ? { formatVersion: 1, projectId: this.options.projectId, files: [] }
            : decodeManifest(await this.requireRecord(id), this.options.projectId);
    }
    current(state) { return state.versions[state.cursor]?.id ?? null; }
    status(loaded) {
        const { state, head } = loaded;
        const currentVersionId = this.current(state);
        return {
            projectId: state.projectId, generation: head.generation, versions: structuredClone(state.versions), currentVersionId,
            pending: currentVersionId !== state.syncedVersionId, canUndo: state.cursor > 0,
            canRedo: state.cursor + 1 < state.versions.length, hasUncertainWrite: state.attempt !== null,
        };
    }
    revisionRecord(id, manifest) {
        return { id, metadata: encodeProject(manifest, this.maxManifestBytes), contents: manifest.files.map((file) => file.content), references: [] };
    }
    async publish(loaded, state, stageId = crypto.randomUUID(), records = []) {
        const references = [...new Set([...state.versions.map((version) => version.id), ...(state.attempt ? [state.attempt.revisionId] : [])])];
        const record = { id: crypto.randomUUID(), metadata: encodeProject(state, this.maxManifestBytes), contents: [], references };
        const head = await this.store.adapter.publish({
            head: this.headName, expectedGeneration: loaded.head.generation, record, stageId, additionalRecords: records,
        });
        return { head, state };
    }
    empty() {
        return { formatVersion: 1, projectId: this.options.projectId, versions: [], cursor: -1,
            syncedVersionId: null, basis: { absent: true }, attempt: null, migrations: Object.create(null) };
    }
}
//# sourceMappingURL=project-storage.js.map