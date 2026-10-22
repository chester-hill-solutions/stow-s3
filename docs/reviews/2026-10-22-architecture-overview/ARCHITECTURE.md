# Architecture — stow-s3

A source-grounded map of the modules, seams, flows and dependencies the run reached. Derived
from `records.json`; every claim here is stated in a record with a file and line, and every record
passed independent validation and verification. Coverage is 15 of 41 units — see §8 and
`OPEN-QUESTIONS.md`.

## 1. The shape

Stow stores bytes for programs that are not themselves storage. It has one object model and
several places that model is exposed.

```
                    ┌──────────────────────────────────────────────┐
   clients ────────▶│ packages/stow-s3 (TS)   packages/stow-s3-py   │  not covered by this run
                    └──────────────────┬───────────────────────────┘
                                       │ child process + inherited fd
                                       ▼
                    ┌──────────────────────────────────────────────┐
   command line ───▶│ cmd/stow-s3                                  │  mapped
                    │  serve · doctor · mcp · workspace · prewarm  │
                    └───────┬──────────────────────┬───────────────┘
                            │                      │
              ┌─────────────▼────────┐   ┌─────────▼──────────┐
   S3 wire ──▶│ internal/s3api       │   │ internal/mcpstorage│  s3api mapped,
              │ SigV4 · routes · XML │   │                    │  mcp not covered
              └─────────┬────────────┘   └─────────┬──────────┘
                        │                          │
                        └────────────┬─────────────┘
                                     ▼
                    ┌──────────────────────────────────────────────┐
   Go library ─────▶│ internal/runtime.Instance                    │  mapped
                    │ authority · policy · quota · accounting      │
                    └──────────────────┬───────────────────────────┘
                                       │ storage.Store
                     ┌─────────────────┼──────────────────┐
                     ▼                 ▼                  ▼
            ┌────────────────┐ ┌──────────────┐ ┌──────────────────┐
            │ storage.Memory │ │ storage/fs   │ │ storage/workspace│  mapped
            └────────────────┘ └──────────────┘ └──────────────────┘
                                       ▲
                              internal/runthrough.Adapter          mapped
                              local + cache + upstream
```

## 2. Modules reached

Nineteen `mapped` records. Grouped by subsystem.

### The object model

- **`internal/storage` — the contract.** `storage.Store` is twelve methods: four bucket
  operations, seven object operations, `Close` (`internal/storage/store.go:13-40`). Multipart is
  deliberately not a member (`:8-12`); it and eleven other capabilities are optional interfaces
  discovered by type assertion. The package also carries the rules the model is made of — bucket
  and key validation, the quoted-MD5 ETag and its multipart composite form, five checksum digests
  and the compute-or-verify decision, the `If-Match` grammar, three pagination algorithms, the
  5 MiB minimum part size — plus one in-memory backend. *Depth: mixed* — deep in the rules,
  shallow in the capability-discovery mechanism, which is re-derived at roughly two dozen sites.
- **`internal/storage/fs` — the durable store.** The whole data-directory contract: layout, the
  version-2 record codec (`STOWOBJ2` magic, protobuf header, raw payload), the exclusive lock,
  atomic publication, staged writes, the save journal, the multipart manifest, capacity
  accounting and recovery holds. Constructed only by `fs.NewFilesystemStore`; the runtime is
  forbidden from importing it (`internal/runtime/dependencies_test.go:25`) and finds its
  capabilities by assertion. *Depth: deep at the boundary, wide inside* — 3,733 non-test lines
  across 27 files owning five separable concerns behind one concrete type with no internal seam.
- **`internal/storage/workspace` — objects as real files.** `Store` satisfies `storage.Store`
  plus multipart, conditional-copy and put-checksum. Owns the key-to-path encoding, the two
  on-disk documents (`<root>/.stow/manifest.json`, `<root>/.stow/index.json`, layout version 3),
  the case-collision index, and the symlink-free confinement every read and write routes through
  (`internal/rooted`). Diverges from an ordinary bucket in three named ways: the workspace bucket
  *is* the root directory, a second bucket is namespaced under `<root>/.stow/buckets/<name>`, and
  the workspace bucket cannot be deleted. *Depth: deep.*
