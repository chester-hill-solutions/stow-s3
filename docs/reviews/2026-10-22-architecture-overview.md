# Architecture overview, and a verified defect register

**A read-only, source-grounded map of stow-s3 in which every claim was independently re-derived.**
Thirty-one records across fifteen subsystems, each citing repository-relative source, each reviewed
by an agent that did not write it, and each of the twenty-seven that describes a module or seam
verified by a second agent that did neither. That review refuted **twenty-seven** claims the first
pass had asserted, and corrected three verdicts. No production file was changed by the run.

Reviewed `main` at `3066d37747aaa56b29a39961801c0116c39600fa`, working tree clean.

## Method and what it is not

The run used the `architecture-overview` pipeline: reconnaissance, coverage-led work waves, a
per-wave coverage critic, then per-candidate validation and per-record verification. Seventy-nine
agent invocations, all read-only — no build, test, process start or network access.

Two properties matter for reading it. First, **the map is partial**: fifteen of forty-one seeded
units were worked, and the twenty-six deferred ones include the seams inside `internal/storage`,
the durable-file primitives, and `internal/authority`, `internal/policy` and `internal/capacity` —
the three modules the runtime record names as co-owners of decisions it also makes. Silence about a
subsystem is not a finding that it is sound. Second, **the defects below are defects, not style
notes**: each is a place where the source does something other than what a caller, a reader or
another layer of the same system believes it does.

The findings cluster in one shape: *a decision that two layers both believe they own, where the
disagreement is invisible to the type system.* The optional-capability interfaces, the readiness
records, the two clients and the write policy are all instances of it.

## Validation

| Phase | Result |
|---|---|
| Candidate validation | 31 of 31, each by a fresh reviewer that did not produce it |
| Record verification | 27 of 27 mapped records, each by a second fresh agent |
| Verdicts upheld | All 27 `mapped` and 4 `rejected`. **No false positive.** |
| Claims refuted | 27, plus line/scope/caller corrections |
| Citation integrity | 814 structured citations and 95 prose references resolved, 0 failures |

The refutations are the strongest evidence the two review phases earned their cost, so they are
recorded in full in [VALIDATION-LOG.md](2026-10-22-architecture-overview/VALIDATION-LOG.md). A
sample: the registry-resolution rule was reported as spelled three times (it is one definition);
`Prune` was reported as surfacing a corrupt registry entry (it skips silently too, so the finding
is *worse*); `storage.Store` was reported with nine optional capability interfaces and seven
implementations (**eleven and nine**); and a trace sink was cited on a path unreachable from the
entry point.

## The authentication boundary

`internal/auth` is the single gate every non-admin request passes, so a defect there admits
unauthenticated access rather than degrading one feature. Four findings, all verified:

- **The access key is compared with a plain `!=`** (`internal/auth/sigv4.go:313`) while the
  signature uses `hmac.Equal` (`:293`). Access-key identifiers are semi-public, so the practical
  timing risk is low; the finding is that the path is not uniformly constant-time and a reader
  would reasonably assume it is.
- **The payload hash is compared with `strings.EqualFold`** (`:382`, `:392`) while the signature is
  deliberately *not* case-folded. Both values are hex, so the two comparisons are inconsistent with
  each other, and the payload-hash path is not constant-time either.
- **The canonical query ordering by encoded names is a signing-compatibility boundary with no
  runtime fallback** (`sigv4_canonical.go:61-71`). An SDK that sorts decoded names produces a
  signature this verifier refuses, and reverting the ordering would silently reject conforming SDKs.
  It is guarded only by one unit test and a pinned Node fixture — not by the conformance corpus.
  This was the subject of [the earlier query-order change](2026-10-19-sigv4-query-order.md); the
  finding now is that nothing but a test holds it.
- **Payload verification requires the caller to have made the body replayable.** A streaming body
  yields `AccessDenied` "cannot verify signed payload without request body" (`:404`) rather than an
  authentication decision — an ordering requirement on the host that the interface does not state.
  Separately, presigned URLs have no lower bound on the signing time beyond the expiry
  (`:354-376`): a URL signed at any past time is accepted until it expires.

## Defects in the storage path

- **Run-through mode silently loses durability.** `runthrough.Adapter` forwards `MultipartStore`,
  `ConditionalCopyStore` and `QuotaStoreProvider` and implements none of the other eight optional
  interfaces, while the CLI binds the *adapter* — not the local store — into the runtime
  (`cmd/stow-s3/main.go:343`, `:349`). Guarded saves, durable save requests and recovery holds are
  therefore refused in run-through mode even though the filesystem store underneath supports them.
  A caller cannot observe this from the type.
