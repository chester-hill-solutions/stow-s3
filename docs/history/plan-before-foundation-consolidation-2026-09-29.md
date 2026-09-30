# Stow storage product plan

> **Frozen before the 2026-09-29 foundation consolidation.** This body records earlier status and ordering; it is not an active queue. Use the [consolidated plan](../storage-foundation-plan.md) and [current disposition map](../planning-index.md).


**Canonical scope, status and ordering:** 2026-09-29, reconciled against `5485a36` and the recorded assessment. This is the single current implementation work order.

**Product:** portable working storage for agents, tools, applications and tests. Stow owns data preparation, supported storage access, checkpoints, inspection, transfer and storage lifecycle. Callers own execution, agent turns and sandbox enforcement. [ADR 0014](../adr/0014-storage-product-caller-owned-execution.md) records the decision.

## Plan relationships

| Document | Role |
| --- | --- |
| This plan | Governs implementation scope and ordering |
| [Storage milestone](../portable-agent-workspace-goal.md) | Defines the complete workflow and adoption decision |
| [MCP storage integration](../mcp-storage-integration-plan.md) | Concrete S3 integration surface and turn-checkpoint guidance; caller retains execution ownership |
| [Resumable transfer detail](../resumable-transfer-plan.md) | Proposed S0-4/S1-5/S4-1 validation of multipart upload and range download continuation, followed by consumer integration; code changes follow reproduced gaps |
| [Portable recovery](../portable-recovery-plan.md) | Accepted data-plus-recovery-state direction; proposed S1-9 contract for cross-host continuation with explicit destination activation |
| [Guarded storage](../guarded-storage-plan.md) | Accepted guarded saves and optional declared-input checks at ready publication; proposed S1-10 extension of existing conditional writes into public Go/CLI/embedded surfaces |
| [Foundation review](../foundation-review-2026-09-29.md) | Dependency order, delegated engineering defaults and qualification gaps for the accepted storage extensions; no separate queue |
| [Workspace notifications](../workspace-notifications-plan.md) | Accepted core capability under ADR 0015; proposed contract and implementation slices for S1-6 |
| [Protected cache](../protected-cache-plan.md) | Accepted ACL/encryption direction under ADR 0016; proposed access/encryption contracts for S1-7/S1-8 and native, Workers and Railway evidence in S4-4 |
| [Pre-code discovery](../pre-code-discovery-2026-09-29.md) | Source findings, proposed decisions and dependency-ordered implementation slices for the IDs below |
| [Storage portability design](../storage-portability-design.md) | S0-7/S1 contracts for facade lifetime, complete object snapshots, Git profiles and registry policy |
| [Planning index and disposition](../planning-index.md) | Complete document inventory, old-item migration map and deferred decisions |
| Older DX, remediation, FOSS, workspace, portability and architecture plans | Superseded historical records; their remaining work is carried into the IDs below |
| [Work-session proposal](../work-session-plan.md), [isolate exploration](../agent-isolate-exploration.md) | Retired proposals; execution designs require a new scope decision |
| [Assessment](../product-assessment-2026-09-29.md), [continuity pilot](../agent-continuity-pilot-2026-09-29.md) | Revision-specific observations, including failures and limitations |
| [Realtime multiplayer experiment](../realtime-multiplayer-plan.md) | Transferred historical RT handover; standalone collaboration repository owns current scope/status |
| [Prior plan](plan-through-2026-09-28.md) | Historical implementation record; superseded ordering |

## Goal and boundary

Deliver one installable storage workflow: prepare files and selected object inputs, use the supported directory/S3 interfaces, checkpoint, inspect changes, move the saved state and reopen it. Validate it through both an S3 fixture use case and one thin integration with an existing agent runner.

The integration may request a checkpoint after each completed turn. The caller detects the boundary and quiesces its writers; Stow captures and verifies state. Task briefs and progress can start as ordinary declared files. A checkpoint is saved data, not a Stow-owned execution attempt or proof of task success.

Existing scoped S3 server sessions remain supported. No Stow agent executor, mandatory attempt framework, model/provider API, sandbox backend or hosted scheduler is required for this milestone.