- **`internal/storage/workspace` — the registry.** One JSON file per workspace ID under the
  user's config directory, partitioned per team by directory rather than by label; three advisory
  locks (session, capture, registry-mutation) with a fixed acquisition order; and the
  recovery-hold journal binding a published capture to the workspace it depends on. *Depth: deep.*
- **`internal/storage/workspace` — the logical snapshot reader.** `ReadLogicalSnapshot` reads a
  workspace's object state without opening a writable store, without taking the session lock and
  without writing: it pins the root, reads the two documents through it, walks the tree refusing
  a symbolic link outright, and applies byte and object caps while reading. Its one production
  consumer is the portable checkpoint's inventory step. *Depth: deep.*

### The governance layer

- **`internal/runtime` — `Instance`.** The in-process layer every surface passes through before
  it touches bytes. Owns the authority-and-policy decision on every verb, quota admission against
  `MaxBytes`/`MaxObjects`/`MaxMultipartUploads`, incremental usage counters and multipart
  reservation accounting, guarded-save condition binding and durable save-request replay, and the
  reset/close lifecycle. Delegates the bytes to a `storage.Store` and publishes itself back as one
  through `runtime.StoreAdapter`. *Depth: deep.*

### The surfaces

- **`internal/s3api` — the S3 wire.** Owns the SigV4 gate, request admission and the body-size
  limit, the route table and dispatch, the XML wire vocabulary, the conditional-request, checksum
  and range rules it evaluates itself, CORS, and the `/_stow` admin and metrics routes. Holds no
  storage state. Owns one decision nothing below it owns: `mapStorageError` maps storage
  sentinels, runtime refusals, authority refusals and upstream refusals onto one S3 code-and-status
  set — which is why it is the only internal package importing both `internal/runtime` and
  `internal/runthrough` (`internal/s3api/errors.go:14-15`). *Depth: deep.*
- **`cmd/stow-s3` — the native CLI.** The interface is the command line *plus the environment*: a
  five-word command table, a seventeen-verb workspace table, 28 `serve` flags, and roughly 30
  environment variables reconciled with `STOW_* > S3_* > AWS_*` precedence. Owns backend
  selection, mode selection, the live-write consent fold, the refusal set, readiness publication
  and shutdown. *Depth: mixed* — deep where it decides, shallow as a routing table.
- **`pkg/stow` — the public Go library.** Mixed by construction: `types.go`, `store.go`,
  `runtime.go`, `errors.go`, `authority.go`, `guarded_save.go`, `object_recovery.go`,
  `save_requests.go`, `staged.go` and `streams.go` are re-export barrels over `internal/embedded`
  (rejected as modules); the workspace, checkpoint, delta, handoff and registry-policy files own
  real logic and are mapped separately:
  - workspace lifecycle and registry — *deep*
  - workspace preparation and seeding — *deep*
  - workspace S3 facade — *deep*
  - checkpoint capture, store and comparison — *deep*
  - checkpoint archives and handoff bundles — *deep*
  - delta documents — *deep*
  - registry policy and recovery holds — *mixed* (real admission arithmetic, pass-through hold verbs)
  - filesystem backend entry — *mixed* (a real validation list and store-before-runtime ordering, but about half the body is pure delegation into `internal/storage/fs` and `internal/runtime`; validation corrected this from *shallow*)

### The authentication boundary

