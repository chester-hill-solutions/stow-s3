# Situation — stow-s3 architecture overview

**Target:** `/home/nathaniel-arfin/Documents/stow` at `3066d37747aaa56b29a39961801c0116c39600fa` (clean tree).
**Object:** `stow-s3` — a portable working-storage library for agents, tools, applications and tests.
**Scope:** the whole repository (Go core, TypeScript client, Python client, MCP extension, site, examples, scripts, tools).
**Prior runs:** none. This is a cold run; the coverage statement must not imply the map exhausts the target.

## What the system is

Stow stores bytes for programs that are not themselves storage. It has one object model
(buckets, objects, multipart uploads, conditional writes, recovery holds) and four places that
model is exposed: an in-process Go API (`pkg/stow` / `internal/embedded` / `internal/runtime`),
an S3-compatible HTTP server (`internal/s3api`), a WebAssembly host bridge (`cmd/stow-wasm`),
and MCP tool servers (`internal/mcpstorage`). Two clients — `packages/stow-s3` (TypeScript) and
`packages/stow-s3-py` (Python) — are independent implementations that drive the native binary as
a child process and read a readiness record from an inherited file descriptor.

The authority documents (`CONTEXT.md`, `docs/adr/*`) state the intended seams explicitly. They
are the yardstick for whether the source keeps them, not a substitute for reading the source.

## The surfaces

| Surface | Entry | What it owns |
|---|---|---|
| Native CLI | `cmd/stow-s3/main.go:165` (`serve`, `doctor`, `mcp`, `workspace`, `prewarm`) | command table, flag/env reconciliation, store construction, readiness publication |
| S3 HTTP | `internal/s3api/server.go:81` (`s3api.New`) | SigV4 gate, request admission, S3 route dispatch, XML error shapes, `/_stow` admin routes |
| Go library | `pkg/stow/runtime.go:9`, `pkg/stow/workspace.go:116` | public surface; re-exports `internal/embedded` and adds workspace lifecycle, checkpoints, deltas, handoffs |
| Embedded runtime | `internal/embedded/runtime.go:19` → `internal/runtime/instance.go` | quota/usage accounting, authority+policy enforcement on every verb, multipart accounting, guarded saves |
| WASM bridge | `cmd/stow-wasm/main.go:125` (`globalThis.stow`), `cmd/stow-wasm/operations.go:14` | JSON v1 / binary v2 request protocol, handle table, JS-host-backed store |
| MCP | `internal/mcpstorage/server.go:42`, `internal/mcpstorage/object_server.go:43` | workspace/checkpoint tools; scoped object-store tools with bounded reads and retained save requests |
| TypeScript client | `packages/stow-s3/src/index.ts:107` | child-process lifecycle, AWS SDK v3 wiring, embedded/WASM facade, browser+worker+project profiles |
| Python client | `packages/stow-s3-py/src/stow_s3/session.py:277` | child-process lifecycle, boto3 wiring, workspace CLI veneer |
| MCP extension | `extensions/stow/src/app.ts` | read-only bucket-browser UI bundled into the Go binary; Agent Plugin manifest |

## The seams (verified during reconnaissance)

| Seam | Interface | Adapters | Real? |
|---|---|---|---|
| `internal/storage/store.go:13` | `storage.Store` | MemoryStore, fs.FilesystemStore, workspace.Store, runthrough.Adapter, runtime.StoreAdapter, embedded.storeAdapter, wasm hostStore | yes (7) |
| `internal/storage/store.go:48` | `storage.MultipartStore` | MemoryStore, fs, workspace, runthrough | yes (4) |
| `internal/storage/store.go:98` | `storage.ConditionalCopyStore` | fs.FilesystemStore, runtime.StoreAdapter | yes (2) |
| `internal/storage/guarded.go:12` | `storage.GuardedWriteStore` | MemoryStore, fs.FilesystemStore | yes (2) |
| `internal/storage/save_requests.go:24` | `storage.SaveRequestStore` | fs.FilesystemStore only | **no** — one adapter |
| `internal/storage/recovery.go:29` | `storage.RecoveryHoldStore` | fs.FilesystemStore, workspace.Store | yes (2) |
| `internal/embedded/store.go:15` | `embedded.Store` | caller-supplied Go stores, wasm hostStore | yes (2) |
| `internal/runthrough/outbox.go:85` | `runthrough.Outbox` | MemoryOutbox, FileOutbox | yes (2) |
| `internal/runthrough/upstream.go:19` | `runthrough.Client` | S3Client only | **no** — one adapter |
| `internal/policy/source.go:15` | `policy.Source` | policy.Fixed, policy.Expiring, policystore.Store | yes (3) |
| `internal/authority/authority.go:59` | `authority.Authority` | All/None/ReadOnly/ReadWrite + run-through attenuation | yes |
| `internal/s3api/auth.go:11` | `s3api.AuthFunc` | SigV4Auth, DevBypass, doctorAuth | yes (3) |

## Dependency facts

- No import cycles among the Go packages. The only back-edge is test-only:
  `internal/storage/backend_contract_test.go` imports the concrete backends it asserts on.
- `internal/s3api` is the only internal package importing both `internal/runtime` and
  `internal/runthrough` (`internal/s3api/errors.go:14-15`), which is what lets one S3 error
  vocabulary cover runtime refusals and upstream refusals.
- `internal/authority` is dependency-free by design. `internal/policy` composes it;
  `internal/policystore` is split out so the decision model never links the filesystem.
