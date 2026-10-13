import { enqueueOperation, operationQueues } from "./operation-queue.js";
import { replaceContentRange } from "./project-content.js";
import { ProjectStorage } from "./project-storage.js";
import { syncProject } from "./project-sync.js";
import { ProjectError } from "./project-types.js";
/** Durable project revisions over lazy immutable content. The caller owns rendering and edit semantics. */
export class BrowserProject {
    storage;
    queue;
    closed = false;
    get retentionLimit() { return this.storage.maxVersions; }
    constructor(storage, queue) {
        this.storage = storage;
        this.queue = queue;
    }
    static async open(store, options) {
        const storage = new ProjectStorage(store, options);
        const project = new BrowserProject(storage, operationQueues(store, `project:${options.projectId}`));
        await project.enqueue(() => storage.load());
        return project;
    }
    status() { return this.enqueue(async () => this.storage.status(await this.storage.load())); }
    listFiles() {
        return this.enqueue(async () => {
            const loaded = await this.storage.load();
            return (await this.storage.manifest(this.storage.current(loaded.state))).files;
        });
    }
    commit(changes, options = {}) {
        const captured = changes.map(captureChange);
        const settings = { ...options };
        return this.enqueue(async () => {
            settings.signal?.throwIfAborted();
            const loaded = await this.storage.load();
            if (settings.migrationToken && Object.hasOwn(loaded.state.migrations, settings.migrationToken))
                return this.storage.status(loaded);
            const manifest = await this.storage.manifest(this.storage.current(loaded.state));
            const files = new Map(manifest.files.map((file) => [file.key, file]));
            const stageId = crypto.randomUUID();
            try {
                for (const change of captured)
                    await this.apply(files, change, { stageId, signal: settings.signal });
                const revision = { id: crypto.randomUUID(), createdAt: new Date().toISOString(), label: settings.label };
                const versions = [...loaded.state.versions.slice(0, loaded.state.cursor + 1), revision].slice(-this.storage.maxVersions);
                const migrations = settings.migrationToken ? { ...loaded.state.migrations, [settings.migrationToken]: revision.id } : loaded.state.migrations;
                const state = { ...loaded.state, versions, cursor: versions.length - 1, migrations };
                const record = this.storage.revisionRecord(revision.id, { ...manifest, files: [...files.values()].sort((a, b) => a.key.localeCompare(b.key)) });
                settings.signal?.throwIfAborted();
                return this.storage.status(await this.storage.publish(loaded, state, stageId, [record]));
            }
            catch (error) {
                await this.storage.store.adapter.releaseStage(stageId);
                throw error;
            }
        });
    }
    read(key) {
        return this.enqueue(async () => {
            const file = await this.file(key);
            if (file.content.size > this.storage.maxReadBytes)
                throw new ProjectError("read_too_large", "use stream or readRange for this file");
            return this.storage.store.readRange(file.content);
        });
    }
    readRange(key, start, end) {
        return this.enqueue(async () => {
            if (end - start > this.storage.maxReadBytes)
                throw new ProjectError("read_too_large", "requested range exceeds the materialization limit");
            return this.storage.store.readRange((await this.file(key)).content, start, end);
        });
    }
    async *stream(key, options = {}) {
        const protection = crypto.randomUUID();
        const file = await this.enqueue(async () => {
            const loaded = await this.storage.load();
            const revisionId = this.storage.current(loaded.state);
            if (!revisionId)
                throw new ProjectError("not_found", "project has no files");
            const file = await this.file(key);
            await this.storage.store.adapter.protect(protection, [revisionId], { lifetime: "handle" });
            return file;
        });
        try {
            yield* this.storage.store.stream(file.content, options);
        }
        finally {
            await this.storage.store.adapter.protect(protection, [], { lifetime: "handle" });
        }
    }
    undo() { return this.move(-1); }
    redo() { return this.move(1); }
    checkout(revisionId) {
        return this.enqueue(async () => {
            const loaded = await this.storage.load();
            const cursor = loaded.state.versions.findIndex((version) => version.id === revisionId);
            if (cursor < 0)
                throw new ProjectError("not_found", "project revision is not retained");
            return this.storage.status(await this.storage.publish(loaded, { ...loaded.state, cursor }));
        });
    }
    migrationProgress(token) {
        return this.enqueue(async () => {
            const migrations = (await this.storage.load()).state.migrations;
            return Object.hasOwn(migrations, token) ? migrations[token] : undefined;
        });
    }
    sync() {
        if (this.closed)
            return Promise.reject(new ProjectError("closed", "project is closed"));
        const { remote, remoteBucket } = this.storage.options;
        if (!remote || !remoteBucket)
            return Promise.reject(new ProjectError("remote_unavailable", "project remote and bucket are required"));
        return enqueueOperation(this.queue, "syncTail", () => syncProject(this.storage, remote, remoteBucket, (operation) => this.schedule(operation)));
    }
    async close() { this.closed = true; await Promise.all([this.queue.tail, this.queue.syncTail]); }
    move(direction) {
        return this.enqueue(async () => {
            const loaded = await this.storage.load();
            const cursor = loaded.state.cursor + direction;
            if (cursor < 0 || cursor >= loaded.state.versions.length)
                throw new ProjectError("history_boundary", "no further retained project revision");
            await this.storage.manifest(loaded.state.versions[cursor]?.id ?? null);
            return this.storage.status(await this.storage.publish(loaded, { ...loaded.state, cursor }));
        });
    }
    async file(key) {
        const loaded = await this.storage.load();
        const manifest = await this.storage.manifest(this.storage.current(loaded.state));
        const file = manifest.files.find((file) => file.key === key);
        if (!file)
            throw new ProjectError("not_found", `project file ${key} is missing`);
        return file;
    }
    async apply(files, change, options) {
        if (!change.key)
            throw new ProjectError("invalid_options", "file key is required");
        if (change.type === "delete") {
            files.delete(change.key);
            return;
        }
        if (change.type === "put") {
            const { content } = await this.storage.store.stage(change.data, options);
            files.set(change.key, { key: change.key, content, contentType: change.contentType, metadata: change.metadata });
            return;
        }
        const file = files.get(change.key);
        if (!file)
            throw new ProjectError("not_found", `project file ${change.key} is missing`);
        const content = await replaceContentRange(this.storage.store, file.content, { ...options, offset: change.offset, data: change.data });
        files.set(change.key, { ...file, content });
    }
    enqueue(operation) {
        if (this.closed)
            return Promise.reject(new ProjectError("closed", "project is closed"));
        return this.schedule(operation);
    }
    schedule(operation) {
        return enqueueOperation(this.queue, "tail", operation);
    }
}
function captureChange(change) {
    if (change.type === "delete")
        return { ...change };
    if (change.type === "replaceRange")
        return { ...change, data: new Uint8Array(change.data) };
    return { ...change, data: change.data instanceof Uint8Array ? new Uint8Array(change.data) : change.data, metadata: change.metadata ? { ...change.metadata } : undefined };
}
//# sourceMappingURL=project.js.map