The user has also directed that the reusable report/subscription alert mechanism move
into core. [ADR 0015](../adr/0015-durable-workspace-notifications.md) adds durable workspace
notifications with automatic file change detection, parent-path subscriptions,
pending/replay, acknowledge, optional webhook delivery and complementary explicit reports.
Consumers choose reactions. Change
detection comes first with reconciled coverage; notification portability requires a
separate verified contract. This feature is planned, not implemented.

The recovery interview further requires explicit ready reports to identify an
immutable saved output version. Pending delivery protects its required data. Optional
change detail may carry a complete text diff if it fits the serialized payload
budget, otherwise a bounded changed-lines report. The default comparison base is the
previous ready version of the same output/scope, with an explicit saved-base override;
both version IDs remain fixed for replay. Delivery acceptance stops transport retries;
separate consumer artifact release ends only that consumer's output-retention hold
after it has secured the data or no longer needs Stow's copy. Comparison work limits
and receipt API/schema details remain proposed; content disclosure respects
both versions' permissions. See the notification detail under S1-6/S1-7/S1-9.
Partial permission revocation pauses the whole queued report; no automatic trimming
or content rewrite under the old identity. Publishers may explicitly create a new
permitted scoped report while preserving the original's retention/discard obligations.
After the user's Protobuf correction and delegation, select one Protobuf notification
schema with native byte-field diffs and optional generated ProtoJSON compatibility
(base64 for byte fields). Use a configurable 64 KiB complete encoded-body default
under host/profile limits. These are planned S1-6 choices, not implemented formats or
performance claims. Existing CLI/MCP/S3 formats retain their own supported contracts.

[ADR 0016](../adr/0016-scoped-access-encrypted-cache.md) accepts resource-scoped ACLs and
authenticated encrypted persistence for protected caching. Requested targets are native
services, Cloudflare Workers and Railway. Extend existing authority/cache paths; host
adapters supply persistence and keys. Plaintext workspaces, legacy development defaults
and current S3 compatibility boundaries remain explicit. APIs and deployment profiles
are proposed, not shipped or qualified.
The interview also selects independent encryption-key lifecycles per trusted storage
namespace: normal rotation preserves retained-data access until safe re-protection;
explicit emergency revocation may block that namespace's recovery. Hosts retain key
custody, and another namespace's keys/work remain unaffected. S1-8 must cover retained
checkpoints, transfer staging, outbox and notification dependencies as well as objects.
Explicit encrypted handoffs may use the recipient's own keys/provider without copying
source storage keys. Extend the existing archive with a qualified authenticated envelope
and protected staging; authorize scope/recipient and preserve destination checks and
confirmed takeover. This is a proposed S1-8/S1-9 capability, not current encrypted export.
Optional backups may be prepared for a separately held recovery key before failure;
the private key stays outside the backup and hosts own custody/scheduling. Qualify
fresh-host recovery with the source provider absent, current destination permissions
and inactive external operations. This extends S1-8/S1-9; no recovery-key API is shipped.

### How to use this plan

The subsequent recovery interview accepted cross-host movement of captured data
plus selected transfer progress and pending storage/notification operations.
Destination authorization and explicit activation are required; bundle adoption
does not start external effects. [Portable recovery](../portable-recovery-plan.md)
records this S1-9 extension. Confirmed takeover is the default: external operations
stay paused until the source relinquishes responsibility or shared ownership prevents
it from continuing. The confirmation mechanism and consistency contract are engineering
qualification gates, including stale-backup fencing. Existing
same-store transfer and same-workspace notification scopes remain the first slices;
cross-host pending-operation recovery is not implemented.
Unfinished operations also protect their required recovery data from automatic
cleanup despite normal expiry. Retain only the necessary recovery set; when capacity
is exhausted, refuse new work with visible blockers rather than deleting it. Dependency
tracking and cleanup receipts are proposed extensions to S1-4/S1-9.
Keep a bounded recovery bookkeeping reserve that new data cannot consume, so admitted
work can record completion, acknowledgements and cleanup when the ordinary budget is
full. Reserve sizing and enforcement need adapter-specific evidence; it does not
provide space for large transfer payloads or prevent direct host disk exhaustion.
Project/customer budgets cover managed files, objects, checkpoints, incomplete
transfers and pending storage/notification records together, including a separate
recovery reserve per trusted namespace. Admission also respects the overall host
limit; namespace ceilings alone do not guarantee future payload capacity.
Explicitly scoped checkpoints/exports may save an authorized portion of a workspace
and declare completeness within that scope. Whole-workspace requests with insufficient
permission refuse rather than silently filter. This is a proposed S1-2/S1-7 extension;
scope-aware restore/diff behavior and dependent recovery need qualification.
Partial recovery is the accepted default for valid artifacts: blocked operations and
their dependents stay inactive while independent operations may activate after their
own checks. Expose permission-safe statuses and blockers; corruption/unsupported required
sections still refuse. This does not introduce a general agent workflow scheduler.

