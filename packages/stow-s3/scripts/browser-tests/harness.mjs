import { BrowserFileHistory, IndexedDbPersistenceAdapter, openBrowserEmbeddedStow, FileHistoryError } from "/runtime/browser.js";
import { editorRemote } from "/remote.mjs";

let abortNextTransaction = false;
let abortAcknowledgement = false;
let store;
let files;
const originalTransaction = IDBDatabase.prototype.transaction;
IDBDatabase.prototype.transaction = function (...args) {
  const transaction = originalTransaction.apply(this, args);
  if (abortNextTransaction && args[1] === "readwrite") {
    abortNextTransaction = false;
    queueMicrotask(() => transaction.abort());
  }
  return transaction;
};

const go = new globalThis.Go();
const { instance } = await WebAssembly.instantiateStreaming(fetch("/runtime/stow-runtime.wasm"), go.importObject);
go.run(instance);
const params = new URL(location.href).searchParams;

const directRemote = {
  async getObject(_bucket, key) {
    const signed = await sign({ method: "GET", key });
    const response = await fetch(signed.url);
    if (response.status === 404) return null;
    if (!response.ok) throw new Error(`Direct remote GET failed (${response.status})`);
    return {
      data: new Uint8Array(await response.arrayBuffer()), etag: response.headers.get("etag"),
      saveId: response.headers.get("x-amz-meta-stow-save-id"),
      contentType: response.headers.get("content-type"),
      metadata: response.headers.get("x-amz-meta-purpose") ? { purpose: response.headers.get("x-amz-meta-purpose") } : {},
    };
  },
  async putObject(_bucket, key, data, options) {
    const signed = await sign({ method: "PUT", key, options });
    const response = await fetch(signed.url, { method: "PUT", body: data, headers: signed.headers });
    if (response.status === 412) throw new FileHistoryError("conflict", "direct remote changed");
    if (!response.ok) {
      const xml = new DOMParser().parseFromString(await response.text(), "text/xml");
      throw new Error(`Direct remote PUT failed (${response.status}, ${xml.querySelector("Code")?.textContent ?? "unknown"})`);
    }
    return { etag: response.headers.get("etag") };
  },
};

async function sign(body) {
  const response = await fetch("/test/sign-object", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
  if (!response.ok) throw new Error(`Could not sign scoped test request (${response.status})`);
  return response.json();
}

window.qa = {
  ready: false,
  error: null,
  async open(options = {}) {
    const transport = params.get("direct") === "1" ? directRemote : editorRemote(() => false);
    const remote = {
      getObject: (...args) => transport.getObject(...args),
      async putObject(...args) {
        const result = await transport.putObject(...args);
        if (abortAcknowledgement) { abortAcknowledgement = false; abortNextTransaction = true; }
        return result;
      },
    };
    store = await openBrowserEmbeddedStow({ call: (request) => globalThis.stow.call(request) }, {
      namespace: params.get("namespace") ?? "browser-contract", persistence: new IndexedDbPersistenceAdapter({ databaseName: "stow-browser-contract-tests" }),
      maxBytes: Number(params.get("maxBytes") ?? 1_000_000), maxObjects: 500,
    });
    files = await BrowserFileHistory.open(store, { remoteBucket: "editor", remote, maxVersions: options.maxVersions ?? 3 });
    this.ready = true;
  },
  async call(method, key, text, options) {
    try {
      const result = method === "save"
        ? await files.save(key, new TextEncoder().encode(text), options)
        : await files[method](key);
      return { ...result, text: result.data ? new TextDecoder().decode(result.data) : undefined, data: undefined };
    } catch (error) { return { error: error.code ?? "transport_error", message: error.message }; }
  },
  abortNextCommit() { abortNextTransaction = true; },
  abortNextAcknowledgement() { abortAcknowledgement = true; },
  async usage() { return store.usage(); },
  async corrupt(key) {
    await store.putObject("stow-file-history", `files/editor/${encodeURIComponent(key)}/state.json`, new TextEncoder().encode('{"formatVersion":999}'));
  },
  async close() { await store.close(); this.ready = false; },
};
try { await window.qa.open(); document.getElementById("state").textContent = "Real WASM and IndexedDB ready"; }
catch (error) { window.qa.error = error.code ?? error.message; document.getElementById("state").textContent = window.qa.error; }
