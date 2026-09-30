---
status: accepted
decision_digest: 7e8a4e66389e51e4
amended: 2026-09-29
relates_to: 0014, 0009
---

# Stow owns durable workspace notifications

## Context

The multiplayer example publishes immutable reports naming affected files, selects
subscribers and records acknowledgements. This is useful beyond agent collaboration:
applications can announce completed imports, available artifacts or changed inputs.
The user has directed that this reusable alert mechanism move into Stow's core.

The example is experimental JavaScript caller code, not a supported core API. Its
process-local queue, active-participant routing and filesystem watcher do not establish
a durable, concurrent notification contract. It must not simply be copied into each SDK.

## Decision

Stow will own workspace-scoped file change detection and durable notifications:
subscriptions, bounded pending/replay queries, acknowledgement and optional explicit
publication. This extends the storage boundary in ADR 0014; execution and reaction
remain caller-owned.

The first useful implementation detects created, modified and deleted files in declared
workspace paths, including ordinary host writes and Stow-mediated writes. Watcher hints
trigger content reconciliation against a persisted baseline; bounded periodic/startup
reconciliation covers missed hints. Unsupported or incomplete coverage is explicit.
Observed changes describe revisions Stow has seen, not every intermediate edit, writer
identity or completion of work. Restart can detect a net change while observation was
offline; it cannot reconstruct edits that left the same final content.

Explicit reports remain complementary: a caller can assert that an artifact is ready.
Stow retains that assertion; it does not infer task success, validate a business
transaction or guarantee writers have stopped. File inactivity is not readiness.

Ready reports identify immutable saved output versions, with retention dependencies
for pending delivery. They may explicitly include bounded change detail: a complete
text diff when the serialized payload budget permits, otherwise a changed-
lines report. Scope and permissions cover both compared versions; content inclusion
is optional. The default base is the previous ready version of the same output/scope,
with an explicit saved-base override; retain both version identities, independent of
recipient acknowledgement history. Transport acceptance and artifact release are
separate acknowledgements: acceptance stops delivery retries, while explicit consumer
release ends that consumer's saved-output retention obligation after it has secured
the data or no longer needs Stow's retained copy. Other consumers/operations still
protect their dependencies. Comparison work limits and receipt API details remain
proposed. Automatic change observations do not imply
this readiness/version guarantee.

Callers may optionally declare the input versions used to produce ready output. A
supplied basis defaults to requiring current, authorized input identities at report
admission; stale, missing, denied or unverifiable required inputs block publication
while preserving the saved draft. Explicit historical snapshot mode makes no current-
input claim, and an omitted basis makes no input-freshness claim. Core checks declared
storage identities within its qualified coordination scope; callers still own read
tracking, quiescence and correctness. The admitted basis/mode remains part of report
meaning through replay. This capability does not observe agent execution.

When current recipient policy forbids any resource/version disclosed by a queued
report, pause the whole delivery rather than automatically redact or change its body
under the original identity. A publisher may explicitly create a new scoped report
after current authorization checks; the original's retention obligations remain until
release or authorized discard. Permission-safe inspection and full-scope rechecks are
required before retry; revocation cannot recall bytes already delivered.

Use one Protobuf notification schema with generated codecs, native diff byte fields
and optional schema-derived ProtoJSON compatibility. The latter encodes byte fields
as base64; it is not a separately designed event protocol. This selects a typed binary
contract without claiming an unmeasured performance advantage or requiring gRPC.
The configurable complete encoded-body default is 64 KiB under explicit host/profile
limits. Compare defined semantic content for idempotency/conflict detection, freeze
delivery encoding/body and sign exact delivered bytes. Version/required-capability
handling must preserve security and recovery restrictions. Existing S3, MCP and CLI
formats are not replaced by this notification decision.

Subscribers are stable caller-chosen identities, not model sessions or live processes.
Publication and acknowledgement survive reopening the same workspace. Delivery is
at least once; an acknowledgement records consumer progress, not exactly-once execution
of an external effect. Callers deduplicate effects using event identity.

