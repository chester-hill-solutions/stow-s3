import { BrowserFileHistory, IndexedDbPersistenceAdapter, openBrowserEmbeddedStow } from "/runtime/browser.js";
import { seeds } from "./seeds.mjs";

const $ = (id) => document.getElementById(id);
const encoder = new TextEncoder();
const decoder = new TextDecoder();
const statusOf = new Map();
const bytesOf = new Map();
const IMAGES_BUCKET = "cookbook-images";
const CATALOG_BUCKET = "cookbook-meta";
const CATALOG_KEY = "catalog.json";
const MAX_IMAGE_BYTES = 5 * 1024 * 1024;

const devCalls = [];
let store;
let devOpen = false;
let images = [];
let files;
let currentKey = seeds[0].key;
let currentMarkdown = "";
let view = "card";
let jobs = Promise.resolve();
let unsaved = false;
let pending = 0;

function message(text, isError = false) {
  $("message").textContent = text;
  $("message-wrap").hidden = !text;
  $("message-wrap").classList.toggle("error", isError);
  $("retry").hidden = !isError;
}

function report(error) {
  message(error.message ?? String(error), true);
}

function queue(operation) {
  const task = jobs.then(operation);
  jobs = task.catch(report);
  return task;
}

function escapeHtml(value) {
  return value.replace(/[&<>"']/g, (char) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  })[char]);
}

function parseRecipe(markdown) {
  const lines = markdown.split("\n");
  const title = lines.find((line) => line.startsWith("# "))?.slice(2).trim() ?? "";
  const metaIndex = lines.findIndex((line) => line.trim() && !line.startsWith("#"));
  const meta = metaIndex >= 0 ? lines[metaIndex].trim() : "";
  let section = "";
  const ingredients = [];
  const steps = [];
  for (const line of lines) {
    if (line.startsWith("## Ingredients")) { section = "i"; continue; }
    if (line.startsWith("## Method")) { section = "m"; continue; }
    const text = line.trim();
    if (!text || text.startsWith("#")) continue;
    if (section === "i" && text.startsWith("- ")) ingredients.push(text.slice(2).trim());
    else if (section === "m" && /^\d+\.\s/.test(text)) steps.push(text.replace(/^\d+\.\s/, "").trim());
  }
  return { title, meta, ingredients, steps };
}

function serializeRecipe(recipe) {
  return [
    `# ${recipe.title}`,
    "",
    recipe.meta,
    "",
    "## Ingredients",
    ...recipe.ingredients.map((item) => `- ${item}`),
    "",
    "## Method",
    ...recipe.steps.map((step, index) => `${index + 1}. ${step}`),
    "",
  ].join("\n");
}

function renderCard() {
  const recipe = parseRecipe(currentMarkdown);
  $("recipe-title").textContent = recipe.title || "Untitled recipe";
  $("recipe-meta").textContent = recipe.meta;
  $("ingredient-list").replaceChildren(...recipe.ingredients.map((item) => {
    const li = document.createElement("li");
    li.textContent = item;
    return li;
  }));
  $("method-list").replaceChildren(...recipe.steps.map((step) => {
    const li = document.createElement("li");
    const span = document.createElement("span");
    span.textContent = step;
    li.append(span);
    return li;
  }));
}

function fillEditForm() {
  const recipe = parseRecipe(currentMarkdown);
  $("edit-title").value = recipe.title;
  $("edit-meta").value = recipe.meta;
  $("edit-ingredients").value = recipe.ingredients.join("\n");
  $("edit-method").value = recipe.steps.join("\n");
}

function collectEditForm() {
  return serializeRecipe({
    title: $("edit-title").value.trim() || "Untitled recipe",
    meta: $("edit-meta").value.trim(),
    ingredients: $("edit-ingredients").value.split("\n").map((line) => line.trim()).filter(Boolean),
    steps: $("edit-method").value.split("\n").map((line) => line.trim()).filter(Boolean),
  });
}

function diffLines(before, after) {
  const a = before.split("\n");
  const b = after.split("\n");
  const dp = Array.from({ length: a.length + 1 }, () => new Uint16Array(b.length + 1));
  for (let i = a.length - 1; i >= 0; i -= 1) {
    for (let j = b.length - 1; j >= 0; j -= 1) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const output = [];
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) { output.push([" ", a[i]]); i += 1; j += 1; }
    else if (dp[i + 1][j] >= dp[i][j + 1]) { output.push(["-", a[i]]); i += 1; }
    else { output.push(["+", b[j]]); j += 1; }
  }
  while (i < a.length) { output.push(["-", a[i]]); i += 1; }
  while (j < b.length) { output.push(["+", b[j]]); j += 1; }
  return output;
}

function renderComparison(version) {
  const rows = diffLines(decoder.decode(bytesOf.get(version.id) ?? new Uint8Array()), currentMarkdown);
  $("diff-title").textContent = `Compare “${version.label ?? "saved edition"}”`;
  const changed = rows.filter(([sign]) => sign !== " ").length;
  $("diff-summary").textContent = changed
    ? `${changed} changed lines against the current card.`
    : "Identical to the current card.";
  $("diff").replaceChildren(...rows.map(([sign, text]) => {
    const row = document.createElement("div");
    row.className = `diff-row ${sign === "+" ? "added" : sign === "-" ? "removed" : "same"}`;
    row.textContent = `${sign} ${text}`;
    return row;
  }));
  $("diff-panel").hidden = false;
  $("diff-panel").open = true;
}

function renderEditions(status) {
  const versions = status.versions;
  $("edition-count").textContent = versions.length;
  $("edition-noun").textContent = versions.length === 1 ? " edition retained" : " editions retained";
  $("history").replaceChildren(...versions.toReversed().map((version) => {
    const item = document.createElement("li");
    if (version.id === status.currentVersionId) item.className = "current";
    const button = document.createElement("button");
    button.type = "button";
    button.innerHTML = `<strong>${escapeHtml(version.label ?? "Recorded edition")}</strong><small>${new Date(version.createdAt).toLocaleString()} · ${version.id.slice(0, 8)}</small>`;
    button.addEventListener("click", () => renderComparison(version));
    item.append(button);
    return item;
  }));
  $("undo").disabled = !status.canUndo;
  $("redo").disabled = !status.canRedo;
}

function renderIndex() {
  const query = $("search").value.trim().toLowerCase();
  const entries = seeds
    .map((seed, index) => ({ seed, index, status: statusOf.get(seed.key) }))
    .filter(({ seed, status }) => !query
      || seed.key.toLowerCase().includes(query)
      || (status?.versions.at(-1)?.label ?? "").toLowerCase().includes(query));

  $("no-results").hidden = entries.length > 0;
  $("recipe-list").replaceChildren(...entries.map(({ seed, index, status }) => {
    const item = document.createElement("li");
    const button = document.createElement("button");
    button.type = "button";
    if (seed.key === currentKey) {
      button.className = "current";
      button.setAttribute("aria-current", "page");
    }
    const title = seed.key.split("/").at(-1).replace(/\.md$/, "").replaceAll("-", " ");
    const count = status?.versions.length ?? 0;
    const latest = status?.versions.at(-1);
    button.innerHTML = [
      `<span class="index-number">${String(index + 1).padStart(2, "0")}</span>`,
      `<span class="index-name">${escapeHtml(title)}</span>`,
      `<span class="index-count">${count}</span>`,
      `<span class="index-meta">${count === 1 ? "edition" : "editions"} · ${escapeHtml(latest?.label?.split(" · ")[0] ?? "unseeded")}</span>`,
    ].join("");
    button.addEventListener("click", () => queue(() => selectRecipe(seed.key)));
    item.append(button);
    return item;
  }));
}

function refreshControls() {
  $("save").disabled = pending > 0;
  $("local-status").textContent = unsaved
    ? "Unrecorded changes on this page"
    : "All changes recorded on this device";
}

async function refresh(includeBytes = false) {
  const status = await files.status(currentKey);
  statusOf.set(currentKey, status);
  if (includeBytes) {
    bytesOf.clear();
    await files.exportRetained(currentKey, async (version, bytes) => {
      bytesOf.set(version.id, bytes);
    });
  }
  renderEditions(status);
  renderIndex();
  refreshControls();
  return status;
}

async function seedIfNeeded() {
  for (const seed of seeds) {
    let needsSeed = false;
    try {
      needsSeed = (await files.status(seed.key)).versions.length === 0;
    } catch { needsSeed = true; }
    if (!needsSeed) continue;
    for (const revision of seed.revisions) {
      let bytes;
      if (revision.markdown) {
        bytes = encoder.encode(revision.markdown);
      } else {
        const response = await fetch(revision.file);
        if (!response.ok) throw new Error(`Could not load ${revision.file}`);
        bytes = new Uint8Array(await response.arrayBuffer());
      }
      await files.save(seed.key, bytes, {
        label: revision.label,
        contentType: "text/markdown",
        metadata: { contributor: revision.label.split(" · ")[0] },
      });
    }
  }
}

async function loadAllStatuses() {
  for (const seed of seeds) {
    try { statusOf.set(seed.key, await files.status(seed.key)); }
    catch { /* not seeded yet */ }
  }
}

function titleOf(seed) {
  return seed.key.split("/").at(-1).replace(/\.md$/, "").replaceAll("-", " ");
}

function resolveRecipe(ref) {
  if (!ref || !ref.trim()) return currentKey;
  const query = ref.trim().toLowerCase();
  const slug = query.replace(/\s+/g, "-");
  return seeds.find((seed) => seed.key.toLowerCase().includes(slug)
    || seed.key.split("/").at(-1).includes(slug)
    || titleOf(seed).toLowerCase().includes(query))?.key ?? null;
}

function devLog(entry) {
  devCalls.push({ at: new Date().toLocaleTimeString(), ...entry });
  if (devCalls.length > 200) devCalls.shift();
  if (devOpen) renderDevCalls();
}

function renderDevCalls() {
  const stats = `calls ${devCalls.length} · wasm ${devCalls.filter((c) => c.layer === "wasm").length} · persisted ${devCalls.filter((c) => c.layer === "persist").length}`;
  $("dev-stats").textContent = stats;
  const rows = devCalls.toReversed().map((entry) => {
    const row = document.createElement("div");
    row.className = "dev-row";
    const time = document.createElement("span");
    time.className = "time";
    time.textContent = entry.at;
    const op = document.createElement("span");
    op.className = "op";
    op.textContent = `${entry.layer}:${entry.op}`;
    const detail = document.createElement("span");
    if (entry.ok === false) detail.className = "bad";
    if (entry.detail) {
      const key = document.createElement("span");
      key.className = "key";
      key.textContent = entry.detail;
      detail.append(key, " ");
    }
    if (entry.ms !== undefined) {
      const ms = document.createElement("span");
      ms.className = "metric";
      ms.textContent = `${entry.ms.toFixed(1)}ms`;
      detail.append(ms, " ");
    }
    if (entry.bytes) {
      const bytes = document.createElement("span");
      bytes.className = "metric";
      bytes.textContent = `${entry.bytes}B`;
      detail.append(bytes);
    }
    row.append(time, op, detail);
    return row;
  });
  $("dev-calls").replaceChildren(...rows);
}

function renderDevLine(label, value) {
  const line = document.createElement("div");
  line.className = "dev-line";
  const name = document.createElement("span");
  name.textContent = `${label} `;
  line.append(name, document.createTextNode(String(value)));
  return line;
}

async function renderDevPlane() {
  renderDevCalls();
  const buckets = await store.listBuckets();
  const objectLines = [];
  for (const bucket of buckets) {
    const page = await store.listObjects(bucket.name, { limit: 12 });
    objectLines.push(renderDevLine(`${bucket.name}:`, `${page.objects.length}${page.truncated ? "+" : ""} objects`));
    for (const object of page.objects) {
      objectLines.push(renderDevLine(`  ${object.key}`, `${object.size}B · ${object.contentType ?? "binary"} · ${object.metadata?.contributor ?? object.metadata?.purpose ?? ""}`));
    }
  }
  $("dev-objects").replaceChildren(...objectLines);

  const [capabilities, usage] = await Promise.all([store.capabilities(), store.usage()]);
  $("dev-runtime").replaceChildren(...[
    ["backend", capabilities.backend],
    ["persistent", capabilities.persistent],
    ["maxBytes", capabilities.maxBytes],
    ["maxObjects", capabilities.maxObjects],
    ["conditionalWrites", capabilities.conditionalWrites ?? false],
    ["guardedSaves", capabilities.guardedSaves ?? false],
    ["usage.bytes", usage.bytes],
    ["usage.objects", usage.objects],
    ["namespace", "cookbook"],
    ["adapter", "IndexedDbPersistenceAdapter"],
  ].map(([label, value]) => renderDevLine(`${label}:`, value)));
}

async function writeCatalog() {
  await store.putObject(CATALOG_BUCKET, CATALOG_KEY, encoder.encode(JSON.stringify(seeds)), {
    contentType: "application/json",
    metadata: { purpose: "cookbook-catalog" },
  });
}

async function ensureCatalog() {
  const buckets = await store.listBuckets();
  if (!buckets.some((bucket) => bucket.name === CATALOG_BUCKET)) {
    await store.createBucket(CATALOG_BUCKET);
  }
  let saved = null;
  try {
    const object = await store.getObject(CATALOG_BUCKET, CATALOG_KEY);
    saved = JSON.parse(decoder.decode(object.data));
  } catch { /* a fresh volume has no catalog yet */ }
  if (Array.isArray(saved)) {
    for (const entry of saved) {
      if (!seeds.some((seed) => seed.key === entry.key)) seeds.push(entry);
    }
  } else {
    await writeCatalog();
  }
}

async function syncCurrentView() {
  const result = await files.read(currentKey);
  currentMarkdown = decoder.decode(result.data);
  renderCard();
  fillEditForm();
  await refresh(true);
  await loadImages();
}

async function createRecipe({ title, meta = "Serves 2 · 20 minutes", ingredients = [], steps = [], contributor = "Agent" }) {
  const key = `recipes/${title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}.md`;
  if (seeds.some((seed) => seed.key === key)) throw new Error(`A recipe called “${title}” already exists`);
  const markdown = serializeRecipe({ title, meta, ingredients, steps });
  seeds.push({ key, revisions: [{ label: `${contributor} · created by agent`, markdown }] });
  await writeCatalog();
  await files.save(key, encoder.encode(markdown), {
    label: `${contributor} · created by agent`,
    contentType: "text/markdown",
    metadata: { contributor },
  });
  await loadAllStatuses();
  renderIndex();
  return { key, ingredients: ingredients.length, steps: steps.length };
}

async function appendToRecipe(ref, { ingredient, step, stepIndex }, contributor = "Agent") {
  const key = resolveRecipe(ref);
  if (!key) throw new Error(`No recipe matches “${ref}”`);
  const recipe = parseRecipe(decoder.decode((await files.read(key)).data));
  if (ingredient) recipe.ingredients.push(ingredient);
  if (stepIndex !== undefined) recipe.steps[stepIndex] = step;
  else if (step) recipe.steps.push(step);
  const markdown = serializeRecipe(recipe);
  await files.save(key, encoder.encode(markdown), {
    label: `${contributor} · agent revision`,
    contentType: "text/markdown",
    metadata: { contributor },
  });
  if (key === currentKey) await syncCurrentView();
  else await loadAllStatuses();
  return { key, ingredients: recipe.ingredients.length, steps: recipe.steps.length };
}

async function recordEditionFor(ref, contributor = "Agent") {
  const key = resolveRecipe(ref);
  if (!key) throw new Error(`No recipe matches “${ref}”`);
  const current = await files.read(key);
  const result = await files.save(key, current.data, {
    label: `${contributor} · agent-recorded edition`,
    contentType: "text/markdown",
    metadata: { contributor },
  });
  if (key === currentKey) await syncCurrentView();
  else await loadAllStatuses();
  return { key, edition: result.version.id.slice(0, 8) };
}

async function attachPhoto(ref, name) {
  const key = resolveRecipe(ref);
  if (!key) throw new Error(`No recipe matches “${ref}”`);
  const all = await store.listObjects(IMAGES_BUCKET);
  const found = all.objects.find((object) => (object.metadata?.name ?? "").toLowerCase().includes(name.toLowerCase())
    || object.key.toLowerCase().includes(name.toLowerCase()));
  if (!found) throw new Error(`No stored photograph matches “${name}”`);
  const stored = await store.getObject(IMAGES_BUCKET, found.key);
  const newKey = `${recipeSlug(key)}/${crypto.randomUUID().slice(0, 8)}-attached`;
  await store.putObject(IMAGES_BUCKET, newKey, stored.data, {
    contentType: stored.contentType,
    metadata: { ...stored.metadata, recipe: key, cover: "false", name: stored.metadata?.name ?? name },
  });
  if (key === currentKey) await loadImages();
  return { from: found.key, to: newKey, bytes: stored.size };
}

function recipeSlug(key = currentKey) {
  return key.split("/").at(-1).replace(/\.md$/, "");
}

async function ensureImageBucket() {
  const buckets = await store.listBuckets();
  if (!buckets.some((bucket) => bucket.name === IMAGES_BUCKET)) {
    await store.createBucket(IMAGES_BUCKET);
  }
}

async function seedImages() {
  for (const seed of seeds) {
    if (!seed.image) continue;
    const prefix = `${recipeSlug(seed.key)}/`;
    const existing = await store.listObjects(IMAGES_BUCKET, { prefix });
    if (existing.objects.length) continue;
    const response = await fetch(seed.image);
    if (!response.ok) throw new Error(`Could not load ${seed.image}`);
    await store.putObject(IMAGES_BUCKET, `${prefix}cover.jpg`,
      new Uint8Array(await response.arrayBuffer()), {
        contentType: response.headers.get("content-type") ?? "image/jpeg",
        metadata: { recipe: seed.key, name: "Finished dish", cover: "true", seeded: "true" },
      });
  }
}

async function loadImages() {
  for (const image of images) URL.revokeObjectURL(image.url);
  images = [];
  const page = await store.listObjects(IMAGES_BUCKET, { prefix: `${recipeSlug()}/` });
  for (const object of page.objects) {
    const stored = await store.getObject(IMAGES_BUCKET, object.key);
    images.push({
      key: object.key,
      url: URL.createObjectURL(new Blob([stored.data], { type: stored.contentType ?? "image/jpeg" })),
      name: object.metadata?.name ?? object.key.split("/").at(-1),
      cover: object.metadata?.cover === "true",
      size: object.size,
    });
  }
  renderImages();
}

function renderImages() {
  const title = parseRecipe(currentMarkdown).title || "this recipe";
  const cover = images.find((image) => image.cover) ?? images[0];
  $("cover-figure").hidden = !cover;
  if (cover) {
    $("cover-image").src = cover.url;
    $("cover-image").alt = `${title}, finished dish`;
    $("cover-caption").textContent = `${cover.name} · stored in Stow`;
  }
  $("image-count").textContent = images.length ? `(${images.length})` : "";
  $("photo-empty").hidden = images.length > 0;
  $("gallery").replaceChildren(...images.map((image) => {
    const figure = document.createElement("figure");
    figure.className = "photo" + (image.key === cover?.key ? " current" : "");
    const img = document.createElement("img");
    img.src = image.url;
    img.alt = `${title} — ${image.name}`;
    img.loading = "lazy";
    const caption = document.createElement("figcaption");
    const name = document.createElement("span");
    name.className = "name";
    name.textContent = image.name;
    const coverButton = document.createElement("button");
    coverButton.type = "button";
    coverButton.textContent = image.key === cover?.key ? "Cover" : "Make cover";
    coverButton.disabled = image.key === cover?.key;
    coverButton.addEventListener("click", () => queue(() => setCover(image.key)));
    const removeButton = document.createElement("button");
    removeButton.type = "button";
    removeButton.textContent = "Remove";
    removeButton.addEventListener("click", () => queue(() => removeImage(image.key)));
    caption.append(name, coverButton, removeButton);
    figure.append(img, caption);
    return figure;
  }));
}

async function setCover(key) {
  for (const image of images) {
    const stored = await store.getObject(IMAGES_BUCKET, image.key);
    await store.putObject(IMAGES_BUCKET, image.key, stored.data, {
      contentType: stored.contentType,
      metadata: { ...stored.metadata, cover: String(image.key === key) },
    });
  }
  await loadImages();
  message("Cover photograph updated.");
}

async function removeImage(key) {
  await store.deleteObject(IMAGES_BUCKET, key);
  await loadImages();
  message("Photograph removed from this recipe.");
}

async function addImages(fileList) {
  let added = 0;
  for (const file of fileList) {
    if (!file.type.startsWith("image/")) { message(`${file.name} is not an image.`, true); continue; }
    if (file.size > MAX_IMAGE_BYTES) { message(`${file.name} is larger than 5 MB.`, true); continue; }
    const safe = file.name.replace(/[^a-zA-Z0-9._-]+/g, "-").toLowerCase();
    const key = `${recipeSlug()}/${crypto.randomUUID().slice(0, 8)}-${safe}`;
    await store.putObject(IMAGES_BUCKET, key, new Uint8Array(await file.arrayBuffer()), {
      contentType: file.type,
      metadata: { recipe: currentKey, name: file.name, cover: "false" },
    });
    added += 1;
  }
  if (added) {
    await loadImages();
    message(`${added} photograph${added === 1 ? "" : "s"} added to this recipe.`);
  }
}

async function selectRecipe(key) {
  currentKey = key;
  message("");
  $("diff-panel").hidden = true;
  const result = await files.read(key);
  currentMarkdown = decoder.decode(result.data);
  const position = seeds.findIndex((seed) => seed.key === key) + 1;
  $("recipe-position").textContent = `Recipe ${String(position).padStart(2, "0")} / ${String(seeds.length).padStart(2, "0")}`;
  setView("card");
  await refresh(true);
  await loadImages();
}

function setView(next) {
  view = next;
  $("card-view").hidden = next !== "card";
  $("edit-view").hidden = next !== "edit";
  $("tab-card").setAttribute("aria-selected", String(next === "card"));
  $("tab-edit").setAttribute("aria-selected", String(next === "edit"));
  if (next === "card") renderCard();
  else fillEditForm();
}

function renderAgentEntry(entry) {
  const item = document.createElement("li");
  const tool = document.createElement("div");
  tool.className = "tool";
  tool.textContent = entry.tool;
  const detail = document.createElement("div");
  detail.className = "detail";
  detail.textContent = entry.detail;
  const result = document.createElement("div");
  result.className = "result";
  result.textContent = entry.result;
  item.append(tool, detail, result);
  return item;
}

function splitList(value) {
  return value.split(value.includes(";") ? ";" : ",").map((part) => part.trim()).filter(Boolean);
}

async function runAgent(prompt) {
  const entries = [];
  const say = (tool, detail, result) => {
    entries.push({ tool, detail, result });
    devLog({ layer: "agent", op: tool, detail, ms: 0, bytes: 0, ok: true });
  };
  const text = prompt.trim();
  const lower = text.toLowerCase();

  try {
    if (/^(list|show all)/.test(lower)) {
      await loadAllStatuses();
      say("cookbook.listRecipes", CATALOG_KEY, `${seeds.length} recipes in the shared catalog`);
    } else if (lower.startsWith("show editions") || lower.startsWith("editions")) {
      const ref = text.replace(/^(show editions|editions)\s*(for)?\s*/i, "").trim();
      const key = resolveRecipe(ref);
      if (!key) throw new Error(`No recipe matches “${ref}”`);
      const status = await files.status(key);
      say("BrowserFileHistory.status", key, `${status.versions.length} editions · ${status.currentVersionId.slice(0, 8)} current`);
    } else if (lower.startsWith("diff")) {
      const ref = text.replace(/^diff\s*(for)?\s*/i, "").trim();
      const key = resolveRecipe(ref);
      if (!key) throw new Error(`No recipe matches “${ref}”`);
      const versions = [];
      await files.exportRetained(key, async (version, bytes) => { versions.push({ version, bytes }); });
      const latest = versions.at(-1);
      const previous = versions.at(-2);
      const changed = previous
        ? diffLines(decoder.decode(previous.bytes), decoder.decode(latest.bytes)).filter(([sign]) => sign !== " ").length
        : 0;
      say("BrowserFileHistory.exportRetained", key, previous
        ? `${changed} changed lines in the latest edition`
        : "only one edition retained");
    } else if (lower.startsWith("record edition") || lower.startsWith("save edition")) {
      const rest = text.replace(/^(record|save) edition\s*(for)?\s*/i, "");
      const by = rest.match(/\s+by\s+(.+)$/i);
      const contributor = by ? by[1].trim() : ($("contributor").value.trim() || "Agent");
      const ref = by ? rest.slice(0, by.index).trim() : rest.trim();
      const result = await recordEditionFor(ref, contributor);
      say("BrowserFileHistory.save", result.key, `edition ${result.edition} recorded by ${contributor}`);
    } else if (lower.startsWith("add ingredient") || lower.startsWith("add step")) {
      const isStep = lower.startsWith("add step");
      const rest = text.replace(/^add (ingredient|step)\s+/i, "");
      const to = rest.match(/\s+to\s+(.+)$/i);
      const value = (to ? rest.slice(0, to.index) : rest).trim();
      if (!value) throw new Error("Give the agent something to add.");
      const result = await appendToRecipe(to ? to[1].trim() : "", isStep ? { step: value } : { ingredient: value });
      say("BrowserFileHistory.save", result.key, `added ${isStep ? "step" : "ingredient"}: ${value}`);
    } else if (lower.startsWith("attach photo")) {
      const rest = text.replace(/^attach photo\s+/i, "");
      const to = rest.match(/\s+to\s+(.+)$/i);
      const name = (to ? rest.slice(0, to.index) : rest).trim();
      const result = await attachPhoto(to ? to[1].trim() : "", name);
      say("BrowserEmbeddedStow.putObject", result.to, `copied ${result.bytes}B from ${result.from}`);
    } else if (lower.startsWith("add ") || lower.startsWith("new recipe")) {
      const body = text.replace(/^(new recipe|add)\s+/i, "");
      const title = (body.match(/"([^"]+)"/)?.[1] ?? body.split("|")[0].replace(/^recipe\s+/i, "")).trim();
      const ingredientsPart = body.match(/ingredients?:\s*([^|]+)/i)?.[1];
      const stepsPart = body.match(/steps?:\s*([^|]+)/i)?.[1];
      if (!title || !ingredientsPart || !stepsPart) {
        throw new Error("Try: add Title | ingredients: a; b | steps: first; second");
      }
      const result = await createRecipe({
        title,
        ingredients: splitList(ingredientsPart),
        steps: splitList(stepsPart),
        contributor: $("contributor").value.trim() || "Agent",
      });
      say("BrowserEmbeddedStow.putObject", `${CATALOG_BUCKET}/${CATALOG_KEY}`, `catalog updated to ${seeds.length} recipes`);
      say("BrowserFileHistory.save", result.key, `${result.ingredients} ingredients, ${result.steps} steps`);
    } else {
      throw new Error("Try 'list recipes', 'add ingredient 1 tsp vanilla to shakshuka', 'record edition for shakshuka by Ada', or 'add Title | ingredients: a; b | steps: first; second'.");
    }
  } catch (error) {
    say("agent.error", text, error.message ?? String(error));
  }
  return entries;
}

window.cookbook = {
  listRecipes: async () => {
    await loadAllStatuses();
    return seeds.map((seed) => ({ key: seed.key, title: titleOf(seed), editions: statusOf.get(seed.key)?.versions.length ?? 0 }));
  },
  openRecipe: async (ref) => {
    const key = resolveRecipe(ref);
    if (!key) throw new Error(`No recipe matches “${ref}”`);
    await selectRecipe(key);
    return { key };
  },
  addRecipe: createRecipe,
  editRecipe: appendToRecipe,
  recordEdition: recordEditionFor,
  attachPhoto,
  addImage: async ({ recipe, name = "photo.jpg", bytes, contentType = "image/jpeg" }) => {
    const key = resolveRecipe(recipe);
    if (!key) throw new Error(`No recipe matches “${recipe}”`);
    const data = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
    const objectKey = `${recipeSlug(key)}/${crypto.randomUUID().slice(0, 8)}-${name.replace(/[^a-zA-Z0-9._-]+/g, "-")}`;
    await store.putObject(IMAGES_BUCKET, objectKey, data, {
      contentType,
      metadata: { recipe: key, name, cover: "false" },
    });
    if (key === currentKey) await loadImages();
    return { key: objectKey, bytes: data.length };
  },
  diff: async (ref) => {
    const key = resolveRecipe(ref);
    if (!key) throw new Error(`No recipe matches “${ref}”`);
    const versions = [];
    await files.exportRetained(key, async (version, bytes) => { versions.push({ version, bytes }); });
    const latest = versions.at(-1);
    const previous = versions.at(-2);
    return {
      key,
      editions: versions.length,
      changedLines: previous
        ? diffLines(decoder.decode(previous.bytes), decoder.decode(latest.bytes)).filter(([sign]) => sign !== " ").length
        : 0,
    };
  },
  agent: runAgent,
  devLog: () => devCalls.slice(),
  devPlane: async () => { await renderDevPlane(); return { calls: devCalls.length }; },
};

for (const id of ["edit-title", "edit-meta", "edit-ingredients", "edit-method"]) {
  $(id).addEventListener("input", () => {
    currentMarkdown = collectEditForm();
    unsaved = true;
    refreshControls();
  });
}

$("save").addEventListener("click", () => queue(async () => {
  if (view === "edit") currentMarkdown = collectEditForm();
  const contributor = $("contributor").value.trim() || "Anonymous";
  await files.save(currentKey, encoder.encode(currentMarkdown), {
    label: `${contributor} · recorded an edition`,
    contentType: "text/markdown",
    metadata: { contributor },
  });
  unsaved = false;
  await refresh(true);
  message(`Edition recorded by ${contributor}.`);
}));

for (const direction of ["undo", "redo"]) {
  $(direction).addEventListener("click", () => queue(async () => {
    const result = await files[direction](currentKey);
    currentMarkdown = decoder.decode(result.data);
    $("diff-panel").hidden = true;
    renderCard();
    fillEditForm();
    unsaved = false;
    await refresh(true);
  }));
}

$("tab-card").addEventListener("click", () => setView("card"));
$("tab-edit").addEventListener("click", () => setView("edit"));
$("search").addEventListener("input", renderIndex);
$("retry").addEventListener("click", () => location.reload());
$("image-upload").addEventListener("change", (event) => {
  const files = [...event.target.files];
  event.target.value = "";
  if (files.length) queue(() => addImages(files));
});

$("agent-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const input = $("agent-input");
  const prompt = input.value.trim();
  if (!prompt) return;
  input.value = "";
  const log = $("agent-log");
  log.prepend(renderAgentEntry({ tool: "you", detail: prompt, result: "" }));
  const entries = await runAgent(prompt);
  for (const entry of entries.toReversed()) log.prepend(renderAgentEntry(entry));
});

