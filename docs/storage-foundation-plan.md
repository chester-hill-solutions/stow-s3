# Consolidated working-storage plan

**Canonical work order:** 2026-09-29. Reconciled against HEAD `f549276` plus the
current uncommitted development tree, the accepted interview decisions and dated
validation records. This is the sole implementation/status queue. Previous S0–S4
IDs remain traceable below; the W IDs identify the remaining actionable slices.

## 1. Plan validation summary

**Verdict: Adjust.** Existing storage, conditional-write and caller integration
work provides the foundation; it must not be reimplemented because older plans
still use future tense. The remaining work combines guarded editing, scoped access,
retention, encrypted persistence, notifications and portable recovery. Release,
cross-host evidence and adoption are separate gates.

Stow owns portable working storage for agents, tools, applications and tests:
preparation, files/objects, capture, inspection, transfer, storage notifications and
lifecycle. Callers own execution, models, read tracking, writer quiescence, readiness
and reactions. Cache is one deployment profile. ADRs [0014](adr/0014-storage-product-caller-owned-execution.md),
[0015](adr/0015-durable-workspace-notifications.md) and
[0016](adr/0016-scoped-access-encrypted-cache.md) govern that boundary.

The user delegated routine technical choices. Resolve API names, bounds, codecs,
fixtures and adapter design through implementation evidence. Escalate only an
unresolved consequential product, custody or compatibility choice. No user answer
is needed to execute this planning order. This reconciliation changes documentation,
not runtime behavior or the status of a release.

## 2. What the plan already covers well

### Reconciled baseline: preserve, do not rebuild

