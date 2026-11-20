import { createServer } from "node:http";
import { readFile, readdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

const root = fileURLToPath(new URL("../../", import.meta.url));
const directory = fileURLToPath(new URL("./", import.meta.url));
const dist = resolve(root, "packages/stow-s3/dist");
const port = Number(process.env.STOW_COOKBOOK_PORT ?? 4174);

const staticFiles = new Map([
  ["/", "index.html"],
  ["/index.html", "index.html"],
  ["/app.mjs", "app.mjs"],
  ["/seeds.mjs", "seeds.mjs"],
  ["/more-recipes.mjs", "seed/more-recipes.mjs"],
  ["/style.css", "style.css"],
  // Reuse Stow's real design tokens and primitives instead of copying them.
  ["/theme.css", resolve(root, "site/assets/theme.css")],
]);

const runtime = new Set((await readdir(dist)).filter((name) => /\.(js|wasm)$/.test(name)));
const mime = {
  ".html": "text/html; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".md": "text/markdown; charset=utf-8",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".png": "image/png",
  ".webp": "image/webp",
  ".gif": "image/gif",
  ".wasm": "application/wasm",
};

const server = createServer(async (request, response) => {
  try {
    const url = new URL(request.url, "http://localhost");
    if (request.method !== "GET") {
      response.writeHead(405).end();
      return;
    }

    let path;
    if (staticFiles.has(url.pathname)) path = resolve(directory, staticFiles.get(url.pathname));
    else if (/^\/seed\/[a-z0-9./-]+\.(md|jpg|jpeg|png|webp|gif)$/.test(url.pathname)) path = resolve(directory, `.${url.pathname}`);
    else if (/^\/runtime\/[a-zA-Z0-9_-]+\.(js|wasm)$/.test(url.pathname)) {
      const name = url.pathname.slice("/runtime/".length);
      if (runtime.has(name)) path = resolve(dist, name);
    }

    if (!path) {
      response.writeHead(404).end("Not found");
      return;
    }

    const bytes = await readFile(path);
    const extension = path.slice(path.lastIndexOf("."));
    response.writeHead(200, {
      "content-type": mime[extension] ?? "application/octet-stream",
      "cache-control": "no-cache",
    }).end(bytes);
  } catch {
    response.writeHead(404).end("Not found");
  }
});

server.listen(port, "127.0.0.1", () => {
  console.log(`Crowdsource Cookbook: http://127.0.0.1:${port}`);
});
