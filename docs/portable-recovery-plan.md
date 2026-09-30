# Portable recovery plan

> **Consolidated on 2026-09-29; reference only.** The [new working-storage plan](storage-foundation-plan.md) is the sole work order. Holds/budgets are W03, portable state/takeover W12/W13 and encrypted recovery W14/W15. Accepted directions are consolidated; the retained body is a design reference. Former checklists, phase numbers and next steps below do not schedule work.

**Status:** accepted product direction, 2026-09-29; contract and implementation
proposed. Expands S1-9 in the [canonical plan](plan.md). This covers working
storage across workspaces, objects, transfers and notifications, including
upstream propagation. It is not limited to cache recovery or agent execution.

## Agreed recovery promise

Moving saved work to another machine should carry both its captured data and
selected recovery state: transfer progress and pending storage/delivery operations.
The destination verifies the received state, checks current permissions and
explicitly activates pending work. Importing or opening a bundle does not start
uploads, upstream writes or webhook delivery.

Portable state does not confer authority. Credentials, signing secrets and key
material are not implicitly copied. The recipient supplies its own authorized
configuration and required keys. Stable operation identity and origin provenance
must survive relocation so previous attempts can be reconciled before retrying.
Copying a bundle does not establish that the original host has stopped.
Each storage namespace has an [independent key lifecycle](protected-cache-plan.md#agreed-independent-namespace-key-lifecycles).
Routine rotation preserves access to protected saved versions until safe re-protection;
explicit emergency revocation may block affected recovery. Key references and required
versions travel as declared metadata, not an implicit grant or copy of secret key bytes.

Explicit [recipient-encrypted handoff](protected-cache-plan.md#agreed-recipient-encrypted-handoff)
may make the selected bundle usable through the destination's own key provider without
sharing the source storage keys. Export must authorize the recipient and selected scope.
Successful decryption does not activate pending operations or establish confirmed takeover.

Explicitly enabled [recovery-key backups](protected-cache-plan.md#agreed-optional-recovery-key-backups)
may be prepared for a separately held recovery key before a host/key-service failure.
They restore selected data without the original provider when the recovery custodian
has that key; the private key stays outside the backup. Decryption still does not
restore source permissions or authorize external effects.

## Agreed responsibility transfer

Confirmed takeover is the default for imported pending external operations. The
destination may activate them only after the source has durably relinquished
responsibility, or a shared ownership mechanism prevents the source from continuing
those operations. Source unreachability, a copied lease or elapsed time on the
destination alone is not proof of relinquishment. Until responsibility is confirmed,
external operations stay paused; authorized local work may continue.

Bind confirmation to the selected operations and recovery snapshot. Define source
restart behavior, stale confirmations, repeated destination activation and uncertain
in-flight attempts before claiming exclusivity. Existing local outbox claims coordinate
workers sharing an outbox; copying that outbox to another machine does not create
shared ownership. This direction does not require a hosted coordinator for ordinary
local use; the cross-host coordination/confirmation contract remains to be designed.

Takeover prevents concurrent authorized owners under the chosen contract; it does
not retract a request already sent or remove ordinary retry duplicates. Preserve
operation identity, reconcile uncertain outcomes and require receiver deduplication
where applicable. No forced-takeover feature is scheduled by this default decision.

An older backup or cloned local ownership record must not revive permission to send
external operations after responsibility has moved. Durable local relinquishment by
itself cannot fence a different machine restored from pre-relinquishment state. Bind
handoff evidence to the selected snapshot, operation identities and intended recipient,
and qualify shared ownership/receiver-enforced fencing wherever independently restored
copies may activate. Without adequate current evidence, keep external effects paused;
do not advertise universal single-owner recovery from a copied lease or receipt.

## Agreed retention protection

Unfinished operations protect the data and recovery records they require from
automatic cleanup, even after ordinary expiry. Keep the smallest verified recovery
set rather than treating the whole workspace as permanently retained. This applies
across workspace files, logical objects, checkpoints, transfer staging and pending
storage/notification records. A normally disposable cache copy becomes protected
when unfinished work depends on it and no verified replacement is available.

Release protection after confirmed completion or explicit authorized discard.
Failed retries, an offline owner, expired credentials or an exhausted retry budget
do not by themselves release it. A confirmed takeover alone also does not prove the
destination has durably received every required byte; define the receipt and source
cleanup boundary before removing source data.

For ready-report output, [delivery acceptance and artifact release](workspace-notifications-plan.md#agreed-delivery-acceptance-and-saved-output-release)
are separate receipts. Queue/transport acceptance does not release the consumer's
data hold; explicit consumer release removes only its own obligation. Other consumers
or unfinished operations can continue to protect the same saved bytes.

Retained recovery data counts against declared storage limits. When necessary data
cannot fit, refuse admission of new work and report which operations/dependencies
hold capacity; do not silently erase pending work. Required recovery data may outlive
expiry until completion or explicit discard. Ordinary disposable cache eviction
remains available for copies outside the protected recovery set.

Persist the dependency/protection records and coordinate their creation/release with
operation and cleanup boundaries. Incomplete or unreadable protection records must
not authorize deletion. If safe separation of required data from a workspace is not
supported, refuse cleanup until it is possible; do not approximate a smaller recovery
set by dropping dependencies. Stow cannot prevent direct host filesystem deletion.

Current collection checks liveness, ownership and TTL; checkpoint deletion checks
local descendants. They do not yet enforce these new pending-operation dependencies.
Extend those shared lifecycle paths rather than adding a second cleanup engine.

## Agreed recovery bookkeeping reserve

Reserve a small, bounded portion of managed storage capacity for recovery bookkeeping.
New payloads, uploads and checkpoints cannot consume it. Existing operations may use
it to durably record completion, acknowledgements, protection release and cleanup
progress while ordinary data admission is blocked. The reserve is for bounded state
transitions, not additional transfer payloads or unlimited retry/event history.

Include required bookkeeping in operation admission and account for crash-safe update
overhead. Specify bounded record growth, safe reclamation and the reserve size before
implementation. Admission must not create more recovery obligations than the chosen
reserve can support. Reuse existing quota/accounting and durable publication paths.

This is a proposed managed-capacity guarantee, not a claim that a quota counter
reserves physical disk space. Each persistence adapter must prove that ordinary Stow
data cannot consume the reserved capacity and define what happens if the host itself
runs out of space. Direct host writes and platform storage failures remain outside
Stow admission control. Large transfer completion still needs separately admitted
payload capacity; the bookkeeping reserve does not guarantee it can fit.

## Agreed project/customer budgets

Use independently configured project/customer storage budgets beneath an overall
host limit. Bind each budget to a trusted storage namespace supplied by the host
identity integration; caller-provided names or headers cannot select another budget.
Single-project local use can use one namespace without an account service.

Account together for the namespace's managed working files, logical objects,
checkpoints, incomplete transfer payloads, pending propagation and notification
backlog. Include encryption/publication overhead and a bounded recovery reserve for
that namespace. Define shared-payload charging so several dependencies do not either
hide physical costs or charge the same physical bytes repeatedly by accident.

New work must fit both its namespace budget and the host limit. One namespace cannot
consume another's recovery reserve. Report whether admission was refused by the
namespace or host budget; blocker details require appropriate inspection authority
and must not expose another customer's paths or pending operations.

Namespace ceilings limit interference; they alone do not guarantee every namespace
has room for future payloads when the host limit is reached. Guaranteed payload
allocations, spare-capacity borrowing and overcommit rules remain separate decisions.
Do not promise absolute filesystem isolation from application-side quota counters;
direct host writes still need external enforcement and reconciled accounting.

Current workspace byte/object limits and registry checkpoint/count limits provide
reuse points. They are not an existing unified project/customer budget over all
these retained categories. Extend their shared admission/accounting paths and
qualified host adapters rather than using independent wrapper counters.

## Scoped handoff boundary

The interview also accepted explicitly scoped checkpoints and exports. The
[scoped-access detail](protected-cache-plan.md#agreed-scoped-checkpointexport-direction)
records their contract: authorized portions may be saved/shared under a declared
scope; insufficient permissions for a whole-workspace request cause refusal. Receiving
such an artifact does not widen destination permissions or prove all pending-operation
dependencies are present.

## Agreed partial recovery

Recover verified, permitted data and keep operation-level blockers explicit. If an
unfinished operation lacks required resources or destination permission, leave that
operation and its dependent operations blocked. Independent operations may activate
after their own permission, confirmed-takeover and reconciliation checks; one blocked
operation does not hold the entire valid handoff.

Report each selected operation's status and blockers through bounded, permission-safe
inspection. Do not treat blocked work as completed, silently discard it, fetch forbidden
inputs or widen a scoped artifact to make it resumable. Retention protection remains
in force until completion or explicit authorized discard.

Define storage-operation dependencies and eligibility explicitly; do not infer an agent
task graph or readiness from filenames. Uncertain independence must not authorize an
external operation. Callers still choose execution and reactions; this is recovery
admission over supported storage/delivery operations, not a general workflow scheduler.

Partial recovery applies after artifact integrity/schema admission. It does not allow
corrupt or unsupported required archive sections to be silently skipped, nor does it
change existing full-checkpoint verification into best-effort file restoration. The
recipient may repair a missing permission/input and explicitly re-evaluate affected
operations without re-importing or reactivating unrelated completed work.

## Existing support and extension boundary

Current checkpoint/handoff APIs preserve supported files, logical objects and
metadata, verify archives and restore under a destination-local workspace identity.
They do not implement this recovery promise. Incomplete multipart staging is
excluded; transfer progress remains consumer-owned. Planned notifications initially
retain their journal on the same workspace only, with webhook configuration and
delivery receipts host-local.

Extend existing versioned capture/archive/adoption paths with explicit, bounded
recovery sections. Do not copy `.stow` wholesale, introduce a second archive format,
or make legacy readers silently drop required recovery state. Preserve existing
file-only and logical-object checkpoint behavior.

An upload record alone cannot move acknowledged part bytes to a different storage
service. A download offset alone cannot replace the verified prefix. Define which
payloads travel, how source/destination identity is verified, and when continuation
must be refused or restarted. Pending webhook/upstream attempts need reconciliation
of possibly completed effects; unknown outcomes must not become unattempted work.
Imported delivery history is evidence, not permission to send to an old endpoint.

## Engineering contracts to qualify

These are implementation/evidence gates to resolve against the owning code paths,
not routine approval questions for the user. The [foundation review](foundation-review-2026-09-29.md)
sets dependency order and conservative defaults.

- Evidence and mechanism for confirmed responsibility transfer, including source
  restart and in-flight requests; distinguish continuation from an independent
  branch and ordinary backup restore.
- Eligible recovery sections, snapshot consistency, payload limits and missing-state
  outcomes; define each section alongside its owning transfer/journal contract.
- Destination policy/key/endpoint binding, expiration and authorization failures.
- Operation dependency representation, permission-safe statuses and re-evaluation
  when a partial recovery blocker is resolved.
- Dependency representation, safe minimal recovery sets and destination receipts
  that permit source cleanup after confirmed adoption.
- Reserve sizing, bounded bookkeeping growth and physical/platform enforcement;
  namespace charging, limit configuration and spare-capacity allocation rules.

## Acceptance to specify with the remaining contracts

Use an interrupted upload, retained download prefix, pending upstream mutation and
pending webhook as separate scenarios. Include lost replies, expired credentials,
policy changes, corrupt/unsupported sections, repeat import, source restart and two
hosts opening the same saved state. Verify that adoption alone causes no external
effects and that explicit activation reports what resumed, stayed paused or requires
reconciliation. Unreachable sources must leave external work paused without accepted
takeover evidence; source restart and stale evidence must not enable another owner.
Exercise expiry with pending work, full budgets, unreadable protection records,
dependency changes during cleanup and incomplete destination receipts. Prove required
data survives, unrelated disposable copies remain reclaimable and capacity refusals
explain their blockers. Completion/discard must release only the selected protections.
Fill the ordinary data budget and prove admitted operations can still durably record
completion, acknowledgement and cleanup through the reserve. Include crash/restart,
concurrent transitions, reserve exhaustion and actual backing-store failures; do not
equate a logical quota pass with a physical-space guarantee.
Exercise two namespaces with a stalled transfer/delivery backlog in one, concurrent
admission and host exhaustion. Verify namespace limits, distinct recovery reserves,
accurate persisted accounting after restart and permission-safe capacity diagnostics.
Use a valid scoped artifact with one missing operation dependency and a separate
eligible operation. Verify the first and its dependents stay blocked while explicitly
activated independent work can proceed. Resolve the blocker and prove re-evaluation
does not duplicate completed work. Corrupt/unsupported required artifacts still refuse.
Retain at-least-once delivery limits; moving state does not provide
exactly-once external effects.

This direction extends the [transfer validation](resumable-transfer-plan.md) and
[notification plan](workspace-notifications-plan.md) beyond their initial same-store
restart scope. ACL/encryption dependencies remain in the
[protected persistence plan](protected-cache-plan.md). Callers still own execution,
writer quiescence and reactions under ADR 0014.
