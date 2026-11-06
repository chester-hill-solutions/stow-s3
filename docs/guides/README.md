# Usage guides

Choose a workflow for the application you are building. The
[API reference](../api/README.md) documents individual methods and parameters.

## Start here

- [Getting started](getting-started.md): install Stow and choose an API.
- [Running a server](../running-and-probing.md): credentials, startup and probes.
- [Current release](../releases/v0.3.6.md): installation and compatibility notes.

## Native storage

| Guide | Covers |
| --- | --- |
| [Portable workspaces](portable-workspace-usage.md) | Preparation, checkpoints, guarded saves and handoff |
| [Filesystem store](filesystem-store.md) | Object records, data directories, ownership and locks |
| [Run-through cache](run-through.md) | Offline reads and explicit upstream selection |
| [Durable outbox](outbox.md) | Queued writes, retries and uncertain outcomes |

## Browser storage

| Guide | Covers |
| --- | --- |
| [File history](browser-file-history.md) | Local stages, undo/redo and explicit sync |
| [Project storage](browser-project-storage.md) | Atomic revisions and shared content |
| [Worker runtime](browser-worker-runtime.md) | Binary transport, streams and recovery |
| [IndexedDB persistence](browser-persistence.md) | Objects that survive page reloads |
| [Editor example](../../examples/browser-editor/README.md) | A runnable integration |

[All documentation](../README.md)