Optional webhooks deliver the same retained events to explicitly configured HTTP
destinations. Exact-path, parent-directory and workspace-wide subscriptions all retain
the original changed path/revision and event identity; overlapping interests for one
subscription produce one delivery. Webhook dispatch is a storage notification transport,
not a task scheduler. The receiver owns any downstream job or agent reaction.

Webhook delivery uses durable per-destination progress, signed bounded payloads, bounded
retry/backoff and inspectable retained failures. A successful HTTP response acknowledges
delivery only; receivers must deduplicate and durably accept work before responding.
Dispatch is explicitly enabled and owned by a running handle/command, with no hidden
daemon or network activity inferred from file contents. Destination configuration and
signing secrets are host-local and excluded from checkpoint/handoff data.

Use one Go implementation behind public Go, CLI and thin language surfaces. Reuse
existing workspace identity, confinement, authority, locking and atomic persistence
primitives where their contracts apply. Specify notification-specific ordering,
publication uncertainty, limits and retention rather than borrowing the upstream-write
outbox's assumptions. S3 bucket notification configuration is a separate compatibility
feature and is not implemented by this decision.

Scheduling, model prompts, context refresh, human communication, harness steering,
agent/harness observation, presence and file claims stay in consumers, including the
standalone collaboration repository. Core observes workspace content; consumers track
what an agent read and whether a change makes its context stale. Notifications do not
require that repository to depend on Stow.

Notification portability requires an explicit versioned capture/export contract.
Current checkpoints exclude `.stow`; putting a journal there does not make it portable.
The first slice promises same-workspace restart only. Callback handles and external
effect receipts must not be revived by restoring an old checkpoint. Broader handoff
semantics are specified and tested before they are advertised.

Implementation is scheduled under S1-6 in the canonical plan and expanded in the
[notification plan](../workspace-notifications-plan.md). The product decision is
accepted; proposed schemas and API names remain design detail until implemented.

## Consequences

- Applications and agents can share a durable signaling contract without a Stow runner.
- The prototype supplies scenarios and semantics to adapt, not production guarantees.
- Existing release gates remain in force; this decision ships no notification API.

## Amendments

- 2026-09-29: Accepted the user's direction to move the reusable alert primitive into
  core, preserving caller-owned execution and distinguishing reports from observation.
- 2026-09-29: Prioritized automatic workspace file change detection following the user's
  clarification; explicit readiness reports remain complementary. Added reconciliation,
  observation coverage and offline-history limits to the core boundary.
- 2026-09-29: Added optional webhook delivery at the user's request and made parent-path
  subscription matching explicit. Retained event identity, durable retries and host-local
  destination/secret configuration preserve the notification/execution boundary.
- 2026-09-29: Bound explicit readiness to immutable saved output and accepted optional
  bounded diff/changed-lines detail from the recovery interview, preserving caller-
  declared readiness and permission checks over historical as well as current content.
- 2026-09-29: Selected the previous ready version as the default comparison base,
  with explicit override and fixed base/target identity rather than recipient-specific
  acknowledged-version comparisons.
- 2026-09-29: Following the user's Protobuf correction and delegated choice, selected
  Protobuf as the primary notification format, optional generated ProtoJSON compatibility
  and a configurable 64 KiB encoded-body engineering default. Byte payloads remain native
  in binary messages; semantic replay identity is distinct from serialized signing bytes.
- 2026-09-29: Separated notification delivery acceptance from explicit consumer release
  of saved-output retention, so queued receipts do not authorize premature cleanup.
- 2026-09-29: Selected whole-report pause on partial recipient permission revocation,
  preserving admitted payload identity and requiring explicit new scoped publication
  instead of automatic trimming. Retention remains distinct from permission to disclose.
- 2026-09-29: Accepted optional caller-declared input-version checks before readiness
  publication. Current-input checks default when a basis is supplied; explicit snapshot
  mode preserves intentional historical work. Stow retains drafts on refusal and checks
  only declared storage identities, preserving caller-owned read tracking and execution.
