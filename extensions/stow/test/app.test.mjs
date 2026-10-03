import assert from 'node:assert/strict';
import { test } from 'node:test';
import { build } from 'esbuild';
import { fileURLToPath } from 'node:url';

class Element {
  children = [];
  listeners = {};
  textContent = '';
  checked = false;
  disabled = false;
  value = '';
  append(...children) { this.children.push(...children); }
  replaceChildren() { this.children = []; }
  setAttribute() {}
  addEventListener(name, handler) { this.listeners[name] = handler; }
  dispatch(name) { this.listeners[name]?.({ preventDefault() {} }); }
  set innerHTML(_) { throw new Error('Untrusted HTML rendering'); }
}
const item = (name = '<script>hello</script>') => ({ type: 'resource_link', name, uri: `stow://objects/objects/${Buffer.from(name).toString('base64url')}`, mimeType: 'text/plain', size: 5 });
const page = (items = [item()]) => ({ bucket: 'objects', items, max_bytes: 65536, truncated: false });
const sdkMock = `
const state = globalThis.__stowTest;
export class App {
  constructor() { state.app = this; }
  addEventListener(name, handler) { (state.events[name] ??= []).push(handler); }
  async connect() { state.connected = true; if (!this.ontoolresult) throw Error('Result handler registered too late'); this.ontoolresult({ structuredContent: state.initial }); }
  getHostContext() { return {}; }
  callServerTool(params) { state.calls.push(params); return Promise.resolve({ structuredContent: state.nextPage ?? state.initial }); }
  readServerResource(params) { state.reads.push(params); return state.readResource(params); }
}
export function applyDocumentTheme() {}
export function applyHostStyleVariables() {}
export class OpenAIExtensions {
  get modelContext() { return state.connected && state.supported ? {
    getCurrent: () => state.context,
    update: async params => { state.updates.push(params); state.context = { ...params, updateId: 'next' }; for (const callback of state.events.hostcontextchanged ?? []) callback(); return { updateId: 'next' }; }
  } : undefined; }
}`;
const root = fileURLToPath(new URL('..', import.meta.url));
const compiled = await build({ absWorkingDir: root, entryPoints: ['src/app.ts'], bundle: true, format: 'esm', platform: 'browser', write: false, loader: { '.css': 'empty' }, plugins: [{ name: 'host-fixture', setup(build) {
  build.onResolve({ filter: /^@(modelcontextprotocol\/ext-apps|openai\/mcp-extensions)/ }, args => ({ path: args.path, namespace: 'mock' }));
  build.onLoad({ filter: /.*/, namespace: 'mock' }, args => ({ contents: args.path.endsWith('.css') ? '' : sdkMock, loader: 'js' }));
}}] });
let instance = 0;
const tick = () => new Promise(resolve => setImmediate(resolve));
async function mount(options = {}) {
  const elements = Object.fromEntries(['#prefix', '#objects', '#status', '#preview', '#next', '#bucket', '#search'].map(name => [name, new Element()]));
  const state = { initial: page(), supported: true, connected: false, context: null, events: {}, calls: [], reads: [], updates: [], readResource: async () => ({ contents: [{ text: 'hello' }] }), ...options };
  globalThis.__stowTest = state;
  globalThis.document = { querySelector: name => elements[name], createElement: () => new Element() };
  await import(`data:text/javascript;base64,${Buffer.from(compiled.outputFiles[0].text).toString('base64')}#${++instance}`);
  return { state, elements };
}

test('initial host result renders once and treats object names as text', async () => {
  const { state, elements } = await mount();
  assert.equal(state.calls.length, 0);
  assert.equal(elements['#objects'].children[0].children[0].textContent, '<script>hello</script> · 5 bytes');
  assert.equal(state.updates.length, 0);
});

test('prefix changes reset paging while next uses the opaque cursor', async () => {
  const { state, elements } = await mount({ initial: { ...page(), truncated: true, next_cursor: 'opaque' } });
  elements['#next'].dispatch('click');
  await tick();
  assert.deepEqual(state.calls[0].arguments, { prefix: '', cursor: 'opaque' });
  elements['#prefix'].value = 'notes/';
  elements['#search'].dispatch('submit');
  await tick();
  assert.deepEqual(state.calls[1].arguments, { prefix: 'notes/', cursor: '' });
});

test('attachment requires explicit selection and reconciles composer removal', async () => {
  const { state, elements } = await mount();
  const checkbox = elements['#objects'].children[0].children[1];
  checkbox.checked = true;
  checkbox.dispatch('change');
  await tick();
  assert.deepEqual(state.updates, [{ content: [item()] }]);
  assert.equal(elements['#objects'].children[0].children[1].checked, true);
  state.context = null;
  for (const callback of state.events.hostcontextchanged) callback();
  assert.equal(elements['#objects'].children[0].children[1].checked, false);
  assert.equal(state.updates.length, 1);
});

test('unsupported hosts keep browsing without attachment controls', async () => {
  const { state, elements } = await mount({ supported: false });
  assert.equal(elements['#objects'].children[0].children[1].disabled, true);
  assert.match(elements['#status'].textContent, /unavailable in this host/);
  elements['#objects'].children[0].children[0].dispatch('click');
  await tick();
  assert.match(elements['#preview'].textContent, /hello/);
  assert.equal(state.updates.length, 0);
});

test('late preview responses cannot replace the selected object', async () => {
  const pending = [];
  const { elements } = await mount({ initial: page([item('one'), item('two')]), readResource: () => new Promise(resolve => pending.push(resolve)) });
  elements['#objects'].children[0].children[0].dispatch('click');
  elements['#objects'].children[1].children[0].dispatch('click');
  pending[1]({ contents: [{ text: 'second' }] });
  await tick();
  pending[0]({ contents: [{ text: 'first' }] });
  await tick();
  assert.equal(elements['#preview'].textContent, 'two\n\nsecond');
});