- **Three refusals reach an S3 client as a 500.** `internal/s3api/errors.go` maps
  `ErrQuotaExceeded` (`:141`) and nothing else in this family: `ErrLiveWritesDisabled`,
  `ErrResourceUnresolved` and `capacity.ErrFull` have no entry. A 500 invites a retry that cannot
  succeed.
- **Three byte budgets decide the same limit, and the runtime counts bytes from two sources.** The
  runtime enforces `MaxBytes`/`MaxObjects` against its own counters (`internal/runtime/state.go:18`);
  the filesystem store can be bound to a `capacity.Namespace` with an independent `MaxBytes`
  (`internal/storage/fs/capacity.go:20`); the run-through adapter carries its own cache budget
  (`internal/runthrough/cache_policy.go:362`). A put is accounted from the caller's *declared* size
  (`stream-writes.go:26`) and a copy from the store's *reported* size (`objects.go:299`), and the two
  are never reconciled.
- **The `storage.Store` capability protocol is unstated and re-derived at roughly two dozen sites**,
  and two of them disagree about whether the `Supports*` predicate must be checked
  (`internal/runtime/save_requests.go:13` against `:24` and `:71`). Four optional interfaces —
  `SaveRequestStore`, `RecoveryHoldStore`, `UsageRevisionStore`, `ObjectUsageStore` — have exactly
  one production adapter each, so they advertise substitutability that does not exist.
- **`uploadDir` joins a caller-supplied upload ID into a path without validating it**
  (`internal/storage/workspace/multipart.go:54-56`), so an ID containing `..` can resolve outside
  `<root>/.stow/multipart`. The S3 and library paths are covered by the runtime's own in-memory
  upload map; a store used directly as a `storage.Store` is not. This is the one item here with a
  security shape.
- **Listing a workspace returns files the capture path excludes.**
  `internal/storage/workspace/list_rooted.go:32` skips only the `.stow` subtree, so `.git`,
  `node_modules` and editor swap files come back as objects, while
  `pkg/stow/checkpoint_portable.go:87-95` filters `.git` and `.stow` separately. The same workspace
  yields two different object sets depending on which surface you ask.
- **Silent-failure cluster.** `Workspace.Touch` returns `nil` when the registry entry is missing
  (`pkg/stow/resume.go:435`); `Registry.All` skips an entry it cannot decode
  (`internal/storage/workspace/registry.go:238-243`); `Collect` *creates* the registry while `List`
  and `Prune` open it read-only (`pkg/stow/resume.go:213`); and a corrupt recovery-hold journal makes
  every hold query fail with no repair or quarantine path.
- **`VerifyDeltaDigest` treats an empty expected digest as a pass** (`pkg/stow/delta.go:378`), so
  the change-list binding is optional and silently absent when a receiver forgets it. Per-file
  digests do not cover the paths a document names.

The full register, with callers, error modes and ordering constraints per module, is in
[the architecture map](2026-10-22-architecture-overview/ARCHITECTURE.md) and
[the record set](2026-10-22-architecture-overview/records.json).

## Documentation that contradicts the source

Corrected in the same change as this report, because each one sent a reader somewhere wrong:

- ADR 0008 §3 and `CONTEXT.md` named a single `stow-workspace.json` at the workspace root. The
  source writes two versioned documents under `<workspace>/.stow/` — `manifest.json` for identity
  and `index.json` for the object index — at layout version 3.
- `docs/workspace-contract.md` §4 showed one document at version 1 with a flat `entries` map.
- `docs/run-through.md`, `docs/compat-contract.md`, `internal/runthrough/writepolicy.go` and the
  startup banner all said a `mirrorWrites` policy without consent leaves writes local. The adapter
  has **refused** such a mutation since `547b3a51` (2026-09-26), before ADR 0005 was published,
  because a mirror-writes policy is not satisfied by a local write. ADR 0005's decision and
  consequences now describe the refusal and state the `readThroughCache` case separately.
- ADR 0004's ready-protocol example omitted `maxMultipartUploads` and `maxConcurrentRequests`, both
  of which the writer publishes, so a client built from the example could not report the multipart
  limit the server applies.
- `parseBackend` cited `docs/architecture/environment-implementation.md`, which is at
  `docs/history/architecture/`. `Config.AllowPublicAdmin` documented a deprecation warning the
  package does not emit. The `--backend` help said "filesystem or memory" without naming that the
  workspace backend is embedded-API-only. The workspace verb table's comment said fifteen verbs; it
  has seventeen.

## The protocol and client surfaces

The theme repeats across languages. **The readiness record has no shared schema**: the Go structs
(`internal/ready/ready.go:21-53`), the TypeScript interfaces (`ready.ts:9-31`) and the Python
dataclasses (`ready.py:38-76`) restate the field set independently, with three separate version
constants, and `scripts/check-version.mjs:169-198` gates `SESSION_GOGC` and the owner marker but
**not `READY_PROTOCOL_VERSION`**. The divergence has already happened: the writer publishes
`capabilities.maxMultipartUploads` which neither reader declares. There are also **two** readiness
records, both reporting version 1 and both carrying a capabilities object, told apart only by the
identity key name.

