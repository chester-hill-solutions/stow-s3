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

## Re-diagnosis: what happened when the defects were acted on

This review's register was produced by re-deriving facts from source. Acting on the
findings exposed what that method cannot see: **whether the behaviour it describes
was chosen on purpose.** In five of the six findings below, the answer was yes, and
the evidence was in the tests or the surrounding structure rather than in the code
the finding cited.

**Treat this section as a correction to the register above, and read the tests
before acting on any remaining row.**

| Finding | What acting on it revealed |
|---|---|
| Run-through write policy contradicts the code (ADR 0005 and the banner say writes stay local; the adapter refuses) | **The refusal is right and the words were wrong.** `547b3a51` (2026-09-26) wrote the refusal with the rationale "a mirror-writes policy is not satisfied by keeping the local write", *before* ADR 0005 was published. The ADR, the banner and the policy doc comment were corrected; the behaviour was not touched. |
| `storage.ErrInvalidUpload` reaches a client as a 500 | **The 500 is deliberate.** `s3api/workspace_owner_test.go` pins it for a corrupt or truncated stored upload record, which is a server-side integrity problem, not a client error. The workaround the finding cited — `ValidateMultipartPartNumbers` returning `ErrInvalidPart` instead — is deliberate for the same reason. Left unmapped. |
| `storage.ErrResourceUnresolved` reaches a client as a 500 | **A real defect.** Fixed: 404 NoSuchUpload. The same condition had answered both 500 and 404 depending on whether a policy was configured. |
| `runthrough.ErrLiveWritesDisabled` reaches a client as a 500 | **A real defect.** Fixed: 403 AccessDenied, following the authority refusal already in the same table. |
| `capacity.ErrFull` reaches a client as a 500 | **A real defect.** Fixed: 507 InsufficientStorage, matching the runtime's own quota refusal directly above it. |
| `uploadDir` joins a caller-supplied upload ID into a path (`internal/storage/workspace/multipart.go:54-56`) | **A real defect, and it was in both stores.** `internal/storage/fs/fs.go:201` had the same exposure. Fixed by validating the ID's shape before a path is built. Two placements the suites required and the finding did not anticipate: the guard must sit *after* the filesystem store's pending-save gate, because `save_requests_test.go` pins that gate as winning over everything including a multipart mutation; and `ValidateMultipartUpload` must keep answering `ErrNoSuchUpload` rather than introducing a third vocabulary. |
| Run-through mode silently loses guarded saves, durable save requests and recovery holds | **Not a defect, or not yet a defect — unresolved, and the strongest case for leaving it alone.** The adapter holds a narrow field per capability it forwards (`localMultipart` in `multipart_adapter.go`) and refuses the rest explicitly, so the omission is a choice rather than an oversight. `storage.GuardedWriteStore` is only a capability predicate, and `SaveOptions` carries a `RequestKey`, so the guarded-save workflow is entangled with durable replay — which the outbox already journals for the same question. Enabling both in run-through mode would give one question two journals, which is the shape the author repeatedly removes ("one mechanism rather than two", ADR 0010). Nothing pins or documents it either way, so the finding is recorded as **unresolved rather than confirmed**. |
| Workspace listing returns `.git`, `node_modules` and swap files that the capture path excludes | **Possibly deliberate, unexamined.** Listing a workspace bucket is documented as listing the caller's directory; the capture path filters because a capture must be portable. The two rules may be correct for their two purposes, which would make the divergence a documentation gap rather than a defect. Not acted on. |

### The lesson worth carrying forward

A source-grounded review can establish *what the code does* and cannot establish
*why*. Where the two disagree with the prose around them, the code is the product
and the prose is the defect — that held in five of six cases here. The test suite
is the closest thing to a specification this repository has, so it belongs in the
first pass of a review, not the last: each of the four real defects above still
required a *second* correction after the gate failed, because the finding had not
read the tests that pinned the surrounding behaviour.

Two open questions the register cannot settle, both now recorded as questions
rather than defects: whether run-through mode should carry guarded saves and
durable replay, and whether the workspace listing exclusion should match the
capture path.

## Actions, in order

The review performed none of these; two have since been done, and are marked so.

1. **Make the readiness protocol one source of truth.** Generate the TypeScript and Python field
   sets and the version constant from `internal/ready/ready.go`, and extend
   `scripts/check-version.mjs` to gate `READY_PROTOCOL_VERSION` and the capability field names.
   Reconcile `capabilities.maxMultipartUploads`, which the writer publishes and neither reader
   declares.
2. **Map the unmapped refusals. DONE** for `ErrLiveWritesDisabled`, `ErrResourceUnresolved` and
   `capacity.ErrFull`. `storage.ErrInvalidUpload` is deliberately excluded: the 500 is pinned by
   the workspace-owner wire test for a corrupt stored record. See the re-diagnosis above.
3. **Do not forward the adapter's missing capabilities without a decision.** The omission looks
   deliberate, and enabling guarded saves and durable replay in run-through mode would give one
   question two journals. Recorded as an open question, not an action.
