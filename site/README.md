# Stow website

The static marketing site is hosted by the `stow-site` Cloudflare Worker in the
Chester Hill Solutions account at https://stow.chesterhillsolutions.ca.
The checked-in configuration serves only built website assets and documentation
pages. Local preview scripts, build tools and package metadata are excluded from
uploads. Markdown guides are rendered from the repository sources with the
route manifest in `docs.mjs`.

After reviewing site changes, deploy from the repository root with an
authenticated Wrangler v4 installation:

```sh
npm ci --ignore-scripts --prefix site
make check-doc-links
wrangler deploy --config site/wrangler.jsonc --dry-run
wrangler deploy --config site/wrangler.jsonc
```

Wrangler runs the build automatically before deployment. Preview the complete
output with `python3 -m http.server 8080 --directory site/.build`.
Agent-readable instructions are available at `/agent.md`, and `/llms.txt` links
to the current API contracts. Add new public guides to `docs.mjs`; the build
rewrites links between published sources into site routes and supplies navigation,
heading anchors, tables of contents and a sitemap.

Publish user guides and API references only. Keep ADRs, plans, test reports and
contributor instructions in the repository. In shared Markdown, surround
repository commentary with `<!-- repo-only:start -->` and `<!-- repo-only:end -->`
on separate lines. The site removes these blocks before rendering or generating
the table of contents. Unbalanced markers fail the build.

Update API references with `node scripts/generate-api-reference.mjs` after
installing the TypeScript package dependencies. `make check-api-reference` checks
that every saved declaration still matches the current public exports. Edit
behavior notes in `api-notes.mjs`; keep generated declarations unchanged.

For deployment from a published branch awaiting merge, set
`STOW_SITE_SOURCE_REVISION` to its full pushed commit SHA. The source links then
point to that revision rather than to files not yet present on `main`.
