import { createServer } from "node:http";
import { readFile, readdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import { openBrowserTestRemote } from "../../packages/stow-s3/scripts/browser-remote.mjs";
import { signBrowserTestObject } from "../../packages/stow-s3/scripts/browser-signing.mjs";

const root = fileURLToPath(new URL("../../", import.meta.url));
const directory = fileURLToPath(new URL("./", import.meta.url));
const dist = resolve(root, "packages/stow-s3/dist");
process.env.STOW_BIN ??= resolve(root, "bin/stow-s3");
const fixture = await openBrowserTestRemote();
const { remote } = fixture;
const metrics = new Map();
const welcome = "# A draft that stays with you\n\nEdit this file. Each saved stage stays in this browser.\n\nTry working offline, reloading, and undoing an edit.\nThen reconnect and sync the version you want to keep.\n";
await remote.putObject(fixture.bucket, `${fixture.prefix}notes.md`, new TextEncoder().encode(welcome), {
  basis: { absent: true }, saveId: crypto.randomUUID(), contentType: "text/markdown",
});
const staticFiles = new Map([
  ["/", "index.html"], ["/index.html", "index.html"], ["/app.mjs", "app.mjs"],
  ["/remote.mjs", "remote.mjs"], ["/style.css", "style.css"], ["/sw.js", "sw.js"],
]);
if (process.env.STOW_BROWSER_TESTING === "1") {
  staticFiles.set("/test/harness.html", "../../packages/stow-s3/scripts/browser-tests/harness.html");
  staticFiles.set("/test/harness.mjs", "../../packages/stow-s3/scripts/browser-tests/harness.mjs");
}
const mime = { ".html": "text/html", ".mjs": "text/javascript", ".js": "text/javascript", ".css": "text/css", ".wasm": "application/wasm" };
const assets = [...staticFiles.keys()].filter((path) => path !== "/sw.js");
for (const name of await readdir(dist)) {
  if (name.endsWith(".js") || name.endsWith(".wasm")) assets.push(`/runtime/${name}`);
}

const server = createServer(async (request, response) => {
  try {
    const url = new URL(request.url, "http://localhost");
    if (url.pathname === "/test/sign-object" && request.method === "POST" && process.env.STOW_BROWSER_TESTING === "1") {
      if (fixture.provider !== "railway") { response.writeHead(404).end(); return; }
      json(response, 200, await signBrowserTestObject(fixture, request)); return;
    }
    if (url.pathname === "/test/stats" && process.env.STOW_BROWSER_TESTING === "1") {
      json(response, 200, metrics.get(url.searchParams.get("key")) ?? { reads: 0, writes: 0 }); return;
    }
    if (url.pathname.startsWith("/api/")) return await api(request, response, url);
    if (request.method !== "GET") { response.writeHead(405).end(); return; }
    if (url.pathname === "/assets.json") { json(response, 200, assets); return; }
    let path;
    if (staticFiles.has(url.pathname)) path = resolve(directory, staticFiles.get(url.pathname));
    else if (/^\/runtime\/[a-zA-Z0-9_-]+\.(js|wasm)$/.test(url.pathname)) path = resolve(dist, url.pathname.slice(9));
    if (!path) { response.writeHead(404).end(); return; }
    const bytes = await readFile(path);
    const extension = path.slice(path.lastIndexOf("."));
    response.writeHead(200, { "content-type": mime[extension] ?? "application/octet-stream", "cache-control": "no-cache" }).end(bytes);
  } catch (error) {
    json(response, error.code === "conflict" ? 412 : error.status ?? 500, { message: error.code === "conflict" ? error.message : "Remote or fixture operation failed" });
  }
});

async function api(request, response, url) {
  const key = url.searchParams.get("key") ?? "notes.md";
  if (!key || key.split("/").some((part) => part === "." || part === "..")) {
    response.writeHead(400).end(); return;
  }
  const remoteKey = `${fixture.prefix}${key}`;
  const stats = metrics.get(key) ?? { reads: 0, writes: 0 };
  metrics.set(key, stats);
  if (url.pathname === "/api/config" && request.method === "GET") {
    json(response, 200, { provider: fixture.provider, description: fixture.description }); return;
  }
  if (url.pathname === "/api/other-edit" && request.method === "POST") {
    const object = await remote.getObject(fixture.bucket, remoteKey);
    const text = new TextDecoder().decode(object?.data) + "\nA change from another writer.\n";
    await remote.putObject(fixture.bucket, remoteKey, new TextEncoder().encode(text), {
      basis: object ? { etag: object.etag } : { absent: true }, saveId: crypto.randomUUID(), contentType: "text/markdown",
    });
    json(response, 200, { changed: true }); return;
  }
  if (url.pathname !== "/api/object") { response.writeHead(404).end(); return; }
  if (request.method === "GET") {
    stats.reads += 1;
    const object = await remote.getObject(fixture.bucket, remoteKey);
    if (!object) { response.writeHead(404).end(); return; }
    response.writeHead(200, {
      etag: object.etag, "content-type": object.contentType ?? "application/octet-stream",
      "x-stow-save-id": object.saveId ?? "", "x-stow-metadata": JSON.stringify(object.metadata ?? {}),
    }).end(object.data); return;
  }
  if (request.method !== "PUT") { response.writeHead(405).end(); return; }
  const chunks = []; let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > 2 * 1024 * 1024) { response.writeHead(413).end(); return; }
    chunks.push(chunk);
  }
  const basis = request.headers["if-match"] ? { etag: request.headers["if-match"] } : { absent: true };
  if (!request.headers["if-match"] && request.headers["if-none-match"] !== "*") { response.writeHead(400).end(); return; }
  const written = await remote.putObject(fixture.bucket, remoteKey, new Uint8Array(Buffer.concat(chunks)), {
    basis, saveId: request.headers["x-stow-save-id"], contentType: request.headers["content-type"],
    metadata: JSON.parse(request.headers["x-stow-metadata"] ?? "{}"),
  });
  stats.writes += 1;
  response.writeHead(200, { etag: written.etag }).end();
}

function json(response, status, value) {
  response.writeHead(status, { "content-type": "application/json" }).end(JSON.stringify(value));
}

const port = Number(process.env.STOW_EDITOR_PORT ?? 4173);
server.listen(port, "127.0.0.1", () => process.stdout.write(`Stow editor: http://127.0.0.1:${port}\nRemote: ${fixture.description}\n`));
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, async () => {
    server.close();
    try { await fixture.close(); process.stdout.write("Disposable remote data cleaned up.\n"); process.exit(0); }
    catch { process.stderr.write("Disposable remote cleanup failed.\n"); process.exit(1); }
  });
}
