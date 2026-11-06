# Getting started

Stow stores objects in a native S3 server, an embedded runtime, or a file workspace.
Browser APIs store editing history in IndexedDB and sync to remote storage.
Choose the API that matches your application.

## Install the TypeScript client

```sh
npm install @chester-hill-solutions/stow-s3@0.3.5
```

The package includes the native carrier for your Linux or macOS host on x64 or ARM64.
Native sessions need no Go installation or `STOW_BIN` setting.
The package also includes the WASM assets for its embedded and browser entry points.
Remove any old `@chester-hill-solutions` registry override to `npm.pkg.github.com`.

## Install the native server

Download the matching Linux or macOS archive from
[Stow v0.3.6](https://github.com/chester-hill-solutions/stow-s3/releases/tag/v0.3.6).
Both x86_64 (`amd64`) and ARM64 (`arm64`) are supported. Check the archive against
`SHA256SUMS.txt` from that release, extract it and put `stow-s3` on your PATH.
Then run `stow-s3 --version`.

The Python client is not yet published to PyPI. Use the source steps below for Python
or to run the repository's development examples. Read the [release notes](../releases/v0.3.6.md) before
upgrading a persistent filesystem object store: older versions cannot read new
version-2 object records.

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

Python installation still requires a source checkout. TypeScript applications can use the npm package above.
<!-- repo-only:start -->
See the [npm publication record](../development/npm-publishing.md#published-035) for exact release verification.
<!-- repo-only:end -->

## Choose your storage surface

| Workload | Start with |
| --- | --- |
| Runnable examples for tests, local apps and pipelines | [Examples](../../examples/stow-recipes/README.md) |
| Existing S3 SDKs or disposable integration tests | [Native server](../running-and-probing.md) and the [TypeScript client](../../packages/stow-s3/README.md) |
| An agent's working directory and checkpoints | [Workspace guide](portable-workspace-usage.md) |
| Small-file offline editing with undo and redo | [Browser file history](browser-file-history.md) |
| Several files, retained revisions or larger assets | [Browser project storage](browser-project-storage.md) |
| Bounded worker requests and streaming object payloads | [Browser worker runtime](browser-worker-runtime.md) |
| A local cache in front of a remote bucket | [Run-through guide](run-through.md) |

## Try the browser editor

```sh
make browser-editor
```

The [editor walkthrough](../../examples/browser-editor/README.md) uses real WASM,
IndexedDB and a private local S3 fixture. It demonstrates offline reload, retained
edits, undo/redo and conditional remote sync.

## Guarantees to check

Local mode is the default. Enable native upstream writes explicitly.
Browser applications supply the transport and choose when to sync.
A conflict preserves local work; the application decides how to resolve it.
Budget storage for retained history. The browser can evict local data.

Read the [compatibility contract](../contracts/compat-contract.md) for the native S3 subset,
and the relevant browser guide for host and persistence limits.
