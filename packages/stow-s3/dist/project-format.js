import { validateContent } from "./content-store.js";
import { ProjectError } from "./project-types.js";
export function encodeProject(value, maxBytes) {
    const bytes = new TextEncoder().encode(JSON.stringify(value));
    if (bytes.byteLength > maxBytes)
        throw new ProjectError("invalid_options", "project metadata exceeds the configured limit");
    return bytes;
}
function parsed(record, projectId) {
    let value;
    try {
        value = JSON.parse(new TextDecoder().decode(record.metadata));
    }
    catch {
        throw corrupt("project metadata is not JSON");
    }
    if (!isRecord(value) || value.formatVersion !== 1 || value.projectId !== projectId)
        throw corrupt("unsupported or mismatched project metadata");
    return value;
}
export function decodeManifest(record, projectId) {
    const value = parsed(record, projectId);
    if (!Array.isArray(value.files))
        throw corrupt("project files are missing");
    const files = value.files.map(decodeFile);
    if (new Set(files.map((file) => file.key)).size !== files.length)
        throw corrupt("project file names repeat");
    if (JSON.stringify(record.contents) !== JSON.stringify(files.map((file) => file.content)))
        throw corrupt("project content references differ from the manifest");
    return { formatVersion: 1, projectId, files };
}
function decodeFile(value) {
    if (!isRecord(value) || typeof value.key !== "string" || !value.key)
        throw corrupt("invalid project file");
    const content = decodeContent(value.content);
    const result = { key: value.key, content };
    if (value.contentType !== undefined) {
        if (typeof value.contentType !== "string")
            throw corrupt("invalid content type");
        result.contentType = value.contentType;
    }
    if (value.metadata !== undefined)
        result.metadata = decodeStrings(value.metadata);
    return result;
}
export function decodeContent(value) {
    if (!isRecord(value) || typeof value.size !== "number" || !Array.isArray(value.chunks))
        throw corrupt("invalid content reference");
    const chunks = value.chunks.map((chunk) => {
        if (!isRecord(chunk) || typeof chunk.hash !== "string" || typeof chunk.size !== "number")
            throw corrupt("invalid chunk reference");
        return { hash: chunk.hash, size: chunk.size };
    });
    const content = { size: value.size, chunks };
    validateContent(content);
    return content;
}
export function decodeState(record, projectId) {
    const value = parsed(record, projectId);
    if (!Array.isArray(value.versions) || !Number.isSafeInteger(value.cursor) || typeof value.cursor !== "number")
        throw corrupt("invalid project history");
    const versions = value.versions.map(decodeRevision);
    if (new Set(versions.map((version) => version.id)).size !== versions.length || value.cursor < -1 || value.cursor >= versions.length || (versions.length > 0 && value.cursor < 0))
        throw corrupt("invalid history cursor");
    if (value.syncedVersionId !== null && typeof value.syncedVersionId !== "string")
        throw corrupt("invalid synced revision");
    const attempt = decodeAttempt(value.attempt);
    const references = [...new Set([...versions.map((version) => version.id), ...(attempt ? [attempt.revisionId] : [])])].sort();
    if (JSON.stringify([...record.references].sort()) !== JSON.stringify(references))
        throw corrupt("history references differ from retained revisions");
    return {
        formatVersion: 1, projectId, versions, cursor: value.cursor,
        syncedVersionId: value.syncedVersionId, basis: decodeBasis(value.basis), attempt,
        migrations: decodeStrings(value.migrations),
    };
}
function decodeRevision(value) {
    if (!isRecord(value) || typeof value.id !== "string" || !value.id || typeof value.createdAt !== "string")
        throw corrupt("invalid project revision");
    if (value.label !== undefined && typeof value.label !== "string")
        throw corrupt("invalid revision label");
    return { id: value.id, createdAt: value.createdAt, ...(typeof value.label === "string" ? { label: value.label } : {}) };
}
function decodeAttempt(value) {
    if (value === null)
        return null;
    if (!isRecord(value) || typeof value.id !== "string" || !value.id || typeof value.revisionId !== "string")
        throw corrupt("invalid sync attempt");
    return { id: value.id, revisionId: value.revisionId, basis: decodeBasis(value.basis) };
}
function decodeBasis(value) {
    if (isRecord(value) && typeof value.etag === "string" && value.etag)
        return { etag: value.etag };
    if (isRecord(value) && value.absent === true)
        return { absent: true };
    throw corrupt("invalid remote basis");
}
function decodeStrings(value) {
    if (!isRecord(value))
        throw corrupt("invalid metadata map");
    const result = Object.create(null);
    for (const [key, item] of Object.entries(value)) {
        if (typeof item !== "string")
            throw corrupt("invalid metadata value");
        result[key] = item;
    }
    return result;
}
export function isRecord(value) { return typeof value === "object" && value !== null && !Array.isArray(value); }
function corrupt(message) { return new ProjectError("corrupt", message); }
//# sourceMappingURL=project-format.js.map