4. **Give capability discovery a protocol.** One probe in `internal/storage` that performs the
   assertions and the `Supports*` predicates together, routed through the ~two dozen inline sites;
   and either give `SaveRequestStore` and `RecoveryHoldStore` a second adapter or stop declaring
   them as interfaces.
5. **Close the two non-constant-time comparisons in the auth path, or state them.** Move the payload
   hash to `hmac.Equal` or document that it is not a secret comparison; likewise the access-key
   comparison. Give the encoded-name query ordering a runtime guard rather than one unit test.
6. **Document the two exclusion rules rather than unifying them.** Listing a workspace bucket is
   listing the caller's directory; a capture must be portable. Both may be right for their purpose,
   so the divergence is a documentation gap until someone decides otherwise.
7. **Validate the upload ID before it becomes a path. DONE**, in both path-backed stores, with the
   guard placed after the filesystem store's pending-save gate.
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

## The whole-run coverage critic, and what examining its proposals found

The coverage critic that never ran during the original waves has now run over the
whole map, asked for coverage the *ledger itself* did not contain rather than for
the 26 deferred units it already names. It proposed seven, and the ledger is now
52 units.

The three highest-value proposals were examined against source, and **none is a
defect**:

| Proposed gap | What examining it found |
|---|---|
| The build version constant and its cross-language agreement | **Already gated.** `scripts/check-version.mjs` compares the Go constant in `internal/version/version.go` against `packages/stow-s3/package.json`, `packages/stow-s3-py/pyproject.toml` and the Python `__init__`, plus the four platform packages. The agreement exists and a gate holds it. |
| The web-worker message protocol between the embedded host and the worker server | **Single-sourced and validated.** `worker-protocol.ts` declares the envelope once and ships runtime validators (`isWorkerRequest`, `isWorkerReply`), and the payload inside it is the WASM protocol the WASM record already covers. |
| The GOOS-selected safe-filesystem primitive seam | **A capability gate, correctly built.** `regular_unix.go` opens and refuses a non-regular file; `regular_other.go` refuses outright with `ErrUnsupported` and `safeRegularOpenSupported` makes `Supported()` report false. It declines rather than opening a path it cannot prove is not a link, and it exposes the predicate so a caller can branch. |

This is the shape to expect from a coverage critic: it proposes coverage by design,
so finding three working mechanisms rather than three bugs is a result about the
code rather than a criticism of the critic. It also sharpens the picture the
register above gives — the areas this map did not reach are not obviously worse
than the areas it did.

The four remaining proposals (the shared recovery-hold error markers, the MCP
browser bundle pipeline, the WASM asset shipping flow, and the CLI out-of-session
checkpoint capture policy flow) are coverage still to buy, not findings.

## Where the map is thin

Coverage is fifteen of fifty-two units. The deferred set includes the seams inside
`internal/storage` (staged writes, guarded writes, the put-checksum capability, the propagation
outbox), the durable-file primitives `internal/atomicfile` and `internal/rooted` that every store's
atomic publication and confinement route through, the extension points, the peripheral trees
(`site`, `examples`, `conformance`, `scripts`, `tools`), and `internal/authority`,
`internal/policy` and `internal/capacity`.

A whole-run coverage critic has now run, and its seven proposals are in the ledger; the three
examined were working mechanisms rather than defects. What no critic settles is whether the
uncovered areas are *sound*: the sections above describe the parts of the system this run read, and
silence about the rest is silence.

[OPEN-QUESTIONS.md](2026-10-22-architecture-overview/OPEN-QUESTIONS.md) lists the deferred set as it
stood at 41 units; the ledger carries the later additions.

## Evidence

| Artifact | What it is |
|---|---|
| [records.json](2026-10-22-architecture-overview/records.json) | The 31 records, sorted by fingerprint, schema-valid: interface, implementation, callers, depth assessment, risks and priority per module. |
| [ARCHITECTURE.md](2026-10-22-architecture-overview/ARCHITECTURE.md) | The map: shape, modules, seams with adapter counts, selection sites, flows and dependencies. |
| [VALIDATION-LOG.md](2026-10-22-architecture-overview/VALIDATION-LOG.md) | What validation and verification found, including all 27 refuted claims. |
| [OPEN-QUESTIONS.md](2026-10-22-architecture-overview/OPEN-QUESTIONS.md) | The deferred coverage set. |
| [SITUATION.md](2026-10-22-architecture-overview/SITUATION.md) | The reconnaissance summary, with corrections appended as they were established. |
| [coverage-ledger.json](2026-10-22-architecture-overview/coverage-ledger.json) | The coverage claim: 52 units with their state - the 15 worked, and 37 not reached, of which 26 were seeded and 11 were proposed by the whole-run coverage critic after the waves closed. |
| [citation-check.json](2026-10-22-architecture-overview/citation-check.json) | Per-record citation resolution: every cited path and line. |
| [metadata.json](2026-10-22-architecture-overview/metadata.json) | Revision, method, counts and integrity results. |
