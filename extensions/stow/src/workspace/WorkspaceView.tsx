import { useEffect, useRef, useSyncExternalStore } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { UiI18nProvider } from './ui/i18n';
import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage } from './ui/breadcrumb';
import { Button } from './ui/button';
import { Checkbox } from './ui/checkbox';
import { WorkspaceController } from './controller';
import type { ComposerExtension, WorkspaceApp, WorkspaceEntry } from './types';

type Props = { controller: WorkspaceController; canAttach: boolean };

function displaySize(size: number): string {
  return `${size.toLocaleString()} bytes`;
}

function formatObservedAt(value: string): string {
  const time = Date.parse(value);
  return Number.isFinite(time) ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(time) : value;
}

function EntryRow({ entry, controller, canAttach, selected, busy }: {
  entry: WorkspaceEntry;
  controller: WorkspaceController;
  canAttach: boolean;
  selected: boolean;
  busy: boolean;
}) {
  return <li className="workspace-entry">
    {entry.kind === 'directory'
      ? <Button className="workspace-entry-open" variant="ghost" size="touch" onPress={() => controller.openDirectory(entry)}>
          <span aria-hidden="true">▸</span><span className="workspace-entry-name">{entry.name}</span><span className="workspace-entry-meta">Folder</span>
        </Button>
      : <Button className="workspace-entry-open" variant="ghost" size="touch" onPress={() => void controller.previewFile(entry)}>
          <span aria-hidden="true">▤</span><span className="workspace-entry-name">{entry.name}</span><span className="workspace-entry-meta">{displaySize(entry.size)}</span>
        </Button>}
    {entry.kind === 'file' && <Checkbox
      aria-label={`Attach ${entry.name} to composer`}
      isSelected={selected}
      isDisabled={!canAttach || busy}
      onChange={(checked) => void controller.toggleAttachment(entry, checked)}
      className="workspace-attach"
    />}
  </li>;
}

function Preview({ controller }: { controller: WorkspaceController }) {
  const preview = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot).preview;
  if (preview.kind === 'idle') return <section className="workspace-preview" aria-label="File preview">
    <h2>Current file bytes</h2><p>Choose a file to read its current contents.</p>
  </section>;
  if (preview.kind === 'loading') return <section className="workspace-preview" aria-label="File preview" aria-busy="true">
    <h2>Current file bytes</h2><p role="status">Loading {preview.name}…</p>
  </section>;
  return <section className="workspace-preview" aria-label={`${preview.name} preview`}>
    <h2>{preview.name}</h2>
    {preview.kind === 'text' && <pre className="workspace-file-text">{preview.text}</pre>}
    {preview.kind === 'empty' && <p>This file is empty.</p>}
    {preview.kind === 'binary' && <p>This file is binary or HTML. Attach its reference to use it in the composer.</p>}
    {preview.kind === 'error' && <p role="status">{preview.message}</p>}
  </section>;
}

function WorkspaceView({ controller, canAttach }: Props) {
  const state = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { void controller.start(); }, [controller]);
  useEffect(() => { heading.current?.focus({ preventScroll: true }); }, [state.focusRequest]);
  const page = state.page;
  return <main className="workspace-shell" aria-label="Workspace browser">
    <header className="workspace-header">
      <div><p className="workspace-eyebrow">STOW / WORKSPACE</p><h1 ref={heading} tabIndex={-1}>Workspace files</h1></div>
      <span className="workspace-readonly">Read-only</span>
    </header>
    {state.workspaceId && <p className="workspace-identity">Workspace ID: <code>{state.workspaceId}</code> · Read roots: <code>{state.readRoots.join(', ')}</code></p>}
    <div className="workspace-navigation">
      <Breadcrumb>
        <BreadcrumbList>
          {state.breadcrumbs.map((crumb, index) => <BreadcrumbItem key={`${crumb.uri}:${index}`}>
            {index === state.breadcrumbs.length - 1
              ? <BreadcrumbPage>{crumb.label}</BreadcrumbPage>
              : <BreadcrumbLink href="#workspace" onPress={() => controller.goToBreadcrumb(index)}>{crumb.label}</BreadcrumbLink>}
          </BreadcrumbItem>)}
        </BreadcrumbList>
      </Breadcrumb>
      <div className="workspace-navigation-actions">
        <Button variant="outline" size="touch" onPress={() => controller.back()} isDisabled={!state.canGoBack}>Back</Button>
        <Button variant="outline" size="touch" onPress={() => controller.refresh()}>Refresh</Button>
      </div>
    </div>
    <p className="workspace-status" role="status" aria-live="polite">{state.message}</p>
    {state.phase === 'ready' && page && <p className="workspace-observation">Folder listing metadata observed at {formatObservedAt(page.observed_at)}. File previews read current bytes.</p>}
    {state.phase === 'error' && <div className="workspace-error"><p>{state.message}</p><Button size="touch" onPress={() => controller.refresh()}>Retry</Button></div>}
    {state.phase === 'loading' && <p className="workspace-loading" role="status">Loading folder contents…</p>}
    {state.phase === 'ready' && <div className="workspace-content">
      <section className="workspace-list" aria-label="Folder contents">
        <ul aria-label="Files and folders">
          {(page?.entries ?? []).map((entry) => <EntryRow key={entry.uri} entry={entry} controller={controller} canAttach={canAttach}
            selected={state.selectedUris.has(entry.uri)} busy={state.attachmentBusy} />)}
        </ul>
        {page?.entries.length === 0 && <div className="workspace-empty"><h2>This folder is empty</h2><p>Go back to a parent folder or refresh to check for updates.</p></div>}
        {page?.page.next_uri && <div className="workspace-next"><Button variant="outline" size="touch" onPress={() => controller.nextPage()}>Next page</Button></div>}
      </section>
      <Preview controller={controller} />
    </div>}
    {!canAttach && <p className="workspace-host-note">Composer references are unavailable in this host. Files remain readable here.</p>}
  </main>;
}

export function mountWorkspace(rootElement: HTMLElement, app: WorkspaceApp, overviewUri: string, composer?: ComposerExtension): WorkspaceController {
  const controller = new WorkspaceController(app, overviewUri, composer);
  const root: Root = createRoot(rootElement);
  root.render(<UiI18nProvider locale="en-US"><WorkspaceView controller={controller} canAttach={Boolean(composer)} /></UiI18nProvider>);
  return controller;
}