The interview also accepts guarded saves as the default in a new managed read/edit/save
interface under [S1-10](../guarded-storage-plan.md): compare the caller's read identity,
refuse stale saves with an explicit conflict and require deliberate unconditional
replacement. Extend existing conditional-write paths into the public runtime/bridges;
legacy direct calls and S3 defaults remain supported. Ordinary filesystem edits still
need caller-owned coordination; no guarded public save API is implemented by this plan.

Callers may also declare the input versions used for output. A supplied input basis
defaults to checking that those inputs remain current at ready-report admission;
stale/missing/denied/unverifiable inputs block publication and preserve the draft.
Explicit snapshot mode permits intentional historical inputs without claiming current
freshness. Omitting a basis makes no input-freshness claim. S1-10 and S1-6 share the
core identity/publication boundary; caller-owned read tracking and quiescence remain.

S1-10's first prerequisite is now implemented locally: public Go and WASM/TypeScript
embedded options carry existing content-ETag write conditions, advertise conditional
support and refuse unsupported/stale requests. This does not implement guarded defaults,
read-bound identities or input-basis ready publication. See the guarded-storage detail.

The numbered S0–S4 work items below govern the storage engineering queue. The authorized collaboration product has moved to its own repository, referenced below.
The milestone defines storage success and the MCP plan expands S3; neither establishes
a competing storage order.
Accepted [ADRs](../adr/README.md), [S3 compatibility](../compat-contract.md),
[workspace](../workspace-contract.md) and [preparation](../task-manifest.md) contracts
remain normative. Retiring a plan does not retire an accepted contract or an
implemented feature. Proposed contracts must be labelled until implemented.

Complete S0 before relying on the storage workflow; S1 and S2 establish the supported
integration foundation. Release preparation and the OpenCode lifecycle spike may
start early. S3 acceptance depends on S0–S2. S4 gathers repeat-use evidence after the
workflow is usable. An old phase number, checkbox, version target or “next step”
does not override this ordering.

Each implementation change should name its item ID, extend the existing code path,
and attach revision/platform-specific acceptance evidence. An uncertain old finding
is an audit task until reproduced or closed by existing evidence, not a confirmed
defect. Update status here rather than reopening a historical checklist.

### Dependency order for the accepted foundation extensions

The [foundation review](../foundation-review-2026-09-29.md) consolidates the interview's
remaining gaps. Resolve routine engineering choices from existing contracts and
qualification evidence; no further user decision is pending. Broader contracts remain
proposed; implemented extension slices are labelled separately from baseline evidence.

| Order | Existing owners | Concrete boundary to establish |
| --- | --- | --- |
| 1 | S1-7, S1-10 | Trusted namespace/resource identity, shared authorization and guarded native read/save; thin wrappers expose the same contract. |
| 2 | S1-4, S1-9 | Persisted dependency holds, unified budgets and bounded recovery bookkeeping admission; qualify physical reserve claims per adapter. |
| 3 | S1-8 | Bounded encrypted native records and independent namespace keys, including retained/staging paths and crash-safe rotation. |
| 4 | S1-6, S1-10 | Change reconciliation/journal first; add immutable ready output, declared-input checks, Protobuf/ProtoJSON and optional diffs, then signed webhook delivery with separate acceptance/release. |
| 5 | S0-4, S1-5, S1-9 | Validate existing same-store transfers; extend archive sections for owned recovery state, inactive adoption and qualified takeover/reconciliation. |
| 6 | S1-8, S1-9 | Recipient-encrypted handoff and opt-in recovery-key backups using the verified archive, protected staging and current destination policy. |
| 7 | S4-4 | Qualify native/Railway and Workers capabilities separately; run an early Workers execution/persistence spike before promising parity. |