- **`internal/auth`.** The one place a request is authenticated, before `s3api` dispatch. It
  parses and validates SigV4 in both forms — a header `Authorization` and a presigned query
  string — rebuilds the canonical request, derives the signing key, and compares the HMAC. It
  owns the canonical URI/query/header construction, the required signed-header rules, clock-skew
  and presigned-expiry validation, payload-hash validation, strict parsing of the authentication
  material (refusing ambiguity rather than repairing it), the SigV2 refusal, and the local
  credential generation the CLI and the Workspace facade use. Its exported surface is small
  (`Verifier`, `Credentials`, `Error`, five functions) over a large body of security decisions.
  *Depth: deep.* Three adapters sit at the seam it defines: `SigV4Auth`, `DevBypass` and the
  doctor's variant.

### The client and protocol surfaces

- **`packages/stow-s3` — the TypeScript client.** Resolves the native binary, spawns `serve` with
  a built argv and child environment, waits for one readiness record on descriptor 3, and returns
  an AWS SDK v3 client bound to the loopback endpoint. Also exposes an in-process WASM facade,
  the workspace verbs, an ownership-guarded reset, and a doctor. *Depth: deep* — the root barrel
  files are thin, but `start.ts`, `ready-reader.ts`, `ready.ts`, `instance.ts`, `session.ts` and
  the embedded facade carry the lifecycle.
- **`packages/stow-s3-py` — the Python client.** A second, hand-maintained implementation of the
  same contract: it starts the same child, reads the same record over an inherited pipe, and
  returns a `Session` owning the child. No S3 I/O of its own; boto3 is an optional extra.
  *Depth: mixed* — `session.py`, `ready.py` and `bin.py` own real decisions, while `workspace.py`
  is a veneer over the native CLI and `__init__.py` is a re-export barrel.
- **`internal/ready` — the session readiness record (a Data flow).** One value, published by
  `serve` to a caller-supplied descriptor (or the legacy `STOW_READY` stdout line when none is
  given) and read by both clients. *Depth: mixed* — the interface's depth is in the wire contract
  it fixes and the credential-off-stdout decision it encodes, not in the size of the code. **The
  three implementations share no schema**: the Go structs, TypeScript interfaces and Python
  dataclasses restate the field set independently.
- **The workspace readiness record (a second Data flow).** A distinct value published by
  `workspace serve`, carrying the workspace identity plus the facade endpoint and credentials.
  Both records report version 1; only the identity key name separates them.
- **`cmd/stow-wasm` — the WASM host bridge.** Publishes one global, `globalThis.stow`, with five
  members; runs a v1 JSON protocol and a v2 binary protocol discriminated by the request's version
  field; addresses runtimes by a monotonic integer handle; and adapts a JS-supplied store into the
  runtime through `embedded.adaptStore`. *Depth: deep* — five global functions hide a protocol, a
  lifetime model, an ownership model and a runtime binding, and the package bypasses `pkg/stow`.
- **`internal/mcpstorage` — two MCP servers in one package.** The object server (`stow-objects`)
  exposes five scoped object tools and a read-only browser, binding the client to one fixed bucket
  and returning bounded bytes plus an observation token that a save consumes. The workspace server
  (`stow-storage`) exposes a workspace inspect plus four checkpoint verbs, and holds no runtime —
  it re-resolves the workspace through `pkg/stow` on every call. Both are *deep*; the package is a
  container rather than a module, and the two halves share `maxResponseBytes` and one CLI flag set
  without a declared relationship.

### The run-through adapter

- **`internal/runthrough`.** Turns a real S3-compatible provider into something an agent can use
  without holding the provider's credential. `Adapter` implements `storage.Store` and
  `storage.MultipartStore` over three stores: an authoritative local store, an optional separate
  cache, and an optional upstream client. Reads resolve local → cache → upstream; a cached copy is
  revalidated against ETag and Last-Modified; a transient upstream failure serves the cached copy.
  Writes land locally first and propagate only through a durable outbox and only with consent. The
  same package owns the environment precedence, the mode switch, the effective write-policy naming
  and the retry classification. *Depth: deep.*

## 3. Seams

