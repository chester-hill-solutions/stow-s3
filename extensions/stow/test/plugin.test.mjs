import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';

test('configured plugin retains exact literal scope and read-only flags', t => {
  const root = mkdtempSync(join(tmpdir(), 'stow-plugin-test-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const binary = join(root, 'stow binary');
  const directory = join(root, 'owned store');
  const output = join(root, 'plugin');
  writeFileSync(binary, '#!/bin/sh\nexit 0\n', { mode: 0o755 });
  mkdirSync(directory);
  const helper = fileURLToPath(new URL('../configure-plugin.mjs', import.meta.url));
  execFileSync(process.execPath, [helper, binary, directory, 'objects', output]);
  const config = JSON.parse(readFileSync(join(output, 'mcp.json'), 'utf8'));
  assert.equal(config.mcpServers.stow.type, 'stdio');
  assert.equal(config.mcpServers.stow.command, binary);
  assert.deepEqual(config.mcpServers.stow.args, ['mcp', '--object-dir', directory, '--bucket', 'objects', '--browser', '--read-only']);
  const original = readFileSync(join(output, 'mcp.json'), 'utf8');
  assert.throws(() => execFileSync(process.execPath, [helper, binary, directory, 'objects', output], { stdio: 'pipe' }));
  assert.equal(readFileSync(join(output, 'mcp.json'), 'utf8'), original);
  const manifest = JSON.parse(readFileSync(join(output, 'plugin.json'), 'utf8'));
  assert.deepEqual(manifest.extensions['com.openai'].interface.capabilities, ['Read']);
  assert.match(readFileSync(join(output, 'skills/stow-storage/SKILL.md'), 'utf8'), /read-only/);
});