Small slices may overlap after their shared prerequisites exist; this is a dependency
order within the canonical IDs, not a requirement to implement every future capability
before testing a useful native slice. Existing release and real-consumer gates remain.

## Current status

**Development candidate: 0.3.0, implemented locally on 2026-09-29.** The baseline
assessment and pre-code discovery below are historical inputs. Current commands
and contracts are in [portable workspace usage](../portable-workspace-usage.md);
[implementation evidence](../implementation-2026-09-29.md) records validation and
remaining gates. This candidate has not been published.

| Area | Implemented in this tree | Remaining acceptance |
| --- | --- | --- |
| S0 storage correctness | Relative handoff paths; CRC64NVME single-object uploads; metadata normalization; shared capture/cleanup locks; durable publication/reconciliation; request limits and upstream transport policy | Full local validation recorded below; second-host runtime and complete provider evidence remain distinct gates |
| S1 coherent storage | Same-runtime facade and usage refresh; portable v2 files/logical objects/buckets/metadata; Git provenance/local-source reconstruction; standing registry policy and deletion | Large-workspace cost, transfer/RSS/concurrency characterization and second-host runtime evidence |
| S2 distribution | Version 0.3.0, public npmjs target, exact-artifact publication retries, preflight/consumer ordering, separate AWS/R2/custom receipts | Publisher/account setup, actual publication, published anonymous installs, Linux runtime and fresh live-provider evidence |
| S3 MCP and OpenCode | Scoped stdio adapter; persisted request keys/resolve; bounded retry/fail-closed caller for pinned OpenCode 2.0.16; local real-model continuation from an adopted bundle | Interruption/cleanup in real execution, second-host continuation and OpenCode-to-MCP runtime configuration |
| S4 adoption | Deterministic native lost-reply and moved-bundle continuation test | Meaningful real-agent task and two independent teams' repeat use |

Receiving-host follow-up (S3): the reported macOS adoption/integrity pass exposed
a caller using the root instead of the recorded cwd and a successful agent no-op.
The caller now uses readiness `working_directory`, records task-file changes and
blocks successful no-ops for review, forwards the named Zen credential, and has an
opt-in real-model acceptance test. The authorized follow-up passed with
`opencode/space-bunny-free`: verified task output, changed notes and restored
checkpoint artifacts after sender data was removed. [The remediation record](../opencode-receiver-remediation-2026-09-29.md)
separates that local model pass from the still-required second-host/Linux and
real interruption gates.

The user subsequently authorized a separate product exploration. The first
[throwaway multiplayer caller](../../examples/multiplayer-prototype/README.md) demonstrated
concurrent OpenCode participants, scoped output ownership, file subscriptions and
portable reports. Its [notes](../../examples/multiplayer-prototype/NOTES.md) are dated
evidence. The user then rejected file claims as the product model and specified
simultaneous shared-file editing, presence/last cursor as the first join message,
live steer/queue/interrupt controls and low room latency. These refinements are planned
in the [realtime experiment](../realtime-multiplayer-plan.md); they are not implemented
by the first demo. Collaboration remains above Stow's storage interface under ADR 0014.

Final local validation: **`make test-all` and `make standards` passed**, including
race tests, all four local conformance profiles, language/WASM/caller tests and
reproducible generated artifacts. Live-provider credentials were not supplied.
No packages were published. See [the implementation record](../implementation-2026-09-29.md).

### What is next

1. Review the locally verified candidate and measured workload limits.
2. Extend the verified local real-model continuation with an interrupted turn.
   Recover that turn, move an unedited bundle to Linux and
   continue from saved notes with a fresh agent.
3. Complete the three disposable live-provider profiles and release/account gates,
   then publish and verify exact-version anonymous consumers.
4. Run the S4 study before promoting execution or sandbox infrastructure into the
   supported storage product. The explicitly authorized caller experiment below is separate.

### Collaboration product transferred to its own repository

