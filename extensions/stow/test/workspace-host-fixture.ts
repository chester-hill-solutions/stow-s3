import { AppBridge, PostMessageTransport } from '@modelcontextprotocol/ext-apps/app-bridge';
import type { ReadResourceRequest, ReadResourceResult } from '@modelcontextprotocol/sdk/types.js';
import type { McpUiHostContext } from '@modelcontextprotocol/ext-apps/app-bridge';
import type { OpenAIModelContextHostState } from '@openai/mcp-extensions/app';

type ContextContent = NonNullable<Exclude<OpenAIModelContextHostState, null>['content']>;
type StoredResource = { kind: 'json'; value: unknown } | { kind: 'text'; text: string } | { kind: 'blob'; blob: string };

declare global {
  interface Window {
    __workspaceContext: ContextContent;
  }
}

const overviewUri = 'stow://workspaces/ui-test/v1';
const rootUri = 'stow://workspaces/ui-test/pages/root';
const nextUri = 'stow://workspaces/ui-test/pages/root?offset=1';
const docsUri = 'stow://workspaces/ui-test/pages/docs';
const readmeUri = 'stow://workspaces/ui-test/files/readme';
const emptyUri = 'stow://workspaces/ui-test/files/empty';

const overview = {
  version: 1, workspace_id: 'ui-test', bucket: 'ui-test-bucket', view: 'mutable working view', read_only: true,
  resources: ['stow://workspaces/ui-test/tree-pages/v1/Lg'], features: ['directory', 'file', 'paged_directory'],
  max_file_bytes: 65536, max_entries: 100, paged_root_uri: rootUri,
  read_roots: ['.'],
  max_observation_entries: 10000, max_observation_bytes: 4194304, max_observations: 8,
  max_cache_bytes: 16777216, observation_ttl_seconds: 300,
};
const entry = (name: string, path: string, kind: 'directory' | 'file', uri: string, size = 0) => ({ name, path, kind, uri, size });
const makePage = (path: string, entries: ReturnType<typeof entry>[], next?: string) => ({
  version: 1, workspace_id: 'ui-test', path, view: 'default', observation_id: 'observation-1',
  observed_at: '2026-11-11T12:00:00Z', observation_complete: true, entries,
  page: { offset: 0, count: entries.length, ...(next ? { next_uri: next } : {}) },
});
const resources = new Map<string, StoredResource>([
  [overviewUri, { kind: 'json', value: overview }],
  [rootUri, { kind: 'json', value: makePage('/', [entry('docs', 'docs', 'directory', docsUri), entry('readme.txt', 'readme.txt', 'file', readmeUri, 19), entry('empty.txt', 'empty.txt', 'file', emptyUri)], nextUri) }],
  [nextUri, { kind: 'json', value: makePage('/', [entry('z-last.txt', 'z-last.txt', 'file', 'stow://workspaces/ui-test/files/last', 4)]) }],
  [docsUri, { kind: 'json', value: makePage('/docs', [entry('guide.txt', 'docs/guide.txt', 'file', 'stow://workspaces/ui-test/files/guide', 5)]) }],
  [readmeUri, { kind: 'text', text: 'safe text <script>not markup</script>' }],
  [emptyUri, { kind: 'text', text: '' }],
  ['stow://workspaces/ui-test/files/last', { kind: 'blob', blob: 'AA==' }],
  ['stow://workspaces/ui-test/files/guide', { kind: 'text', text: 'guide' }],
]);

const iframe = document.querySelector<HTMLIFrameElement>('#workspace-frame');
if (!iframe) throw new Error('Missing workspace iframe.');
window.addEventListener('message', event => {
  document.body.dataset.lastMessage = JSON.stringify(event.data).slice(0, 500);
});
window.__workspaceContext = [{ type: 'text', text: 'preserve this host context' }];

const initialContext: McpUiHostContext = {
  'openai/modelContext': { content: window.__workspaceContext, updateId: 'initial' },
};
const bridge = new AppBridge(null, { name: 'Stow browser fixture', version: '1.0.0' }, {
  serverResources: {}, updateModelContext: { resourceLink: {} },
  experimental: { 'openai/modelContext': {} },
}, { hostContext: initialContext });
bridge.onreadresource = async (params: ReadResourceRequest['params']): Promise<ReadResourceResult> => {
  const value = resources.get(params.uri);
  if (!value) throw new Error('Resource expired');
  if (value.kind === 'json') return { contents: [{ uri: params.uri, mimeType: 'application/json', text: JSON.stringify(value.value) }] };
  if (value.kind === 'text') return { contents: [{ uri: params.uri, mimeType: 'text/plain', text: value.text }] };
  return { contents: [{ uri: params.uri, mimeType: 'application/octet-stream', blob: value.blob }] };
};
bridge.onupdatemodelcontext = async ({ content }) => {
  window.__workspaceContext = content ?? [];
  await bridge.sendHostContextChange({ 'openai/modelContext': { content: window.__workspaceContext, updateId: `update-${window.__workspaceContext.length}` } });
  return {};
};
bridge.oninitialized = () => { document.body.dataset.bridgeReady = 'true'; };
document.body.dataset.connecting = 'true';
void bridge.connect(new PostMessageTransport(iframe.contentWindow ?? undefined, iframe.contentWindow ?? window)).then(() => {
  document.body.dataset.connected = 'true';
}).catch(error => {
  document.body.dataset.bridgeError = error instanceof Error ? error.message : 'Bridge connection failed';
});
iframe.src = '/workspace.html';