An interface with one adapter is a hypothetical seam; two is a real one. Adapter counts below
were established by reading the implementations.

| Seam | Interface declared at | Production adapters | Real? |
|---|---|---|---|
| Storage backend | `internal/storage/store.go:13` | 9 — memory, fs, workspace, runthrough, runtime adapter, embedded adapter, and three decorators that embed the interface | **yes** |
| Conditional copy | `internal/storage/store.go:98` | 5 — memory, fs, workspace, runthrough, runtime adapter | **yes** |
| Multipart | `internal/storage/store.go:48` | 4 | **yes** |
| Guarded write | `internal/storage/guarded.go:12` | 2 — memory, fs (fs only on darwin/linux) | **yes** |
| Run-through outbox | `internal/runthrough/outbox.go` | 2 — memory, file | **yes** |
| Policy source | `internal/policy/source.go:15` | 3 — fixed, expiring, policystore | **yes** |
| S3 authenticator | `internal/s3api/auth.go:11` | 3 — SigV4, dev bypass, doctor | **yes** |
| Embedded store | `internal/embedded/store.go:15` | 2 — caller-supplied Go stores, WASM host store | **yes** |
| Authority | `internal/authority/authority.go:59` | All/None/ReadOnly/ReadWrite plus run-through attenuation | **yes** |
| Save request | `internal/storage/save_requests.go:24` | 1 — `fs.FilesystemStore` only | **no** |
| Recovery hold | `internal/storage/recovery.go:29` | 1 — `fs.FilesystemStore` only | **no** |
| Usage revision | `internal/storage` (`UsageRevisionStore`) | 1 — `fs.FilesystemStore` only | **no** |
| Object usage | `internal/storage` (`ObjectUsageStore`) | 1 — `fs.FilesystemStore` only | **no** |
| Run-through upstream client | `internal/runthrough/upstream.go:19` | 1 — `S3Client` only | **no** |

Four of the fourteen are declared as substitutable and are not. That is the single most repeated
finding in the run.

## 4. Selection sites

Backend selection happens in **three** places, not one. A policy that makes a backend illegal in
one place can be bypassed in another.

1. **The native CLI** — `parseBackend` (`cmd/stow-s3/main.go:99`) → `openLocalStore` (`:117`) →
   `buildStore` (`cmd/stow-s3/store.go:24`) → `bindNativeRuntimeStore` (`cmd/stow-s3/runtime_store.go:37`).
   Refuses `--backend workspace` (`:103-108`).
2. **The Go library** — `pkg/stow.OpenFilesystem`, `pkg/stow.OpenWorkspace`, `pkg/stow.Open`.
3. **The WASM bridge** — `openHostRuntime` (`cmd/stow-wasm/host_store_js.go:37`). The WASM
   `hostStore` implements the *public* `embedded.Store`, not `storage.Store`, and reaches the seam
   only by being adapted at `internal/embedded/store.go:120`.

The library supports `workspace`; the CLI refuses it. The same word names two different
capabilities depending on the surface.

## 5. Data flows reached

Two of the mapped units are flows. Both terminate where the map says they should.

- **Object PUT through the S3 surface.** `internal/s3api/server.go:207` → `:267` → `router.go:16`
  → `dispatch.go:23` → `handlers_bucket.go:153` → `:203` → the error vocabulary at
  `errors.go:38`. The body is buffered whole (`request.go:68`) before it reaches the store.
- **Object PUT through the runtime, staged.** `cmd/stow-s3/runtime_store.go:38` → `:47` →
  `internal/runtime/adapter.go:100` → `stream-writes.go:45` → `staged-writes.go:38` → `:63` →
  sink `internal/storage/fs/staged_put.go:200`.
- **A run-through read.** `cmd/stow-s3/store.go:24` → `internal/runthrough/adapter.go:77` → `:372`
  → `cache_read.go:19` → `:52` → sink `internal/runthrough/upstream.go:83`.
