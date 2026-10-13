# Getting started

Stow provides an object store you can embed, a native S3 endpoint, and prepared
file workspaces. Hosts own execution and networking. Choose a surface before
choosing persistence.

## Build a checkout

```sh
git clone https://github.com/chester-hill-solutions/stow-s3
cd stow-s3
make build build-wasm
```

The Go version is pinned in `.go-version`. For the TypeScript library and browser
examples, also install and build the package:

```sh
npm ci --ignore-scripts --omit=optional --prefix packages/stow-s3
npm run build --prefix packages/stow-s3
```

The Go module can be added directly:

```sh
go get github.com/chester-hill-solutions/stow-s3/pkg/stow
```

The documented 0.3.0 candidate uses source-checkout instructions for TypeScript
and Python. Check the [release checkpoint](release-verification-2026-10-03.md)
before treating a registry install as qualified.

## Choose your storage surface

| Workload | Start with |
| --- | --- |
| Existing S3 SDKs or disposable integration tests | [Native server](running-and-probing.md) and the [TypeScript client](../packages/stow-s3/README.md) |
| An agent's working directory and checkpoints | [Workspace guide](portable-workspace-usage.md) |
| Small-file offline editing with undo and redo | [Browser file history](browser-file-history.md) |
| Several files, retained revisions or larger assets | [Browser project storage](browser-project-storage.md) |
| Bounded worker requests and streaming object payloads | [Browser worker runtime](browser-worker-runtime.md) |
| A local cache in front of a remote bucket | [Run-through guide](run-through.md) |

## Try the browser editor

```sh
make browser-editor
```

The [editor walkthrough](../examples/browser-editor/README.md) uses real WASM,
IndexedDB and a private local S3 fixture. It demonstrates offline reload, retained
edits, undo/redo and conditional remote sync.

## Guarantees to check

Local mode is the default. Native live writes require explicit consent.
Browser applications supply the remote transport and decide when to sync.
Conflicts preserve local work and need an application decision. Browsers can
evict storage; quotas and retained history need an appropriate byte budget.

Read the [compatibility contract](compat-contract.md) for the native S3 subset,
and the relevant browser guide for host and persistence limits.
