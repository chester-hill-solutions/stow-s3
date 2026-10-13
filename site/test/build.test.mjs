import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, access } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { buildSite, publicMarkdown, renderMarkdown, rewriteLink } from "../build.mjs";
import { pages } from "../docs.mjs";

test("public guides omit repository commentary, retain code examples and reject broken markers", () => {
  const input = "Visible\n<!-- repo-only:start -->\n## ADRs\nPrivate\n<!-- repo-only:end -->\n```md\n<!-- repo-only:start -->\n```\nEnd";
  assert.equal(publicMarkdown(input), "Visible\n```md\n<!-- repo-only:start -->\n```\nEnd");
  assert.throws(() => publicMarkdown("<!-- repo-only:start -->\nforgotten"), /Unclosed/);
  assert.throws(() => publicMarkdown("<!-- repo-only:end -->"), /Unexpected/);
  assert.throws(() => publicMarkdown("<!-- repo-only:start -->\n<!-- repo-only:start -->"), /Nested/);
});

test("published guides render examples and turn repo documentation links into site routes", () => {
  const page = pages.find((entry) => entry.source === "docs/outbox.md");
  const { body, headings } = renderMarkdown('# Outbox\n\n## Recovery\n\n[Cache](run-through.md#working-with-the-network-gone)\n\n```js\nconst value = "<script>";\n```', page, pages);
  assert.match(body, /href="\/docs\/run-through\/#working-with-the-network-gone"/);
  assert.match(body, /&lt;script&gt;/);
  assert.equal(headings[0].id, "recovery");
  assert.equal(rewriteLink("https://github.com/chester-hill-solutions/stow-s3/blob/main/docs/browser-project-storage.md", page.source, pages), "/docs/browser-projects/");
  const license = "https://github.com/chester-hill-solutions/stow-s3/blob/main/LICENSE";
  assert.equal(rewriteLink(license, page.source, pages), license);
});

test("public output contains complete guides and excludes build tools, source files and credentials", async (t) => {
  const output = await mkdtemp(join(tmpdir(), "stow-site-build-"));
  t.after(() => rm(output, { recursive: true, force: true }));
  const manifest = await buildSite(output);
  assert.equal(manifest.some((page) => page.route.startsWith("/docs/decisions/")), false);
  for (const route of ["docs/decisions", "docs/contributing", "docs/standards"]) {
    await assert.rejects(access(join(output, route)));
  }
  for (const page of manifest) {
    const html = await readFile(join(output, page.route, "index.html"), "utf8");
    assert.match(html, /<main id="main"/);
    assert.match(html, /<h1 /);
    assert.match(html, /aria-label="Documentation"/);
    assert.match(html, /rel="canonical"/);
    assert.doesNotMatch(html, /Architecture decisions|repo-only:|docs\/adr\/|W02\/W09/);
    assert.doesNotMatch(html, /href="[^"]*\/(?:history|adr|assessment-evidence|benchmarks)\//);
    assert.doesNotMatch(html, /href="[^"]*\/(?:storage-foundation-plan|planning-index|implementation-\d|release-verification-\d)/);
  }
  for (const name of ["serve.py", "build.mjs", "package.json", "wrangler.jsonc", ".env"]) {
    await assert.rejects(access(join(output, name)));
  }
  const home = await readFile(join(output, "index.html"), "utf8");
  assert.doesNotMatch(home, /href="https:\/\/github\.com\/chester-hill-solutions\/stow-s3(?:#|\/(?:blob|tree)\/main\/(?:docs|examples\/browser-editor))/);
  const documents = [{ route: "/", html: home }, ...await Promise.all(manifest.map(async (page) => ({
    route: page.route, html: await readFile(join(output, page.route, "index.html"), "utf8"),
  })))];
  const byRoute = new Map(documents.map((document) => [document.route, document.html]));
  const broken = [];
  for (const document of documents) {
    for (const match of document.html.matchAll(/href="([^"]+)"/g)) {
      const url = new URL(match[1].replaceAll("&amp;", "&"), `https://site.test${document.route}`);
      if (url.origin !== "https://site.test") continue;
      const target = byRoute.get(url.pathname);
      if (target !== undefined && url.hash && !target.includes(`id="${decodeURIComponent(url.hash.slice(1))}"`)) {
        broken.push(`${document.route}: ${url.pathname}${url.hash}`);
      } else if (target === undefined) {
        try { await access(join(output, decodeURIComponent(url.pathname))); }
        catch { broken.push(`${document.route}: ${url.pathname}`); }
      }
    }
  }
  assert.deepEqual(broken, [], "Every public navigation target and section anchor must exist");
});
