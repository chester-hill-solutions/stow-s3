import assert from 'node:assert/strict';
import { test } from 'node:test';
import { App } from '@modelcontextprotocol/ext-apps';
import { AppBridge } from '@modelcontextprotocol/ext-apps/app-bridge';
import { InMemoryTransport } from '@modelcontextprotocol/sdk/inMemory.js';
import { OpenAIExtensions } from '@openai/mcp-extensions/app';

test('installed SDKs negotiate browsing, previews and composer attachments', async (t) => {
  const app = new App({ name: 'Stow compatibility test', version: '0.3.0' }, {}, { autoResize: false });
  const extensions = new OpenAIExtensions(app);
  const bridge = new AppBridge(null, { name: 'Stow test host', version: '1.0.0' }, {
    serverTools: {}, serverResources: {}, experimental: { 'openai/modelContext': {} },
  });
  t.after(async () => { await app.close(); await bridge.close(); });
  const reference = { type: 'resource_link', uri: 'stow://objects/notes/readme', name: 'readme', mimeType: 'text/plain', size: 5 };
  bridge.oncalltool = async (params) => {
    assert.equal(params.name, 'stow_browser');
    assert.deepEqual(params.arguments, { prefix: 'notes/', cursor: '' });
    return { content: [], structuredContent: { bucket: 'objects', items: [reference], truncated: false, max_bytes: 65536 } };
  };
  bridge.onreadresource = async (params) => {
    assert.equal(params.uri, reference.uri);
    return { contents: [{ uri: reference.uri, mimeType: 'text/plain', text: 'hello' }] };
  };
  bridge.onupdatemodelcontext = async (params) => {
    assert.deepEqual(params.content, [reference]);
    return { _meta: { 'openai/modelContext': { updateId: 'attached' } } };
  };
  const [viewTransport, hostTransport] = InMemoryTransport.createLinkedPair();
  await bridge.connect(hostTransport);
  await app.connect(viewTransport);
  assert.equal(app.getHostVersion().name, 'Stow test host');
  const result = await app.callServerTool({ name: 'stow_browser', arguments: { prefix: 'notes/', cursor: '' } });
  assert.deepEqual(result.structuredContent.items, [reference]);
  const preview = await app.readServerResource({ uri: reference.uri });
  assert.equal(preview.contents[0].text, 'hello');
  assert.ok(extensions.modelContext);
  assert.deepEqual(await extensions.modelContext.update({ content: [reference] }), { updateId: 'attached' });
});