The explicitly authorized RT exploration now lives in the standalone local
[Agent Collaboration repository](../../../agent-collaboration/README.md). Its
[plan](../../../agent-collaboration/docs/plan.md) owns RT scope/order/status; the
[capability record](../../../agent-collaboration/docs/CAPABILITIES.md) separates
deterministic foundation, native diagnostic outcomes and outstanding realtime gates.

Stow retains the historical multiplayer prototype and dated review/handover records.
New agent/harness observation, presence, decisions, controls and shared-editor work belongs
to the collaboration repository. It consumes Stow only through an optional public
API/CLI storage adapter. The extraction changed no storage schema, executor API or
release decision. The subsequent ADR 0015 adds core workspace content observation and
durable notifications under S1-6; agent read tracking and reactions remain in consumers.
No remote repository/publication was created.

Full-copy checkpoints remain the initial implementation. Profiling removed repeated
directory enumeration, and bounded copy/flush workers preserve durability barriers.
Five captures measured 129 ms median for 256 files / 8 MiB and 1.74 seconds for
4,096 files / 64 MiB after tests stopped. The larger fixture also had an 11.10-second
first capture. Retention still costs roughly one payload copy per save. These
results support measured workload profiles, not universal every-turn latency claims.

## Ordered work

### S0. Close observed storage contract defects

Close reproduced failures and inherited contract gaps using existing implementations.
Required provider checks may be skipped during local development, but unavailable
evidence does not satisfy the release gate.

| ID | Work and acceptance |
| --- | --- |
| S0-1 | Fix relocated archive references, CRC64NVME default-client uploads and metadata casing. Exercise real clients and moved bundles with original sender paths unavailable. |
| S0-2 | Verify explicit upstream bucket scope after local bucket creation, separate live-write consent, conditional conflicts, committed-local-write errors and durable outbox retry/restart. Extend the existing real-pair/runtime tests; do not reopen resolved composition defects. |
| S0-3 | Audit supported path, symlink, replacement-race, adoption/ownership and concurrent-capture guarantees on supported hosts. Reproduce gaps before fixing them; document unenforced arbitrary filesystem writes and preserve refusal of unsafe/corrupt input. |
| S0-4 | Close compatibility traceability gaps in the existing shared corpus: presigned flows/expiry, virtual-hosted routing, bucket/batch operations, multipart variants, negative listing tokens and conditional-copy combinations. Reuse raw-HTTP tests for protocol edges and both pinned SDK runners; map existing coverage before adding cases. |
| S0-5 | Resolve inherited resource/transport findings: effective configurable request-body limits through CLI/readiness/wrappers; multipart staging and concurrent request bounds; explicit upstream HTTPS/insecure-endpoint policy with deliberate local test support. Record compatibility decisions before changing defaults; verify refusal and cleanup at actual enforcement points. |
| S0-6 | Restore `make standards` without increasing debt allowances. Preserve generated-artifact, command-surface, ADR, coverage and quality checks. Retain residual `serve` decomposition and evidence-driven coverage improvements as maintenance work; do not duplicate subprocess contract tests solely to increase a percentage. |
| S0-7 | Establish one capture/publication/cleanup contract: shared cross-process locking for handle/external paths; fail-closed unexpected lock errors; validated read-only lookup; cancellable scans/copies; durable publication with explicit commit/uncertain outcomes. Reproduce mixed capture/retention and collect/destroy races, then fix shared paths before promising reliable retries. |

**Exit:** supported examples need no observed workaround, changed/corrupt inputs are handled predictably, and documented storage/authority promises have positive and adverse-case evidence.

### S1. Complete one coherent working-storage workflow

Complete the prepare → access → capture → inspect → transfer → reopen workflow.
Extend the existing runtime, registry and transports; keep one shared storage implementation.

