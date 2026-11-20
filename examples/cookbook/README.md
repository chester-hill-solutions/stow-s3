# Crowdsource Cookbook

A single-page, local-first demo of Stow file history. Twenty-three real recipes
are seeded from `seed/`, then refined by contributors or by the on-page agent —
every save is a durable, receipt-backed checkpoint you can diff, undo and redo.

From the repository root (Node.js 20+ required):

```sh
node examples/cookbook/serve.mjs
```

Open http://127.0.0.1:4174. Set `STOW_COOKBOOK_PORT` to use another port.
Open one tab at a time: the persisted namespace admits a single writer, so a
second tab reports `persistence namespace is already open` until the first closes.

The demo serves the prebuilt browser runtime from `packages/stow-s3/dist`, so
run `npm install && npm run build` in `packages/stow-s3` first if that folder
is missing `browser.js`, `wasm_exec.js` or `stow-runtime.wasm`.

## Photographs

Three recipes have seeded cover photographs from `seed/images/`, stored in a
second Stow bucket, `cookbook-images`, using `putObject()` on the same store.
**Add photograph** uploads local files as Stow objects with the recipe key and
filename in their metadata; the gallery lists them with **Make cover** and
**Remove** actions. Files are capped at 5 MB each; the store is budgeted at
32 MiB / 500 objects.

## What to try

1. Open a recipe from the **Index**. The card shows ingredients and method in
   two columns, with serving and timing in the header.
2. Pick an older **Edition** in the margin and open the comparison to see a
   line-by-line diff against the current card.
3. Choose **Edit recipe**, change a step, then **Record edition**. The new
   edition is attributed to the name in **Recorded by**.
4. Use **Previous** / **Next** to move through the editions.
5. Reload the page. The recipes, editions and selected card are still in
   IndexedDB — nothing came from a server.
6. Use **Add photograph** to store a local image in the cookbook, then make it
   the cover or remove it.
7. Use **Export volume** to download a JSON snapshot.

## Agent and dev plane

**Agent** opens a non-modal panel. Prompts are parsed locally (no model, no
network) into real Stow operations: `list recipes`, `add ingredient … to …`,
`add step … to …`, `record edition for … by …`, `show editions for …`,
`diff …`, `attach photo … to …`, and `add Title | ingredients: a; b | steps: x; y`.
Each operation is logged with the Stow call, its arguments and its result.
Agent-created recipes are appended to a catalog object at
`cookbook-meta/catalog.json`, so they survive a reload.

The same operations are exposed programmatically:

```js
await cookbook.listRecipes()
await cookbook.openRecipe("shakshuka")
await cookbook.addRecipe({ title: "Shakerato", ingredients: [...], steps: [...] })
await cookbook.editRecipe("shakshuka", { ingredient: "1 tsp cumin" })
await cookbook.recordEdition("shakshuka", "Ada")
await cookbook.addImage({ recipe: "shakshuka", name: "photo.jpg", bytes, contentType })
await cookbook.diff("shakshuka")
await cookbook.agent("list recipes")
```

**Dev plane** is a fixed bottom panel showing the raw bridge traffic
(`host.call` ops with op, key, duration, response bytes), IndexedDB persistence
commits and generation numbers, every Stow object with its size and metadata,
and runtime capabilities/usage. Open it with the **Dev plane** button; it can be
refreshed or cleared without reloading.

The page is styled with Stow's own `site/assets/theme.css` (served at
`/theme.css`) plus cookbook-specific composition in `style.css`, so its tokens,
focus treatment and button primitives stay identical to the main site.

## What it demonstrates

- `BrowserFileHistory` with one checkpoint chain per recipe (`recipes/<slug>.md`).
- Contributor labels and metadata on every `save()`.
- `exportRetained()` to read every retained stage for the diff view.
- `undo()` / `redo()` with a persisted cursor.
- `IndexedDbPersistenceAdapter` for offline-first, single-page storage.
- `putObject()` / `getObject()` / `listObjects()` / `deleteObject()` for photos
  and the recipe catalog in separate buckets on the same store.
- Live observability by wrapping the WASM `host.call` bridge and the adapter's
  `commit()` method.

There is no sync transport configured, so this demo is entirely local. The
`remoteBucket` option is still required by `BrowserFileHistory`.

## Shared recipe bucket

The seed recipes are also uploaded to the Cloudflare R2 bucket
`crowdsource-cookbook` on the Chester Hill Solutions account. To refresh them:

```sh
export CLOUDFLARE_ACCOUNT_ID=ad5ec479b9a421faa2ed06c3d1c2b23a
for file in examples/cookbook/seed/*.md; do
  cf r2 objects put "$(basename "$file")" --bucket-name crowdsource-cookbook --file "$file"
done
for file in examples/cookbook/seed/images/*.jpg; do
  cf r2 objects put "images/$(basename "$file")" --bucket-name crowdsource-cookbook --file "$file"
done
```

The browser never talks to R2 directly; the bucket is the shared source of the
real recipes that seed each local cookbook.