for (const button of document.querySelectorAll(".agent-examples button")) {
  button.addEventListener("click", () => {
    $("agent-input").value = button.dataset.example;
    $("agent-input").focus();
  });
}

async function setDevOpen(open) {
  devOpen = open;
  document.body.classList.toggle("dev-open", open);
  $("dev-plane").hidden = !open;
  $("dev-toggle").setAttribute("aria-expanded", String(open));
  if (open) await renderDevPlane();
}

$("dev-toggle").addEventListener("click", () => setDevOpen(!devOpen));
$("dev-close").addEventListener("click", () => setDevOpen(false));
$("dev-refresh").addEventListener("click", () => renderDevPlane());
$("dev-clear").addEventListener("click", () => { devCalls.length = 0; renderDevCalls(); });

$("export").addEventListener("click", () => {
  const volume = Object.fromEntries(seeds.map((seed) => [
    seed.key,
    {
      editions: statusOf.get(seed.key)?.versions.map((v) => ({ id: v.id, label: v.label, createdAt: v.createdAt })) ?? [],
      markdown: seed.key === currentKey ? currentMarkdown : null,
    },
  ]));
  const url = URL.createObjectURL(new Blob([JSON.stringify(volume, null, 2)], { type: "application/json" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = "cookbook-volume.json";
  link.click();
  URL.revokeObjectURL(url);
  message("Volume exported as JSON.");
});

try {
  const go = new Go();
  const { instance } = await WebAssembly.instantiateStreaming(fetch("/runtime/stow-runtime.wasm"), go.importObject);
  go.run(instance);
  const host = { call: (request) => {
    const started = performance.now();
    let meta = {};
    try { meta = JSON.parse(request); } catch { /* opaque bridge request */ }
    const response = globalThis.stow.call(request);
    let ok = true;
    try {
      const parsed = JSON.parse(response);
      ok = parsed.ok !== false && !parsed.error;
    } catch { /* opaque bridge response */ }
    devLog({
      layer: "wasm",
      op: meta.op ?? "call",
      detail: meta.key ?? meta.bucket ?? meta.requestKey ?? "",
      ms: performance.now() - started,
      bytes: response.length,
      ok,
    });
    return response;
  } };
  const adapter = new IndexedDbPersistenceAdapter({ databaseName: "stow-cookbook" });
  const commit = adapter.commit.bind(adapter);
  adapter.commit = async (...args) => {
    const started = performance.now();
    const generation = await commit(...args);
    devLog({
      layer: "persist",
      op: "adapter.commit",
      detail: `${args[2]?.length ?? 0} changes → generation ${generation}`,
      ms: performance.now() - started,
      ok: true,
    });
    return generation;
  };
  store = await openBrowserEmbeddedStow(host, {
    namespace: "cookbook",
    persistence: adapter,
    maxBytes: 32 * 1024 * 1024,
    maxObjects: 500,
  });
  files = await BrowserFileHistory.open(store, { remoteBucket: "cookbook", maxVersions: 50 });
  await ensureImageBucket();
  await ensureCatalog();
  await seedImages();
  await seedIfNeeded();
  await loadAllStatuses();
  await selectRecipe(currentKey);
  $("loading").hidden = true;
  $("recipe-card").hidden = false;
  $("ready").textContent = "Volume open · stored on this device";
  message("");
} catch (error) {
  report(error);
}
