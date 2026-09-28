import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { promisify } from "node:util";
import { resolveStowBinary } from "./bin.js";

const execFileAsync = promisify(execFile);

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
export function decodeWorkspaceJSON(raw: string): WorkspaceJSON {
  const value: unknown = JSON.parse(raw);
  if (!isWorkspaceJSON(value)) {
    throw new Error("stow-s3 workspace command returned a non-object JSON value");
  }
  return value;
}

export async function runWorkspaceCommand(
  args: readonly string[],
): Promise<WorkspaceJSON> {
  const binary = resolveStowBinary();
  const { stdout } = await execFileAsync(binary, ["workspace", ...args], {
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
  });
  return decodeWorkspaceJSON(stdout);
}

export function prepareWorkspace(manifestPath: string): Promise<WorkspaceJSON> {
  return runWorkspaceCommand(["prepare", "--manifest", manifestPath]);
}

export function resumeWorkspace(options: ResumeWorkspaceOptions): Promise<WorkspaceJSON> {
  const args = ["resume"];
  appendFlag(args, "--id", options.id);
  appendFlag(args, "--handoff", options.handoffPath);
  appendFlag(args, "--registry-dir", options.registryDir);
  return runWorkspaceCommand(args);
}

export function destroyWorkspace(id: string, registryDir?: string): Promise<WorkspaceJSON> {
  const args = ["destroy", "--id", id];
  appendFlag(args, "--registry-dir", registryDir);
  return runWorkspaceCommand(args);
}

export function collectWorkspaces(registryDir?: string): Promise<WorkspaceJSON> {
  const args = ["collect"];
  appendFlag(args, "--registry-dir", registryDir);
  return runWorkspaceCommand(args);
}

export async function handoffWorkspace(
  id: string,
  options: {
    readonly registryDir?: string;
    readonly checkpointId?: string;
    /** Write the checkpoint to this path so another machine can adopt the handoff. */
    readonly archive?: string;
    readonly output?: string;
  } = {},
): Promise<WorkspaceJSON> {
  const args = ["handoff", "--id", id];
  appendFlag(args, "--registry-dir", options.registryDir);
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
export function adoptWorkspaceHandoff(options: WorkspaceAdoptOptions): Promise<WorkspaceJSON> {
  const args = ["adopt", "--handoff", options.handoffPath, "--root", options.root];
  appendArchiveOptions(args, options);
  return runWorkspaceCommand(args);
}

/** Write the difference between two checkpoints to a document the other side can apply. */
export function createWorkspaceDelta(options: WorkspaceDeltaOptions): Promise<WorkspaceJSON> {
  const args = ["delta", "--from", options.from, "--to", options.to, "--output", options.output];
  appendArchiveOptions(args, options);
  return runWorkspaceCommand(args);
}

/** Bring a base checkpoint to the state a delta describes, publishing a new checkpoint. */
export function applyWorkspaceDelta(options: WorkspaceApplyOptions): Promise<WorkspaceJSON> {
  const args = ["apply", "--delta", options.delta, "--base", options.base];
  appendFlag(args, "--registry-dir", options.registryDir);
  return runWorkspaceCommand(args);
}

export function checkpointWorkspace(options: WorkspaceCheckpointOptions): Promise<WorkspaceJSON> {
  const args = ["checkpoint", "--id", options.id];
  appendFlag(args, "--registry-dir", options.registryDir);
  appendFlag(args, "--parent", options.parent);
  appendFlag(args, "--max-bytes", options.maxBytes);
  appendFlag(args, "--max-files", options.maxFiles);
  appendBooleanFlag(args, "--include-sensitive", options.includeSensitive);
  return runWorkspaceCommand(args);
}

export function diffWorkspaces(
  from: string,
  to: string,
  registryDir?: string,
): Promise<WorkspaceJSON> {
  const args = ["diff", "--from", from, "--to", to];
  appendFlag(args, "--registry-dir", registryDir);
  return runWorkspaceCommand(args);
}

export function restoreWorkspaceCheckpoint(
  checkpointId: string,
  root: string,
  registryDir?: string,
): Promise<WorkspaceJSON> {
  const args = ["restore", "--checkpoint-id", checkpointId, "--root", root];
  appendFlag(args, "--registry-dir", registryDir);
  return runWorkspaceCommand(args);
}

export function exportWorkspaceCheckpoint(
  checkpointId: string,
  output: string,
  options: WorkspaceArchiveOptions = {},
): Promise<WorkspaceJSON> {
  const args = ["export", "--checkpoint-id", checkpointId, "--output", output];
  appendArchiveOptions(args, options);
  return runWorkspaceCommand(args);
}

export function importWorkspaceCheckpoint(
  archive: string,
  options: WorkspaceArchiveOptions = {},
): Promise<WorkspaceJSON> {
  const args = ["import", "--archive", archive];
  appendArchiveOptions(args, options);
  return runWorkspaceCommand(args);
}

function appendArchiveOptions(args: string[], options: WorkspaceArchiveOptions): void {
  appendFlag(args, "--registry-dir", options.registryDir);
  appendFlag(args, "--max-bytes", options.maxBytes);
  appendFlag(args, "--max-files", options.maxFiles);
  appendBooleanFlag(args, "--include-sensitive", options.includeSensitive);
}

function appendFlag(args: string[], name: string, value: string | number | undefined): void {
  if (value !== undefined) {
    args.push(name, String(value));
  }
}

function appendBooleanFlag(args: string[], name: string, value: boolean | undefined): void {
  if (value) {
    args.push(name);
  }
}

function isWorkspaceJSON(value: unknown): value is WorkspaceJSON {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
