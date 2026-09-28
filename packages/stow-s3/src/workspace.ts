import { execFile } from "node:child_process";
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

/** Run the native workspace command contract without invoking a shell. */
export async function runWorkspaceCommand(
  args: readonly string[],
): Promise<WorkspaceJSON> {
  const binary = resolveStowBinary();
  const { stdout } = await execFileAsync(binary, ["workspace", ...args], {
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
  });
  const value: unknown = JSON.parse(stdout);
  if (!isWorkspaceJSON(value)) {
    throw new Error("stow-s3 workspace command returned a non-object JSON value");
  }
  return value;
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

export function handoffWorkspace(
  id: string,
  options: { readonly registryDir?: string; readonly checkpointId?: string; readonly output?: string } = {},
): Promise<WorkspaceJSON> {
  const args = ["handoff", "--id", id];
  appendFlag(args, "--registry-dir", options.registryDir);
  appendFlag(args, "--checkpoint-id", options.checkpointId);
  appendFlag(args, "--output", options.output);
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
