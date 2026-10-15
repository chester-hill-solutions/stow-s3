# Stow website redesign plan

Use the [landing page concept](design/stow-landing-concept.png) to redesign the homepage, documentation theme and social preview around portable working storage. The design should help visitors understand Stow, choose an API and reach a working example quickly.

Implemented in the working tree on 2026-10-13: the homepage, shared documentation theme, illustrations, language examples, retained walkthrough and six social graphics are complete. Local build tests, documentation links, site claims and installation checks pass. Browser checks cover 320–1440px layouts, keyboard tabs, copying, the offline walkthrough and examples with JavaScript disabled.

Published on 2026-10-13 to [Stow](https://stow.chesterhillsolutions.ca/) as Cloudflare version `0eda14d0-605b-4ee3-844a-040a078a51e2`. The live homepage, documentation index, TypeScript API, theme, interaction script, hero illustration, social preview and agent index all return HTTP 200 and match the deployment bundle. Documentation source links use published revision `41d22d0728d5ffdb2e5fb2417958588a90bc7d5d`.

Repositioned on 2026-10-14: the homepage now leads with the category rather than the vision. The hero reads "S3 storage, without the server." under the eyebrow "Embeddable object storage", and the capabilities, path, session and API copy present checkpoints, workspaces and browser persistence as extensions of the embedded object engine. Ephemeral workloads are named alongside testing and agent scenarios at the process level (scoped sessions and the embedded runtime), not as qualified edge or Workers deployment. The retained line "Your work. Stored. Ready to move." now heads the workspace section instead of the hero, so the headline row in the structure table above is superseded. The homepage metadata, the LLM index and the hero and social launch cards changed together; the visual design, section order and fragment targets are unchanged. See the note on the old headline below.

This is a website design and implementation brief. Storage implementation and release status remain governed by the [consolidated storage plan](../docs/storage-foundation-plan.md), including W20 for publication and documentation gates. This brief adds no package capabilities or release claims.

## Design direction

Replace the current dark, terminal-led presentation with warm ivory, large black headings, forest green actions, thin dividers and generous spacing. Keep monospace for commands and small technical labels. Use the concept's modular containers and documents to make objects, files and checkpoints tangible.

Suggested starting tokens, to be verified against their actual backgrounds:

| Role | Starting value |
| --- | --- |
| Page background | `#F7F5EE` |
| Primary text | `#101613` |
| Secondary text | `#4B554E` |
| Primary action and feature panel | `#123F30` |
| Code background | `#101A16` |
| Dividers | `#B8BDB3` |
| Decorative highlight | `#9AC43B` |

Use the existing system sans and monospace stacks first. Aim for a 1200–1280px content width, 64–96px between major desktop sections, 16–20px mobile gutters, and fluid headings. Reserve the lime highlight for decorative checkpoints; it should not carry readable text on ivory.

The reference establishes hierarchy and visual language. Rebuild its text, links, code and layout as real HTML. Extract neither the entire page nor its generated code listing into a screenshot asset.

## Homepage structure

| Order | Content and purpose | Destination or behavior |
| --- | --- | --- |
| 1 | Header with a restrained Stow wordmark and container mark | Overview `/`, APIs `/docs/api/`, Documentation `/docs/`, GitHub repository |
| 2 | Eyebrow “Portable working storage”; headline “Your work. Stored. Ready to move.”; supporting text “Objects, files and checkpoints for agents, apps and tests. Start locally. Keep history. Take your work with you.” | Primary “Explore the docs” to `/docs/getting-started/`; secondary “View source” to GitHub |
| 3 | Large container illustration beside the hero; language line “Go · TypeScript · Python · WebAssembly” | Decorative art, with any explanatory labels in HTML |
| 4 | Ruled capability strip | Local by default; S3-compatible APIs; Portable checkpoints; Apache 2.0 |
| 5 | “Three ways to stow” with three open columns, individual icons and hairline separators | Local S3 to `/docs/server/`; Agent workspaces to `/docs/workspaces/`; Browser storage to `/docs/browser-projects/` |
| 6 | Pine-green panel: “Give every test its own bucket.” plus scoped-session explanation and a working TypeScript example | TypeScript, Python and Go examples; source setup and API links |
| 7 | “Keep the checkpoint. Carry the work.” plus Prepare → Checkpoint → Handoff illustration | Workspace guide and task manifests; mention verified portable archives |
| 8 | Compact browser demo and API overview | Retain the existing sync simulation, real demo instructions, embedded runtime and read-through cache discovery |
| 9 | “Build with storage you control.” with documentation action and source availability | Copyable source checkout command, Go install command, getting-started link |
| 10 | Minimal footer | License, repository, Chester Hill Solutions attribution and existing AWS trademark context |

The three columns describe entry paths rather than the entire feature inventory. Keep embedded Go/WASM, project revisions, persistent workers, bounded streams and run-through caching discoverable in the API overview and documentation.

Use `#problem` for the three-path overview, `#testing` for the session panel, `#workspace` for checkpoints, `#browser` and `#demo` for browser content, and `#surfaces` and `#edge` for the compact API/cache content. Preserve these existing fragment targets so external section links continue to work.

## Examples and interactive content

Start the session panel with the actual `withStow` API, not the generated image's code. Include both imports and a complete upload operation:

```ts
import { PutObjectCommand } from "@aws-sdk/client-s3";
import { withStow } from "@chester-hill-solutions/stow-s3";

await withStow(async ({ s3, bucket }) => {
  await s3.send(new PutObjectCommand({
    Bucket: bucket,
    Key: "hello.txt",
    Body: "Hello, Stow.",
  }));
});
```

Explain that TypeScript creates its bucket automatically. For Python, use `with_session`, explicitly create a bucket, then upload; for Go, show `stow.Open` with `stow.BackendMemory`, with error handling and cleanup. The Go example should be labeled as an embedded runtime because it starts no HTTP server.

Render all examples as selectable text. If language tabs are added, implement keyboard navigation, focus state and selected-panel semantics with a small enhancement to `assets/stow.js`. Without JavaScript, show each example with a heading. Reuse the existing copy-button behavior.

Retain `assets/sync-demo.js` and its required element IDs. Put the walkthrough in a compact native disclosure under browser storage, with a clear “Try the sync walkthrough” link. Opening `#demo` should expand the disclosure. Keep the simulation's tab-only storage and reload-reset explanation visible, alongside `make browser-editor` and `/docs/browser-editor/` for the real demo. Preserve offline/save/undo/redo/sync/reset behavior and status announcements.

## Assets and shared styling

Create a standalone hero illustration and a smaller checkpoint/handoff illustration. Prefer SVG for crisp containers, document slabs and connecting lines. If illustration fidelity calls for raster artwork, generate only the artwork, export responsive WebP sizes and keep labels in HTML. Set dimensions to reserve layout space. Avoid loading the large design-reference image on the public site.

Refine the existing container-based mark and use a lowercase Stow wordmark that works on ivory. Coordinate `logo.svg`, `wordmark.svg` and `favicon.svg`, including accessible names and dark-panel variants. Retain package identifiers such as `stow-s3` in code and commands.

Documentation currently loads both `assets/stow.css` and `assets/docs.css`; global token changes will affect every guide. Add a small shared `assets/theme.css` for tokens, base typography, buttons, focus treatment, header and footer. Keep landing layout in `stow.css` and documentation layout in `docs.css`. Update the generated template in `build.mjs` to load the shared theme and documentation styles without homepage layout styles.

Carry ivory, pine links and the shared header into documentation. Keep existing sidebar groups, tables of contents, heading anchors, code scrolling and source links. Check API tables, long declarations and mobile navigation carefully. Preserve current working-tree changes to `build.mjs`, `docs.css` and `api-notes.mjs` when implementing this brief.

Update `launch/og.html` and its 1200×630 output in `images/og.png` with the new headline and storage motif. Bring the other launch cards into the same palette and wordmark. Align page title, description and Open Graph copy with “portable working storage”; add a homepage canonical link. Keep `/agent.md`, `/llms.txt` and public guide routes accurate and available.

## Implementation sequence

| Step | Changes | Completion criterion |
| --- | --- | --- |
| 1 | Finalize section copy, link map, complete examples and compact compatibility content | Every claim matches current code or documented behavior; existing fragment targets have a home |
| 2 | Add shared theme, coordinated mark and standalone illustration assets | Homepage and representative documentation render legibly with the new theme |
| 3 | Rebuild `index.html` and landing styles around the hero, three paths, session panel and checkpoint story | Desktop and mobile layouts express the reference's hierarchy with real HTML |
| 4 | Integrate language examples, copy controls and the existing sync walkthrough | Keyboard, no-JavaScript and demo flows work; simulation labels remain explicit |
| 5 | Finish documentation styling, footer, metadata and launch graphics | Landing, guides, API pages and social previews share one visual identity |
| 6 | Run focused checks and visual review, then prepare the deployment diff | All completion criteria below pass; implementation is ready for review |

Keep the existing static build and asset deployment model. The work fits HTML, CSS, SVG and small JavaScript enhancements; it needs no frontend framework, hosted backend or package API change. Leave deployment for a subsequent publishing request.

## Product accuracy

Retain the source-checkout availability note for the unpublished TypeScript and Python candidate. Display the full working commands `git clone https://github.com/chester-hill-solutions/stow-s3` and `go get github.com/chester-hill-solutions/stow-s3/pkg/stow`. Derive any version label from the current package rather than treating the concept's 0.3.0 label as permanent.

Describe Stow as storage. Callers own execution, network access and conflict resolution. Browser sync is explicit; workspace archives transfer saved files. Keep concise links to S3 compatibility and workspace limits. Advertise qualified host support only, with no invented cloud-platform guarantees or performance figures.

`scripts/check-site-claims.mjs` currently requires the homepage to state the embedded runtime defaults of **64 MiB** and **10,000 objects**. Retain those in the compact API details with the runtime explicitly named. Scoped sessions have different defaults; use their API documentation for the details rather than presenting one quota as universal. Keep existing claim checks in force.

## Validation and completion

For implementation, run the existing site build/tests, documentation link checks and relevant claim/install checks:

```sh
make check-site
make check-doc-links
make check-site-claims
make check-install-surface
```

Check installation claims against the release state at implementation time. A site redesign does not certify package publication. Extend `site/test/build.test.mjs` only where needed to cover asset references, retained anchors and generated documentation styling; add interaction tests only for changed language-tab or disclosure behavior.

Review the built local output at 1440px, 1024px, 768px and 390px, plus a narrow 320px overflow check. Include the homepage, documentation index, an API page with a long declaration, a guide with tables, and the Open Graph image. Verify:

- Visitors can identify the three storage paths and reach a relevant guide from the first sections.
- Hero, illustrations, code and footer remain readable without overlap or whole-page horizontal scrolling.
- Normal text has at least 4.5:1 contrast; controls and focus treatment remain visible on both ivory and pine.
- Heading order, skip link, keyboard controls, active tabs and live announcements work. Touch targets are at least 44px.
- JavaScript-disabled visitors can read examples and follow setup instructions; reduced-motion visitors get static artwork.
- Artwork reserves its space, loads at an appropriate size and causes no visible layout jump. Code panels scroll within their own container when needed.
- Existing docs routes, source links, homepage fragments, copy controls and sync simulation still work.
- The reference image and planning file remain repository-only and are excluded from `site/.build`.

The redesign is complete when the landing page visibly follows the reference, documentation uses the same brand treatment, examples and navigation work, and the focused checks pass. Storage behavior and public installation claims remain tied to their existing sources and release gates.
