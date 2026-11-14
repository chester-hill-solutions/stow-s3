import type {
  ComposerContext,
  ComposerContent,
  ComposerExtension,
  ResourceContent,
  WorkspaceApp,
  WorkspaceEntry,
  WorkspaceLocation,
  WorkspaceOverview,
  WorkspacePage,
  WorkspaceState,
} from './types';

const attachmentLimit = 10;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every(item => typeof item === 'string');
}

function hasOverviewIdentity(value: Record<string, unknown>): boolean {
  return value.version === 1 && typeof value.workspace_id === 'string' &&
    typeof value.bucket === 'string' && typeof value.view === 'string' &&
    value.read_only === true && typeof value.paged_root_uri === 'string';
}

function hasOverviewLists(value: Record<string, unknown>): boolean {
  return isStringArray(value.read_roots) && isStringArray(value.resources) && isStringArray(value.features);
}

function hasOverviewLimits(value: Record<string, unknown>): boolean {
  return typeof value.max_entries === 'number' && typeof value.max_file_bytes === 'number' &&
    typeof value.max_observation_entries === 'number' && typeof value.max_observation_bytes === 'number' &&
    typeof value.max_observations === 'number' && typeof value.max_cache_bytes === 'number' &&
    typeof value.observation_ttl_seconds === 'number';
}

function isOverview(value: unknown): value is WorkspaceOverview {
  return isRecord(value) && hasOverviewIdentity(value) && hasOverviewLists(value) && hasOverviewLimits(value);
}

function parseOverview(value: unknown): WorkspaceOverview {
  if (!isOverview(value)) throw new Error('The workspace overview could not be read.');
  return value;
}

function isEntry(value: unknown): value is WorkspaceEntry {
  return isRecord(value) && typeof value.name === 'string' && typeof value.path === 'string' &&
    (value.kind === 'directory' || value.kind === 'file') && typeof value.size === 'number' &&
    Number.isSafeInteger(value.size) && value.size >= 0 && typeof value.uri === 'string';
}

function isPageIdentity(value: Record<string, unknown>): boolean {
  return value.version === 1 && typeof value.workspace_id === 'string' &&
    typeof value.path === 'string' && typeof value.view === 'string' &&
    typeof value.observation_id === 'string' && typeof value.observed_at === 'string' &&
    typeof value.observation_complete === 'boolean';
}

function isPageEntries(value: Record<string, unknown>): boolean {
  return Array.isArray(value.entries) && value.entries.every(isEntry);
}

function isPageMetadata(value: unknown): value is WorkspacePage['page'] {
  return isRecord(value) && typeof value.offset === 'number' && typeof value.count === 'number' &&
    (value.next_uri === undefined || typeof value.next_uri === 'string');
}

function isPage(value: unknown): value is WorkspacePage {
  return isRecord(value) && isPageIdentity(value) && isPageEntries(value) && isPageMetadata(value.page);
}

function parsePage(value: unknown): WorkspacePage {
  if (!isPage(value)) throw new Error('This workspace page could not be read.');
  return value;
}

function parseContent(value: unknown): ResourceContent | undefined {
  if (!isRecord(value)) return undefined;
  const text = typeof value.text === 'string' ? value.text : undefined;
  const blob = typeof value.blob === 'string' ? value.blob : undefined;
  const mimeType = typeof value.mimeType === 'string' ? value.mimeType : undefined;
  return text === undefined && blob === undefined ? undefined : { text, blob, mimeType };
}

function parseComposer(value: ComposerContext | null | undefined): ComposerContent {
  return Array.isArray(value?.content) ? value.content : [];
}

export class WorkspaceController {
  private state: WorkspaceState = {
    phase: 'loading', message: 'Connecting to workspace…', readRoots: [], breadcrumbs: [], canGoBack: false,
    preview: { kind: 'idle' }, selectedUris: new Set(), attachmentBusy: false, focusRequest: 0,
  };
  private readonly listeners = new Set<(state: WorkspaceState) => void>();
  private route = '';
  private backStack: Array<{ uri: string; baseUri: string; breadcrumbs: WorkspaceLocation[] }> = [];
  private requestId = 0;
  private previewId = 0;
  private attachmentBusy = false;
  private baseUri = '';

  constructor(
    private readonly app: WorkspaceApp,
    private readonly overviewUri: string,
    private readonly composer?: ComposerExtension,
  ) {}

  getSnapshot = (): WorkspaceState => this.state;

