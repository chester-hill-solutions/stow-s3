# Changelog

## Unreleased — 0.3.0 development candidate

This is the development tree. Release qualification and publication are separate
from local implementation; see the [release checkpoint](docs/release-verification-2026-10-03.md).

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
