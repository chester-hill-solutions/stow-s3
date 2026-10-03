import { build } from 'esbuild';
import { readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
const root = fileURLToPath(new URL('.', import.meta.url));
const result = await build({ absWorkingDir: root, entryPoints: ['src/app.ts'], bundle: true, minify: true, format: 'esm', platform: 'browser', target: 'es2022', write: false, outfile: 'app.js', metafile: true });
const script = result.outputFiles.find(file => file.path.endsWith('.js')).text.replaceAll('</script', '<\\/script');
const style = result.outputFiles.find(file => file.path.endsWith('.css'))?.text ?? '';
const packages = new Set();
for (const input of Object.keys(result.metafile.inputs)) {
  const match = [...input.matchAll(/(?:^|\/)node_modules\/((?:@[^/]+\/)?[^/]+)/g)].at(-1);
  if (match) packages.add(`${input.slice(0, match.index)}${match[0].startsWith('/') ? '/' : ''}node_modules/${match[1]}`);
}
const notices = [...packages].sort().map(path => {
  const metadata = JSON.parse(readFileSync(`${root}${path}/package.json`, 'utf8'));
  const texts = readdirSync(`${root}${path}`).filter(name => /^licen[cs]e/i.test(name) && statSync(`${root}${path}/${name}`).isFile())
    .sort().map(name => readFileSync(`${root}${path}/${name}`, 'utf8'));
  if (!texts.length) throw new Error(`Missing license text for bundled dependency ${metadata.name}`);
  return `${metadata.name} ${metadata.version}\n\n${texts.join('\n')}\n`;
}).join('\n');
const html = readFileSync(`${root}src/index.html`, 'utf8').replace('<!-- SDK_STYLE -->', `<style>${style}</style>`)
  .replace('<!-- SDK_SCRIPT -->', `<script type="module">${script}</script><!-- Bundled dependency notices\n${notices.replaceAll('-->', '-- >')}\n-->`);
const compressed = execFileSync('go', ['run', './tools/mcp-browser-pack'], {
  cwd: `${root}../..`, input: html, maxBuffer: 4 << 20,
  env: { ...process.env, GOTOOLCHAIN: `go${readFileSync(`${root}../../.go-version`, 'utf8').trim()}` },
});
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
