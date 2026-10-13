import { cp, mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { Marked } from "marked";
import { decisionPages, pages } from "./docs.mjs";

const site = dirname(fileURLToPath(import.meta.url));
const root = resolve(site, "..");
const output = join(site, ".build");
const repository = "https://github.com/chester-hill-solutions/stow-s3";
const revision = process.env.STOW_SITE_SOURCE_REVISION ?? "main";
if (revision !== "main" && !/^[a-f0-9]{40}$/.test(revision)) throw new Error("Site source revision must be a full Git commit SHA");
const sourceBase = `${repository}/blob/${revision}`;

export function escapeHtml(text) {
  return String(text).replaceAll("&", "&amp;").replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#39;");
}

export function headingSlug(text) {
  return text.toLowerCase().replace(/<[^>]*>/g, "").replace(/&[^;]+;/g, "")
    .replace(/[^\p{L}\p{N}_\s-]/gu, "").trim().replace(/\s/g, "-");
}

export function rewriteLink(href, source, manifest) {
  if (!href || href.startsWith("#")) return href;
  let target = href;
  let repositoryLink = false;
  for (const prefix of [`${repository}/blob/main/`, `${repository}/tree/main/`]) {
    if (target.startsWith(prefix)) {
      target = `/${target.slice(prefix.length)}`;
      repositoryLink = true;
    }
  }
  if (/^[a-z][a-z\d+.-]*:/i.test(target) || target.startsWith("//")) return target;
  const [path, fragment] = target.split("#", 2);
  const resolved = path.startsWith("/") ? path.slice(1) : relative(root, resolve(root, dirname(source), path)).split(sep).join("/");
  const page = manifest.find((entry) => entry.source === resolved || entry.source === `${resolved.replace(/\/$/, "")}/README.md`);
  if (page) return page.route + (fragment ? `#${fragment}` : "");
  if (repositoryLink) return `${sourceBase}/${resolved}${fragment ? `#${fragment}` : ""}`;
  if (target.startsWith("/") && !target.endsWith(".md")) return target;
  return `${sourceBase}/${resolved}${fragment ? `#${fragment}` : ""}`;
}

export function renderMarkdown(markdown, page, manifest) {
  const headings = [];
  const used = new Map();
  const parser = new Marked({ gfm: true, renderer: {
    heading({ tokens, depth, text }) {
      const base = headingSlug(text);
      const count = used.get(base) ?? 0;
      used.set(base, count + 1);
      const id = base + (count ? `-${count}` : "");
      if (depth === 2 || depth === 3) headings.push({ id, depth, text });
      return `<h${depth} id="${escapeHtml(id)}">${this.parser.parseInline(tokens)}</h${depth}>\n`;
    },
    link({ href, title, tokens }) {
      const url = rewriteLink(href, page.source, manifest);
      if (/^(javascript|data):/i.test(url)) return this.parser.parseInline(tokens);
      return `<a href="${escapeHtml(url)}"${title ? ` title="${escapeHtml(title)}"` : ""}>${this.parser.parseInline(tokens)}</a>`;
    },
    html({ text }) { return escapeHtml(text); },
  } });
  const body = parser.parse(markdown.replace(/^---\n[\s\S]*?\n---\n/, ""));
  return { body, headings };
}

function navigation(page, manifest) {
  const groups = [...new Set(manifest.filter((entry) => !entry.hidden).map((entry) => entry.group))];
  return groups.map((group) => `<div class="doc-nav-group"><p>${escapeHtml(group)}</p><ul>${manifest
    .filter((entry) => entry.group === group && !entry.hidden).map((entry) =>
      `<li><a href="${entry.route}"${page.route === entry.route ? ' aria-current="page"' : ""}>${escapeHtml(entry.title)}</a></li>`).join("")}</ul></div>`).join("");
}

export function renderPage(page, markdown, manifest) {
  const { body, headings } = renderMarkdown(markdown, page, manifest);
  const toc = headings.filter((heading) => heading.depth === 2).map((heading) =>
    `<li><a href="#${escapeHtml(heading.id)}">${escapeHtml(heading.text.replace(/[`*_]/g, ""))}</a></li>`).join("");
  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>${escapeHtml(page.title)} · Stow docs</title><meta name="description" content="${escapeHtml(page.title)}: Stow storage documentation, API examples and guarantees.">
<link rel="canonical" href="https://stow.chesterhillsolutions.ca${page.route}">
<link rel="icon" href="/assets/favicon.svg" type="image/svg+xml"><link rel="stylesheet" href="/assets/stow.css?v=20261012-docs"><link rel="stylesheet" href="/assets/docs.css?v=20261012">
</head><body><a class="skip" href="#main">Skip to content</a><div class="docs-shell">
<header class="masthead"><a href="/" aria-label="Stow home"><img src="/assets/wordmark.svg" alt="Stow S3" width="186" height="28"></a><nav aria-label="Primary"><a href="/">Overview</a><a href="/docs/" aria-current="page">Docs</a><a href="${repository}">GitHub</a></nav></header>
<div class="docs-layout"><aside class="docs-sidebar"><details open><summary>Documentation</summary><nav aria-label="Documentation">${navigation(page, manifest)}</nav></details></aside>
<main id="main" class="doc-content"><div class="doc-meta"><span>${escapeHtml(page.group)}</span><a href="${sourceBase}/${page.source}">View source</a></div>${body}
<footer class="doc-footer">Stow · Apache 2.0 · <a href="/docs/">Documentation index</a></footer></main>
<aside class="doc-toc" aria-label="On this page"><p>On this page</p><ul>${toc}</ul></aside></div></div><script src="/assets/docs.js" defer></script></body></html>\n`;
}

export async function buildSite(destination = output) {
  const manifest = [...pages, ...decisionPages(await readdir(join(root, "docs/adr")))];
  await rm(destination, { recursive: true, force: true });
  await mkdir(destination, { recursive: true });
  for (const name of ["assets", "images", "launch", "index.html", "agent.md", "llms.txt", "_headers"]) {
    await cp(join(site, name), join(destination, name), { recursive: true });
  }
  for (const page of manifest) {
    const markdown = await readFile(join(root, page.source), "utf8");
    const directory = join(destination, page.route);
    await mkdir(directory, { recursive: true });
    await writeFile(join(directory, "index.html"), renderPage(page, markdown, manifest));
  }
  const urls = ["/", ...manifest.map((page) => page.route)].map((route) =>
    `<url><loc>https://stow.chesterhillsolutions.ca${route}</loc></url>`).join("");
  await writeFile(join(destination, "sitemap.xml"), `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${urls}</urlset>\n`);
  await writeFile(join(destination, "robots.txt"), "User-agent: *\nAllow: /\nSitemap: https://stow.chesterhillsolutions.ca/sitemap.xml\n");
  return manifest;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const manifest = await buildSite();
  console.log(`Built Stow site and ${manifest.length} documentation pages`);
}