The two clients agree on the environment-strip prefixes, `GOGC=50`, binary precedence and the
required field set — and diverge on the readiness descriptor (fixed fd 3 versus a dynamic pipe), the
legacy `STOW_READY` fallback (TypeScript has one, Python does not, though ADR 0004 says it "remains
supported"), the environment-strip scope on the `workspace serve` child, the serve argv, and whether
`max_bytes: 0` means "no limit".

The WASM bridge hand-duplicates its wire contract across the language boundary, and
`hostStore.SupportsConditionalWrites()` returns unconditionally true (`host_store_js.go:281`) while
enforcement is delegated to a JS callback, so a host that cannot evaluate conditions fails
mid-commit rather than at `openHost`. Both MCP servers hide state that changes what every later call
accepts: the object server's bucket is fixed at construction and its observation tokens are
process-local; the workspace server's adopted-workspace set is process-local too.

## Risk register

Every verified record's demonstrated risks, at the record's own priority. `impact` is the blast
radius of being wrong; `overall` never exceeds it.

| Priority | Record | The risk in one line |
|---|---|---|
| P1 | `internal/auth` | The single authentication gate: non-constant-time comparisons, and a query ordering guarded only by a test. |
| P1 | `internal/runthrough` | A reported write policy that describes the opposite of what the code does, on the one boolean that decides whether a mutation leaves the machine. |
| P1 | `internal/storage` (contract) | Optional capabilities discovered by unstated type assertions; wrappers and platforms silently remove them. |
| P1 | `internal/storage` (seam) | Nine implementations behind one interface, selection spread over three surfaces, capability coupling no type records. |
| P1 | `internal/storage/fs` | The record codec is shared by every object ever written; a version change makes a data directory unreadable rather than partially readable. |
| P1 | `internal/storage/workspace` (store) | The default agent-facing surface, where the bucket *is* the caller's directory, and the on-disk format was documented wrongly in two places. |
| P1 | `internal/storage/workspace` (registry) | The last thing between an unattended sweep and somebody's work; a registry-wide corrupt journal has no repair path. |
| P1 | `internal/runtime` | Three byte budgets, two byte-counting sources, refusals that reach clients as server faults. |
| P1 | `pkg/stow` (checkpoint store) | The durability core four other features share; an "outcome unknown" branch a caller must distinguish from failure. |
| P2 | `internal/ready` | Three independent restatements of one wire record, three version constants, no drift gate — one field already diverged. |
| P2 | `packages/stow-s3-py` | Two hand-maintained clients, one protocol, a drift gate covering only GOGC and the owner marker. |
| P2 | `packages/stow-s3` | The root export surface is not the implemented surface; the npm platform package silently outranks an exported `STOW_BIN`. |
| P2 | `cmd/stow-wasm` | The wire contract is hand-mirrored across the language boundary; conditional-write support advertised unconditionally. |
| P2 | `cmd/stow-s3` (workspace readiness) | Two readiness records both at version 1, separable only by a key name. |
| P2 | `cmd/stow-s3` (CLI) | The interface is the command line plus an environment that appears in no help text and is copied into two clients as a regex. |
| P2 | `internal/s3api` | The store's concrete type is an undeclared part of the HTTP contract; the error table is order-sensitive. |
| P2 | `internal/storage` (conditional copy) | Five adapters, one of which — the public path — silently gives the weaker copy guarantee. |
| P2 | `pkg/stow` (workspace lifecycle) | The resolve-then-open composition is repeated with copies that disagree. |
| P2 | `pkg/stow` (archive/handoff) | The cross-machine path: an unknown publication outcome and two incompatible handoff versions. |
| P2 | `pkg/stow` (preparation) | Path and sensitivity rules shared with three features; a Git seed depending on an external program. |
| P2 | `pkg/stow` (S3 facade) | Link-time coupling of the public library to the HTTP layer; an unredacted secret in the descriptor. |
| P2 | `pkg/stow` (delta) | An optional digest check that silently binds nothing when omitted. |
| P2 | `internal/storage/workspace` (snapshot) | The capture input; its exclusions have already diverged from the listing surface's. |
| P2 | `internal/mcpstorage` (object server) | Bucket fixed at construction; observation tokens live in memory only. |
| P2 | `internal/mcpstorage` (workspace server) | The adopted-workspace set is process-local; the scope checks are the safety property for agent checkpointing. |
| P3 | `pkg/stow` (filesystem entry) | Platform limits discovered at the call rather than at compile time. |
| P3 | `pkg/stow` (registry policy) | A public administrative surface with no caller at all; a staging-prefix rule inlined in three places. |

## Actions, in order

None was performed by the review. Each names the interface that should change.

1. **Make the readiness protocol one source of truth.** Generate the TypeScript and Python field
   sets and the version constant from `internal/ready/ready.go`, and extend
   `scripts/check-version.mjs` to gate `READY_PROTOCOL_VERSION` and the capability field names.
   Reconcile `capabilities.maxMultipartUploads`, which the writer publishes and neither reader
   declares.
2. **Map the unmapped refusals.** Add `ErrLiveWritesDisabled`, `ErrResourceUnresolved` and
   `capacity.ErrFull` to `internal/s3api/errors.go` so a refusal is a 403/404/507 rather than a 500.
3. **Stop `runthrough.Adapter` erasing the capabilities of the store it wraps.** Forward
   `GuardedWriteStore`, `SaveRequestStore`, `RecoveryHoldStore` and `PutChecksumStore`.
4. **Give capability discovery a protocol.** One probe in `internal/storage` that performs the
   assertions and the `Supports*` predicates together, routed through the ~two dozen inline sites;
   and either give `SaveRequestStore` and `RecoveryHoldStore` a second adapter or stop declaring
   them as interfaces.
5. **Close the two non-constant-time comparisons in the auth path, or state them.** Move the payload
   hash to `hmac.Equal` or document that it is not a secret comparison; likewise the access-key
   comparison. Give the encoded-name query ordering a runtime guard rather than one unit test.
6. **Decide the workspace exclusion policy once** and share it between the listing surface and the
   capture path, so one workspace cannot yield two object sets.
7. **Validate the upload ID** in `internal/storage/workspace/multipart.go:54-56`, so the direct
   `storage.Store` path gets the confinement the S3 path gets from the runtime map.
8. **Reconcile the two clients**, or make the drift visible: the descriptor, the legacy fallback,
   the environment-strip scope, the serve argv, and the meaning of a zero limit.
9. **Make the WASM host capability honest** — a declared property checked at `openHost` rather than
   an unconditional `true` — and give the hand-mirrored protocol a parity test.
10. **Give the MCP servers' hidden state an owner.** Persist or expose the observation tokens and
    the adopted-workspace set rather than hiding them.
11. **Give the runtime one accounting source**, deriving the accounted size from the store's
    reported meta on both the put and copy paths, and state on `Instance.Close` that it closes the
    bound store.
12. The remaining per-module actions — `Workspace.Touch` returning a named error, `Collect` opening
    the registry read-only, `All` reporting the entries it skipped, the delta digest becoming the
    default, splitting `pkg/stow` off `internal/s3api` — are listed with their interfaces in the
    full report.

## Where the map is thin

Coverage is fifteen of forty-one units. The deferred set includes the seams inside
`internal/storage` (staged writes, guarded writes, the put-checksum capability, the propagation
outbox), the durable-file primitives `internal/atomicfile` and `internal/rooted` that every store's
atomic publication and confinement route through, the extension points, the peripheral trees
(`site`, `examples`, `conformance`, `scripts`, `tools`), and `internal/authority`,
`internal/policy` and `internal/capacity`. **No coverage critic ran after the second wave**, so
nothing independently states what the later waves missed.
[OPEN-QUESTIONS.md](2026-10-22-architecture-overview/OPEN-QUESTIONS.md) lists the whole set.

## Evidence

| Artifact | What it is |
|---|---|
| [records.json](2026-10-22-architecture-overview/records.json) | The 31 records, sorted by fingerprint, schema-valid: interface, implementation, callers, depth assessment, risks and priority per module. |
| [ARCHITECTURE.md](2026-10-22-architecture-overview/ARCHITECTURE.md) | The map: shape, modules, seams with adapter counts, selection sites, flows and dependencies. |
| [VALIDATION-LOG.md](2026-10-22-architecture-overview/VALIDATION-LOG.md) | What validation and verification found, including all 27 refuted claims. |
| [OPEN-QUESTIONS.md](2026-10-22-architecture-overview/OPEN-QUESTIONS.md) | The deferred coverage set. |
| [SITUATION.md](2026-10-22-architecture-overview/SITUATION.md) | The reconnaissance summary, with corrections appended as they were established. |
| [coverage-ledger.json](2026-10-22-architecture-overview/coverage-ledger.json) | The coverage claim: 41 units with their state, the 15 worked and the 26 deferred. |
| [citation-check.json](2026-10-22-architecture-overview/citation-check.json) | Per-record citation resolution: every cited path and line. |
| [metadata.json](2026-10-22-architecture-overview/metadata.json) | Revision, method, counts and integrity results. |