  subscribe = (listener: (state: WorkspaceState) => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  private publish(patch: Partial<WorkspaceState>): void {
    this.state = { ...this.state, ...patch };
    for (const listener of this.listeners) listener(this.state);
  }

  async start(): Promise<void> {
    const current = ++this.requestId;
    this.previewId++;
    this.publish({ phase: 'loading', message: 'Loading workspace…' });
    try {
      const overviewResult = await this.app.readServerResource({ uri: this.overviewUri });
      const overviewText = parseContent(overviewResult.contents[0])?.text;
      const overviewValue: unknown = overviewText ? JSON.parse(overviewText) : undefined;
      const overview = parseOverview(overviewValue);
      if (current !== this.requestId) return;
      this.route = overview.paged_root_uri;
      this.baseUri = this.route;
      this.backStack = [];
      this.publish({ workspaceId: overview.workspace_id, readRoots: overview.read_roots,
        breadcrumbs: [{ label: 'Workspace', uri: this.route }], preview: { kind: 'idle' },
        canGoBack: false, focusRequest: this.state.focusRequest + 1 });
      this.reconcileComposer();
      await this.loadPage(this.route, current);
    } catch {
      if (current === this.requestId) this.publish({ phase: 'error', message: 'Workspace unavailable. Retry to reconnect.' });
    }
  }

  private async loadPage(uri: string, request: number): Promise<void> {
    try {
      const result = await this.app.readServerResource({ uri });
      const raw = parseContent(result.contents[0])?.text;
      if (!raw) throw new Error('empty page');
      const pageValue: unknown = JSON.parse(raw);
      const page = parsePage(pageValue);
      if (request !== this.requestId) return;
      this.route = uri;
      this.publish({ phase: 'ready', message: !page.observation_complete
        ? `Partial folder listing: ${page.entries.length} items shown. Some items may be missing.`
        : page.entries.length === 0 ? 'This folder is empty.' : `${page.entries.length} items shown. Read-only workspace.`, page,
      });
    } catch {
      if (request === this.requestId) this.publish({ phase: 'error', message: 'This workspace page is unavailable or expired. Retry or return to the workspace.' });
    }
  }

  private navigate(uri: string, breadcrumbs: WorkspaceLocation[], baseUri = uri, saveBack = true): void {
    if (saveBack && this.route) this.backStack.push({ uri: this.route, baseUri: this.baseUri, breadcrumbs: this.state.breadcrumbs });
    const request = ++this.requestId;
    this.previewId++;
    this.route = uri;
    this.baseUri = baseUri;
    this.publish({ phase: 'loading', message: 'Loading folder…', breadcrumbs, preview: { kind: 'idle' }, canGoBack: this.backStack.length > 0, focusRequest: this.state.focusRequest + 1 });
    void this.loadPage(uri, request);
  }

  openDirectory(entry: WorkspaceEntry): void {
    const breadcrumbs = [...this.state.breadcrumbs, { label: entry.name, uri: entry.uri }];
    this.navigate(entry.uri, breadcrumbs);
  }

  nextPage(): void {
    const nextUri = this.state.page?.page.next_uri;
    if (nextUri) this.navigate(nextUri, this.state.breadcrumbs, this.baseUri);
  }

  back(): void {
    const previous = this.backStack.pop();
    if (previous) this.navigate(previous.uri, previous.breadcrumbs, previous.baseUri, false);
  }

  goToBreadcrumb(index: number): void {
    const location = this.state.breadcrumbs[index];
    if (!location || index === this.state.breadcrumbs.length - 1) return;
    this.backStack = [];
    this.navigate(location.uri, this.state.breadcrumbs.slice(0, index + 1), location.uri, false);
  }

  refresh(): void {
    if (!this.route) { void this.start(); return; }
    const request = ++this.requestId;
    this.previewId++;
    this.route = this.baseUri;
    this.backStack = [];
    this.publish({ phase: 'loading', message: 'Refreshing folder and creating a fresh listing…', preview: { kind: 'idle' },
      canGoBack: false, focusRequest: this.state.focusRequest + 1 });
    void this.loadPage(this.baseUri, request);
  }

  async previewFile(entry: WorkspaceEntry): Promise<void> {
    const current = ++this.previewId;
    this.publish({ preview: { kind: 'loading', name: entry.name } });
    try {
      const result = await this.app.readServerResource({ uri: entry.uri });
      if (current !== this.previewId) return;
      const content = parseContent(result.contents[0]);
      if (content && ((content.text !== undefined && content.text.length === 0) || (content.blob !== undefined && content.blob.length === 0))) {
        this.publish({ preview: { kind: 'empty', name: entry.name } });
      } else if (content?.text !== undefined && !['text/html', 'application/xhtml+xml'].includes(content.mimeType?.split(';', 1)[0]?.trim().toLowerCase() ?? '')) {
        this.publish({ preview: { kind: 'text', name: entry.name, text: content.text } });
      } else {
        this.publish({ preview: { kind: 'binary', name: entry.name } });
      }
    } catch {
      if (current === this.previewId) this.publish({ preview: { kind: 'error', name: entry.name, message: 'Preview unavailable. The file may have changed or its reference may have expired.' } });
    }
  }

  reconcileComposer(): void {
    const content = parseComposer(this.composer?.getCurrent());
    this.publish({ selectedUris: new Set(content.flatMap(item => item.type === 'resource_link' && typeof item.uri === 'string' ? [item.uri] : [])) });
  }

  async toggleAttachment(entry: WorkspaceEntry, checked: boolean): Promise<void> {
    if (!this.composer || this.attachmentBusy) return;
    const content = parseComposer(this.composer.getCurrent());
    const refs = content.filter(item => item.type === 'resource_link');
    const selected = new Map(refs.map(item => [item.uri, item]));
    if (checked) selected.set(entry.uri, { type: 'resource_link', uri: entry.uri, name: entry.name, mimeType: 'application/octet-stream', size: entry.size });
    else selected.delete(entry.uri);
    if (selected.size > attachmentLimit) {
      this.publish({ message: 'Attach up to 10 file references at a time.' });
      return;
    }
    const updatedContent = [...content.filter(item => item.type !== 'resource_link'), ...selected.values()];
    this.attachmentBusy = true;
    this.publish({ attachmentBusy: true, message: 'Updating composer references…' });
    try {
      await this.composer.update({ content: updatedContent });
      this.reconcileComposer();
      this.publish({ message: 'Composer references updated.' });
    } catch {
      this.publish({ message: 'The file reference could not be attached. Try again.' });
    } finally {
      this.attachmentBusy = false;
      this.publish({ attachmentBusy: false });
    }
  }
}

export type WorkspaceControllerState = WorkspaceState;
