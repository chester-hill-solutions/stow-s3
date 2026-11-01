import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';

const root = fileURLToPath(new URL('..', import.meta.url));
const extensionRequire = createRequire(new URL('../package.json', import.meta.url));
const lintRequire = createRequire(new URL('../../../packages/stow-s3/package.json', import.meta.url));
const cssModule = '@openai/mcp-extensions/app/styles.css';

test('side-effect checking accepts only the declared CSS module', async (t) => {
  const fixture = await mkdtemp(join(tmpdir(), 'stow-css-import-'));
  t.after(() => rm(fixture, { recursive: true, force: true }));
  await writeFile(join(fixture, 'styles.d.ts'), await readFile(join(root, 'src/styles.d.ts')));
  await writeFile(join(fixture, 'tsconfig.json'), JSON.stringify({
    extends: join(root, 'tsconfig.json'), include: ['styles.d.ts', 'entry.ts'],
  }));
  for (const [name, loader, flags] of [
    ['native', extensionRequire, []],
    ['classic with side-effect checking', lintRequire, ['--noUncheckedSideEffectImports']],
  ]) {
    const compiler = join(dirname(loader.resolve('typescript/package.json')), 'bin/tsc');
    for (const [label, source, rejected] of [
      ['real stylesheet', `import '${cssModule}';`, null],
      ['misspelled stylesheet', "import '@openai/mcp-extensions/app/style.css';", 'style.css'],
      ['missing script', "import '@openai/mcp-extensions/app/missing.js';", 'missing.js'],
      ['nonexistent CSS export', `import { imaginaryClass } from '${cssModule}'; void imaginaryClass;`, 'imaginaryClass'],
    ]) {
      await t.test(`${name}: ${label}`, async () => {
        await writeFile(join(fixture, 'entry.ts'), source);
        const result = spawnSync(process.execPath, [compiler, '--project', join(fixture, 'tsconfig.json'), '--pretty', 'false', ...flags], {
          cwd: root, encoding: 'utf8', timeout: 30000,
        });
        assert.equal(result.error, undefined);
        const output = result.stdout + result.stderr;
        if (rejected) {
          assert.notEqual(result.status, 0, output);
          assert.ok(output.includes(rejected), output);
        } else assert.equal(result.status, 0, output);
      });
    }
  }
});

test('the exported stylesheet contributes CSS to the actual browser bundle', async () => {
  const stylesheet = extensionRequire.resolve(cssModule);
  assert.ok((await readFile(stylesheet, 'utf8')).length > 0);
  const result = await build({
    absWorkingDir: root, entryPoints: ['src/app.ts'], bundle: true, minify: true,
    format: 'esm', platform: 'browser', target: 'es2022', write: false, outfile: 'app.js', metafile: true,
  });
  const css = Object.entries(result.metafile.outputs).find(([file]) => file.endsWith('.css'));
  assert.ok(css);
  const contribution = Object.entries(css[1].inputs).find(([input]) => resolve(root, input) === stylesheet);
  assert.ok(contribution && contribution[1].bytesInOutput > 0);
  assert.ok(result.outputFiles.some(file => file.path.endsWith('.css') && file.contents.length > 0));
});

test('shared lint tooling uses its existing compiler API rather than native TypeScript', async () => {
  const parserRequire = createRequire(lintRequire.resolve('@typescript-eslint/typescript-estree'));
  assert.notEqual(parserRequire.resolve('typescript'), extensionRequire.resolve('typescript'));
  assert.equal(typeof parserRequire('typescript').createProgram, 'function');
  assert.equal(typeof extensionRequire('typescript').createProgram, 'undefined');
  const { ESLint } = lintRequire('eslint');
  const [result] = await new ESLint({ cwd: root }).lintText('const value: string = "checked";\nexport { value };\n', {
    filePath: 'src/compiler-probe.ts',
  });
  assert.equal(result.errorCount, 0);
});
