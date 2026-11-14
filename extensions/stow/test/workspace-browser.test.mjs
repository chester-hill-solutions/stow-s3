/* global document, getComputedStyle, location, window */
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { existsSync, readFileSync } from 'node:fs';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { gunzipSync } from 'node:zlib';
import { test } from 'node:test';
import { build } from 'esbuild';
import { chromium } from 'playwright-core';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const browserPaths = [process.env.CHROME_BIN, process.env.CHROME_PATH,
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome', '/usr/bin/google-chrome',
  '/usr/bin/chromium', '/usr/bin/chromium-browser'].filter(Boolean);
const browserPath = browserPaths.find(path => existsSync(path));

test('generated workspace view navigates through an SDK bridge and survives keyboard and 200% zoom', {
  skip: browserPath ? false : 'No system Chrome or Chromium was found for rendered browser verification.',
}, async t => {
  const screenshots = await mkdtemp(join(tmpdir(), 'stow-workspace-browser-'));
  t.after(() => rm(screenshots, { recursive: true, force: true }));
  const built = await build({ absWorkingDir: root, entryPoints: ['test/workspace-host-fixture.ts'], bundle: true,
    format: 'esm', platform: 'browser', target: 'es2022', write: false });
  const hostScript = built.outputFiles[0].text;
  const generatedHtml = gunzipSync(readFileSync(`${root}../../internal/mcpstorage/browser.html.gz`)).toString('utf8');
  const bootstrap = '<script type="application/json" id="stow-bootstrap">{"mode":"workspace","overview_uri":"stow://workspaces/ui-test/v1"}</script>';
  const workspaceHtml = generatedHtml.replace('</head>', `${bootstrap}</head>`)
    .replace('<script type="module">', '<script type="module">document.documentElement.dataset.testAppExecuting = "true";');
  assert.notEqual(workspaceHtml, generatedHtml, 'bootstrap mount point must exist in the generated HTML');
  assert.ok(workspaceHtml.includes('dataset.testAppExecuting'), 'inline test instrumentation must be inserted');
  const hostHtml = '<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Workspace host fixture</title><style>html,body{margin:0;width:100%;height:100%;}iframe{width:100%;height:100%;border:0}</style></head><body><iframe id="workspace-frame" title="Stow workspace view"></iframe><script type="module" src="/host.js"></script></body></html>';
  const server = createServer((request, response) => {
    if (request.url === '/host.js') { response.writeHead(200, { 'content-type': 'text/javascript' }); response.end(hostScript); return; }
    if (request.url === '/workspace.html') { response.writeHead(200, { 'content-type': 'text/html' }); response.end(workspaceHtml); return; }
    response.writeHead(200, { 'content-type': 'text/html' }); response.end(hostHtml);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Could not start the local workspace host.');
  const browser = await chromium.launch({ headless: true, executablePath: browserPath, args: ['--no-sandbox'] });
  t.after(async () => { await browser.close(); await new Promise(resolve => server.close(resolve)); });
  const page = await browser.newPage({ viewport: { width: 1100, height: 850 } });
  const errors = [];
  const logs = [];
  page.on('pageerror', error => errors.push(error.stack ?? error.message));
  page.on('console', message => { if (message.type() === 'error') logs.push(message.text()); });
  await page.goto(`http://127.0.0.1:${address.port}/`);
  const frame = page.frameLocator('#workspace-frame');
  try { await frame.getByRole('heading', { name: 'Workspace files' }).waitFor({ timeout: 7000 }); }
  catch (error) {
    const diagnostics = await Promise.all(page.frames().map(framePage => framePage.evaluate(() => ({ url: location.href, title: document.title, ready: document.body.dataset.bridgeReady, connected: document.body.dataset.connected, bridgeError: document.body.dataset.bridgeError, lastMessage: document.body.dataset.lastMessage, appExecuting: document.documentElement.dataset.testAppExecuting, bootstrap: document.querySelector('#stow-bootstrap')?.textContent, scripts: [...document.scripts].map(script => script.src || script.type), workspaceHidden: document.querySelector('#workspace-root')?.hidden, legacyHidden: document.querySelector('#legacy-app')?.hidden, context: window.__workspaceContext, body: document.body.innerText.slice(0, 400) })).catch(() => undefined)));
    throw new Error(`${String(error)}\nPage errors: ${errors.join('; ')}\nConsole: ${logs.join('; ')}\nFrames: ${JSON.stringify(diagnostics)}`, { cause: error });
  }
  await frame.getByText('3 items shown. Read-only workspace.').waitFor();
  await frame.getByText('Workspace ID: ui-test · Read roots: .', { exact: true }).waitFor();
  await page.screenshot({ path: join(screenshots, 'normal.png') });
  await page.setViewportSize({ width: 320, height: 720 });

  await frame.locator('body').evaluate(element => element.focus());
  await page.frames()[1].locator('body').press('Tab');
  const focused = await page.frames()[1].evaluate(() => ({ tag: document.activeElement?.tagName, visible: document.activeElement?.matches(':focus-visible') }));
  assert.equal(focused.tag, 'BUTTON');
  assert.equal(focused.visible, true);

  await frame.getByRole('button', { name: /readme\.txt/ }).click();
  await frame.getByText('safe text <script>not markup</script>', { exact: true }).waitFor();
  assert.equal(await frame.locator('.workspace-file-text script').count(), 0, 'file text must never be inserted as markup');
  await frame.getByRole('checkbox', { name: 'Attach readme.txt to composer' }).press('Space');
  await page.waitForFunction(() => window.__workspaceContext?.some(content => content.type === 'resource_link'));
  const context = await page.evaluate(() => window.__workspaceContext);
  assert.equal(context[0].text, 'preserve this host context');
  assert.equal(context[1].uri, 'stow://workspaces/ui-test/files/readme');
  await frame.getByRole('button', { name: /empty\.txt/ }).click();
  await frame.getByText('This file is empty.').waitFor();
  await frame.getByRole('button', { name: 'Refresh' }).click();
  await frame.getByText('Choose a file to read its current contents.').waitFor();

  await frame.getByRole('button', { name: /docs/ }).click();
  await page.waitForFunction(() => document.querySelector('#workspace-frame')?.contentDocument?.activeElement?.tagName === 'H1');
  await frame.getByRole('link', { name: 'Workspace' }).click();
  await frame.getByRole('button', { name: /Next page/ }).click();
  await frame.getByRole('button', { name: /z-last\.txt/ }).click();
  await frame.getByText('This file is binary or HTML. Attach its reference to use it in the composer.').waitFor();
  await frame.getByRole('button', { name: 'Back' }).click();
  await frame.getByRole('button', { name: /docs/ }).click();
  await frame.getByRole('button', { name: /guide\.txt/ }).click();
  await frame.getByText('guide', { exact: true }).waitFor();

  await frame.getByRole('button', { name: 'Back' }).click();
  await frame.getByRole('button', { name: 'Refresh' }).click();
  await frame.getByRole('button', { name: /readme\.txt/ }).waitFor();
  await frame.getByRole('button', { name: /docs/ }).click();
  await frame.getByRole('button', { name: /guide\.txt/ }).click();
  await frame.getByText('guide', { exact: true }).waitFor();
  await frame.getByRole('link', { name: 'Workspace' }).click();
  await frame.getByRole('button', { name: /readme\.txt/ }).waitFor();
  await page.frames()[1].evaluate(() => { document.documentElement.style.zoom = '200%'; });
  await page.frames()[1].evaluate(() => {
    const content = document.querySelector('.workspace-content');
    if (content) window.scrollTo(0, content.getBoundingClientRect().top + window.scrollY);
  });
  await page.waitForFunction(() => document.querySelector('#workspace-frame')?.contentWindow?.scrollY && document.querySelector('#workspace-frame')?.contentWindow?.scrollY > 0);
  const dimensions = await page.frames()[1].evaluate(() => ({
    width: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
    columns: getComputedStyle(document.querySelector('.workspace-content')).gridTemplateColumns,
    overflow: [...document.querySelectorAll('body *')].map(element => ({ selector: `${element.tagName}.${element.className?.baseVal ?? element.className ?? ''}`, right: Math.round(element.getBoundingClientRect().right), width: Math.round(element.getBoundingClientRect().width) })).filter(element => element.right > document.documentElement.clientWidth + 2).slice(0, 10),
    clipped: [...document.querySelectorAll('.workspace-navigation [data-slot="breadcrumb-item"]')].some(element => element.scrollWidth > element.clientWidth + 1),
  }));
  assert.ok(dimensions.scrollWidth <= dimensions.width, `horizontal overflow at narrow 200% zoom: ${JSON.stringify(dimensions)}`);
  assert.equal(dimensions.clipped, false, 'breadcrumb labels must wrap instead of clipping at narrow 200% zoom');
  assert.ok(dimensions.scrollWidth <= dimensions.width, 'vertical zoom must leave the view scrollable');
  await page.screenshot({ path: join(screenshots, '200-zoom.png') });
  await page.frames()[1].evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await page.waitForFunction(() => document.querySelector('#workspace-frame')?.contentWindow?.scrollY && document.querySelector('#workspace-frame')?.contentWindow?.scrollY > 0);
  await page.screenshot({ path: join(screenshots, '200-zoom-preview.png') });
  assert.equal(errors.length, 0, errors.join('\n'));
});
