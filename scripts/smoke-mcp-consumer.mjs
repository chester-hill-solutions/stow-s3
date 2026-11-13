import assert from 'node:assert/strict';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StdioClientTransport, getDefaultEnvironment } from '@modelcontextprotocol/sdk/client/stdio.js';
import { openStow, prepareWorkspace, resolveStowBinary } from '@chester-hill-solutions/stow-s3';
import { assertConsumerBinary } from './assert-consumer-binary.mjs';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { execFile } from 'node:child_process';
import { randomUUID } from 'node:crypto';

const exec = promisify(execFile);
const binary = assertConsumerBinary(resolveStowBinary, import.meta.url);
const deadline = 30_000;
const bounded = (promise, label) => new Promise((resolve, reject) => {
  const timer = setTimeout(() => reject(new Error(`${label} timed out after ${deadline}ms`)), deadline);
  Promise.resolve(promise).then(resolve, reject).finally(() => clearTimeout(timer));
});

const session = await bounded(openStow(), 'installed binary session');
try { assert.ok(session.endpoint.startsWith('http://127.0.0.1:')); }
finally { await session.close(); }

const temp = await mkdtemp(join(tmpdir(), 'stow-mcp-consumer-'));
const clients = [];
try {
  const source = join(temp, 'source.txt');
  const task = join(temp, 'task.json');
  const original = 'release artifact saved bytes\n';
  const saved = `saved through MCP ${randomUUID()}\n`;
  await writeFile(source, original);
  await writeFile(task, JSON.stringify({ version: 1, root: join(temp, 'workspace'), registry_dir: join(temp, 'registry'),
    inputs: [{ source, destination: 'docs/state.txt' }] }));
  const prepared = await bounded(prepareWorkspace(task), 'workspace preparation');
  const workspaceRoot = prepared.root ?? join(temp, 'workspace');
  const env = { ...getDefaultEnvironment(), OPENROUTER_API_KEY: '' };
  const connect = async args => {
    const transport = new StdioClientTransport({ command: binary, args, env, cwd: workspaceRoot, stderr: 'pipe' });
    const client = new Client({ name: 'stow-release-consumer', version: '1' });
    clients.push({ client, transport });
    await bounded(client.connect(transport), 'MCP connection');
    return client;
  };
  const base = ['mcp', '--registry-dir', prepared.registry_dir, '--workspace-id', prepared.workspace_id,
    '--workspace-files', '--timeout', '10s', '--read-root', 'docs'];
  const resourceClient = await connect([...base, '--resources-only']);
  assert.equal((await bounded(resourceClient.listTools(), 'list tools')).tools.length, 0);
  assert.equal((await bounded(resourceClient.listPrompts(), 'list prompts')).prompts.length, 0);
  const overviewURI = (await bounded(resourceClient.listResources(), 'list resources')).resources[0].uri;
  const overview = JSON.parse((await bounded(resourceClient.readResource({ uri: overviewURI }), 'read overview')).contents[0].text);
  const tree = JSON.parse((await bounded(resourceClient.readResource({ uri: overview.paged_root_uri }), 'read root tree')).contents[0].text);
  const docs = tree.entries.find(entry => entry.name === 'docs');
  assert.ok(docs?.uri, 'advertised root tree must include docs');
  const docsPage = JSON.parse((await bounded(resourceClient.readResource({ uri: docs.uri }), 'read docs tree')).contents[0].text);
  const file = docsPage.entries.find(entry => entry.name === 'state.txt');
  assert.ok(file?.uri, 'advertised docs tree must include state.txt');
  assert.equal((await bounded(resourceClient.readResource({ uri: file.uri }), 'read source file')).contents[0].text, original);
  await close(clients.pop());

  const editClient = await connect([...base, '--workspace-edit', '--write-root', 'docs', '--coordination', 'host_quiesced']);
  const basis = payload(await bounded(editClient.callTool({ name: 'stow_workspace_file_read_for_save', arguments: { path: 'docs/state.txt' } }), 'observe file'));
  assert.equal(basis.outcome, 'observed');
  assert.equal(Buffer.from(basis.data_base64, 'base64').toString(), original);
  const key = randomUUID();
  const result = payload(await bounded(editClient.callTool({ name: 'stow_workspace_file_save', arguments: {
    path: 'docs/state.txt', text: saved, request_key: key, observation_token: basis.observation_token,
  } }), 'save file'));
  assert.equal(result.outcome, 'committed');
  assert.ok(result.generation);
  const resolved = payload(await bounded(editClient.callTool({ name: 'stow_workspace_file_resolve_save', arguments: { path: 'docs/state.txt', request_key: key } }), 'resolve save'));
  assert.equal(resolved.generation, result.generation);
  assert.equal(await readFile(join(workspaceRoot, 'docs/state.txt'), 'utf8'), saved);
  await Promise.all(clients.splice(0).map(close));

  const { stdout } = await bounded(exec(binary, ['workspace', 'checkpoint', '--registry-dir', prepared.registry_dir,
    '--id', prepared.workspace_id, '--portable', '--request-key', randomUUID(), '--timeout', '30s'], { cwd: workspaceRoot, timeout: deadline }), 'checkpoint capture');
  const checkpoint = JSON.parse(stdout);
  assert.ok(checkpoint.checkpoint_id);
  const changed = 'live bytes after checkpoint\n';
  await writeFile(join(workspaceRoot, 'docs/state.txt'), changed);
  const checkpointClient = await connect([...base, '--resources-only', '--checkpoint-id', checkpoint.checkpoint_id]);
  assert.equal((await bounded(checkpointClient.listTools(), 'checkpoint list tools')).tools.length, 0);
  assert.equal((await bounded(checkpointClient.listPrompts(), 'checkpoint list prompts')).prompts.length, 0);
  const cpOverviewURI = (await bounded(checkpointClient.listResources(), 'checkpoint list resources')).resources[0].uri;
  const cpOverview = JSON.parse((await bounded(checkpointClient.readResource({ uri: cpOverviewURI }), 'read checkpoint overview')).contents[0].text);
  const savedTree = JSON.parse((await bounded(checkpointClient.readResource({ uri: cpOverview.root_uri }), 'read checkpoint root')).contents[0].text);
  const savedFile = savedTree.entries.find(entry => entry.name === 'docs');
  const savedDocs = JSON.parse((await bounded(checkpointClient.readResource({ uri: savedFile.uri }), 'read checkpoint docs')).contents[0].text);
  const savedEntry = savedDocs.entries.find(entry => entry.name === 'state.txt');
  assert.equal((await bounded(checkpointClient.readResource({ uri: savedEntry.uri }), 'read saved file')).contents[0].text, saved);
  assert.notEqual(await readFile(join(workspaceRoot, 'docs/state.txt'), 'utf8'), saved);
} finally {
  await Promise.all(clients.splice(0).map(close));
  await rm(temp, { recursive: true, force: true });
}
console.log('Installed native carrier MCP resources, editing, and checkpoint passed');

function payload(response) {
  assert.notEqual(response.isError, true, JSON.stringify(response));
  return response.structuredContent ?? JSON.parse(response.content.find(item => item.type === 'text').text);
}
async function close({ client, transport }) {
  await Promise.allSettled([client.close(), transport.close()]);
}