- `cmd/stow-wasm` imports `internal/embedded` directly and bypasses `pkg/stow`, so the WASM
  surface is not covered by the public package's alias layer.
- `internal/runthrough` is the only package that reads the environment
  (`STOW_*` > `S3_*` > `AWS_*`), reached from the CLI through one reconciliation function.
- `pkg/stow` is mixed: `types.go`, `store.go`, `runtime.go` are pure re-export barrels over
  `internal/embedded`; the workspace, checkpoint, delta, handoff and registry-policy files own
  real logic.

## The switches (where the real seams live)

- CLI backend selection: `openLocalStore` (`cmd/stow-s3/main.go:117`); the flag becomes a typed
  value at `parseBackend` (`:99`). The CLI refuses `--backend workspace` (`:103-108`).
- Local vs run-through: `resolveMode` (`cmd/stow-s3/main.go:243`) plus
  `runthrough.DetectMode`; whether an upstream client is constructed at all is decided in
  `buildStore` (`cmd/stow-s3/store.go:26`). Local mode never constructs one.
- Live-write consent: `STOW_ALLOW_LIVE_WRITES` / `--allow-live-writes` only; folded once into
  the authority at `internal/runthrough/config.go:114`.
- Storage backend in the library: `pkg/stow.OpenFilesystem` (durable, exclusive lock) vs
  `pkg/stow.OpenWorkspace` (real files in the caller's directory) vs `pkg/stow.Open` (memory).

## Known documentation drift (established during reconnaissance, to be checked, not assumed)

- ADR 0008 and `CONTEXT.md` name a manifest `stow-workspace.json` at the workspace root; the
  source keeps `<dir>/.stow/manifest.json`, which is what `docs/workspace-contract.md` §4 specifies.
- ADR 0003's out-of-scope paragraph still describes a memory-only `pkg/stow`; the source exports
  `BackendFilesystem`, `BackendHost`, `BackendWorkspace`.
- ADR 0010 §1 says `EnvironmentPromote` was removed from the operation set; the source still
  exports it and `internal/authority` records it as `Ungated`.
- ADR 0013, 0015, 0016, 0017 and 0018 state surfaces that do not exist in the source. These are
  stated-but-unimplemented, which is a fact about the documents, not a defect in the code.

## Companion selection

`docs/CODE_STANDARDS.md` (the ratchet policy and the gate list), `CONTEXT.md` (the product
glossary and boundary), `docs/adr/README.md` (which records govern), `docs/planning-index.md`
(which document is the current work order). The ADR files themselves are read directly by the
workers that cover a boundary they decide.

## Corrections to this summary (appended by the parent as the run proceeds)

This file is the parent's Phase 1 reading. Where a worker later established that a fact here is
wrong, the correction is appended here rather than editing the Phase 1 text, so the audit trail
survives and later waves are not misled.

- **(mapper-2, wave 1)** The `storage.Store` adapter count is **nine**, not seven: three backing
  stores (`storage.MemoryStore`, `fs.FilesystemStore`, `workspace.Store`), three adapters over
  another store (`runthrough.Adapter`, `runtime.StoreAdapter`, `embedded.storeAdapter`) and three
  decorators that embed it. The WASM `hostStore` does **not** implement `storage.Store` — it
  implements the public `embedded.Store` and reaches the seam only through `embedded.adaptStore`.
- **(mapper-2, wave 1)** `storage.ConditionalCopyStore` has **five** production adapters (memory,
  fs, workspace, runthrough, runtime) — a real seam. `storage.SaveRequestStore` and
  `storage.RecoveryHoldStore` each have **one** (`fs.FilesystemStore`) and are hypothetical seams,
  not real ones.
- **(mapper-2, wave 1)** `workspace.Registry.AdmitRecoveryHold` is a different concept with a
  different signature from `storage.RecoveryHoldStore`; the workspace does **not** implement the
  storage-level recovery-hold interface.
- **(mapper-2, wave 1)** Backend selection happens in **three** places, not one: the CLI, the Go
  library, and the WASM bridge. Reset availability is a property of the binding
  (`runtime.OpenWithStore`'s `resetStore` hook), not of the store.
- **(mapper-1, wave 1)** Independently confirmed the same three corrections: `storage.RecoveryHoldStore`
  has one adapter (fs only), `storage.ConditionalCopyStore` has five, and the WASM host store is an
  `embedded.Store` adapter rather than a `storage.Store` one.
- **(mapper-5, wave 1)** "Local mode never constructs an upstream client" is a property of `serve`
  **alone**: the `prewarm` verb constructs an upstream client from the environment without
  consulting the mode switch. `pkg/stow`'s precedence (`STOW_*` > `S3_*` > `AWS_*`) lives in
  `internal/runthrough/config.go` and is named by no CLI surface; `--backend workspace` is refused
  at `cmd/stow-s3/main.go:103` while the flag help says only "filesystem or memory".

## Excluded trees (not modules of the target)

`packages/stow-s3/dist`, `internal/mcpstorage/browser.html.gz`, `extensions/stow/plugin/LICENSE`
and `THIRD_PARTY_LICENSES.txt` — generated build output. The four `packages/stow-s3-*-{platform}`
directories — binary carriers with no source of their own. `**/node_modules`, `**/.cache`,
`**/__pycache__`, `site/.build` — installed or generated trees. `site/node_modules` in particular
must never be read as source.