| Capability | Current status and evidence | Remaining qualification |
| --- | --- | --- |
| Storage correctness | Local relocated handoff, CRC64NVME single-object uploads, metadata normalization, confinement, request/transport limits and coordinated durable capture are implemented. [Baseline evidence](implementation-2026-09-29.md). | Required provider profiles, second-host behavior and regression maintenance. CRC64NVME multipart remains unsupported. |
| Working-storage workflow | Same-runtime directory/S3 facade, usage refresh, file-v1/portable-v2 checkpoint capture/diff/archive/restore, relative cwd, declared Git provenance and recipient-local-source reconstruction are implemented locally. | Workload costs, supported Linux runtime and independent use. Enhanced deltas/self-contained Git bases are not implied. |
| Existing registry policy | Versioned workspace/checkpoint count and checkpoint-byte caps, publication admission and explicit descendant-protected deletion are implemented. | Aggregate namespace budgets, pending-operation holds and recovery reserve are new work. |
| Conditional writes | Public Go and WASM/TypeScript now forward content-ETag/expected-absence conditions, advertise support and refuse unsupported/stale requests. [Slice evidence](guarded-storage-plan.md#verification-of-the-conditional-write-prerequisite). | Managed-save consumer adoption, wrapper/resource identity contracts and input-basis publication; see the bounded native slice below. |
| Native managed saves | Memory and owned filesystem reads/saves compare managed generation, content and metadata. Filesystem request keys retain bounded receipts for lost-reply/reopen resolution. A fixed-bucket native object MCP adapter exposes the same core with bounded expiring observations. [2026-09-30 evidence](implementation-2026-09-30.md). | Two real consumers, further wrapper/backend contracts, Linux runtime evidence, resource ACLs and input-basis publication. |
| MCP and caller | Local scoped stdio adapter, checkpoint request/resolve receipts, negotiated protocols, MCP SDK 1.8.0/Go 1.25.6 and pinned OpenCode 2.0.16 caller are implemented. | Real interruption/cleanup, Linux/second physical host, and actual OpenCode-to-MCP configuration. |
| Meaningful agent continuation | Later [receiver remediation](opencode-receiver-remediation-2026-09-29.md) records local real-model continuation, independently checked output and restored checkpoint contents after sender data was removed. | The same-host result does not close the second-host/Linux or interruption gates. |
| Distribution | 0.3.0 versions, npmjs publication targets, artifact identity/retry checks and local packed consumers are prepared. | Account/publisher setup, actual publication, exact-version anonymous consumers and fresh provider receipts. |

The baseline record's aggregate test counts/hashes predate the conditional slice and
later model follow-up. Keep their dates and scopes; do not relabel them a fresh
whole-tree receipt. The conditional slice separately passed full Go race/standards,
four local conformance profiles, focused wrapper/browser tests and real WASM checks.
Local evidence is macOS arm64; Linux compilation alone is not a runtime pass.

## 3. Gaps, conflicts, or redundancies to fix

| Finding | Source and resolution | Work owner |
| --- | --- | --- |
| Custom-store ADR contradicts public API | Accepted [ADR 0010 §§4–5](adr/0010-an-enforcement-site-or-not-a-permission.md) removes Options.Store/Store; current [public adapter](../pkg/stow/store.go) and conditional slice preserve it. Reconcile explicitly before extending this seam. Resolved on 2026-09-29: ADR 0010 §§4–5 now retain the qualified opt-in seam, backed by public Runtime adapter tests. Remaining fidelity gaps are explicit; the original enforcement decisions remain in force. | W01 |
| Legacy authority decisions still have open enforcement | The destroy bypass was reproduced on disposable restricted workspaces and fixed, including after Close and constructor rollback. [Ungated](../internal/authority/authority.go) now retains only unimplemented `environment.promote`; its public symbol needs explicit compatibility/versioning disposition. Explicit host `DestroyRegisteredWorkspace`/CLI administration retains liveness, capture exclusion, ownership and durable identity checks without widening actor grants. Resource/background ACL admission remains unqualified. No full authority matrix claim. | W04, W20 |
| Completed defects still appear open | Previous queue/disposition text calls handoff/checksum/metadata fixes and standards unfinished. Close those local implementation entries using evidence; carry only named remaining qualification. | Baseline above; W16, W19–W22 |
| Transfer primitives confused with resume | Multipart/range operations exist; a restarted client recipe and portable unfinished payloads are unverified. Prove same-store continuation before portable sections. | W06, W12 |
| Saved bytes confused with current inputs | Conditional ETags guard content only. Fresh resource identity and ready-publication checks need a core admission boundary, not wrapper check-then-write. | W01, W02, W09 |
| Quotas confused with recovery capacity | Current registry caps have no dependency holds, unified namespace accounting or reserved physical headroom. Qualify logical and adapter guarantees separately. | W03 |
| Local claims confused with cross-host ownership | Copying a lease/outbox or restoring an old backup cannot prove one current sender. Require current takeover evidence/fencing and reconcile uncertain effects. | W12, W13 |
| Authorization denial confused with outage | Legacy [cache fallback](../internal/runthrough/cache_read.go) serves cached data after non-not-found upstream errors. Protected profiles must distinguish denial/revocation from transient failure and persist freshness deadlines. This is a protected-profile gap, not a newly reproduced legacy defect. | W04, W16, W18 |
| Receipt confused with retention release | HTTP acceptance ends retries; explicit consumer release ends only its output hold. Partial revocation pauses the whole immutable report. | W09, W11 |
| Proposed hosts/formats confused with shipment | No notification .proto, encrypted persistence, portable operation schema or protected Workers/Railway deployment is implemented. Selected defaults stay planned until their acceptance evidence exists. | W07–W15, W17, W18 |

Old proposal bodies remain design/history references, with leading supersession
notices. Actual usage, source and dated evidence resolve implemented names/defaults.
The standalone [collaboration plan](../../agent-collaboration/docs/plan.md) owns
shared editing, presence, harness observation and controls; no RT work is scheduled here.

## 4. Existing code/product references to reuse or extend

| Concern | Reuse boundary |
| --- | --- |
| Identity, grants, write conditions | [authority](../internal/authority/authority.go), [runtime operations](../internal/runtime/objects.go), [public options](../pkg/stow/types.go), [custom-store declaration](../pkg/stow/store.go), [workspace writes](../internal/storage/workspace/objects.go). |
| Capture, lifecycle and receipts | [capture request/resolve](../pkg/stow/checkpoint_request.go), [registry admission](../internal/storage/workspace/admission.go), [registry policy/deletion](../pkg/stow/registry_policy.go), existing capture/cleanup locks and atomic publication. |
| Portable data and Git | [archive](../pkg/stow/checkpoint_archive.go), [handoff](../pkg/stow/handoff.go), existing checkpoint import/restore/diff and Git provenance paths. Do not copy raw .stow or create another registry. |
| Transfer | [filesystem multipart](../internal/storage/fs/fs_multipart.go), [workspace multipart](../internal/storage/workspace/multipart.go), [S3 multipart](../internal/s3api/handlers_multipart.go) and existing range/conditional GET handlers. |
| Origin/cache and pending writes | [cache read](../internal/runthrough/cache_read.go), [cache policy](../internal/runthrough/cache_policy.go), [write policy](../internal/runthrough/writepolicy.go), durable outbox claims/retry and transport policy. Reuse mechanisms only where semantics match; webhook POST is not an S3 mutation. |
| API surfaces | [MCP](../internal/mcpstorage/server.go), existing native CLI/workspace commands, [WASM bridge](../cmd/stow-wasm/main.go), [embedded wrapper](../packages/stow-s3/src/embedded.ts), [persistent wrapper](../packages/stow-s3/src/persistent-embedded.ts), Python/TypeScript lifecycle wrappers. Keep wrappers thin. |
| Qualification | Shared [conformance](../conformance/CONFORMANCE.md), [coverage map](compatibility-coverage-2026-09-29.md), existing release/provider/script checks, portable workload benchmarks and OpenCode fixtures. Extend uncovered cases rather than copying test suites. |

The [shared identity/admission contract](storage-admission-contract.md) fixes W01
semantics and separates current profiles from target guarantees. Normative contracts
also include [S3 compatibility](compat-contract.md),
[workspace](workspace-contract.md), [preparation](task-manifest.md),
[browser persistence](browser-persistence.md), and accepted ADRs. Current
[usage](portable-workspace-usage.md) is the runnable surface. Prior detailed plans
retain rationale/examples, not independent work orders or stronger implemented claims.

## 5. API impact and validation notes

This consolidation changes no API. Implementing the work below adds contracts;
freeze versioning, capability refusal and replay meaning before emitting new state.
Preserve current S3/legacy unconditional behavior, caller-owned execution, separate
live-write consent and explicit upstream selection/bucket scope.

### Shared identity, access and guarded work

- Bind trusted principal/namespace, resource and scope in core; caller-supplied tenant
  names/headers are not identity. Preserve S3 key identity separately from OS paths.
  Protected profiles default deny and attenuate grants, with explicit-deny precedence.
- Authorize before cache hits/decryption/origin fetch and across list/count/Head/range,
  copy, batch, multipart, capture/export/import, historical diffs and background retries.
  Recheck current policy after restart; define expiry, local revocation, in-flight
  boundaries and bounded cross-host refresh. Host filesystem access remains externally enforced.
- New managed read/edit/save defaults to a resource-bound expected read identity or
  explicit expected absence. Stale saves conflict without overwrite; unconditional
  replacement is deliberate and still authorized. Define content/version/metadata
  semantics and supported coordination; do not promise every intermediate edit or ABA detection.
- Optional caller-declared input basis defaults to current authorized versions at
  ready admission. Changed/missing/denied/unverifiable input blocks publication and
  preserves the draft. Explicit snapshot mode records historical inputs; omitted basis
  makes no freshness claim. Check inside qualified core admission; external writers
  require caller quiescence. Input references alone do not create indefinite holds.
- Expose committed/refused/uncertain outcomes separately from denied/conflict/blocked/
  unavailable/unsupported. Bind retained request identity to operation, scope and
  admitted meaning. Resolve uncertainty before retry; missing/expired receipts do not
  prove no effect. A checkpoint receipt is not a universal save/effect receipt.
- Scoped artifacts declare completeness only within requested authorized scope.
  Unauthorized full/explicit requests refuse, never silently filter. Destination grants
  are independent; absence outside scope never authorizes deletion. Unsupported scoped
  diff/delta/restore combinations refuse until qualified.

### Retention and capacity

- Unfinished owned operations protect their smallest required data/recovery set through
  expiry until completion or explicit authorized discard. No silent deletion of required
  pending data; unrelated disposable copies remain evictable. Unreadable protection
  records fail closed. Cleanup and dependency changes share admission/locking.
- Unified namespace budgets charge managed files/objects/checkpoints/transfer staging/
  pending records and encryption/publication overhead under a total host ceiling.
  Define shared-payload charging and reconcile after restart/host edits.
- Admit bounded recovery bookkeeping with operations; new payloads and other namespaces
  cannot consume its reserve. First profile permits no reserve borrowing or payload
  overcommit. Quota ceilings do not guarantee future payload space. Physical headroom,
  crash-safe updates and actual disk-full behavior require adapter evidence; direct host
  writes/platform failure remain outside Stow admission.
- Provide paged, permission-safe usage/holds/blockers and explicit retry/release/discard
  operations. Bound history, response size, memory, concurrency and comparison work;
  metrics/logs must not disclose another namespace's paths or secrets.

### Notifications, readiness and delivery

- Observe declared-path create/modify/delete first. Watcher hints trigger bounded
  content reconciliation against a durable baseline; startup/periodic reconciliation
  detects net offline changes. Gaps/coalescing/unsupported coverage are explicit.
  Observations imply neither every intermediate edit nor readiness/writer identity.
- Exact, ancestor-path and workspace subscriptions preserve original path/revision.
  Overlap for one subscription produces one delivery. Stable consumer identity,
  bounded pending/replay and at-least-once acknowledgement survive same-workspace reopen.
  Observation/sending require explicit owning lifetime, not a hidden daemon.
- Ready reports reference committed verified immutable saved output with required
  retention. Caller decides readiness and quiesces capture. Default comparison base
  is the previous ready version of the same output/scope; explicit saved-base override
  is allowed. Freeze base/target identities and concurrent publication order, independent
  of recipient acknowledgement history. Missing/pruned/first base is explicit.
- Optional content detail is a complete text diff if it fits, otherwise bounded
  changed-line ranges/counts without line text by default, otherwise explicit omitted
  detail plus authorized saved-version reference. Binary/unavailable outcomes are
  explicit; never label a truncated patch complete. Check both versions' permissions,
  including deleted/context content, and bound input size/CPU/memory/time.
- One generated Protobuf schema is primary; optional generated ProtoJSON encodes bytes
  as base64. Complete encoded-body default is configurable 64 KiB (65,536 bytes) under
  profile ceilings, including envelope/encoding overhead. If both representations are
  admitted, detail must fit both before freezing. Semantic equality is separate from
  serialization/signing bytes; no gRPC or unmeasured performance claim is required.
- Transport acceptance stops retries once durably recorded. Explicit authenticated,
  idempotent artifact release ends only that consumer's hold after it secures data or
  no longer needs Stow's copy. Other holds remain; GET/task success does not imply release.
  Administrative discard is distinct. Cover late/lost/out-of-order receipts and restart.
- Revocation of any disclosed resource/version pauses the whole queued report. No
  automatic trimming/rewrite under old identity; explicit newly scoped report has a
  new identity and does not erase original holds. Recheck full scope before retry;
  already transmitted bytes cannot be recalled.
- Optional webhook POST uses this journal, stable event/delivery identity and frozen
  destination revision/body/encoding. Host-local endpoint/secret configuration is
  explicit; HMAC-SHA256 signs exact bytes with a versioned timestamp/window contract.
  HTTPS default, literal loopback HTTP for development, bounded private-network policy,
  no URL credentials/fragments/redirects. Destination changes never silently retarget
  pending work. No network I/O while holding workspace mutation/capture locks.
- Receiver 2xx means durable acceptance, not artifact release/exactly-once effects.
  Retry network/timeout/408/429/5xx with bounded backoff/jitter/Retry-After; retain other
  failures and exhausted attempts for inspection. Serialize per subscription initially,
  isolate healthy destinations, persist intent, coordinate claims and cancel safely on close.
  Correlation/causation on explicit reports is optional; consumers prevent feedback loops.

### Encryption, portable recovery and keys

- Opt-in authenticated encrypted records bind namespace/resource/format/key version;
  preserve logical size/validators independently of ciphertext. Inventory metadata,
  temp/multipart/outbox/checkpoint/notification dependencies and export staging. No
  plaintext fallback in a protected profile; ordinary directory workspaces remain plaintext.
- Keys have independent namespace lifecycles; host providers may serve multiple namespaces
  without sharing lifecycle authority. Normal rotation preserves access to required older
  data until verified re-protection/removal. Explicit emergency revocation stops key use
  and may block recovery without deleting protected ciphertext. Host owns custody.
- Recovery carries captured data plus selected owned transfer/upstream/notification state,
  not arbitrary caller tasks or raw .stow. Bound and verify each section/payload and
  required capabilities. Upload IDs do not carry remote part bytes; offsets do not carry
  verified download prefixes. Current archives exclude unfinished multipart/private state.
- Adoption imports data but does not activate external operations. Apply current
  destination grants/configuration/keys, preserve original operation/event identity
  and provenance, distinguish destination-local stream identity and reconcile uncertain
  prior effects. Imported history is evidence, never permission to send to old endpoints.
- Confirmed takeover is default: source durably relinquishes selected work or shared
  ownership/receiver fencing prevents concurrent senders. Unreachability, elapsed local
  time and copied claims are insufficient. Bind evidence to snapshot/operations/recipient;
  test source restart, two recipients and pre-relinquishment backups. Without current
  evidence, keep external effects paused. No forced takeover or exactly-once promise.
- Valid artifacts may recover partially: affected operations/dependents stay blocked,
  independent operations explicitly activate after their own checks, and permitted
  blockers support re-evaluation. Corrupt/unsupported required artifacts still refuse.
- Recipient-encrypted handoff wraps the verified archive in a qualified authenticated
  envelope for a trusted authorized recipient's own provider/key; source storage keys
  stay source-side. Protected staging is part of the contract. Decryption grants neither
  destination permission nor activation/takeover.
- Optional backups are prepared before failure for a separately held recovery key.
  Its private key stays outside the archive; no silent escrow/hosted custody. Hosts own
  schedules and retained key versions. Qualify recovery with source/provider absent;
  losing every usable key is unrecoverable, and source revocation cannot recall copies.

### Target profiles and compatibility

Native first; Railway initially one native/container instance with an explicit volume,
secrets, authenticated networking and qualified shutdown/redeploy. Workers first proves
shared-core execution and minimal persistence; select topology from measured needs.
Its Cache API is disposable acceleration, not durable/global policy or journal storage.
Workers observe managed changes/origin reconciliation, not native filesystem watchers.

Publish capabilities by host/API/operation, including confidentiality, coordination,
offline freshness, revocation bounds and size/memory limits. Do not transplant native
locks/process lifetimes into request-scoped execution. Unknown required security/recovery
capabilities refuse; Protobuf permissiveness is not authority. Define old-reader refusal,
bounded decoding, optional-field round trips, migration and rollback before new formats
ship. Existing S3/CLI/MCP protocols remain separate; AWS ACL/IAM/SSE wire APIs are not
implied by Stow policy/encryption.

## 6. Final adjusted plan, summarized

### One open work queue

W01 is **complete** for the shared decision/contract gate; W02 and W04 are
**in progress**, with bounded native implementation slices described below. Other items are **planned** except where a prerequisite is explicitly complete.
Dependencies refer to qualified supported profiles, not universal capability. Native
slices can ship/test independently; future Workers/full-recovery work does not block a
baseline release that makes no such claims. W06/W16/W17/W19/W20 preparation may start early.

| ID | Slice and completion evidence | Depends on | Legacy trace |
| --- | --- | --- | --- |
| W01 | **Complete, 2026-09-29.** ADR 0010 §§4–5 visibly retain the qualified Store seam; [shared admission contract](storage-admission-contract.md) defines trusted namespace/resource/scope, comparison, effect/reason and capability semantics. Public conditional adapter compatibility tests passed locally. The target identity/policy model is not a shipping namespace/ACL API; remaining legacy authority gaps stay in W04/W20. | Existing source/ADRs | S0-2, S1-7, S1-10 |
| W02 | **In progress:** native memory and owned macOS/Linux filesystem `ReadForSave`/`SaveObject` implement runtime/resource observations, generation/content/metadata comparison, absence/replacement and effect outcomes. Filesystem request keys retain bounded receipts; replay preserves newer bytes, and reopen resolution verifies publication or keeps uncertainty paused. The [native object MCP profile](object-mcp-contract.md) wraps that core with expiring observations, mandatory request keys and bounded calls/payloads. [2026-09-30 local evidence](implementation-2026-09-30.md) records core and adapter checks. Workspace/custom stores, existing workspace MCP and WASM/in-process TypeScript/S3 managed-save surfaces remain unqualified. Prove two real consumers, further wrapper contracts, Linux execution and declared coordination. Conditional forwarding prerequisite is locally complete. | W01 | S1-10 |
| W03 | **Partially landed:** persisted recovery dependency holds now exist at both levels. The registry holds checkpoints and refuses cleanup/capture that would strand them, on a checksummed journal with an identity marker, so a missing or unreadable journal blocks deletion rather than forgetting the dependency. Object-level holds exist on the native filesystem store: a consumer pins the bytes it read, and put, guarded save, delete and batch delete are refused until the owner releases, across a close and reopen. An unreadable hold journal refuses mutation for the same reason the registry's does. Admission reserves transition bookkeeping before publication and withdraws the reservation when the effect fails, so a failed write cannot leave a store permanently unable to admit; payload and recovery budgets are accounted separately and the reserve is excluded from payload growth. Focused store, capacity and SDK tests, the resume conformance suite and full standards passed locally, and the landed paths then ran green on ubuntu-latest (Build, Conformance, Test, Lint, Ratcheted standards, and the TypeScript corpus lanes). Still open: registry-level expiry races, adapter disk-full behaviour, protected profiles, and any physical-headroom claim. | W01 | S1-4, S1-9 |
| W04 | **In progress:** legacy `Workspace.Destroy` now enforces its retained authority before any lifecycle/registry mutation, including after Close. Private constructor rollback preserves cleanup for newly created unfinished workspaces. Focused races, full standards, four local conformance profiles and shared TypeScript/Python workspace contracts passed. Enforce resource ACLs through every supported access/background path; qualify denial versus outage, persisted freshness/expiry, isolation and attenuation. Reuse existing authority/consent and preserve legacy profiles. `internal/policy` now implements the contract's resource identity, scope selectors and effective-access rule: environment authority intersected with policy, explicit-deny precedence, default denial, selectors scoped by namespace/collection/kind that never exchange object key-prefix and workspace path-subtree semantics, and a freshness deadline reported distinctly from a refusal. `authority.Intersect` is the single implementation of that intersection, because `With` only adds and therefore widens. Denial versus outage was already qualified — `ErrNotAuthorized` maps to 403 AccessDenied ahead of the storage and upstream tables. Nothing consults a policy yet, so no profile has changed behaviour. A revision can now be persisted. `policy.Record` carries a host-issued revision identity, a sequence and an absolute deadline, sealed by a digest over the canonical encoding; `policystore.Store` writes it atomically under a file lock and refuses a write whose expected sequence is not the stored one, so a revocation cannot lose a race to a re-grant. The store is a separate package because `internal/policy` is in the embedded runtime's dependency closure, which a test asserts must not link the filesystem. The deadline is absolute rather than a TTL, because a stored lifetime is re-based on every load and a restart inside the freshness window would extend the permission once per restart. Isolation is tested with eight concurrent writers that all believe they are first, and exactly one is. Still open: enforcement on the non-object paths (checkpoints, background retries, run-through propagation, scoped enumeration counts), saved-version selectors, and consulting a revision per operation rather than at open, so a revocation takes effect without a restart. Wiring this found a second bypass: an operation naming no resource was answered by the environment alone, so a policy denying `bucket.create` or `environment.reset` was written and did nothing, and a widening policy refused only the operations on a resource. `Set.Denies` now answers operations with no locator - a deny is honoured wherever it can be attributed, an allow needs a resource - and `TestEveryOperationReachesThePolicy` derives its table from `authority.Defined()`, so an operation with no case fails rather than waiting to be found. Moving the bucket operations to a new gate spelling then made `authority`'s enforcement detector demand an `Ungated` entry for enforced operations, the third time it has needed widening; a rename has teeth there. The run-through propagation funnel now consults a policy through `Config.ResourcePolicy`, so a revocation effective between an enqueue and the per-second drain stops the write rather than only having been checked when the caller asked for it. The routing question (`upstreamReachable`) and the permission question (`upstreamEnabled`, and the funnel) are deliberately separate functions: merging them put one check on the propagation path twice, and because the pre-filter runs first it answered for the funnel, leaving every test green with the funnel's copy deleted. That was found by a deliberate break rather than by reading, and it is why the split is recorded here. The workspace is now a resource a policy can decide about, which it could not be: a workspace is a registered resource whose `Destroy` consulted the environment only, so a policy could not deny it however it was written, and `KindWorkspace` was a selector nothing could reach — defined, persisted, matched, and built by no enforcement point anywhere. `policy.Workspace(id, path)` names the resource and `Instance.Authorize` is the seam, exported rather than left to each caller because resolving the revision and deciding must have one implementation. Three rules fell out and each was found by a test failing for a reason that was not the intended one. A selector with no locator is the collection as a whole, because `Exact: ""` and `Prefix: ""` are indistinguishable in a literal and the alternative makes a whole workspace unnameable; the deny test had been passing for the wrong reason, since with nothing matching, default denial refused the destroy. Default denial spans kinds, and that is not a bug: an object-only policy refuses a workspace destroy, and the tempting fix — a policy constraining only the kinds it names — is a widening, because a policy naming no object entries would then grant every object operation; `TestAPolicyNamingNoKindGrantsNothingAtAll` is the assertion it would fail. And a policy is a complete statement, not a patch: naming one workspace denies destroy for every workspace it does not name. A policy refusal is now its own type rather than `authority.ErrNotAuthorized`, whose message names the environment as the thing that refused and which reported a narrowed grant that never existed — a caller told to look at its `Authority` for a policy denial goes to the wrong place. `Set.Denies` became `Set.Deny` returning the refusal, and `policy.IsRefusal` is how a caller tells the two apart without parsing prose. Both were found the moment the predicate existed: the reach test's non-object cases had been passing because the environment refused, which the old assertion could not distinguish. Still open: checkpoint capture, which has no operation to be denied with and so needs a vocabulary change ADR 0010 requires enforcement and refusal coverage for; the remaining background paths; and saved-version selectors. `Options.Policy` is a `policy.Source` consulted per decision, so a revocation is effective when it is issued rather than when the process restarts, and a later revision wider than an earlier one is caught where it is read. It is a function type rather than an interface so a nil `*Set` cannot read as set and fail open. A source that cannot say returns its own error rather than a refusal, because a caller handed "you may not" during an outage cannot tell it was "I do not know". `Source` must be cheap and must not block, since the multipart paths consult it under the instance mutex; the maximum stale-policy interval is a host-supplied wrapper's to state. Getting the ordering wrong — resolving the policy after the environment check — reported the environment's reason instead of `ErrWidening`, and the widening test from the previous slice caught it. `internal/runtime` now consults a policy per resource, through `Options.Policy` (nil means no policy, so the default is unchanged). Three check functions divide the surface: `checkResource` for a named object, `checkUpload` for a multipart handle — where the resource is resolved from the instance's own record rather than the store, because the store's answer would have to be read before authorization — and `check` for operations naming no resource. `TestObjectChecksNameAResource` fails if an object operation reaches for the last one, so bypass-by-omission cannot land silently. Wiring it found and fixed a real defect: `CopyObjectCond` checked only the destination, so a write-only environment could copy object content out of the environment. Both ends are now authorized, source first. | W01 | S0-2, S1-7 |
| W05 | Add explicit scoped capture/export/restore/diff admission and versioned completeness. Prove forbidden/full refusal, destination checks, metadata disclosure and preservation outside scope; refuse unsupported deltas. | W04 | S1-2, S1-7 |
| W06 | **In progress:** implement a consumer-owned ordinary-SDK transfer example and continuation tests using existing multipart/range storage. Prove client and retained-server restart, source/remote changes, corrupt progress, expired links, lost part/completion replies and exact bytes without resending acknowledged data. Measure supported backends; add only reproduced missing storage behavior. No restart result is claimed before verification. | Existing multipart/range | S0-4, S1-5, S4-1 |
| W07 | Implement bounded encrypted native persistence and independent namespace key lifecycle across retained/staging paths. Prove tamper/wrong-key refusal, range/validators, crash publication, interrupted rotation, emergency revocation and explicit plaintext boundaries. | W03, W04 | S1-8 |
| W08 | Implement content reconciliation, durable baseline/journal, parent matching, bounded replay/acks and explicit observation lifetime. Define the primary generated Protobuf envelope/ProtoJSON compatibility now. Prove external/managed edits, missed hints, gaps, overlap, restart and concurrent observers without duplicating the store. | W03, W04 | S1-6 |
| W09 | Publish immutable ready versions with holds, guarded declared-input checks and separate consumer acceptance/release state. Prove changed inputs preserve drafts, strict atomic-coverage refusal, snapshot/omitted modes, multi-consumer delayed fetch, lost/out-of-order receipts and revocation. | W02, W03, W05, W08 | S1-6, S1-10 |
| W10 | Add bounded optional text diff/line report to the shared schema. Freeze previous-ready/explicit base and target; prove permission to both versions, first/pruned base, concurrency, binary/unavailable outcomes and complete binary/ProtoJSON body boundaries. | W09 | S1-6, S1-7 |
| W11 | Add explicit signed webhook dispatch over the journal, host-local configuration and coordinated bounded retries. Prove acceptance/release separation, crash duplicates, endpoint/key changes, denial pause, healthy-destination isolation and close/cancel. Optional diff support is not required to send a plain ready report. | W08, W09 | S1-6 |
| W12 | Extend verified archives with versioned selected owned recovery sections, consistent bounded payloads and inactive adoption. Preserve operation/event identity; prove missing dependencies, repeat import, partial operation recovery and corrupt/unsupported refusal. Sections depend on their owner; do not block transfer-only profiles on every notification feature. | W03, W05, W06; W08/W09 for notification sections | S1-9 |
| W13 | Qualify confirmed takeover/fencing and uncertain-effect reconciliation for portable external operations. Prove current source release/shared ownership, source restart, two recipients and stale backups; absence of evidence leaves sends paused. No forced failover. | W04, W12 | S1-9 |
| W14 | Add recipient-encrypted handoff using the verified archive/envelope and protected staging. Prove wrong recipient, namespace/scope binding, destination authorization and inactive effects. Data-only export can precede portable operation sections. | W05, W07; W12 for recovery sections | S1-8, S1-9 |
| W15 | Add opt-in separately held recovery-key backup support. Prove fresh-host recovery with original provider absent, rotated/wrong/missing keys, protected staging, current grants and no revived sends. Host retains custody/scheduling. | W14 | S1-8, S1-9 |
| W16 | Maintain workload/compatibility evidence: capture/transfer retained bytes, upstream fills, list/index size, encrypted range peaks and namespace concurrency. Publish enforced limits and coverage; reproduce new gaps before fixes. Existing local measurements remain dated, not universal promises. | Existing baseline; repeat for each changed profile | S0-3–S0-6, S1-5 |
| W17 | Run an early Workers shared-core/execution/persistence spike with synthetic data. Select a minimal adapter topology and supported operations; reject unsupported native assumptions before building a deployment promise. | W01; existing WASM/browser mechanisms | S4-4 |
| W18 | Qualify protected native, Railway and Workers profiles separately. Prove warm denial, outage/freshness, key/policy expiry/rotation, restart/redeploy, persistence and resource bounds. Initial edge profile is read-only; write propagation needs separate qualified behavior. | W04, W07, W16; W17 for Workers; W08/W11 only if advertised | S4-4 |
| W19 | Finish caller acceptance with a meaningful interrupted real-model turn, confirmed cleanup/save, fresh context on Linux/second host and actual OpenCode-to-MCP configuration. Reuse pinned controller/fixtures; distinguish storage failure, successful no-op and task failure. Local meaningful model continuation and stdio negotiation are already complete. | Existing baseline; explicit model/host profile | S3-1–S3-3, S4-1 |
| W20 | Complete release/provider/install/documentation gates for the selected supported profile. Reuse 0.3.0 alignment and immutable artifact pipeline; reconcile npmjs guidance, account setup, separate fresh AWS/R2/custom receipts and published anonymous macOS/Linux consumers. Preserve legacy-format diagnostics/security/contribution maintenance. | Applicable feature evidence, W16, required Linux/provider checks | S2-1–S2-5 |
| W21 | Pilot a meaningful S3 fixture and transfer recipe in a named consumer: cococlips first, pg_backup next if needed. Start with synthetic data; consumer owns progress/reactions. Package convenience APIs follow demonstrated reusable need. Do not mark source-fit proposals as actual integrations. | W06; supported artifact/profile | S4-1 |
| W22 | Run independent repeat-use study and decide deepen/narrow/rework. Two teams each complete at least three genuine save/reopen/review events over two weeks and retain use; measure setup/recovery effort, correctness and costs against actual alternatives. Research/outreach requires its own actual evidence/authorization. | Usable supported workflow; W19/W21 evidence | S4-2, S4-3 |

**Active code slices:** W04 resource/background ACLs; independent W06
ordinary-SDK transfer continuation. W03 holds and namespace admission are landed
and verified locally, with registry expiry races and protected profiles still
open. W02 consumer adoption of the bounded native object MCP workflow remains
open, carrying original request meaning and resolving uncertainty before any new
mutation. The wrapper contract is implemented. Native filesystem
interruption/reopen evidence is local macOS arm64; physical Linux execution and
two real consumers remain open. W04 resource/background ACLs remain open. No
deployment or release is implied.

### Legacy item disposition

| Previous IDs | Reconciled disposition |
| --- | --- |
| S0-1 | Local defects repaired; preserve regression/provider evidence through W16/W20. |
| S0-2, S0-3 | Existing authority/confinement locally implemented; ADR/policy/host qualification goes to W01/W04/W16/W20. |
| S0-4 | Existing local corpus/coverage retained; uncovered resume/client/provider evidence goes to W06/W16/W20. |
| S0-5, S0-6 | Request/transport bounds and standards locally pass; broaden measured workload evidence in W16, maintain gates in W20. |
| S0-7 | Shared capture/publication/cleanup and checkpoint receipts locally implemented; preserve semantics while extending W03/W05/W12. |
| S1-1, S1-2, S1-3 | Baseline facade/portable-v2/Git direction implemented; scoped additions W05, workload/host evidence W16/W19/W20. Self-contained Git bases and enhanced deltas stay deferred. |
| S1-4 | Standing registry policy complete; new dependency/budget/reserve work W03. |
| S1-5 | Existing cost measurements retained; further transfer/resource evidence W06/W16. |
| S1-6 | Core notifications/readiness/codecs/diffs/webhooks W08–W11; portable sections W12. |
| S1-7 | Trusted identity/policy/scope enforcement W01/W04/W05 plus all delayed-operation checks. |
| S1-8 | Encrypted records/lifecycle W07, recipient and recovery envelopes W14/W15. |
| S1-9 | Dependencies W03, portable state/takeover W12/W13, encrypted recovery W14/W15. |
| S1-10 | Conditional prerequisite complete; richer identity/guarded save W01/W02 and input-basis readiness W09. |
| S2-1–S2-5 | Pipeline/version preparation retained; actual release, installs, providers and docs W20. |
| S3-1–S3-3 | Local adapter/caller/receipts and meaningful local model work retained; interruption/Linux/MCP caller acceptance W19. |
| S4-1 | Meaningful fixture/transfer/agent evidence W06/W19/W21. |
| S4-2, S4-3 | Independent adoption and investment decision W22. |
| S4-4 | Workers spike and three-target qualification W17/W18. |

### Release and product acceptance

- Select and document the supported profile and scenario traceability. Required checks
  cannot silently skip. Preserve pinned SDKs, explicit unsupported operations and formats.
- At the actual release revision pass Go/unit/race, all applicable local conformance,
  language/WASM/caller tests, raw auth/protocol/run-through/outbox checks, standards and
  reproducible artifacts. Today's planning change performs only document validation.
- Preflight and verify exact artifacts before publication; handle partial retries by
  identity and distinguish missing packages from lookup errors. Verify actual published
  anonymous consumers afterward before declaring release complete.
- Retain separate revision-bound disposable AWS S3, R2 and custom-profile evidence for
  required conflict/retry/restart/multipart cases and cleanup. Use isolated short-lived/
  session credentials; missing credentials block the claim. Any exception needs an
  explicit recorded scoped decision, not application credentials or a silent skip.
- Check secret-safe logs/metrics, local credential separation, shutdown, artifact contents,
  checksums, versions/tags and actionable legacy-record refusal. No silent migration.
- Product completion additionally requires usable movable saved state, current supported
  metadata/modes/deletions, real caller continuation on the supported second host,
  meaningful fixture use and W22 repeat use. A release is not proof of product fit.

### Deferred and transferred scope

Execution registries, model brokers, sandbox/CPU allocation, full chat/process-memory
restoration, hosted accounts/scheduler/remote MCP, automatic forced takeover, general
collaborative editing and AWS ACL/IAM/SSE APIs are not scheduled. Streaming/deduplication,
new compilers, extra hosts/handles/pools, Windows expansion, self-contained Git base
bundles and enhanced deltas require a measured named need and supported contract.
The retired custom S3-client replacement and old release ladders remain retired.

Native/Workers/Railway adapters and resource ACL/encryption are specifically authorized
by the accepted foundation scope; they do not reactivate every earlier deploy-anywhere
proposal. Preserve existing browser persistence. Collaboration is transferred to its
own repo; optional public Stow integration does not make Stow its execution owner.

## 7. Handover items for engineering, enumerated

1. Work from W IDs above. Update status here with exact changed API/profile, revision,
   platform and evidence. Do not maintain progress checklists in retired proposal bodies.
2. W01 is complete: use the amended ADR 0010 and shared admission contract for
   W02/W04. The plan authorizes no silent breaking removal or unenforced permission;
   retain per-profile qualification and explicitly track the legacy lifecycle gaps.
3. Implement small native slices through the existing core. Each owning subsystem
   defines its bounds, durable outcome/replay contract and capability refusal before
   wrappers/export sections/host adapters consume them.
4. Verify behavior through real public consumers and shared fixtures. Include stale saves,
   changed input basis, lost replies, cleanup holds, full budgets/physical failure,
   warm denial/outage, interrupted key rotation, delayed fetch/release, encoded limits,
   source restart/two owners/old backups and wrong recipient/recovery keys as applicable.
5. Keep evidence gaps separate from bugs. A source observation requires reproduction
   before a compatibility-changing fix. Scope same-store/native results precisely;
   neither compilation nor synthetic local smoke tests prove cross-host/production behavior.
6. Keep [planning index](planning-index.md) as a document disposition map only. Prior
   detailed designs retain stable paths/anchors; accepted ADRs and current usage remain
   active references. The [previous queue](history/plan-before-foundation-consolidation-2026-09-29.md)
   is frozen history, with every old item accounted for above.
7. Treat publication, deployment, provider mutations and consumer integrations as concrete
   later work with their applicable authorization/evidence. This reconciliation performs
   none of them and does not reopen settled interview choices.
