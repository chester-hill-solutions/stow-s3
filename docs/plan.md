# Stow storage product plan

**Canonical scope, status and ordering:** 2026-09-29, reconciled against `5485a36` and the recorded assessment. This is the single current implementation work order.

**Product:** portable working storage for agents, tools, applications and tests. Stow owns data preparation, supported storage access, checkpoints, inspection, transfer and storage lifecycle. Callers own execution, agent turns and sandbox enforcement. [ADR 0014](adr/0014-storage-product-caller-owned-execution.md) records the decision.

## Plan relationships

| Document | Role |
| --- | --- |
| This plan | Governs implementation scope and ordering |
| [Storage milestone](portable-agent-workspace-goal.md) | Defines the complete workflow and adoption decision |
| [MCP storage integration](mcp-storage-integration-plan.md) | Concrete S3 integration surface and turn-checkpoint guidance; caller retains execution ownership |
| [Pre-code discovery](pre-code-discovery-2026-09-29.md) | Source findings, proposed decisions and dependency-ordered implementation slices for the IDs below |
| [Storage portability design](storage-portability-design.md) | S0-7/S1 contracts for facade lifetime, complete object snapshots, Git profiles and registry policy |
| [Planning index and disposition](planning-index.md) | Complete document inventory, old-item migration map and deferred decisions |
| Older DX, remediation, FOSS, workspace, portability and architecture plans | Superseded historical records; their remaining work is carried into the IDs below |
| [Work-session proposal](work-session-plan.md), [isolate exploration](agent-isolate-exploration.md) | Retired proposals; execution designs require a new scope decision |
| [Assessment](product-assessment-2026-09-29.md), [continuity pilot](agent-continuity-pilot-2026-09-29.md) | Revision-specific observations, including failures and limitations |
| [Prior plan](history/plan-through-2026-09-28.md) | Historical implementation record; superseded ordering |

## Goal and boundary

Deliver one installable storage workflow: prepare files and selected object inputs, use the supported directory/S3 interfaces, checkpoint, inspect changes, move the saved state and reopen it. Validate it through both an S3 fixture use case and one thin integration with an existing agent runner.

The integration may request a checkpoint after each completed turn. The caller detects the boundary and quiesces its writers; Stow captures and verifies state. Task briefs and progress can start as ordinary declared files. A checkpoint is saved data, not a Stow-owned execution attempt or proof of task success.

Existing scoped S3 server sessions remain supported. No Stow agent executor, mandatory attempt framework, model/provider API, sandbox backend or hosted scheduler is required for this milestone.

### How to use this plan

The numbered work items below are the only active engineering queue. The milestone
defines success and the MCP plan expands S3; neither establishes a competing order.
Accepted [ADRs](adr/README.md), [S3 compatibility](compat-contract.md),
[workspace](workspace-contract.md) and [preparation](task-manifest.md) contracts
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

## Current status

**Development candidate: 0.3.0, implemented locally on 2026-09-29.** The baseline
assessment and pre-code discovery below are historical inputs. Current commands
and contracts are in [portable workspace usage](portable-workspace-usage.md);
[implementation evidence](implementation-2026-09-29.md) records validation and
remaining gates. This candidate has not been published.

| Area | Implemented in this tree | Remaining acceptance |
| --- | --- | --- |
| S0 storage correctness | Relative handoff paths; CRC64NVME single-object uploads; metadata normalization; shared capture/cleanup locks; durable publication/reconciliation; request limits and upstream transport policy | Full local validation recorded below; second-host runtime and complete provider evidence remain distinct gates |
| S1 coherent storage | Same-runtime facade and usage refresh; portable v2 files/logical objects/buckets/metadata; Git provenance/local-source reconstruction; standing registry policy and deletion | Large-workspace cost, transfer/RSS/concurrency characterization and second-host runtime evidence |
| S2 distribution | Version 0.3.0, public npmjs target, exact-artifact publication retries, preflight/consumer ordering, separate AWS/R2/custom receipts | Publisher/account setup, actual publication, published anonymous installs, Linux runtime and fresh live-provider evidence |
| S3 MCP and OpenCode | Scoped stdio adapter; persisted request keys/resolve; bounded retry/fail-closed caller for pinned OpenCode 2.0.16 | Successful real-model turn, interruption/cleanup in real execution, cross-host continuation and OpenCode-to-MCP runtime configuration |
| S4 adoption | Deterministic native lost-reply and moved-bundle continuation test | Meaningful real-agent task and two independent teams' repeat use |

Final local validation: **`make test-all` and `make standards` passed**, including
race tests, all four local conformance profiles, language/WASM/caller tests and
reproducible generated artifacts. Live-provider credentials were not supplied.
No packages were published. See [the implementation record](implementation-2026-09-29.md).

### What is next

1. Review the locally verified candidate and measured workload limits.
2. Run the documented OpenCode caller with a real model on a small prepared
   workspace. Recover an interrupted turn, move an unedited bundle to Linux and
   continue from saved notes with a fresh agent.
3. Complete the three disposable live-provider profiles and release/account gates,
   then publish and verify exact-version anonymous consumers.
4. Run the S4 study before expanding into execution or sandbox infrastructure.

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
| S1-4 | Add versioned standing registry policy outside workspace-entry enumeration. Coordinate prepare/capture/import/delta admission and deletion, count only committed state, refuse corrupt accounting and protect active/reconciling data. Specify precedence/defaults and safe explicit checkpoint deletion. No automatic eviction; collection stays caller-scheduled. |
| S1-5 | Measure repeated full captures, transfer cost and maximum supported object size under realistic concurrency. Publish memory/disk limits and object-record overhead; storage quotas do not imply process memory ceilings. Reuse existing baselines/soak evidence and extend only for uncovered workloads. |

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

Follow the [MCP/OpenCode integration plan](mcp-storage-integration-plan.md).
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

| ID | Work and acceptance |
| --- | --- |
| S4-1 | Use both a meaningful S3 fixture application and OpenCode recovery workflow. Include Git plus fixtures, non-Git data, interrupted work and transfer. Separate missing state from model/task failure. |
| S4-2 | Run the milestone's two-independent-team repeat-use study; record setup interventions, recovery effort, cost and retained use against actual alternatives. Refresh market research before an investment decision; old landscape snapshots are evidence of their dates only. |
| S4-3 | Decide deepen/narrow/rework from that evidence. Reconsider deferred work through the [disposition register](planning-index.md), with a named consumer need and explicit scope decision for execution or hosted services. |

**Exit:** evidence beyond internal smoke tests determines expansion. Finishing S0–S3 does not automatically reactivate the deferred execution plan.

## Engineering reuse and API constraints

Extend `pkg/stow`, `internal/storage/workspace`, `internal/runtime`, `internal/runthrough` and existing CLI/wrappers. `CheckpointOf` already captures an externally held workspace and shares the publication path with `Workspace.CreateCheckpoint`. A turn integration does not require a second registry or scheduler.

Current checkpoint/transport formats are versioned, but their readers differ: handoff documents reject unknown fields while checkpoint manifests can ignore them and lose them on a typed round trip. Optional context is not an existing public checkpoint field. Prefer ordinary captured files for the first recipe; any schema extension needs explicit old-reader behavior and round-trip coverage. Storage credentials and live-write consent remain separate from handoff data.

Implementation and evidence now live in this development tree. Remaining acceptance above stays open until its specific evidence exists. The [plan alignment review](storage-plan-alignment-2026-09-29.md) preserves the earlier engineering handover.