| ID | Work and acceptance |
| --- | --- |
| S1-1 | Expose an optional loopback facade by borrowing the existing runtime adapter. Drain HTTP before closing runtime/releasing liveness; synchronize lifecycle calls. Reconcile seeded/host-written usage before quota-sensitive mutations. Verify same bytes/accounting and preserve scoped session APIs. |
| S1-2 | Implement bounded portable checkpoint v2 with logical object/bucket inventory and payloads, including escaped keys/secondary buckets excluded today. Preserve selected metadata, modes and deletions with metadata-aware capture and diff; read v1 as file-only, refuse unsupported enhanced deltas until a compatible codec exists. Follow the storage design's versioned archive/restore contract. |
| S1-3 | Persist relative cwd and pinned Git provenance. Offer file continuation and explicit recipient-local-source Git reconstruction, preserving captured deletions; self-contained base bundles remain a focused conditional spike. Keep local parent lineage separate from adopted origin provenance; verify source immutability and destination-local identity/credentials. |
| S1-4 | Add versioned standing registry policy outside workspace-entry enumeration. Coordinate prepare/capture/import/delta admission and deletion, count only committed state, refuse corrupt accounting and protect active/reconciling data. Extend lifecycle admission/cleanup with persisted pending-operation dependencies under S1-9: required recovery data survives expiry until completion or explicit discard, with visible capacity blockers. Add unified project/customer budgets over managed data/checkpoints/transfers/backlogs, bound to trusted namespaces under an overall host limit. Reserve bounded recovery bookkeeping capacity per namespace that new data and other namespaces cannot consume; prove full-budget completion/ack/cleanup, concurrent admission and restart accounting per persistence profile. Specify precedence/defaults, permission-safe diagnostics and safe explicit checkpoint deletion. No automatic checkpoint eviction; collection stays caller-scheduled. |
| S1-5 | Measure repeated full captures, transfer cost and maximum supported object size under realistic concurrency. Publish memory/disk limits and object-record overhead; storage quotas do not imply process memory ceilings. Reuse existing baselines/soak evidence and extend only for uncovered workloads. |
| S1-6 | Implement automatic workspace file change detection and durable notifications under ADR 0015 and the notification detail. Start with declared-path create/modify/delete observation, watcher hints plus bounded startup/periodic content reconciliation, durable baseline/event commit, parent-path subscriptions, pending/replay and ack over one Go implementation and public CLI/thin wrappers. Add optional signed webhooks over the same journal with host-local endpoint/secret configuration, bounded retries and retained failures. Prove external/facade edits, missed hints, coverage, restart, concurrency, uncertain outcomes, overlapping-subscription deduplication, receiver acceptance/retry crashes and limits. Explicit readiness reports use the same journal; consumers own read tracking and effects. Portable notification history is a separate follow-up. |
| S1-7 | Add resource-scoped ACLs under ADR 0016 using trusted principals/namespaces and authority attenuation. Enforce local/cache hits, origin fetch, listing, copy, multipart, capture/export and notifications/background retries. Extend S1-2 with explicitly scoped checkpoints/exports: declare selected-scope completeness, refuse unauthorized full/requested scope, check destination grants and preserve resources outside scoped restore/delta boundaries. Define policy version/expiry/revocation and offline freshness. Prove denied warm-cache reads, namespace isolation and scoped artifact behavior through shared policy/capture tests; preserve legacy development behavior and separate live-write consent. |
| S1-8 | Add opt-in authenticated encrypted object/cache persistence under ADR 0016 with a versioned envelope, host key provider, resource binding, explicit plaintext leakage and rotation/recovery. Prove protected persisted paths, tamper/wrong-key refusal, range/validator preservation, restart and limits. Ordinary workspaces remain plaintext; encrypted bundles and host adapters need explicit qualified contracts. |
| S1-9 | Specify and implement portable recovery of selected transfer progress and pending storage/notification operations alongside captured data. Extend existing versioned archives; preserve operation identity/provenance, reconcile uncertain effects and require destination authorization plus explicit activation. Require confirmed takeover for external operations by default; source unreachability alone leaves them paused. Support partial recovery of valid artifacts: block affected operations/dependents, expose permitted statuses and allow independent activation after its own checks. Specify confirmation/source-restart and dependency contracts before implementation; prove adoption stays inactive, blocker re-evaluation preserves completed work, duplicate imports/stale evidence are handled and corrupt/unsupported required sections are refused. Depends on the owning transfer, journal and scoped-access contracts; see the portable recovery plan. |
| S1-10 | **In progress:** public Go and WASM/TypeScript conditional-write forwarding, typed failures and explicit support are implemented locally. Remaining: new managed read/edit/save API with guarded defaults, scoped read/comparison identity and expected absence, explicit replacement and lost-reply outcomes. Add optional caller-declared input-basis checks at ready-report admission with S1-6: supplied bases default to current-input checks; explicit snapshot mode and omitted-basis semantics remain clear. Preserve drafts on refused publication. Extend CLI/thin wrappers as appropriate; preserve S3/legacy defaults. Qualify input changes/concurrency, authority/quota interactions and supported process/host-edit profiles. See the guarded-storage detail; no automatic merge or caller execution enters core. |

