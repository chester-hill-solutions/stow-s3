import assert from 'node:assert/strict';
import { test } from 'node:test';
import { build } from 'esbuild';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const compiled = await build({ absWorkingDir: root, entryPoints: ['src/workspace/controller.ts'], bundle: true, format: 'esm', platform: 'node', write: false });
const { WorkspaceController } = await import(`data:text/javascript;base64,${Buffer.from(compiled.outputFiles[0].text).toString('base64')}`);
const overviewUri = 'stow://workspaces/test/v1';
const rootUri = 'stow://workspaces/test/pages/root';
const nextUri = 'stow://workspaces/test/pages/root?offset=1';
const folderUri = 'stow://workspaces/test/pages/docs';
const fileUri = 'stow://workspaces/test/files/readme';
const page = (path, entries, next) => ({
  version: 1, workspace_id: 'test', path, view: 'default', observation_id: 'obs-1',
  observed_at: '2026-11-11T00:00:00Z', observation_complete: true, entries,
  page: { offset: 0, count: entries.length, ...(next ? { next_uri: next } : {}) },
});
const file = { name: 'readme.txt', path: 'readme.txt', kind: 'file', size: 5, uri: fileUri };
const folder = { name: 'docs', path: 'docs', kind: 'directory', size: 0, uri: folderUri };
const json = value => ({ contents: [{ mimeType: 'application/json', text: JSON.stringify(value) }] });
const overview = { version: 1, workspace_id: 'test', bucket: 'objects', view: 'mutable working view', read_only: true,
  resources: ['stow://workspaces/test/tree-pages/v1/Lg'], features: ['directory', 'file', 'paged_directory'],
  max_file_bytes: 65536, max_entries: 100, paged_root_uri: rootUri,
  read_roots: ['.'],
  max_observation_entries: 10000, max_observation_bytes: 4194304, max_observations: 8,
  max_cache_bytes: 16777216, observation_ttl_seconds: 300 };
const tick = () => new Promise(resolve => setImmediate(resolve));

function createController(resources, composer) {
  const calls = [];
  const app = { readServerResource: async ({ uri }) => {
    calls.push(uri);
    if (resources instanceof Map) {
      const result = resources.get(uri);
      if (result instanceof Error) throw result;
      if (typeof result === 'function') return result();
      if (!result) throw new Error('missing resource');
      return result;
    }
    return resources(uri);
  } };
  return { controller: new WorkspaceController(app, overviewUri, composer), calls };
}

test('reads overview and pages resources with back, breadcrumb and refresh navigation', async () => {
  const resources = new Map([
    [overviewUri, json(overview)],
    [rootUri, json(page('/', [folder], nextUri))],
    [nextUri, json(page('/', [file]))],
    [folderUri, json(page('/docs', [file]))],
    [fileUri, { contents: [{ mimeType: 'text/plain', text: 'hello' }] }],
  ]);
  const { controller, calls } = createController(resources);
  await controller.start();
  assert.deepEqual(calls, [overviewUri, rootUri]);
  assert.equal(controller.getSnapshot().phase, 'ready');
  controller.openDirectory(folder);
  await tick();
  assert.equal(controller.getSnapshot().breadcrumbs.at(-1)?.label, 'docs');
  await controller.previewFile(file);
  assert.deepEqual(controller.getSnapshot().preview, { kind: 'text', name: 'readme.txt', text: 'hello' });
  controller.refresh();
  assert.deepEqual(controller.getSnapshot().preview, { kind: 'idle' }, 'refresh clears preview bytes from the previous listing');
  await tick();
  controller.goToBreadcrumb(0);
  await tick();
  assert.equal(controller.getSnapshot().breadcrumbs.length, 1);
  controller.nextPage();
  await tick();
  assert.equal(controller.getSnapshot().page?.entries[0]?.name, 'readme.txt');
  assert.equal(controller.getSnapshot().canGoBack, true);
  controller.back();
  await tick();
  assert.equal(controller.getSnapshot().page?.entries[0]?.name, 'docs');
  resources.set(nextUri, new Error('expired cursor'));
  controller.nextPage();
  await tick();
  assert.equal(controller.getSnapshot().phase, 'error');
  controller.refresh();
  await tick();
  assert.equal(controller.getSnapshot().phase, 'ready');
  assert.equal(calls.at(-1), rootUri, 'refresh must create a fresh directory observation');
});

