# Changelog

## Unreleased

Close the three correctness findings the bounded closure review left open after
v0.3.3. A signed HTTP header now covers every value it carries, so a request
that changes one after signing is refused
([repeated signed header fix](docs/reviews/2026-10-19-sigv4-repeated-header.md)).
An entity tag is compared as the opaque identifier it is rather than folded by
case, so a conditional request carrying a case-changed tag no longer matches
([opaque entity tag fix](docs/reviews/2026-10-19-opaque-etag.md)). The canonical
query is ordered by the encoded parameter names, as the signing rules require,
so a query signed by the pinned Node signer with an encoded name authenticates
([query order fix](docs/reviews/2026-10-19-sigv4-query-order.md)).

These are corrections on `main` after the `v0.3.3` tag. The published v0.3.3
archives carry the three issues their
[release notes](docs/releases/v0.3.3.md) list.

## 0.3.3 — 2026-10-18

Combine PR #77 cleanup, multipart identity, read-only snapshot reconciliation
and preparation/checkpoint resource admission with PR #78 core quality fixes.
Report only a confirmed absence as an absent object, so a create-only write is
refused over workspace bytes the store could not read.
Native GitHub downloads remain selected; npm and PyPI stay deferred. See the
[release notes](docs/releases/v0.3.3.md) for compatibility and known issues.

The public v0.3.2 tag identifies #78 before reconciliation. Its publication was
cancelled; no v0.3.2 GitHub release was published.

## 0.3.2 — 2026-10-18

Correct conditional COPY snapshots, ambiguous storage outcomes, short reads,
recovery and namespace capacity accounting. Reduce measured COPY, read, multipart
and outbox allocation costs. Outbox writes now use framed protobuf; older JSON
journals migrate on write and older releases cannot read the new format.

Four confirmed correctness limitations remain, including workspace create-only
overwrite on an unreadable destination. See the [release notes](docs/releases/v0.3.2.md)
for distribution, compatibility and the complete known-issue list.

## 0.3.1 — 2026-10-14

Adds reproducible performance measurement tools and carefully scoped benchmark
results to the documentation site. These exploratory measurements do not claim
sustained production capacity or broad performance superiority. Runtime APIs and
storage formats are unchanged from v0.3.0.

See the [release notes](docs/releases/v0.3.1.md) and [performance testing guide](docs/performance-testing.md).

## 0.3.0 — 2026-10-14

Native downloads for Linux and macOS on x86_64 and ARM64 are distributed through
GitHub Releases. npm and PyPI publication remain deferred; use source checkouts
for those clients. See the [release notes](docs/releases/v0.3.0.md) and
[verification checkpoint](docs/release-verification-2026-10-14.md).

### Storage performance and compatibility

- Protobuf metadata and raw filesystem payloads, header-only metadata reads,
  incremental managed usage accounting and streaming GET/COPY.
- Bounded staging for ordinary unbound filesystem uploads outside the publication
  lock, with integrity checks and commit-time conditions and quotas.
- Safe overlapping reads, caller-owned GET results and adoption of internally
  owned HTTP request bodies.
- A smaller embedded WASM runtime, streaming browser instantiation where supported,
  and stream-buffer reuse after copying returned bytes into JavaScript.
- Compact native workspace indexes with unchanged fields and format version.
- Legacy JSON object records remain readable. New writes use version 2, which
  older Stow versions cannot read; preserve a backup if you need to downgrade.

### Browser storage

- Durable file stages with undo/redo, read-through access and conditional S3 sync.
- Atomic multi-file project revisions with shared content chunks, bounded caches,
  explicit migration and safe garbage collection.
- A binary WASM bridge, persistent workers, range reads and bounded object streams.
- A real offline editor demo, Chromium qualification and a dedicated Railway
  live-test workflow using encrypted environment secrets.

### Native storage and integration

- Prepared workspaces with immutable file/portable checkpoints, comparison,
  verified handoff, adoption and managed saves with replayable requests.
- Resource-scoped policy checks, explicit live-write consent and conditional
  upstream propagation with durable outbox recovery.
- Recovery checks content type, normalized metadata and requested checksums as
  well as the remote ETag; stale remote state is refused rather than overwritten.
- An opt-in read-only MCP bucket browser and caller-owned agent integration.
- Reproducible generated artifacts and gates for standards, conformance,
  installation claims, documentation and platform behavior.

The full dated development narrative and previous validation results are retained
in [development history](docs/history/changelog-through-2026-10-03.md).
Usage and limits live in the [documentation index](docs/README.md).

## 0.1.0

- Initial local S3-compatible development service and TypeScript wrapper.
