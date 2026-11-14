# Consolidated working-storage plan

**Sole canonical scope, status, ordering and dispatch queue — reconciled 2026-11-14**
against published v0.4.0, source revision
`3bb3e890bbe56f11dd783c2ccfb9bd2813156919`, accepted ADRs/contracts and dated
qualification records. W01–W28 and the legacy S0–S4 mappings remain stable.
The [planning index](planning-index.md) records document dispositions, not another backlog.
Older plans retain design rationale, anchors and dated receipts; their checklists,
assignments and “next” instructions no longer schedule work.

**Release baseline:** [v0.4.0](../releases/v0.4.0.md) native Linux/macOS x64/ARM64
binaries and the Go module are published. The site and release documentation are live.
Public npm packages remain at **0.3.7**; Python is source-only. Automated npm/OIDC
and PyPI publication remain disabled. Native release CI is not second-physical-host,
model-host, protected-deployment or repeat-use acceptance.

## 1. Plan validation summary

**Verdict: Adjust.** Existing storage, conditional-write and caller integration
work provides the foundation; it must not be reimplemented because older plans
still use future tense. Remaining work qualifies the implemented editing/access/transfer paths and extends
retention, encrypted persistence, notifications and portable recovery where still absent. Release,
cross-host evidence and adoption are separate gates.

Stow owns portable working storage for agents, tools, applications and tests:
preparation, files/objects, capture, inspection, transfer, storage notifications and
lifecycle. Callers own execution, models, read tracking, writer quiescence, readiness
and reactions. Cache is one deployment profile. ADRs [0014](../adr/0014-storage-product-caller-owned-execution.md),
[0015](../adr/0015-durable-workspace-notifications.md) and
[0016](../adr/0016-scoped-access-encrypted-cache.md) govern that boundary.

The user delegated routine technical choices. Resolve API names, bounds, codecs,
fixtures and adapter design through implementation evidence. Escalate only an
unresolved consequential product, custody or compatibility choice. No user answer
is needed to execute this planning order. This reconciliation changes documentation,
not runtime behavior. It records the already published release; it publishes nothing.

## 2. What the plan already covers well

### Reconciled baseline: preserve, do not rebuild

| Capability | Implemented / qualified baseline | Remaining qualification or extension |
| --- | --- | --- |
| Storage and workspace | Shared directory/S3 runtime, bounded transport, conditional writes, portable-v2 capture/diff/archive/restore, Git provenance and recipient-local reconstruction. [Baseline](../history/implementation/implementation-2026-09-29.md). | Named workload/provider and physical second-host evidence; no enhanced deltas or self-contained Git bases. CRC64NVME multipart remains unsupported. |
| Guarded saves | Native object managed saves and bounded request receipts; workspace file observation/save/resolve, recovery ownership and capture/cleanup barriers. [Object evidence](../history/implementation/implementation-2026-09-30.md), [workspace contract](../contracts/workspace-editing-contract.md), [CLI/MCP/SDK qualification](../reviews/2026-11-12-workspace-editing-mcp.md). | Additional wrapper/store coordination, protected-policy profiles, two real consumers and receipt lifecycle beyond current bounds. W02/W26. |
| Retention and capacity | Registry caps/deletion, persisted dependency holds, native filesystem object holds and admission bookkeeping are landed. | Registry expiry races, physical disk-full/headroom, protected and broader adapter accounting. W03. |
| Resource policy | Resource selectors/intersection, per-decision policy Source, object/multipart/copy/list/batch gates, workspace capture and run-through/background checks exist in source and regression tests. | Named host Source wiring/freshness and supported-profile qualification. Source/test coverage does not certify multi-tenant endpoints or OS isolation. W04/W05/W18. |
| Resumable transfers | Consumer-owned uploader/downloader and ordinary AWS SDK **upload** retained-server-restart conformance exist. | Ordinary SDK **download** restart/refusal conformance and measured profile report. W06; portable unfinished operations remain W12. |
| Workspace access | W23/W24 resource reader/host usability and W25/W26 native saved-view/editing APIs are included in v0.4.0. Local OpenCode 2.2.7 / Free Router read-only review passed; direct Luna saved-state and editing continuation passed on one host. | Real second-host/interruption and protected-profile acceptance. Failed Free Router editing/strict-output runs remain failed, not superseded by native CI. W19/W25/W26. |
| MCP / callers | Scoped stdio and sessionless HTTP, explicit remote-listener consent/bearer token, request/resolve receipts and caller-owned continuation fixtures. | Qualify the named caller/model/version with the published binary; caller owns execution and shutdown. W19/W21. |
| Distribution | v0.4.0 native/Go published; four-platform native, wheel-carrier and packed npm/MCP consumers passed release CI. Site/docs deployed. npm public distribution remains 0.3.7. | Fresh required live-provider receipts and future registry trust/publication decisions are separate W20 work. |