**Exit:** a documented directory/S3 fixture can be used, saved, moved and reopened with its declared semantics. The recipient does not need sender paths or upstream credentials for captured inputs.

### S2. Deliver through supported install surfaces

Start release preparation alongside S0. Publish only after the applicable
correctness/release checks pass, and verify actual published consumers afterward.

| ID | Work and acceptance |
| --- | --- |
| S2-1 | Choose a new release version and align binary, Go module, main/platform npm packages and Python wheels. Preserve existing tags including incomplete `v0.2.0`. Plan npmjs for anonymous main/platform installs; GitHub package visibility alone cannot meet this goal. Complete publisher/account setup and exact-version artifact validation. |
| S2-2 | Prove published clean installs on macOS arm64 and Linux x64, without repository binary overrides or private registry configuration. Exercise prepare/use/checkpoint/reopen/transfer/cleanup, diagnostics and generated artifacts. Preserve other existing targets while stating their evidence level separately. |
| S2-3 | Restructure the carried-forward release gate: preflight → artifact checks → publication → exact-version anonymous consumers → completion. Distinguish lookup failures from absent packages and verify identity on partial retries. Collect separate revision-bound AWS/R2/custom evidence; one configured endpoint cannot certify all three. |
| S2-4 | Make install/quickstart guidance consistent across README, packages, site, skill and machine-readable pages. Verify the chosen public documentation URL or replace broken guidance. Surface dev-scale storage/memory costs, supported platforms and the execution boundary. |
| S2-5 | Resolve legacy object-record upgrade behavior: preserve the accepted clean-break boundary, make detection actionable in diagnostics and release notes, and refuse silent data reuse. Migration requires a separate format decision. Keep implemented contribution/security/templates/dependency-update surfaces current. |

#### Release acceptance carried forward from remediation R11

- Record the supported contract/profile and scenario traceability, pinned Go/Node SDK
  versions, and any explicit unsupported behavior. No required scenario silently skips.
- Pass Go/unit/race, aggregate local conformance, supported language build/tests,
  raw protocol/auth edges, run-through/outbox checks, `make standards`, and generated
  artifact reproducibility at the release revision.
- Retain isolated disposable live-provider evidence for AWS S3, Cloudflare R2 and
  the declared custom-provider profile. The current workflow's successful configured
  profile is not evidence for every provider; record each result/credential form and
  verify cleanup. Use isolated test credentials, never application credentials, and
  require short-lived/session credentials. Any provider-specific exception needs an
  explicit recorded decision and bounded disposable scope. Expand beyond the single mirror round trip to required upstream
  scenarios, including conflict, retry/restart and multipart. Missing required
  credentials block that claim/gate rather than yielding a fabricated pass.
- Check secret-safe logs/status/metrics, local credential separation, shutdown and
  artifact contents, checksums, version/tag identity and supported install pilots.
- Verify artifacts before publication and perform clean-consumer checks against the
  published artifacts before declaring release completion. Keep independent repeat-use
  evidence in S4 visible separately; a package release is not proof of product fit.

This checklist replaces the old R0–R11 execution order and obsolete version targets;
it preserves the release-quality obligations. Any change to required provider/profile
coverage needs an explicit contract decision, not a silent skip in a historical plan.

**Exit:** a newcomer can install and complete the storage workflow without local binary overrides or unpublished source assumptions.

### S3. Deliver a thin MCP storage adapter and caller-owned agent integration

