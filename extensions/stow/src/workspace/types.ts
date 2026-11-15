export type WorkspaceBootstrap = {
  mode: 'workspace';
  overview_uri: string;
};

export type WorkspaceEntry = {
  name: string;
  path: string;
  kind: 'directory' | 'file';
  size: number;
  uri: string;
};

export type WorkspacePage = {
  version: 1;
  workspace_id: string;
  path: string;
  view: string;
  observation_id: string;
  observed_at: string;
  observation_complete: boolean;
  entries: WorkspaceEntry[];
  page: { offset: number; count: number; next_uri?: string };
};

export type WorkspaceOverview = {
  version: 1;
  workspace_id: string;
  bucket: string;
  view: string;
  read_only: true;
  resources: string[];
  features: string[];
  paged_root_uri: string;
  max_entries: number;
  max_file_bytes: number;
  read_roots: string[];
  max_observation_entries: number;
  max_observation_bytes: number;
  max_observations: number;
  max_cache_bytes: number;
  observation_ttl_seconds: number;
};

export type WorkspaceLocation = { label: string; uri: string };
export type WorkspacePreview =
  | { kind: 'idle' }
  | { kind: 'loading'; name: string }
  | { kind: 'text'; name: string; text: string }
  | { kind: 'binary'; name: string }
  | { kind: 'empty'; name: string }
  | { kind: 'error'; name: string; message: string };

export type WorkspaceState = {
  phase: 'loading' | 'ready' | 'error';
  message: string;
  workspaceId?: string;
  readRoots: string[];
  page?: WorkspacePage;
  breadcrumbs: WorkspaceLocation[];
  canGoBack: boolean;
  preview: WorkspacePreview;
  selectedUris: Set<string>;
  attachmentBusy: boolean;
  focusRequest: number;
};

export type ResourceContent = { text?: string; blob?: string; mimeType?: string };
export type WorkspaceApp = {
  readServerResource: (params: { uri: string }) => Promise<{ contents: ResourceContent[] }>;
};

export type ComposerContext = Exclude<OpenAIModelContextHostState, null>;
export type ComposerContent = NonNullable<ComposerContext['content']>;
export type ComposerExtension = OpenAIModelContext;
import type { OpenAIModelContext, OpenAIModelContextHostState } from '@openai/mcp-extensions/app';

