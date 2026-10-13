import { BrowserFileHistory, IndexedDbPersistenceAdapter, openBrowserEmbeddedStow } from "/runtime/browser.js";
import { editorRemote } from "./remote.mjs";

const element = (id) => document.getElementById(id);
const editor = element("editor");
const key = "notes.md";
let files;
let latestStatus;
let busy = false;
let jobs = Promise.resolve();
let outstanding = 0;
let unsaved = false;

function reportError(error) {
  element("message").textContent = error.message;
  element("message").hidden = false;
  element("conflict-actions").hidden = error.code !== "conflict";
  if (error.code === "conflict") element("sync-status").textContent = "Remote changed · local stages retained";
}

function controls() {
  editor.disabled = busy || !files;
  const waitingForSave = unsaved || outstanding > 0;
  element("undo").disabled = busy || waitingForSave || !latestStatus?.canUndo;
  element("redo").disabled = busy || waitingForSave || !latestStatus?.canRedo;
  element("sync").disabled = busy || waitingForSave || element("offline").checked || !latestStatus?.pending;
  element("adopt").disabled = busy || waitingForSave || element("offline").checked;
  element("other-edit").disabled = busy || element("offline").checked;
}

function render(status) {
  latestStatus = status;
  element("stage-count").textContent = status.versions.length;
  element("bytes").textContent = `${new TextEncoder().encode(editor.value).length} bytes`;
  element("local-status").textContent = outstanding ? "Saving locally…" : unsaved ? "This edit has not been saved" : "Saved on this device";
  element("sync-status").textContent = status.pending ? "Local changes ready to sync" : "Remote version confirmed";
  element("sync-detail").textContent = status.hasUncertainWrite
    ? "A write needs reconciliation. Retry sync when connected."
    : status.pending ? "Your retained stages stay local. Sync publishes the selected stage." : "New edits save locally until you choose to sync.";
  element("history").replaceChildren(...status.versions.toReversed().map((version) => {
    const item = document.createElement("li");
    if (version.id === status.currentVersionId) item.className = "current";
    item.textContent = version.label ?? "Saved edit";
    const time = document.createElement("small");
    time.textContent = new Date(version.createdAt).toLocaleTimeString();
    item.append(time); return item;
  }));
  controls();
}

function schedule(operation) {
  const task = jobs.then(operation);
  jobs = task.catch(reportError);
  return task;
}

async function action(operation) {
  busy = true; controls();
  element("message").hidden = true;
  element("conflict-actions").hidden = true;
  try { await operation(); }
  catch (error) { reportError(error); }
  finally { busy = false; controls(); }
}

editor.addEventListener("input", () => {
  const bytes = new TextEncoder().encode(editor.value);
  unsaved = true;
  outstanding += 1;
  element("local-status").textContent = "Saving locally…";
  controls();
  schedule(async () => {
    try {
      const result = await files.save(key, bytes, { contentType: "text/markdown" });
      if (new TextDecoder().decode(bytes) === editor.value) unsaved = false;
      outstanding -= 1; render(result.status);
    } catch (error) {
      outstanding -= 1; element("local-status").textContent = "This edit has not been saved";
      controls();
      throw error;
    }
  }).catch(() => {});
});

for (const direction of ["undo", "redo"]) {
  element(direction).addEventListener("click", () => action(() => schedule(async () => {
    const result = await files[direction](key);
    editor.value = new TextDecoder().decode(result.data); render(result.status);
  })));
}
element("sync").addEventListener("click", () => action(() => schedule(async () => {
  try { render(await files.sync(key)); }
  catch (error) { render(await files.status(key)); throw error; }
})));
element("adopt").addEventListener("click", () => action(() => schedule(async () => {
  const result = await files.adoptRemote(key);
  editor.value = new TextDecoder().decode(result.data); render(result.status);
})));
element("offline").addEventListener("change", controls);
element("other-edit").addEventListener("click", () => action(async () => {
  const response = await fetch("/api/other-edit", { method: "POST" });
  if (!response.ok) throw new Error("Could not simulate the remote edit");
  element("message").hidden = false;
  element("message").textContent = "Another writer changed the remote file. Sync your local edit to see the conflict.";
}));

try {
  const go = new Go();
  const { instance } = await WebAssembly.instantiateStreaming(fetch("/runtime/stow-runtime.wasm"), go.importObject);
  go.run(instance);
  const host = { call: (request) => globalThis.stow.call(request) };
  const store = await openBrowserEmbeddedStow(host, {
    namespace: "browser-editor", persistence: new IndexedDbPersistenceAdapter({ databaseName: "stow-browser-editor" }),
    maxBytes: 8 * 1024 * 1024, maxObjects: 200,
  });
  files = await BrowserFileHistory.open(store, { remoteBucket: "editor", remote: editorRemote(() => element("offline").checked), maxVersions: 50 });
  const result = await files.read(key);
  editor.value = new TextDecoder().decode(result.data); render(result.status);
  const existing = await navigator.serviceWorker.getRegistration();
  if (existing?.active) {
    element("offline-ready").textContent = "Available offline";
    navigator.serviceWorker.register("/sw.js").catch(() => {});
  } else {
    await navigator.serviceWorker.register("/sw.js");
    await navigator.serviceWorker.ready;
    if (!navigator.serviceWorker.controller) await new Promise((resolve) => navigator.serviceWorker.addEventListener("controllerchange", resolve, { once: true }));
    element("offline-ready").textContent = "Available offline";
  }
} catch (error) { reportError(error); }
