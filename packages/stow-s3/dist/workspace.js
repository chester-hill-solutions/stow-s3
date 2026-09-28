import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { promisify } from "node:util";
import { resolveStowBinary } from "./bin.js";
const execFileAsync = promisify(execFile);
/** Run the native workspace command contract without invoking a shell. */
export function decodeWorkspaceJSON(raw) {
    const value = JSON.parse(raw);
    if (!isWorkspaceJSON(value)) {
        throw new Error("stow-s3 workspace command returned a non-object JSON value");
    }
    return value;
}
export async function runWorkspaceCommand(args) {
    const binary = resolveStowBinary();
    const { stdout } = await execFileAsync(binary, ["workspace", ...args], {
        encoding: "utf8",
        maxBuffer: 16 * 1024 * 1024,
    });
    return decodeWorkspaceJSON(stdout);
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
/**
 * Enumerate the workspaces this machine knows about, quietest first.
 *
 * Every other verb needs an id, and there was no way to get one short of having
 * kept a note of it. `readable: false` marks an entry whose directory is gone, which
 * is the state a crashed or hand-cleaned run leaves behind and the thing a list is
 * most useful for finding — so every entry is reported, and filtering out the
 * unreadable ones is the caller's one line rather than a flag it has to know about.
 */
export function listWorkspaces(options = {}) {
    const args = ["list"];
    appendFlag(args, "--registry-dir", options.registryDir);
    appendFlag(args, "--team", options.team);
    return runWorkspaceCommand(args);
}
export function collectWorkspaces(registryDir) {
    const args = ["collect"];
    appendFlag(args, "--registry-dir", registryDir);
    return runWorkspaceCommand(args);
}
export async function handoffWorkspace(id, options = {}) {
    const args = ["handoff", "--id", id];
    appendFlag(args, "--registry-dir", options.registryDir);
    appendFlag(args, "--team", options.team);
    appendFlag(args, "--checkpoint-id", options.checkpointId);
    appendFlag(args, "--archive", options.archive);
    appendFlag(args, "--output", options.output);
    if (options.output === undefined) {
        return runWorkspaceCommand(args);
    }
    // `handoff --output` is the whole point of a handoff: the document is the
    // artifact the receiving machine gets, and the command writes it to the named
    // path and prints nothing. There is no JSON on stdout to parse, so the document
    // the caller asked for is the one on disk. Reading it back is what makes this
    // wrapper return the same value whether or not a path was given.
    const binary = resolveStowBinary();
    await execFileAsync(binary, ["workspace", ...args], {
        encoding: "utf8",
        maxBuffer: 16 * 1024 * 1024,
    });
    return decodeWorkspaceJSON(await readFile(options.output, "utf8"));
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
    appendFlag(args, "--expect-sha256", options.expectSHA256);
    appendFlag(args, "--registry-dir", options.registryDir);
    // The CLI takes --team on apply, and a checkpoint filed under a team partition is
    // invisible without it, so omitting this made every apply against a partitioned
    // registry fail to find its own base.
    appendFlag(args, "--team", options.team);
    return runWorkspaceCommand(args);
}
export function checkpointWorkspace(options) {
    const args = ["checkpoint", "--id", options.id];
    appendFlag(args, "--registry-dir", options.registryDir);
    appendFlag(args, "--team", options.team);
    appendFlag(args, "--parent", options.parent);
    appendFlag(args, "--max-bytes", options.maxBytes);
    appendFlag(args, "--max-files", options.maxFiles);
    appendBooleanFlag(args, "--include-sensitive", options.includeSensitive);
    return runWorkspaceCommand(args);
}
export function diffWorkspaces(from, to, registryDir, team) {
    const args = ["diff", "--from", from, "--to", to];
    appendFlag(args, "--registry-dir", registryDir);
    appendFlag(args, "--team", team);
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
    appendFlag(args, "--team", options.team);
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