### Browser project storage extension on 2026-10-12

The browser extension under [ADR 0019](../adr/0019-browser-project-content.md) is
implemented: asynchronous host-backed content, chunk transport, atomic project
history and conditional manifest publication. [Usage](../guides/browser-project-storage.md)
and the [dated workload receipt](../benchmarks/browser-project-2026-10-12.json)
remain authoritative for their scope. The completed [execution record](../design/browser-project-plan.md)
is frozen; ongoing regression/performance work belongs to W16/W20.

### 0.4.0 release candidate qualification

The candidate ladder RC1–RC5 is **closed by publication**, not an active queue.
[PR #139](https://github.com/chester-hill-solutions/stow-s3/pull/139) merged at
`3bb3e89`; [main CI](https://github.com/chester-hill-solutions/stow-s3/actions/runs/43619657925)
and the [tagged release workflow](https://github.com/chester-hill-solutions/stow-s3/actions/runs/43619704722)
passed. Four public native archives/checksums and the public Go module were verified.
The release uses a native-download policy; the live-provider job was explicitly
skipped, and npm/PyPI v0.4 publication was not enabled. W20 owns future release work.

Earlier disk-space interruptions and candidate test-setup failures remain historical
non-passes. They were followed by corrected, revision-bound qualification; they are
not evidence of a remaining release blocker. The original dirty checkout and its
missing tracked dist files were preserved. Older local/model/browser receipts keep
their original dates and limits; release CI does not relabel them new field studies.

## 3. Gaps, conflicts, or redundancies to fix

| Finding | Reconciled disposition | Work owner |
| --- | --- | --- |
| Future tense schedules landed work | Resource-policy and upload-restart implementations already exist. W23–W26 native APIs shipped in v0.4.0. Replace duplicate implementation with named remaining qualification. | W04/W06/W19/W20/W23–W26 |
| Compatibility text conflicts with current symbol | ADR 0010 still directs removal in its original decision/consequences, while its later enforcement amendment records `environment.promote` as ungated and current code exports `EnvironmentPromote`. **Open documentation conflict:** reconcile those specific ADR consequences before any promotion/API change. The compatibility default for current work is to preserve the symbol and explicit ungated status; no incidental breaking removal or claim of enforced promotion. | W01/W04/W20 |
| Runtime gates mistaken for a protected host | Per-call Source and background checks exist, but trusted identity, host freshness bounds and endpoint/direct-filesystem access still require profile qualification. Ordinary unscoped APIs preserve compatibility. | W04/W05/W18 |
| Transfer primitives mistaken for complete continuation | Upload restart is qualified in repository conformance; download unit tests are not SDK/server-restart acceptance. Portable unfinished payloads remain a separate extension. | W06/W12 |
| Bounded receipts mistaken for an approved retirement policy | Current receipts fail closed at bounds. The retirement proposal does not select archive ownership/durability/budgets or a replay horizon. Preserve unknown outcomes; do not silently evict or re-execute old keys. | W02/W03, deferred decision |
| Scale proposal mistaken for an unstarted backlog | Sized streaming/COPY REPLACE and verified parent-checkpoint payload reuse are implemented. Unsized buffered writes and broader index/dedup changes require a measured named need. No general persistent ordered index claim. | W16 |
| Logical quotas mistaken for physical recovery space | Holds/admission landed; physical headroom, disk-full behavior and broader adapter guarantees require independent evidence. | W03 |
| Saved bytes mistaken for readiness or takeover | Current bytes do not establish current inputs, one sender, consumer release or permission. Ready reports, portable operation sections and confirmed takeover remain separate planned contracts. | W09/W11/W12/W13 |
| Accepted target mistaken for shipped profile | Durable notification schema/delivery, encrypted persistence, recovery envelopes and protected Workers/Railway profiles remain planned. | W07–W15/W17/W18 |

Old proposal bodies remain design/history references, with leading supersession
notices. Actual usage, source and dated evidence resolve implemented names/defaults.
The standalone [collaboration plan](https://github.com/chester-hill-solutions/gangcode/blob/main/docs/plan.md) owns
shared editing, presence, harness observation and controls; no RT work is scheduled here.

## 4. Existing code/product references to reuse or extend

| Concern | Reuse boundary |
| --- | --- |
| Identity, grants, write conditions | [policy reach tests](../../internal/runtime/policy_reach_test.go), [per-call Source tests](../../internal/runtime/policy_source_test.go), [outbox funnel](../../internal/runthrough/outbox_adapter.go), [authority](../../internal/authority/authority.go), [runtime operations](../../internal/runtime/objects.go), [public options](../../pkg/stow/types.go), [custom-store declaration](../../pkg/stow/store.go), [workspace writes](../../internal/storage/workspace/objects.go). |
| Capture, lifecycle and receipts | [capture request/resolve](../../pkg/stow/checkpoint_request.go), [registry admission](../../internal/storage/workspace/admission.go), [registry policy/deletion](../../pkg/stow/registry_policy.go), existing capture/cleanup locks and atomic publication. |
| Portable data and Git | [archive](../../pkg/stow/checkpoint_archive.go), [handoff](../../pkg/stow/handoff.go), existing checkpoint import/restore/diff and Git provenance paths. Do not copy raw .stow or create another registry. |
| Transfer | [consumer-owned resume example](../../examples/resumable-transfer/download.go), [SDK upload restart tests](../../conformance/resume_upload_test.go), [filesystem multipart](../../internal/storage/fs/fs_multipart.go), [workspace multipart](../../internal/storage/workspace/multipart.go), [S3 multipart](../../internal/s3api/handlers_multipart.go) and existing range/conditional GET handlers. |
| Origin/cache and pending writes | [cache read](../../internal/runthrough/cache_read.go), [cache policy](../../internal/runthrough/cache_policy.go), [write policy](../../internal/runthrough/writepolicy.go), durable outbox claims/retry and transport policy. Reuse mechanisms only where semantics match; webhook POST is not an S3 mutation. |
| API surfaces | [MCP](../../internal/mcpstorage/server.go), existing native CLI/workspace commands, [WASM bridge](../../cmd/stow-wasm/main.go), [embedded wrapper](../../packages/stow-s3/src/embedded.ts), [persistent wrapper](../../packages/stow-s3/src/persistent-embedded.ts), Python/TypeScript lifecycle wrappers. Keep wrappers thin. |
| Qualification | Shared [conformance](../../conformance/CONFORMANCE.md), [coverage map](../history/assessments/compatibility-coverage-2026-09-29.md), existing release/provider/script checks, portable workload benchmarks and OpenCode fixtures. Extend uncovered cases rather than copying test suites. |

The [shared identity/admission contract](../storage-admission-contract.md) fixes W01
semantics and separates current profiles from target guarantees. Normative contracts
also include [S3 compatibility](../contracts/compat-contract.md),
[workspace](../workspace-contract.md), [preparation](../contracts/task-manifest.md),
[browser persistence](../guides/browser-persistence.md), and accepted ADRs. Current
[usage](../guides/portable-workspace-usage.md) is the runnable surface. Prior detailed plans
retain rationale/examples, not independent work orders or stronger implemented claims.

## 5. API impact and validation notes

This consolidation changes no API. The defaults below distinguish accepted targets
from current implementation; each W row states what remains. Future extensions add contracts;
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

Status is profile-specific: **complete/released** closes only the named contract,
**partial** carries explicit remaining acceptance, **planned** is absent extension,
and **conditional/deferred** has no implementation dispatch until its stated gate.
Dependencies mean qualified profiles, not universal capability. Stable W IDs are the
only backlog; the milestone below decomposes those IDs without a second queue.

| ID | Slice and completion evidence | Depends on | Legacy trace |
| --- | --- | --- | --- |
| W01 | **Complete, 2026-09-29.** ADR 0010 §§4–5 visibly retain the qualified Store seam; [shared admission contract](../storage-admission-contract.md) defines trusted namespace/resource/scope, comparison, effect/reason and capability semantics. Public conditional adapter compatibility tests passed locally. The target identity/policy model is not a shipping namespace/ACL API; remaining legacy authority gaps stay in W04/W20. | Existing source/ADRs | S0-2, S1-7, S1-10 |
| W02 | **Partial; native object saves and W26 workspace editing shipped.** Reuse managed observations, generation/content/metadata checks, bounded request receipts and reopen resolution; see [object contract](../contracts/object-mcp-contract.md) and [workspace contract](../contracts/workspace-editing-contract.md). Qualify additional wrappers/custom stores, declared coordination and two real consumers; do not reimplement the native editor. **Deferred receipt lifecycle:** the native filesystem object-save profile's current 1,024-record / 8 MiB bounds fail closed. Before extending them, decide archive owner/durability/access/budgets/marker growth versus an explicit replay horizon with permanent refusal. The [retirement proposal](../design/receipt-retirement-proposal.md) approves neither option; unknown evidence never means no effect, and current generation must remain retained. | W01 | S1-10 |
| W03 | **Partially landed:** persisted recovery dependency holds now exist at both levels. The registry holds checkpoints and refuses cleanup/capture that would strand them, on a checksummed journal with an identity marker, so a missing or unreadable journal blocks deletion rather than forgetting the dependency. Object-level holds exist on the native filesystem store: a consumer pins the bytes it read, and put, guarded save, delete and batch delete are refused until the owner releases, across a close and reopen. An unreadable hold journal refuses mutation for the same reason the registry's does. Admission reserves transition bookkeeping before publication and withdraws the reservation when the effect fails, so a failed write cannot leave a store permanently unable to admit; payload and recovery budgets are accounted separately and the reserve is excluded from payload growth. Focused store, capacity and SDK tests, the resume conformance suite and full standards passed locally, and the landed paths then ran green on ubuntu-latest (Build, Conformance, Test, Lint, Ratcheted standards, and the TypeScript corpus lanes). Still open: registry-level expiry races, adapter disk-full behaviour, protected profiles, and any physical-headroom claim. | W01 | S1-4, S1-9 |
| W04 | **Partial; core enforcement landed.** `internal/policy`, per-decision `Options.Policy`, resource/multipart/copy/list/batch checks, `Instance.Authorize`, authorized capture and background outbox/read-through checks are implemented. [Reach](../../internal/runtime/policy_reach_test.go) and [Source](../../internal/runtime/policy_source_test.go) tests guard revocation, widening and unavailable-policy semantics. Next: qualify one named native host-supplied Source and its freshness/identity wiring. Extend gates only for a reproduced missing path; no new policy engine. Preserve nil/unscoped compatibility and the exported, unimplemented `EnvironmentPromote`. Protected endpoint/encrypted/Workers/Railway claims remain W18. | W01 | S0-2, S1-7 |
| W05 | **Partial prerequisite; scoped artifact contract remains planned.** Workspace capture authorization and external authorized capture exist. Extend explicit scoped export/restore/diff and versioned completeness only where current checkpoint contracts do not cover them. Prove forbidden/full refusal, destination authorization, metadata disclosure and preservation outside scope. Existing checkpoint reads are not universal protected historical-version access; refuse unsupported deltas. | W04 | S1-2, S1-7 |
| W06 | **Partial; upload and download restart conformance implemented.** Reuse [consumer-owned resume](../../examples/resumable-transfer/download.go) and [upload tests](../../conformance/resume_upload_test.go). **W06.A implemented, 2026-11-14:** [download tests](../../conformance/resume_download_test.go), source `5e433f7a7dacdf5b2b4ec64a41ff878272165902`, exercise retained filesystem/runtimefs/workspace restart, exact bounded ranges/validators/bytes and completed reopen without another GET. Memory restart refuses missing remote data without changing the prefix/state. The first W06.B case also refuses overwritten objects with SDK PreconditionFailed/412 and unchanged prefix/state (filesystem profile). [PR #140](https://github.com/chester-hill-solutions/stow-s3/pull/140) records integrated qualification; [conformance scope](../../conformance/CONFORMANCE.md#retained-store-transfer-continuation) fixes bounds and exclusions. Remaining: SDK corrupt/truncated-prefix and interrupted-body cases plus measured W06.C workload evidence. Signed-link expiry requires a named signed-link profile. Product APIs are unchanged; portable unfinished payloads remain W12. | Existing multipart/range | S0-4, S1-5, S4-1 |
| W07 | Implement bounded encrypted native persistence and independent namespace key lifecycle across retained/staging paths. Prove tamper/wrong-key refusal, range/validators, crash publication, interrupted rotation, emergency revocation and explicit plaintext boundaries. | W03, W04 | S1-8 |
| W08 | Implement content reconciliation, durable baseline/journal, parent matching, bounded replay/acks and explicit observation lifetime. Define the primary generated Protobuf envelope/ProtoJSON compatibility now. Prove external/managed edits, missed hints, gaps, overlap, restart and concurrent observers without duplicating the store. | W03, W04 | S1-6 |
| W09 | Publish immutable ready versions with holds, guarded declared-input checks and separate consumer acceptance/release state. Prove changed inputs preserve drafts, strict atomic-coverage refusal, snapshot/omitted modes, multi-consumer delayed fetch, lost/out-of-order receipts and revocation. | W02, W03, W05, W08 | S1-6, S1-10 |
| W10 | Add bounded optional text diff/line report to the shared schema. Freeze previous-ready/explicit base and target; prove permission to both versions, first/pruned base, concurrency, binary/unavailable outcomes and complete binary/ProtoJSON body boundaries. | W09 | S1-6, S1-7 |
| W11 | Add explicit signed webhook dispatch over the journal, host-local configuration and coordinated bounded retries. Prove acceptance/release separation, crash duplicates, endpoint/key changes, denial pause, healthy-destination isolation and close/cancel. Optional diff support is not required to send a plain ready report. | W08, W09 | S1-6 |
| W12 | Extend verified archives with versioned selected owned recovery sections, consistent bounded payloads and inactive adoption. Preserve operation/event identity; prove missing dependencies, repeat import, partial operation recovery and corrupt/unsupported refusal. Sections depend on their owner; do not block transfer-only profiles on every notification feature. | W03, W05, W06; W08/W09 for notification sections | S1-9 |
| W13 | Qualify confirmed takeover/fencing and uncertain-effect reconciliation for portable external operations. Prove current source release/shared ownership, source restart, two recipients and stale backups; absence of evidence leaves sends paused. No forced failover. | W04, W12 | S1-9 |
| W14 | Add recipient-encrypted handoff using the verified archive/envelope and protected staging. Prove wrong recipient, namespace/scope binding, destination authorization and inactive effects. Data-only export can precede portable operation sections. | W05, W07; W12 for recovery sections | S1-8, S1-9 |
| W15 | Add opt-in separately held recovery-key backup support. Prove fresh-host recovery with original provider absent, rotated/wrong/missing keys, protected staging, current grants and no revived sends. Host retains custody/scheduling. | W14 | S1-8, S1-9 |
| W16 | **Maintenance / measured extensions.** Preserve dated [comparison evidence](../benchmarks/2026-10-19-marketing-comparison) and browser workload receipts; measure changed capture/transfer/list/index/range/concurrency profiles. Sized `PutObjectStream` and S3 COPY REPLACE staging are implemented; unsized readers may buffer. Verified parent-checkpoint payload linking is implemented, not universal deduplication. The [scale proposal](../design/plan-storage-scale-and-transfer.md) is a frozen design reference: further streaming, a general persistent ordered index or broader COW/dedup needs a named measured bottleneck, bounds and regression acceptance. | Existing baseline; repeat for each changed profile | S0-3–S0-6, S1-5 |
| W17 | Run an early Workers shared-core/execution/persistence spike with synthetic data. Select a minimal adapter topology and supported operations; reject unsupported native assumptions before building a deployment promise. | W01; existing WASM/browser mechanisms | S4-4 |
| W18 | Qualify protected native, Railway and Workers profiles separately. Prove warm denial, outage/freshness, key/policy expiry/rotation, restart/redeploy, persistence and resource bounds. Initial edge profile is read-only; write propagation needs separate qualified behavior. | W04, W07, W16; W17 for Workers; W08/W11 only if advertised | S4-4 |
| W19 | **Partial caller acceptance.** Existing OpenCode/MCP configuration, local read-only Free Router review and same-host direct Luna continuation are recorded; do not repeat setup as missing implementation. Qualify the published binary with a named caller/model: meaningful interrupted turn, confirmed cleanup/save and fresh context on a second physical host. Reuse pinned controllers/fixtures; distinguish storage failure, successful no-op and task failure. Native Linux release CI does not close caller/physical-host acceptance. | Existing baseline; explicit model/host profile | S3-1–S3-3, S4-1 |
| W20 | **v0.4.0 native/Go release complete; ongoing maintenance.** Exact-source CI, four native build/carrier/packed MCP consumers, verified public archives/checksums, Go resolution and deployed site/docs close the release ladder. npm remains 0.3.7; Python source-only; future npm OIDC/PyPI and fresh required AWS/R2/custom receipts remain separate work. Retain legacy-format diagnostics, reproducible artifacts and scoped provider exceptions. See [release baseline](../releases/v0.4.0.md). | Applicable feature evidence, W16, required Linux/provider checks | S2-1–S2-5 |
| W21 | **Consumer pilot remaining.** Use the existing Aboiteau native storage integration as the first available host/controller acceptance target, with a published v0.4 binary, synthetic state and caller-owned interruption/recovery. Preserve prior B2/C2 receipts and failed local QEMU attempts separately. An S3 transfer pilot in cococlips (pg_backup if needed) follows demonstrated access/ownership and W06 evidence; these are source-fit candidates, not completed integrations. No new Stow runner/client SDK. | W06; supported artifact/profile | S4-1 |
| W22 | Run independent repeat-use study and decide deepen/narrow/rework. Two teams each complete at least three genuine save/reopen/review events over two weeks and retain use; measure setup/recovery effort, correctness and costs against actual alternatives. Research/outreach requires its own actual evidence/authorization. | Usable supported workflow; W19/W21 evidence | S4-2, S4-3 |
| W23 | **Released v0.4.0 native profile.** Opt-in live workspace overview, bounded complete direct-child observations, regular-file resources, shared read leases and exclusive registered-removal guard. [Contract](../contracts/workspace-resources-contract.md) and [dated qualification](../reviews/2026-11-11-workspace-resources.md). Maintain platform regressions; host/second-host/protected claims remain separate. | Existing workspace/MCP baseline; W04 for protected claims | Workspace access extension, 2026-11-11 |
| W24 | **Released native host-usability surface; named local read-only pilot passed.** Static folder scope, bounded metadata/pages and optional same-bundle browser; [qualification](../reviews/2026-11-11-workspace-resource-hosts.md) records OpenCode 2.2.7 / Free Router resource review. Native release CI closes its platform/artifact gates, not live App-host or second-physical-host acceptance. No new UI work is scheduled. | W23; W04 for protected profiles; W16/W19/W20 for qualification/release | Workspace access extension, 2026-11-11 |
| W25 | **Native APIs released; same-host direct Luna continuation qualified; physical second host open.** Authorized saved trees/files/ranges, bounded ephemeral lifetime and verified local adoption are covered by the [contract](../contracts/checkpoint-resources-contract.md) and [qualification](../reviews/2026-11-11-checkpoint-resources.md). [Direct Luna receipt](../reviews/2026-11-12-checkpoint-luna-continuation.md) proves meaningful saved-input continuation after sender removal on one host. Both earlier Free Router strict-output failures remain failed. Remaining: second-host fresh context with source unavailable, broader W03/W05 protected retention/scope profiles and actual caller acceptance. | W23/W24; qualified W03; W05 for scoped saved views; W19 | Workspace access extension, 2026-11-11 |
| W26 | **Native SaveFile/ResolveSave, CLI/MCP/SDK and recovery shipped v0.4.0; model/host qualification partial.** [Recovery](../reviews/2026-11-11-workspace-editor-recovery.md) and [CLI/MCP/SDK](../reviews/2026-11-12-workspace-editing-mcp.md) cover request identity, owner reuse, lost reply/reopen and barriers. [Direct Luna editing](../reviews/2026-11-12-workspace-editing-luna-direct.md) and its [repeat receipt](../assessment-evidence/2026-11-12/workspace-editing-luna-repeat.json) passed on one host. Free Router F.1/F.2/F.4/F.5 remain unqualified for their recorded reasons; F.6 fake-CLI telemetry repair is not live-model acceptance (see [frozen evidence detail](../design/workspace-resource-plan.md)). Four-platform native CI does not close physical second-host, protected-policy or deployment gates. Honor `host_quiesced`, honest unknown outcomes and refusal of unsupported policy modes; no OS sandbox claim. | W02 workspace capability, W03, W04, W23/W24 | Workspace access extension, 2026-11-11 |
| W27 | **Conditional:** a named shell-only consumer and repeat-use evidence must justify filesystem attachment. Compare local directories, checkpoint materialization, existing S3 adapters and an MCP-backed mount first. If justified, qualify one OS/read-only Stow checkpoint profile, lifecycle/cache/range/name behavior and real CLI tools. No arbitrary MCP or writable POSIX compatibility claim. | W25; W16/W22 evidence; consumer gate | Workspace access extension, 2026-11-11 |
| W28 | **Conditional:** a named caller must need executable discovery beyond ordinary README/scripts. Evaluate a small versioned descriptor and one caller-owned adapter, bound to verified saved bytes. Prove no execution from read/list/import and caller-owned runtime, sandbox and effects. No Stow runner or plugin registry. | W25; named caller gate; independent of W27 | Workspace access extension, 2026-11-11 |

### Next milestone: supported continuation

**Outcome:** qualify a supported native storage continuation workflow using the
published surface. W06.A is implemented; the next open slice is W04.A, with
remaining W06.B/C work independently dispatchable. Success adds precise access/transfer/consumer evidence without
adding execution, a filesystem mount or new UI. This is the next dispatch order,
the 2026-11-14 W06 implementation does not imply consumer or second-host runs.

1. **W06.A — SDK download across retained-server restart (implemented).**
   The [implementation and qualification record](https://github.com/chester-hill-solutions/stow-s3/pull/140)
   closes the code gap; preserve the acceptance below as regression requirements,
   rather than dispatching it again. The tests extend conformance beside `conformance/resume_upload_test.go`, reusing its
   `resumeHost`/`resumeSource`/`resumeWire` helpers and the existing `Downloader`.
   Seed through the ordinary AWS SDK; persist one verified bounded range; stop and
   reopen the same fs/runtimefs/workspace profile; recreate client/downloader and
   complete from that offset. Accept only exact final bytes/digest, no prefix resend,
   and correct If-Match, 206 and Content-Range behavior. Keep memory restart loss
   explicit. No new transfer journal/SDK wrapper; change product code only for a
   reproduced defect. Does not depend on completing protected deployment.
2. **W04.A — native host policy-Source qualification (independent of W06.A).**
   Bound the profile first: existing native runtime with a host-issued identity and
   host-supplied Source, with a stated maximum-stale policy and cheap/nonblocking
   lookup. Trace that constructor and background callbacks through existing gates.
   Reuse `TestEveryOperationReachesThePolicy`,
   `TestTheSourceIsConsultedPerDecisionAndNotOnceAtOpen` and the outbox revocation
   tests. Verify changed revision, expiry, damaged/unavailable source, attenuation
   and deny/outage distinction at the named caller. Add only uncovered regressions.
   No universal multi-tenant endpoint or protected editor claim follows. **W04.B**
   is conditional: only fix a concrete reachable bypass reproduced by this audit.
3. **W06.B/C — remaining refusal cases and measured report after W06.A.**
   Remote overwrite/If-Match conflict is covered on the filesystem profile in
   PR #140. Extend the same fixture for corrupt/truncated saved prefix and
   interrupted body where not covered; broaden overwrite profiles only for a named gap. Preserve the prefix; never mix object
   versions. Record exact profiles, bytes, parts, limits and commands. Signed-link
   expiry requires a separately named signed-link profile, not endpoint credentials.
   Upload conformance already passes; portable multipart/private state stays W12.
4. **W19/W21, closing W25/W26 evidence where applicable — published-binary consumer.**
   Reuse Aboiteau's native controller or another available named host, synthetic data
   and a pinned caller/model configuration. First reconcile/reuse existing native
   amd64 receipts; run only missing scenarios: meaningful interrupted turn, confirmed
   save/cleanup, fresh receiver with sender state unavailable, then a genuinely
   separate physical host. Record binary digest, host/caller/model versions, source
   revision, effects and exact success/failure. Native Linux CI is already release
   evidence. If no second host is available, leave that gate open; another local
   container is not a substitute. Provider/cloud/model mutations use applicable
   existing authorization; this planning task performs none.
5. **W22 — independent repeat use after a usable qualified workflow.** Two teams,
   three genuine save/reopen/review events each over two weeks, with setup/recovery
   effort, correctness and retained use compared to their actual alternatives.
   Outreach requires explicit authorization; no outreach is scheduled by this edit.
   The result determines deepen/narrow/rework and whether W27/W28 deserve a proposal.

**Ownership and verification:** parent owns contracts, profile/compatibility choices,
acceptance and integrated status. Future bounded implementation/test tasks go to
`gpt-6-luna` at `medium` effort, one owning subsystem/file set per task, with a separate review
of behavior and claims. W06.A/B share fixtures and serialize edits; W04 can proceed
independently. Before dispatch, each task names exact owned files, test commands,
profile, done-when and stop condition; an unknown host/caller is a missing evidence
input, not a reason to invent infrastructure. Update this queue with resulting
revision-bound receipts. Planning review is not an implementation receipt.

**Initial dispatch bindings:** W06.A owns new
`conformance/resume_download_test.go` and only required instrumentation in
`conformance/resume_harness_test.go`; W06.B follows under that owner. Existing
`examples/resumable-transfer/download.go` is a reuse boundary, changed only after a
failing reproducer. Run `go test ./conformance -run TestResume -count=1` and
`go test ./examples/resumable-transfer -count=1`; integrated acceptance adds
`go test ./conformance/... -count=1` and affected races. W04.A is initially a
read-only native Source wiring audit of `internal/runtime/types.go`,
`internal/runtime/policy_source_test.go`, `internal/runthrough/policy_gate_test.go`
and `pkg/stow` authorized capture callers; lock the actual host constructor before
adding a regression there. Run
`go test ./internal/runtime ./internal/runthrough ./pkg/stow -run Policy -count=1`;
include any newly named constructor/source tests explicitly rather than relying on
a name filter. W04.B requires a demonstrated missing call before file ownership is
assigned. For W06.A, these commands are now regression/CI gates; PR #140 records results.
W04 commands remain future acceptance, not qualification supplied by the W06 tests.

**Later dependency order:** W03/W04 qualification gates stronger W05 retention/scope;
W07/W08 lead into W09–W11, and owned recovery sections W12 precede W13 activation.
Data-only W14 need not wait for every notification section; W15 follows W14.
Workers W17 and protected three-target W18 remain separately qualified profiles.
W23–W26 maintenance uses W16/W19/W20/W22 rather than a second extension backlog.
W27/W28 stay conditional on their named consumer gates even after a successful release.

### Aboiteau native consumer intake — 2026-11-11; rechecks — 2026-11-12

The intake captured the earlier `cf-opencode` generic filesystem-store path as a
historical gap. B2 later integrated native Workspace prepare/identity/readiness,
and the consumer plan records B2 native-amd64 CI and C2 R2 recovery as passed.
The current controller source matched the cached image used for two local
rechecks. See the [consumer requirements and outcome](../design/aboiteau-native-consumer.md),
[first review](../reviews/2026-11-12-aboiteau-consumer-recheck.md) and
[retry review](../reviews/2026-11-12-aboiteau-consumer-retry.md).

Both QEMU full-smoke attempts on the local aarch64 Docker daemon failed before
full completion. The first stopped at its final container-state observation;
the authorized retry stopped before the lifecycle stream, with a main-container
OOM event recorded. Neither result establishes a specific consumer bug. After
the authorized Docker Desktop restart, the earlier and retry-owned containers
were confirmed absent. The 32 host contract tests passed separately. These
attempts do not replace prior native-amd64 CI or C2 evidence, and do not close
the local consumer container-smoke, second-host/protected ACL or W22 repeat-use
gates. Public native W26 editing was subsequently released in v0.4.0; this old
consumer failure does not reopen that release gate. No consumer source, cloud resource or model changed or
ran. Stow owns storage; the consumer owns execution, authorization and external
persistence.

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
collaborative editing and AWS ACL/IAM/SSE APIs are not scheduled. Further streaming/index/deduplication optimization,
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
   active references. The [previous queue](../history/plan-before-foundation-consolidation-2026-09-29.md)
   is frozen history, with every old item accounted for above.
7. For this documentation reconciliation, run `git diff --check`,
   `make check-doc-links` and `make check-adr-index`; inspect generated changes and
   retain only intentional source docs. No runtime/API behavior changed, so a fresh
   native/model/provider suite is not claimed. Backout is a documentation revert;
   historical plans/anchors and receipts remain available.
8. Treat publication, deployment, provider mutations and consumer integrations as concrete
   later work with their applicable authorization/evidence. This reconciliation performs
   none of them and does not reopen settled interview choices.