Follow the [MCP/OpenCode integration plan](../mcp-storage-integration-plan.md).
OpenCode runs in a separate prepared workspace; callers own execution and writer
quiescence. Preserve files, required object metadata and explicitly saved context.
Use bounded checkpoint retries with backoff, then explicit failure; reconcile
ambiguous publication before retrying. Agent-requested saves remain best effort.

| ID | Work and acceptance |
| --- | --- |
| S3-1 | Runtime-verify the caller-controlled profile against pinned OpenCode v2.0.16: exclusive prompt admission, explicit settled wait, terminal outcome and known-writer quiescence. Hold a caller-owned workspace lifetime; ordinary cwd usage is not a liveness claim. Start with three bounded capture attempts and a required caller deadline. |
| S3-2 | Add shared capture request identity/resolve with atomically co-published local receipts; select/pin a compatible MCP SDK/toolchain. Expose bounded scoped stdio tools and guidance over those APIs plus typed shared handoff operations. Verify replay, partial outcomes, negotiated protocol and error semantics; do not auto-retry handoff mutations initially. |
| S3-3 | Demonstrate separate prepared workspace → completed turn → saved context → confirmed checkpoint → next prompt. Test interruption, read-only turns, concurrent writers, retry exhaustion and restart; use transferred state with fresh context on the second supported host. Full chat restoration is deferred. |

**Exit:** a real MCP client uses the existing storage semantics and portable state without document edits or missing-input reconstruction. Verified caller-controlled admission, settlement and save barriers support defined every-turn saves; prompt-only agent saves are labelled best effort. The result demonstrates an integration, not an implemented Stow runner or sandbox.

### S4. Validate repeat use and choose the next investment

Compare the completed workflow against users' actual tools and let retained use
determine the next investment.

The [resumable transfer plan](../resumable-transfer-plan.md) expands S0-4/S1-5/S4-1
with interrupted multipart upload and range download exercises against retained
storage, then a small cococlips consumer pilot. Server-side operations already
exist; client progress/retry behavior needs explicit evidence. This follows the
user's clarification of upload/download resumability and replaces the separately
proposed task-context follow-up. Existing OpenCode acceptance and release gates
remain in force; no transfer-resume pass is claimed by the planning change.

| ID | Work and acceptance |
| --- | --- |
| S4-1 | Use both a meaningful S3 fixture application and OpenCode recovery workflow. Include Git plus fixtures, non-Git data, interrupted work and transfer. Separate missing state from model/task failure. |
| S4-2 | Run the milestone's two-independent-team repeat-use study; record setup interventions, recovery effort, cost and retained use against actual alternatives. Refresh market research before an investment decision; old landscape snapshots are evidence of their dates only. |
| S4-3 | Decide deepen/narrow/rework from that evidence. Reconsider deferred work through the [disposition register](../planning-index.md), with a named consumer need and explicit scope decision for execution or hosted services. |
| S4-4 | Qualify the protected-cache profile requested for native services, Railway and Cloudflare Workers under ADR 0016. Native/Railway reuse the service with encrypted persistence and a single-instance volume profile; Workers first proves shared-core execution and minimal platform persistence. Record scoped warm-cache denial, origin outage, policy/key expiry/rotation, restart/redeploy, size/memory limits and supported operations per target. No production-cache or deployment pass is implied. |

**Exit:** evidence beyond internal smoke tests determines expansion. Finishing S0–S3 does not automatically reactivate the deferred execution plan.

## Engineering reuse and API constraints

Extend `pkg/stow`, `internal/storage/workspace`, `internal/runtime`, `internal/runthrough` and existing CLI/wrappers. `CheckpointOf` already captures an externally held workspace and shares the publication path with `Workspace.CreateCheckpoint`. A turn integration does not require a second registry or scheduler.

Current checkpoint/transport formats are versioned, but their readers differ: handoff documents reject unknown fields while checkpoint manifests can ignore them and lose them on a typed round trip. Optional context is not an existing public checkpoint field. Prefer ordinary captured files for the first recipe; any schema extension needs explicit old-reader behavior and round-trip coverage. Storage credentials and live-write consent remain separate from handoff data.

Implementation and evidence now live in this development tree. Remaining acceptance above stays open until its specific evidence exists. The [plan alignment review](../storage-plan-alignment-2026-09-29.md) preserves the earlier engineering handover.
