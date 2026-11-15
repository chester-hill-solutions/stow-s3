import { build } from 'esbuild';
import { mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
const root = fileURLToPath(new URL('.', import.meta.url));
const cssTemp = mkdtempSync(join(tmpdir(), 'stow-workspace-css-'));
let workspaceStyle;
try {
  execFileSync(process.execPath, [
    `${root}node_modules/@tailwindcss/cli/dist/index.mjs`,
    '--input', 'src/workspace/theme.css', '--output', `${cssTemp}/workspace.css`, '--minify',
  ], { cwd: root });
  workspaceStyle = readFileSync(`${cssTemp}/workspace.css`, 'utf8');
} finally { rmSync(cssTemp, { recursive: true, force: true }); }
const result = await build({ absWorkingDir: root, entryPoints: ['src/app.ts'], bundle: true, minify: true, format: 'esm', platform: 'browser', target: 'es2022', write: false, outfile: 'app.js', metafile: true });
const script = result.outputFiles.find(file => file.path.endsWith('.js')).text.replaceAll('</script', '<\\/script');
const style = result.outputFiles.find(file => file.path.endsWith('.css'))?.text ?? '';
const packages = new Set();
for (const input of Object.keys(result.metafile.inputs)) {
  const includedBytes = Object.values(result.metafile.outputs).reduce((total, output) => total + (output.inputs[input]?.bytesInOutput ?? 0), 0);
  if (includedBytes === 0) continue;
  const match = [...input.matchAll(/(?:^|\/)node_modules\/((?:@[^/]+\/)?[^/]+)/g)].at(-1);
  if (match) packages.add(`${input.slice(0, match.index)}${match[0].startsWith('/') ? '/' : ''}node_modules/${match[1]}`);
}
packages.add('node_modules/tailwindcss');
const dependencyNotices = [...packages].sort().map(path => {
  const metadata = JSON.parse(readFileSync(`${root}${path}/package.json`, 'utf8'));
  const texts = readdirSync(`${root}${path}`).filter(name => /^licen[cs]e/i.test(name) && statSync(`${root}${path}/${name}`).isFile())
    .sort().map(name => readFileSync(`${root}${path}/${name}`, 'utf8'));
  if (!texts.length) throw new Error(`Missing license text for bundled dependency ${metadata.name}`);
  return `${metadata.name} ${metadata.version}\n\n${texts.join('\n')}\n`;
}).join('\n');
const notices = `${dependencyNotices}\nChester Hill Solutions UI Kit vendored sources 0.9.1\n\n${readFileSync(`${root}src/workspace/CHS_UI_KIT_LICENSE.txt`, 'utf8')}`;
const html = readFileSync(`${root}src/index.html`, 'utf8').replace('<!-- SDK_STYLE -->', `<style>${style}</style>`)
  .replace('<!-- WORKSPACE_STYLE -->', `<style>${workspaceStyle}</style>`)
  .replace('<!-- SDK_SCRIPT -->', () => `<script type="module">${script}</script><!-- Bundled dependency notices\n${notices.replaceAll('-->', '-- >')}\n-->`);
const compressed = execFileSync('go', ['run', './tools/mcp-browser-pack'], {
  cwd: `${root}../..`, input: html, maxBuffer: 4 << 20,
  env: { ...process.env, GOTOOLCHAIN: `go${readFileSync(`${root}../../.go-version`, 'utf8').trim()}` },
});
if (html.length > 2 * 1024 * 1024 || compressed.length > 512 * 1024) {
  throw new Error(`Browser bundle exceeds its budget (${html.length} HTML bytes, ${compressed.length} gzip bytes).`);
}
const license = readFileSync(`${root}../../LICENSE`, 'utf8');
if (process.argv.includes('--check')) {
  if (!readFileSync(`${root}../../internal/mcpstorage/browser.html.gz`).equals(compressed)) throw new Error('Browser HTML differs from a fresh build. Run npm run build.');
  if (readFileSync(`${root}plugin/THIRD_PARTY_LICENSES.txt`, 'utf8') !== notices) throw new Error('Bundled license notices differ from a fresh build.');
  if (readFileSync(`${root}plugin/LICENSE`, 'utf8') !== license) throw new Error('Plugin license differs from the repository license.');
} else {
  writeFileSync(`${root}../../internal/mcpstorage/browser.html.gz`, compressed);
  writeFileSync(`${root}plugin/THIRD_PARTY_LICENSES.txt`, notices);
  writeFileSync(`${root}plugin/LICENSE`, license);
}
