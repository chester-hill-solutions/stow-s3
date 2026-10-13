import type { ContentReference } from "./content-types.js";
import type { ObjectSyncRemote, RemoteObjectBasis } from "./file-history-types.js";

export type ProjectInput = Uint8Array | AsyncIterable<Uint8Array>;
export interface ProjectFile {
  key: string;
  content: ContentReference;
  contentType?: string;
  metadata?: Record<string, string>;
}
export interface ProjectRevision { id: string; createdAt: string; label?: string }
export interface ProjectManifest { formatVersion: 1; projectId: string; files: ProjectFile[] }
export interface ProjectAttempt { id: string; revisionId: string; basis: RemoteObjectBasis }
export interface ProjectState {
  formatVersion: 1;
  projectId: string;
  versions: ProjectRevision[];
  cursor: number;
  syncedVersionId: string | null;
  basis: RemoteObjectBasis;
  attempt: ProjectAttempt | null;
  migrations: Record<string, string>;
}
export interface BrowserProjectOptions {
  projectId: string;
  maxVersions?: number;
  maxReadBytes?: number;
  maxManifestBytes?: number;
  remote?: ObjectSyncRemote;
  remoteBucket?: string;
}
export type ProjectChange =
  | { type: "put"; key: string; data: ProjectInput; contentType?: string; metadata?: Record<string, string> }
  | { type: "delete"; key: string }
  | { type: "replaceRange"; key: string; offset: number; data: Uint8Array };
export interface ProjectCommitOptions { label?: string; signal?: AbortSignal; migrationToken?: string }
export interface ProjectStatus {
  projectId: string;
  generation: number;
  versions: ProjectRevision[];
  currentVersionId: string | null;
  pending: boolean;
  canUndo: boolean;
  canRedo: boolean;
  hasUncertainWrite: boolean;
}
export type ProjectErrorCode = "not_found" | "conflict" | "corrupt" | "invalid_options" | "history_boundary" | "read_too_large" | "remote_unavailable" | "uncertain_write" | "closed";
export class ProjectError extends Error {
  constructor(readonly code: ProjectErrorCode, message: string) { super(message); this.name = "ProjectError"; }
}