- **A workspace capture.** `pkg/stow/checkpoint.go:85` → `:97` → `:100` →
  `checkpoint_capture_core.go:47` → `checkpoint_capture.go:139` → `:77` → sink
  `checkpoint_publish.go:48`.

The **run-through write propagation** flow is *not* mapped — it was proposed as a unit and
deferred. Its endpoint is an upstream PUT reached from `internal/runthrough/adapter.go:312` through
`outbox_adapter.go:39` and the retry worker at `cmd/stow-s3/main.go:267-290`.

## 6. Dependency facts

- **No import cycles** among the Go packages. The only back-edge is test-only:
  `internal/storage/backend_contract_test.go` imports the concrete backends it asserts on.
- `internal/s3api` is the only internal package importing both `internal/runtime` and
  `internal/runthrough`, which is what lets one S3 error vocabulary cover runtime and upstream
  refusals.
- `internal/authority` is dependency-free by design. `internal/policy` composes it;
  `internal/policystore` is split out so the decision model never links the filesystem.
- **`internal/runtime` is forbidden from importing `internal/storage/fs`**
  (`internal/runtime/dependencies_test.go:25`), so it depends on that concrete package's behaviour
  through a type assertion it cannot name. Two of the four hypothetical seams exist for this
  reason.
- `cmd/stow-wasm` imports `internal/embedded` directly and bypasses `pkg/stow`.
- **`pkg/stow` imports `internal/s3api`** (via the workspace facade), so the public library links
  the HTTP surface.
- `internal/runthrough` is the only package reading the environment, reached from the CLI through
  one reconciliation function.
- Largest packages by line count: `internal/storage` (23k), `internal/runthrough` (20.7k),
  `pkg/stow` (17.4k), `internal/runtime` (12.5k), `internal/s3api` (8.7k), `cmd/stow-s3` (6.6k),
  `conformance` (6.6k).

## 7. Extension points reached

Only one extension point is described by a mapped record: the **optional capability interfaces**
of `internal/storage`. They are the system's real extension mechanism — a store declares what it
can do by satisfying an interface — and they are discovered by inline type assertion rather than
through a stated protocol. The dedicated Extension-point unit for the browser persistence adapter
was deferred, so the TypeScript-side extension surface is unmapped.

The **coverage critic** proposed an embedded host bridge extension point in the TypeScript client
and a policy source hook; both were deferred.

## 8. Where this map is thin

Fifteen of 41 units were worked. The gaps that remain:

- **The durable-file primitives are unmapped.** `internal/atomicfile` and `internal/rooted` are
  what every store's atomic publication and symlink-free confinement route through. Their
  behaviour is described only from the callers' side.
- **The seams inside `internal/storage` are unmapped.** Staged writes, guarded writes, the
  put-checksum capability and the propagation outbox each have a unit that was never assigned a
  worker, so the capability seams are mapped only through the contract record.
- **Three modules the runtime names as co-owners of its decisions were never reached**:
  `internal/authority`, `internal/policy` and `internal/capacity`. The runtime record's finding
  that three byte budgets decide one limit is therefore mapped from one side only.
- **The extension points are unmapped.** The browser persistence adapter, the embedded host bridge
  in the TypeScript client, and the policy source hook have no unit. Only the optional-capability
  interfaces of `internal/storage` are described as an extension mechanism.
- **The peripheral trees are unmapped**: `site`, `examples`, `conformance`, `scripts` and `tools`.
  The conformance tree in particular is the thing that asserts the S3 contract, so its coverage of
  the behaviours this map flags (the auth query ordering, the error vocabulary) is unknown.
- **No coverage critic ran after wave 2**, so nothing independently states what waves 2 and 3
  missed. The wave-1 critic's proposals are the only external check on coverage.

What the map does now cover, and did not before: the authentication boundary, both published
client packages, both readiness records, the WASM bridge, and both MCP servers.
