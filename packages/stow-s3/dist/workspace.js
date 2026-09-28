import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { resolveStowBinary } from "./bin.js";
const execFileAsync = promisify(execFile);
/** Run the native workspace command contract without invoking a shell. */
export async function runWorkspaceCommand(args) {
    const binary = resolveStowBinary();
    const { stdout } = await execFileAsync(binary, ["workspace", ...args], {
        encoding: "utf8",
        maxBuffer: 16 * 1024 * 1024,
    });
    const value = JSON.parse(stdout);
    if (!isWorkspaceJSON(value)) {
        throw new Error("stow-s3 workspace command returned a non-object JSON value");
    }
    return value;
}
export function prepareWorkspace(manifestPath) {
    return runWorkspaceCommand(["prepare", "--manifest", manifestPath]);
}
export function resumeWorkspace(options) {
    const args = ["resume"];
    appendFlag(args, "--id", options.id);
    appendFlag(args, "--handoff", options.handoffPath);
    appendFlag(args, "--registry-dir", options.registryDir);
    return runWorkspaceCommand(args);
}
export function destroyWorkspace(id, registryDir) {
    const args = ["destroy", "--id", id];
    appendFlag(args, "--registry-dir", registryDir);
    return runWorkspaceCommand(args);
}
export function collectWorkspaces(registryDir) {
    const args = ["collect"];
    appendFlag(args, "--registry-dir", registryDir);
    return runWorkspaceCommand(args);
}
export function handoffWorkspace(id, options = {}) {
    const args = ["handoff", "--id", id];
    appendFlag(args, "--registry-dir", options.registryDir);
    appendFlag(args, "--checkpoint-id", options.checkpointId);
    appendFlag(args, "--archive", options.archive);
    appendFlag(args, "--output", options.output);
    return runWorkspaceCommand(args);
}
/**
 * Adopt a portable handoff on the machine that received it.
 *
 * The document and the archive it names are verified before anything is written,
 * so a handoff that arrived over a channel is checked rather than trusted.
 */
export function adoptWorkspaceHandoff(options) {
    const args = ["adopt", "--handoff", options.handoffPath, "--root", options.root];
    appendArchiveOptions(args, options);
    return runWorkspaceCommand(args);
}
/** Write the difference between two checkpoints to a document the other side can apply. */
export function createWorkspaceDelta(options) {
    const args = ["delta", "--from", options.from, "--to", options.to, "--output", options.output];
    appendArchiveOptions(args, options);
    return runWorkspaceCommand(args);
}
/** Bring a base checkpoint to the state a delta describes, publishing a new checkpoint. */
export function applyWorkspaceDelta(options) {
    const args = ["apply", "--delta", options.delta, "--base", options.base];
    appendFlag(args, "--registry-dir", options.registryDir);
    return runWorkspaceCommand(args);
}
export function checkpointWorkspace(options) {
    const args = ["checkpoint", "--id", options.id];
    appendFlag(args, "--registry-dir", options.registryDir);
    appendFlag(args, "--parent", options.parent);
    appendFlag(args, "--max-bytes", options.maxBytes);
    appendFlag(args, "--max-files", options.maxFiles);
    appendBooleanFlag(args, "--include-sensitive", options.includeSensitive);
    return runWorkspaceCommand(args);
}
export function diffWorkspaces(from, to, registryDir) {
    const args = ["diff", "--from", from, "--to", to];
    appendFlag(args, "--registry-dir", registryDir);
    return runWorkspaceCommand(args);
}
export function restoreWorkspaceCheckpoint(checkpointId, root, registryDir) {
    const args = ["restore", "--checkpoint-id", checkpointId, "--root", root];
    appendFlag(args, "--registry-dir", registryDir);
    return runWorkspaceCommand(args);
}
export function exportWorkspaceCheckpoint(checkpointId, output, options = {}) {
    const args = ["export", "--checkpoint-id", checkpointId, "--output", output];
    appendArchiveOptions(args, options);
    return runWorkspaceCommand(args);
}
export function importWorkspaceCheckpoint(archive, options = {}) {
    const args = ["import", "--archive", archive];
    appendArchiveOptions(args, options);
    return runWorkspaceCommand(args);
}
function appendArchiveOptions(args, options) {
    appendFlag(args, "--registry-dir", options.registryDir);
    appendFlag(args, "--max-bytes", options.maxBytes);
    appendFlag(args, "--max-files", options.maxFiles);
    appendBooleanFlag(args, "--include-sensitive", options.includeSensitive);
}
function appendFlag(args, name, value) {
    if (value !== undefined) {
        args.push(name, String(value));
    }
}
function appendBooleanFlag(args, name, value) {
    if (value) {
        args.push(name);
    }
}
function isWorkspaceJSON(value) {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}
//# sourceMappingURL=workspace.js.map