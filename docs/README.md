# Stow documentation

Start with the guide for the storage surface your application uses. The
[README](../README.md) is the full product reference; [CONTEXT](../CONTEXT.md)
defines the repository's terms.

## Getting started

- [Getting started](getting-started.md): build a checkout and choose an API.
- [Running a native server](running-and-probing.md): startup, credentials and probes.
- [TypeScript client](../packages/stow-s3/README.md) and
  [Python client](../packages/stow-s3-py/README.md).

## Native storage and workspaces

- [Portable workspace usage](portable-workspace-usage.md): preparation,
  checkpoints, guarded saves, handoff and adoption.
- [Workspace contract](workspace-contract.md), [task manifests](task-manifest.md)
  and [shared conformance vocabulary](workspace-contract-steps.md).
- [Run-through cache](run-through.md) and [durable outbox](outbox.md): offline
  reads, conditional propagation, retry and recovery.
- [S3 compatibility contract](compat-contract.md): supported behavior and refusals.
- [Native object MCP](object-mcp-contract.md) and
  [optional MCP bucket browser](../extensions/stow/README.md).

## Browser storage

- [File history](browser-file-history.md): local stages, undo/redo and explicit sync.
- [Project storage](browser-project-storage.md): atomic revisions and shared content.
- [Worker runtime](browser-worker-runtime.md): binary transport, streams and recovery.
- [IndexedDB persistence](browser-persistence.md): the original bounded object profile.
- [Runnable editor](../examples/browser-editor/README.md).

## Decisions and development

- [Architecture decisions](adr/README.md) explain settled choices. Acceptance of
  a design does not mean every proposed API is implemented.
- [Consolidated storage plan](storage-foundation-plan.md) owns remaining work.
- [Planning index](planning-index.md) separates the current queue, design
  references and historical records.
- [Storage admission design](storage-admission-contract.md) describes the shared
  foundation contract; consult usage guides for implemented API names.
- [Contributing](../CONTRIBUTING.md) and [code standards](CODE_STANDARDS.md).
- [Release verification](release-verification-2026-10-03.md) is a dated checkpoint,
  not evidence that a later candidate was published.

## Evidence and history

[History](history/README.md) contains retired work orders and experiments.
[Assessment receipts](assessment-evidence/2026-09-29/README.md),
[Railway browser qualification](assessment-evidence/2026-10-12/railway-browser.json)
and [benchmarks](benchmarks/session-baseline.md) have their own dates and scope.
They do not turn a proposal or an untested host into a supported feature.
