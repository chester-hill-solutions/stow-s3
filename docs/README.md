# Stow documentation

Choose a guide for your application. The [full reference](../README.md) covers the product.
<!-- repo-only:start -->
[CONTEXT](../CONTEXT.md) defines development terms.

## Browse by folder

| Folder | Start here |
| --- | --- |
| [Guides](guides/README.md) | Installation, native storage and browser workflows |
| [Contracts](contracts/README.md) | S3, workspaces, task manifests and MCP |
| [API reference](api/README.md) | Go, TypeScript, Python and MCP interfaces |
| [Design references](design/README.md) | Product boundaries and retained design rationale |
| [Development](development/README.md) | Current work plan, publishing and performance tools |
| [Release notes](releases/README.md) | Published versions and upgrade notes |
| [History](history/README.md) | Dated assessments, implementation and release checks |
| [Reviews](reviews/README.md) and [benchmarks](benchmarks/README.md) | Findings, measurements and supporting evidence |
<!-- repo-only:end -->

## Getting started

- [Getting started](guides/getting-started.md): install from npm or native downloads and choose an API.
- [v0.3.6 release notes](releases/v0.3.6.md): repository cleanup, native downloads and compatibility.
- [Running a native server](running-and-probing.md): startup, credentials and probes.
- [TypeScript client](../packages/stow-s3/README.md) and
  [Python client](../packages/stow-s3-py/README.md).
- [API reference](api/README.md): methods, parameters, return types, defaults and errors.

## Native storage and workspaces

- [Portable workspace usage](guides/portable-workspace-usage.md): preparation,
  checkpoints, guarded saves, handoff and adoption.
- [Task manifests](contracts/task-manifest.md): inputs, Git checkouts and workspace limits.
<!-- repo-only:start -->
- [Workspace contract](workspace-contract.md) and [shared conformance vocabulary](contracts/workspace-contract-steps.md).
<!-- repo-only:end -->
- [Filesystem store](guides/filesystem-store.md): the data-directory layout, the object-record framing, and
  the ownership and lock rules.
- [Run-through cache](guides/run-through.md) and [durable outbox](guides/outbox.md): offline
  reads, conditional propagation, retry and recovery.
- [S3 compatibility contract](contracts/compat-contract.md): supported behavior and refusals.
- [Native object MCP](contracts/object-mcp-contract.md) and
  [optional MCP bucket browser](../extensions/stow/README.md).

## Browser storage

- [File history](guides/browser-file-history.md): local stages, undo/redo and explicit sync.
- [Project storage](guides/browser-project-storage.md): atomic revisions and shared content.
- [Worker runtime](guides/browser-worker-runtime.md): binary transport, streams and recovery.
- [IndexedDB persistence](guides/browser-persistence.md): objects that survive browser reloads.
- [Runnable editor](../examples/browser-editor/README.md).

<!-- repo-only:start -->
## Decisions and development

- [Architecture decisions](adr/README.md) explain settled choices. Acceptance of
  a design does not mean every proposed API is implemented.
- [Consolidated storage plan](development/storage-foundation-plan.md) owns remaining work.
- [Planning index](development/planning-index.md) separates the current queue, design
  references and historical records.
- [Storage admission design](storage-admission-contract.md) describes the shared
  foundation contract; consult usage guides for implemented API names.
- [Contributing](../CONTRIBUTING.md) and [code standards](CODE_STANDARDS.md).
- [Release verification](history/releases/release-verification-2026-10-14.md) records the downloads-only release; it is a dated checkpoint,
  not evidence that a later candidate was published.
- [npm publication record](development/npm-publishing.md) records the verified 0.3.5 release and remaining automated-publishing requirements.

## Evidence and history

[History](history/README.md) contains dated records and retired plans.

| Browse | Contents |
| --- | --- |
| [Release checks](history/releases/README.md) | Dated readiness and download verification |
| [Implementation records](history/implementation/README.md) | Completed slices and receiver experiments |
| [Assessments](history/assessments/README.md) | Product evaluation and compatibility coverage |
| [Code reviews](reviews/README.md) | Findings, fixes and the bounded correctness register |
| [Benchmarks](benchmarks/README.md) | Current baselines, published comparisons and investigations |
| [Assessment evidence](assessment-evidence/README.md) | Probe methods and test receipts |
| [Evidence archive](evidence-archive.md) | Original logs, profiles and source snapshots in Git history |

Use the [performance testing guide](development/performance-testing.md) to reproduce throughput,
latency, resource and quarantine workflow measurements.
The [October 13 memory results](benchmarks/2026-10-13-memory-performance/README.md)
include a quarantine rate sweep and five-minute memory observations.
The [October 13 Railway comparison](benchmarks/2026-10-13-railway-performance/README.md)
compares Stow with Afero, Go buffers/files, S3Proxy and MinIO.
The [storage optimization results](benchmarks/2026-10-13-storage-optimization/README.md)
compare protobuf records and owned upload buffers against the earlier implementation.
The [expanded competitor results](benchmarks/2026-10-13-competitor-performance/README.md)
add SQLite, RustFS, agent startup/first-write measurements and a private chart preview.
<!-- repo-only:end -->
