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

For deployment from a published branch awaiting merge, set
`STOW_SITE_SOURCE_REVISION` to its full pushed commit SHA. The source links then
point to that revision rather than to files not yet present on `main`.