test('keeps partial directory metadata visible as a partial listing state', async () => {
  const partial = page('/', [file]);
  partial.observation_complete = false;
  const { controller } = createController(new Map([[overviewUri, json(overview)], [rootUri, json(partial)]]));
  await controller.start();
  assert.equal(controller.getSnapshot().phase, 'ready');
  assert.match(controller.getSnapshot().message, /Partial folder listing/);
});

test('renders empty, binary, HTML and expired preview states as plain status', async () => {
  const resources = new Map([
    [overviewUri, json(overview)],
    [rootUri, json(page('/', []))],
    [fileUri, { contents: [{ mimeType: 'text/plain', text: '' }] }],
  ]);
  const { controller } = createController(resources);
  await controller.start();
  await controller.previewFile({ ...file, size: 0 });
  assert.deepEqual(controller.getSnapshot().preview, { kind: 'empty', name: file.name });
  resources.set(fileUri, { contents: [{ mimeType: 'application/octet-stream', blob: 'AA==' }] });
  await controller.previewFile(file);
  assert.deepEqual(controller.getSnapshot().preview, { kind: 'binary', name: file.name });
  resources.set(fileUri, { contents: [{ mimeType: 'text/html', text: '<script>unsafe</script>' }] });
  await controller.previewFile(file);
  assert.deepEqual(controller.getSnapshot().preview, { kind: 'binary', name: file.name });
  resources.set(fileUri, new Error('expired'));
  await controller.previewFile(file);
  assert.equal(controller.getSnapshot().preview.kind, 'error');
  assert.match(controller.getSnapshot().preview.message, /expired/);
});

test('late page and preview replies cannot replace the current selection', async () => {
  let resolveOldPage;
  let resolveOldPreview;
  const resources = uri => {
    if (uri === overviewUri) return json(overview);
    if (uri === rootUri) return json(page('/', [file]));
    if (uri === 'old-page') return new Promise(resolve => { resolveOldPage = resolve; });
    if (uri === 'old-file') return new Promise(resolve => { resolveOldPreview = resolve; });
    return json(page('/current', [folder]));
  };
  const { controller } = createController(resources);
  await controller.start();
  controller.openDirectory({ ...folder, uri: 'old-page' });
  controller.openDirectory({ ...folder, name: 'current', uri: 'current-page' });
  await tick();
  resolveOldPage(json(page('/old', [])));
  await tick();
  assert.equal(controller.getSnapshot().page?.path, '/current');
  const oldPreview = { ...file, uri: 'old-file', name: 'old.txt' };
  const newPreview = { ...file, name: 'new.txt' };
  void controller.previewFile(oldPreview);
  void controller.previewFile(newPreview);
  await tick();
  resolveOldPreview({ contents: [{ mimeType: 'text/plain', text: 'stale' }] });
  await tick();
  assert.equal(controller.getSnapshot().preview.name, 'new.txt');
});

test('composer updates preserve other context, cap file references at ten and recover from errors', async () => {
  const initialContent = [{ type: 'text', text: 'keep this' }];
  let current = { content: initialContent };
  const updates = [];
  let failUpdate = false;
  const composer = {
    getCurrent: () => current,
    update: async ({ content }) => {
      if (failUpdate) throw new Error('host refused');
      updates.push(content);
      current = { content };
    },
  };
  const resources = new Map([
    [overviewUri, json(overview)],
    [rootUri, json(page('/', []))],
  ]);
  const { controller } = createController(resources, composer);
  await controller.start();
  await controller.toggleAttachment(file, true);
  assert.equal(updates[0][0].text, 'keep this');
  assert.equal(updates[0][1].uri, fileUri);
  for (let index = 1; index < 10; index++) {
    await controller.toggleAttachment({ ...file, name: `file-${index}`, uri: `stow://file/${index}` }, true);
  }
  assert.equal(updates.length, 10);
  await controller.toggleAttachment({ ...file, name: 'eleventh', uri: 'stow://file/eleventh' }, true);
  assert.equal(updates.length, 10);
  assert.match(controller.getSnapshot().message, /up to 10/);
  failUpdate = true;
  await controller.toggleAttachment(file, false);
  assert.match(controller.getSnapshot().message, /could not be attached/);
});
