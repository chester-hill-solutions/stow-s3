import { accessSync, constants, cpSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { isAbsolute, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const [binary, directory, bucket, output] = process.argv.slice(2);
if (!binary || !directory || !output || !isAbsolute(binary) || !isAbsolute(directory) || !isAbsolute(output) ||
    !/^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$/.test(bucket ?? '')) {
  throw new Error('Usage: node configure-plugin.mjs /absolute/stow-s3 /absolute/owned-object-dir bucket /absolute/new-plugin-dir');
}
accessSync(binary, constants.X_OK);
accessSync(directory, constants.R_OK);
mkdirSync(output); // An existing destination is never overwritten.
const template = fileURLToPath(new URL('./plugin/', import.meta.url));
// The template's children are copied into the fresh directory. Copying the
// template onto the directory would copy onto an existing destination, which
// Node 24.22 refuses when errorOnExist is set. Each child is copied onto a
// target that does not exist yet, so the copy never overwrites anything.
for (const entry of readdirSync(template)) {
  cpSync(join(template, entry), join(output, entry), { recursive: true, force: false, errorOnExist: true });
}
const path = resolve(output, 'mcp.json');
const config = JSON.parse(readFileSync(path, 'utf8'));
config.mcpServers.stow.command = binary;
config.mcpServers.stow.args = ['mcp', '--object-dir', directory, '--bucket', bucket, '--browser', '--read-only'];
writeFileSync(path, `${JSON.stringify(config, null, 2)}\n`);
console.log(`Configured read-only Stow plugin: ${output}`);
