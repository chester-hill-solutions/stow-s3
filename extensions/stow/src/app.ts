import { App, applyDocumentTheme, applyHostStyleVariables } from '@modelcontextprotocol/ext-apps';
import { OpenAIExtensions } from '@openai/mcp-extensions/app';
import '@openai/mcp-extensions/app/styles.css';

type Item = { type: 'resource_link'; uri: string; name: string; mimeType: string; size: number };
type Page = { bucket: string; items: Item[]; truncated: boolean; next_cursor?: string; max_bytes: number };
const app = new App({ name: 'Stow bucket browser', version: '0.3.0' });
const extensions = new OpenAIExtensions(app);
function element<T extends Element>(selector: string): T {
  const found = document.querySelector<T>(selector);
  if (!found) throw new Error(`Missing browser element: ${selector}`);
  return found;
}
const prefix = element<HTMLInputElement>('#prefix');
const rows = element<HTMLUListElement>('#objects');
const status = element<HTMLElement>('#status');
const preview = element<HTMLElement>('#preview');
const next = element<HTMLButtonElement>('#next');
const selected = new Map<string, Item>();
let page: Page | undefined;
let sequence = 0;
let previewSequence = 0;
let attachmentBusy = false;

function message(text: string) { status.textContent = text; }
function parsePage(value: unknown): Page {
  const data = value as Partial<Page> | undefined;
  if (!data || typeof data.bucket !== 'string' || !Array.isArray(data.items) || data.items.length > 50 ||
      !data.items.every(item => item.type === 'resource_link' && typeof item.uri === 'string' && typeof item.name === 'string') ||
      typeof data.max_bytes !== 'number') throw new Error('The browser response could not be read.');
  return data as Page;
}

function render(value: unknown) {
  page = parsePage(value);
  element('#bucket').textContent = page.bucket;
  rows.replaceChildren();
  for (const item of page.items) {
    const row = document.createElement('li');
    const open = document.createElement('button');
    open.className = 'object cursor-interaction';
    open.textContent = `${item.name} · ${item.size.toLocaleString()} bytes`;
    open.addEventListener('click', () => { void showPreview(item); });
    const attach = document.createElement('input');
    attach.type = 'checkbox';
    attach.checked = selected.has(item.uri);
    attach.disabled = !extensions.modelContext || attachmentBusy;
    attach.setAttribute('aria-label', `Attach ${item.name} to composer context`);
    attach.addEventListener('change', () => { void changeAttachment(item, attach.checked); });
    row.append(open, attach);
    rows.append(row);
  }
  next.disabled = !page.truncated || !page.next_cursor;
  message(`${page.items.length} objects. Previews are limited to ${page.max_bytes.toLocaleString()} bytes.${extensions.modelContext ? ' Select a checkbox to attach a reference to the composer.' : ' Composer attachments are unavailable in this host.'}`);
}

async function load(cursor = '') {
  const current = ++sequence;
  message('Loading objects…');
  next.disabled = true;
  try {
    const result = await app.callServerTool({ name: 'stow_browser', arguments: { prefix: prefix.value, cursor } });
    if (current !== sequence) return;
    if (result.isError) throw new Error('Object listing is unavailable.');
    render(result.structuredContent);
  } catch (error) { if (current === sequence) message(error instanceof Error ? error.message : 'Object listing is unavailable.'); }
}

async function showPreview(item: Item) {
  const current = ++previewSequence;
  preview.textContent = `Loading ${item.name}…`;
  try {
    const result = await app.readServerResource({ uri: item.uri });
    if (current !== previewSequence) return;
    const content = result.contents[0];
    if (item.size === 0) { preview.textContent = `${item.name}\n\n(empty object)`; return; }
    preview.textContent = content && 'text' in content && typeof content.text === 'string'
      ? `${item.name}\n\n${content.text}`
      : `${item.name}\n\nBinary object. Attach its resource reference to the composer to use it.`;
  } catch { if (current === previewSequence) preview.textContent = `${item.name}\n\nPreview unavailable: the object may be missing or exceed the preview limit.`; }
}

function reconcileAttachments() {
  const context = extensions.modelContext?.getCurrent();
  if (context === undefined) return;
  selected.clear();
  for (const content of context?.content ?? []) {
    if (content.type === 'resource_link') selected.set(content.uri, content as Item);
  }
  if (page) render(page);
}

async function changeAttachment(item: Item, checked: boolean) {
  const context = extensions.modelContext;
  if (!context || attachmentBusy) return;
  reconcileAttachments();
  const updated = new Map(selected);
  if (checked) updated.set(item.uri, item); else updated.delete(item.uri);
  if (updated.size > 10) { if (page) render(page); message('Attach up to 10 object references at a time.'); return; }
  attachmentBusy = true;
  if (page) render(page);
  try {
    await context.update({ content: [...updated.values()] });
    reconcileAttachments();
  } catch { attachmentBusy = false; if (page) render(page); message('The reference could not be attached.'); return; }
  finally { attachmentBusy = false; }
  if (page) render(page);
}

function applyHostContext() {
  const context = app.getHostContext();
  if (context?.theme) applyDocumentTheme(context.theme);
  if (context?.styles?.variables) applyHostStyleVariables(context.styles.variables);
  reconcileAttachments();
}
element('#search').addEventListener('submit', event => { event.preventDefault(); void load(); });
next.addEventListener('click', () => { void load(page?.next_cursor); });
app.addEventListener('hostcontextchanged', applyHostContext);
app.ontoolresult = result => { try { render(result.structuredContent); } catch { message('Object listing is unavailable.'); } };
await app.connect();
applyHostContext();
if (page) render(page); else await load();
