# Durable workspace notifications

> **Consolidated on 2026-09-29; reference only.** The [new working-storage plan](storage-foundation-plan.md) is the sole work order. Observation/readiness/codecs/diffs/webhooks are W08–W11 and portable notification state W12. Confirmed takeover is settled; its mechanism/evidence is engineering work under W13. Former checklists, phase numbers and next steps below do not schedule work.

**Status:** accepted core capability; proposed implementation contract, not shipped.
**Authority:** [ADR 0015](adr/0015-durable-workspace-notifications.md).
**Work order:** S1-6 in [the canonical plan](plan.md).

## Purpose

Detect changes to workspace inputs and outputs so interested agents, tools and
applications can respond without relying on a producer to announce every edit. Retain
notifications so consumers can resume after interruption. The consumer chooses the
response and knows which prior reads matter to its work.

Automatic change detection is the first useful slice. Explicit reports complement it
when a caller needs to announce a reviewable draft, completed import or ready media
asset. Observed content changes and caller-declared readiness have distinct event kinds.
File inactivity does not establish readiness.

Optional webhooks let the same subscriptions notify services outside the Stow process.
Parent-directory subscriptions provide path bubbling; downstream services can chain
work from received events while owning their execution and feedback-loop policy.

## Source to reuse

- `examples/multiplayer-prototype/room.mjs`: publication identity, normalized paths,
  subscription matching, immutable reports, pending queries and acknowledgements.
- `examples/multiplayer-prototype/coordinator.mjs`: caller decides readiness after
  producer settlement. Keep this decision outside the storage API.
- `pkg/stow/workspace.go`, `pkg/stow/resume.go`: durable workspace identity/lifecycle.
- `internal/rooted`, `internal/atomicfile`: confined access and durable atomic writes.
- `internal/storage/workspace/capture_lock.go`: inspect lock semantics before reuse;
  avoid blocking a live workspace owner or claiming unsupported platform guarantees.
- `pkg/stow/checkpoint_request.go`: model explicit replay/conflict/uncertain outcomes.
- The standalone collaboration repository's `src/file-observer.mjs`: existing watcher,
  content-hash reconciliation and coverage scenarios to inspect/adapt. It is JavaScript
  caller code, not a core dependency or durable observer implementation. Keep native
  harness feeds, read tracking and decisions in that repository.
- `cmd/stow-s3/workspace*.go`, `packages/stow-s3/src/workspace.ts`: existing command
  contract and thin wrapper pattern. Do not add SDK-local journals.

Do not carry participant presence, ownership claims, model scheduling or prompt text
into the core. The run-through outbox propagates storage mutations upstream; it is not
an existing generic notification journal and must retain its separate semantics.

## Proposed first contract

All operations are explicitly workspace-scoped. API spelling is provisional.

| Operation | Required behavior |
| --- | --- |
| Subscribe | Persist a stable consumer ID and exact-path/directory-prefix interests; no live agent registration required. Changes affect future publications. |
| Observe/reconcile | Establish declared observation scope and a baseline; detect created/modified/deleted files from verified current content. Publish bounded durable change events and expose observation coverage. |
| Publish | Accept a caller-persisted request ID, event kind, opaque source, relative resource paths and bounded summary. Return a committed event identity/sequence; matching retries replay, different content under the same request ID conflicts. |
| Pending | Return bounded pages of retained, unacknowledged events addressed to that consumer, in workspace sequence order. Provide an opaque continuation token and explicit invalid/gap outcomes. |
| Acknowledge | Durably acknowledge named events addressed to that consumer. Retry is idempotent; callbacks alone never acknowledge. |
| Inspect | Show bounded backlog, publication and retention state without requiring a listener. |
| Configure webhook | Bind a notification subscription to an explicitly enabled host-local destination/signing-secret reference; persist configuration revision and delivery state. |
| Dispatch | Deliver pending events with bounded HTTP requests/retries, durable per-destination progress and inspectable retained failures. |

Freeze recipient IDs at publication, including offline subscribers. Later subscriptions
do not silently alter prior deliveries. An explicit replay query can expose retained
history without pretending it was originally addressed to a new consumer. Deletions
still name affected paths; published resource paths need not currently exist.

Parent-path matching uses path-component boundaries: `campaigns/spring/` includes
`campaigns/spring/data/contacts.csv`, while `campaigns/springboard/` does not match.
Provide an explicit workspace-root scope. Preserve the originating path/revision and
event ID rather than generating a new event at each ancestor. Overlapping interests
within one subscription yield one delivery; separately configured subscriptions remain
independent. Directory matching cannot extend detection beyond declared observation scope.

Define schema version, maximum event bytes/path count, subscriber count and journal
bytes/events before implementation. Persist unknown-version refusal and input validation.
Start with exact paths and directory prefixes, not a general glob engine. Opaque source
IDs convey provenance rather than authentication. Mutating operations respect workspace
write authority; identity is not a permission boundary against host filesystem access.

Use one durable publication boundary for event content, ordering and frozen recipients.
Cross-process publication must not duplicate sequence numbers or lose subscription/
acknowledgement updates. A crash after publication but before response is reconciled by
request ID. Filesystem watchers are low-latency hints; reconciliation establishes changes
and pending/replay establishes deliveries. Callback failure, disconnect and process
restart leave work pending.

## Change detection contract

Observe declared files and directory prefixes in an ordinary workspace. Include edits
from host tools and editors as well as the Stow directory/S3 facade. New files under a
declared prefix must be discovered. Exclude reserved bookkeeping and honor configured
scope/limits. Do not claim observation of upstream objects or unsupported storage
profiles as a side effect of local workspace watching.

On first observation, establish a baseline rather than calling every existing file a
new change. Subscription setup and baseline admission must have a documented boundary;
changes during the initial scan require reconciliation. Record file identity as a
relative path plus a verified content digest/presence state and observed revision.
Define relevant mode/metadata changes explicitly. Rename is deletion plus creation
initially; do not infer writer identity from a watcher event.

Use bounded watcher-driven scans with coalescing plus periodic and startup reconciliation.
Watch errors, missing directories, unsupported watch backends and event overflow must
trigger fallback reconciliation or explicit incomplete coverage. Read instability or
permission/size limits must not look like confirmed deletion. An intermediate readable
revision may be observed while an external writer is still working; no multi-file
transaction or settled-output claim follows from it.

Persist observation baseline advancement with the corresponding committed change event,
or an equivalent replayable recovery record. Never advance the baseline and lose the
notification on a crash. Multiple observer processes must coordinate baseline ownership
and event identity; watchers alone are not a cross-process lock. Reopening reconciles
against retained observations and emits net changes, including deletions while offline.
Historical coverage stays incomplete: coalesced/offline intermediate edits may be lost.

If the journal is full, ordinary host edits still happen. Do not advance the baseline
past changes that cannot be recorded; expose a coverage/backlog failure and retry after
capacity is restored. Keep detection byte/file budgets, event batching, scan frequency
and maximum delay explicit. Debouncing reduces noise; it does not establish readiness.

Delivery is at least once. Consumers acknowledge after their own durable handling and
deduplicate external effects by event ID. Stow cannot atomically commit a CHS database
transaction and its own journal; application publication needs retry/reconciliation or
the application's transactional outbox. Do not label a blob upload alone as a committed
media record or import.

Default retention is bounded and rejects new publication when full. Never silently
evict unacknowledged events. Explicit pruning may remove only fully acknowledged history;
record the replay floor and deduplication horizon. Subscription removal, abandoned
consumers and force-discard require explicit documented operations rather than age-based
assumptions. Keep query pages and in-memory indexes bounded too.

The subsequent [portable recovery direction](portable-recovery-plan.md) also requires
unfinished operations to protect their required data across lifecycle cleanup, even
after normal expiry. Integrate pending delivery dependencies with S1-4/S1-9 retention
protection; a retry-exhausted delivery is still unfinished until accepted or explicitly
discarded. Do not pin unrelated workspace contents. This lifecycle extension is proposed,
not an existing collection guarantee.

## Agreed version-bound readiness and optional change detail

Explicit readiness reports identify an immutable saved version of the files or
objects they announce. Use an existing verified checkpoint or equivalent retained
immutable object version, with declared scope and origin provenance. A delayed
consumer retrieves that version rather than the current mutable workspace contents,
subject to current permissions. The caller decides readiness and quiesces its writers
for capture; a report is not business validation or a filesystem transaction.

Admit the report only against a committed, verified saved version. Coordinate its
retention dependency with publication and reconcile interrupted publication by request
identity; do not publish a ready report naming an uncommitted or missing version.
Pending delivery protects the required saved data through the recovery-set lifecycle
in [portable recovery](portable-recovery-plan.md). Transport acceptance and saved-output
release are separate acknowledgements, as specified below.

The user also requested optional inline change detail: a complete text diff when it
fits the configured payload budget, otherwise a changed-lines report. Carry the diff
as native bytes in Protobuf and as base64 in its optional ProtoJSON representation. Record
the base and target version identities and resource scope. The accepted default base
is the previous ready version of the same output and declared scope; callers may
explicitly select another permitted saved base. This is a comparison of saved content, not a patch
against whichever bytes happen to be present when delivery is attempted.

Select and persist the base when admitting the report. It is shared comparison context,
not each recipient's last acknowledged version, and does not change during retries or
because the recipient missed a report. Define stable output/scope identity and ordering
for concurrent ready reports before implementation. The recipient may need authorized
retrieval of the named base; a patch does not imply the recipient already has it.

For a first ready report with no prior version, mark comparison unavailable unless a
caller supplies an eligible saved base. Missing/pruned bases and incompatible scopes
must be explicit; never substitute a different base silently or broaden scope. Optional
comparison failure does not turn a verified ready output into a fabricated comparison.
The base and target must both pass the comparison/disclosure checks below.

Set limits on the final serialized notification in its selected transport encoding,
including base64 expansion and envelope overhead when represented as JSON, and on
comparison input size, CPU/memory and elapsed time. The complete encoded notification
body has a configurable default limit of 64 KiB (65,536 bytes), subject to host/profile
limits. Comparison work budgets and precise changed-lines format remain proposed.
A fallback must also be bounded;
if it cannot fit, identify omitted/truncated detail explicitly and retain the version
reference for authorized retrieval. Never truncate a patch and represent it as complete.
Binary objects and unavailable text comparisons need explicit non-text/unavailable
outcomes rather than invented line counts. Base64 is transport encoding, not encryption.

Diff bodies, deleted lines and context can disclose earlier content. Require explicit
content inclusion and permission to disclose both compared versions to the destination;
recheck before query/dispatch. Summary paths, line information and version references
also respect scope. Missing access does not permit computing a diff from forbidden
bytes. If the recipient loses permission to any resource/version disclosed by a queued
report, pause that entire delivery and retain an inspectable permission blocker. Do not
automatically trim paths, deleted/context lines, summaries or references and send a
different body under the same identity. The publisher may explicitly admit a new scoped
report with a new identity after checking current permissions; it does not silently
replace or acknowledge the original obligation.

Re-evaluate the full original disclosure scope before any explicit retry. Restored
permission does not authorize an unrelated destination or new body. Already transmitted
bytes cannot be recalled by a later policy change; define dispatch admission/in-flight
boundaries without claiming retroactive revocation. A paused report retains its recovery
dependencies until consumer release or explicit authorized discard. Status inspection
must not reveal newly forbidden paths or historical contents to the revoked consumer.

Current `CompareCheckpoints`/`ComparePortableCheckpoints` identify file/object changes
and metadata; they do not produce text patches or changed-line reports. Reuse their
inventory/version comparison and archive validation, adding bounded content comparison
once in core. Thin wrappers must not invent different diff formats. Automatic change
events remain observations; this readiness contract does not make every observed edit
a saved ready version or reconstruct intermediate content that was not retained.

Verify delayed delivery after subsequent edits, retained target retrieval, explicit
scope, missing/pruned base, multi-file readiness, permission changes, binary input,
comparison limits and the encoded payload boundary. Patch replay must verify its base
identity; a received summary is not a complete patch. Freeze the admitted optional
detail for retry identity and count its storage against namespace/backlog budgets.
Include consecutive/concurrent ready reports, an explicit base override, first-report
behavior and recipients with different acknowledgement histories. Every retained
comparison must keep its selected base/target identities through replay and relocation.
Include partial revocation of a multi-resource report, permission restoration and an
explicit new scoped replacement. The original delivery must stay byte-stable and paused
while denied; the replacement has independent admission/identity and does not erase
the original retention obligations.

### Declared input basis at publication

Callers may provide the [input basis](guarded-storage-plan.md#agreed-declared-input-checks)
used to produce a saved output. By default, a supplied basis must still match current
authorized inputs when the ready report is admitted. A stale, missing, denied or
unverifiable required input blocks that publication while retaining the saved draft.
An explicit snapshot-basis mode records intentional historical inputs; an omitted
basis makes no freshness claim. Stow checks declared storage identities, not agent
reads or output correctness.

Check at the core publication boundary under the qualified coordination profile, not
in a wrapper before a later independent publish. Preserve the basis/mode with report
identity, bound its size/disclosure and recheck recipient scope before delivery.
Unsupported atomic coverage refuses a strict request; caller quiescence still applies
to ordinary host writers. Later input changes do not rewrite an admitted report.

## Agreed delivery acceptance and saved-output release

Webhook acceptance records durable receipt of the notification and stops its delivery
retries after local acknowledgement is committed. It does not release the consumer's
retention dependency on the announced saved output. A receiver may have queued the
event without retrieving the referenced bytes.

Require a separate explicit, durable artifact-release acknowledgement when that consumer
has secured the required data or no longer needs Stow's retained copy. Apply the same
distinction to in-process/CLI consumers rather than making webhook users a separate
retention model. Bind release to consumer/report and immutable version identity, make
retries idempotent, and authenticate authority to release that consumer's obligation.
API spelling and callback transport remain proposed; a delivery URL alone is not release
authority and Stow does not infer release from a successful GET or task completion.

One consumer's release removes only its own hold. Other consumers, pending operations
and standing retention may still protect the saved version; release is not an immediate
delete request. Preserve required base-version dependencies as well when the consumer
needs them to use a patch. Unreachable consumers and exhausted transport retries do not
automatically release retained output; abandonment uses explicit authorized discard.
The held bytes and receipt bookkeeping count against the namespace budget/reserve.

Coordinate receipts and protection changes through the same durable lifecycle paths.
Define out-of-order release versus delivery-reply persistence, lost replies, replay and
handoff provenance without reviving completed obligations or releasing another consumer's
data. Distinguish transport receipt, consumer release and administrative discard in
inspection. Consumer release is a data-lifecycle assertion, not proof of business success.

Verify queued acceptance followed by a delayed fetch, restart between acceptance and
release, repeated/lost release replies, multiple consumers, release before local delivery
receipt persistence, permission changes and explicit abandonment. A 2xx transport receipt
alone must never make required output eligible for cleanup.

## Selected notification encoding and payload default

The user delegated the encoding choice after recalling the Protobuf direction.
Select one Protobuf notification schema with generated codecs and native `bytes` for
diff content as the primary format. Offer an optional schema-derived ProtoJSON
representation for webhook/diagnostic compatibility, preserving one event contract.
This favors a typed binary contract with direct byte payloads while retaining a
readable integration surface; it is not a measured performance claim or a requirement
to introduce gRPC. [Binary Protobuf](https://protobuf.dev/programming-guides/encoding/)
supports byte fields; [ProtoJSON](https://protobuf.dev/programming-guides/json/) maps
them to base64 strings. These are transport representations of the same change detail,
not separate event contracts. Freeze encoding/content type per retained delivery and
sign its exact bytes. Equality/retry semantics must compare defined event meaning rather
than assume arbitrary Protobuf encodings are canonical.

Choose 64 KiB as the configurable engineering default for the actual complete encoded
body, including metadata and encoding overhead; larger settings remain subject to
explicit host/profile limits and workload evidence. If both representations are required
for publication, select detail that fits their admitted encoded bodies before freezing
it. Prefer a complete patch, then a bounded changed-lines report, then an explicit
omitted-detail outcome with the saved-version reference. Required envelope fields that
cannot fit cause an explicit admission error, not silent truncation.

Freeze encoding/content type in destination configuration; a configuration change
does not recode pending deliveries silently. Define the versioned semantic fields used
for idempotency/conflict checks separately from exact delivery bytes used for signing.
Wire versioning, bounded decoding, unknown-field preservation and refusal of unsupported
required security/recovery capabilities need qualification. Add generated Go/language
codec checks, binary/ProtoJSON equivalence and size-boundary fixtures to implementation.
Existing S3, MCP and CLI protocols retain their own supported formats. Current Stow
sources have no `.proto` notification schema or protobuf runtime dependency; these are
selected design choices, not shipped APIs.

## Webhook delivery contract

Use the notification journal as the event source. Persist destination-specific delivery
attempts/acknowledgements alongside notification bookkeeping; do not create another
authoritative event stream or copy the run-through S3 propagation outbox wholesale.
Inspect its locking/claims and transport patterns for reuse where the contracts match;
webhook POST retry behavior and acknowledgement differ from upstream object writes.

Each request is an HTTP POST with a versioned envelope: workspace/stream ID, stable event
ID, stable delivery ID, sequence, event kind, original relative paths, observed revisions/
digests, observation time and coverage where relevant. Ready reports retain their distinct
kind. Include subscription identity/matched scope without replacing the original path.
No file contents, credentials, absolute host paths or arbitrary HTTP headers are included
by default. Explicitly enabled readiness diff detail follows the bounded, authorized
content contract above. Select primary Protobuf or explicitly configured ProtoJSON
content type according to the shared schema and limits above;
keep persisted request bytes bounded and stable across retries.

Sign exact request bytes with a host-local HMAC-SHA256 secret reference. Specify timestamp,
signature version/header and receiver verification window in the public contract; each
attempt can have a fresh signed timestamp while keeping event/delivery identity. The
receiver deduplicates by delivery ID and uses event ID when multiple subscriptions feed
the same effect. Secret rotation and historical configuration revisions must be explicit.

Only explicit caller configuration selects destinations. Default to HTTPS; permit literal
loopback HTTP for local development. Reject URL credentials/fragments and redirects;
support private-network destinations through explicit endpoint policy rather than
accepting destinations from observed files/events. Keep destination credentials, URLs
and policy host-local and redact secrets from inspection/logs. Do not reuse S3 credentials
or assume that S3 live-write consent enables webhook delivery.

Admission freezes a subscription's destination configuration revision. Changing a URL
does not silently send existing pending events to another endpoint; pause/drain or
explicitly retarget retained deliveries. Signing-key rotation must not corrupt retained
payload identity. Endpoint disablement pauses delivery rather than acknowledging it.

Any 2xx response counts as transport acceptance. The receiver must durably enqueue or
handle the event before responding. Persist acknowledgement only after response; a crash
after receiver acceptance but before local persistence can cause duplicate delivery.
No exactly-once processing is promised. An unreachable endpoint never blocks file writes
or healthy destinations; retained events still consume declared journal/delivery limits.
For ready reports, this receipt is distinct from the consumer's saved-output release
acknowledgement; accepting a queued notification does not release artifact protection.

Retry timeouts/network failures, 408, 429 and 5xx using bounded exponential backoff with
jitter and bounded `Retry-After`. Retain other non-2xx outcomes as inspectable failures;
do not follow redirects. Specify request/body/response limits, worker concurrency,
attempt/deadline limits and cancellation before implementation. Exhausted failures stay
visible for explicit retry/discard rather than disappearing. Discard is recorded separately
from successful acknowledgement. Serialize attempts per subscription initially; a failed
delivery can hold later events there while other subscriptions progress independently.

Coordinate dispatcher claims across processes and reconcile after restart. Persist intent
before POST; never hold workspace mutation/capture locks during network I/O. Closing the
owning handle cancels requests and retains unresolved work. A one-shot publish/inspect
command never silently starts a sender. Observation continues even when webhook dispatch
is paused, subject to the documented backlog/coverage behavior when storage fills.

For downstream chains, preserve optional correlation/causation IDs on explicit reports.
Automatic filesystem observation cannot reliably identify which webhook caused an edit.
Consumers must prevent feedback loops with scoped outputs, idempotent jobs and bounded
chain rules; Stow does not infer a causal graph from watcher hints.

## Restart and portability

Store schema-versioned state under reserved workspace bookkeeping, with rooted access
and coordinated writes. Reopening the same workspace retains subscriptions, events and
acks and observation baseline. Destroy/collection owns cleanup; close stops observation
owned by that handle and does not delete the journal. Observation needs an explicitly
held process lifetime; one-shot CLI commands do not secretly start a daemon.

Webhook destination/secret configuration and delivery receipts remain host-local. Restoring
a checkpoint must not silently activate network sends or treat old receiver acknowledgements
as current. Portability of notification history does not imply portability of webhook
credentials or authorization; destination reconfiguration is explicit on the recipient.

Initial support does not include notification capture/restore or cross-host handoff.
The subsequent [portable recovery direction](portable-recovery-plan.md) schedules
selected pending-operation portability under S1-9, with destination authorization
and explicit activation. Responsibility transfer and effect reconciliation remain
open decisions; this does not broaden the initial same-workspace implementation.
Existing `.stow` exclusions remain in force. Before adding portability, define selected
notification history as a versioned checkpoint section, byte/count admission and old-
reader behavior. Adoption must create a new stream identity and retain origin provenance;
it must not silently re-execute old effects or restore callback/process handles. Consumer
replay and effect reconciliation on a new host remain explicit. Reuse existing capture/
archive publication, not a second snapshot format.

## Ordered implementation slices and evidence

1. **Freeze the change detection contract.** Specify observation scope, baseline admission,
   create/modify/delete semantics, coverage, watcher/fallback platforms and scan budgets,
   alongside durable event schema, limits, authority, replay and commit outcomes. Keep
   existing S3 notification configuration unsupported; add workspace operation/error types.
   Define the selected Protobuf schema and generated ProtoJSON mapping, semantic replay
   identity, required-capability refusal and the configurable 64 KiB encoded-body default.
2. **Build the shared observer and journal.** Implement content reconciliation, persisted
   baseline/event publication, subscribe, pending and ack in Go with confined storage.
   Verify external and facade edits, new prefix files, deletion/atomic replacement,
   missed watcher hints, unstable reads, failed sync, journal exhaustion, multiple
   observers/publishers and restart before/after baseline/event commit. Explicit publish
   uses the same journal and retains request retry/conflict semantics.
3. **Expose the existing delivery surfaces.** Add bounded JSON workspace CLI operations
   and thin TypeScript wrappers. Verify real CLI round trips and a fresh process resuming
   pending work. Provide an explicitly held observation command/API with cancellation
   and clear lifecycle ownership. Document the supported surface; browser/WASM and Python
   expansion follow consumer need and their actual persistence model.
4. **Add optional webhook delivery.** Use the shared event source with host-local endpoint/
   secret configuration, signed POSTs, durable claims/acknowledgements and bounded retry.
   Verify exact/ancestor/root subscription deduplication, signature/timestamp checks,
   receiver deduplication, retry after timeout/429/5xx, retained terminal failure, destination
   configuration changes, cancellation and crash after receiver acceptance. Use local
   fake receivers and two destinations to prove one failure does not stall the other.
5. **Use it in a concrete caller.** Demonstrate an external edit to a subscribed input
   notifying a consumer without an explicit report. Include an offline subscriber,
   reconciliation after downtime and interruption before/after acknowledgement. Preserve
   the multiplayer example's historical claims/ownership logic as caller code. Include a
   non-agent targeted validation fixture and an explicit artifact-ready report to prove
   change/readiness separation without requiring model execution. Route one parent-path
   subscription to a local webhook receiver that durably accepts a validation job.
6. **Qualify and document.** Run appropriate Go/race, wrapper/CLI and required standards
   checks at the implementation revision. State same-host restart and at-least-once
   limits, supported observation profiles and incomplete intermediate history clearly.
   Portable notification history remains a separately specified extension with a named
   consumer and meaningful failure tests.

The standalone collaboration repository remains independent. Any later adapter consumes
a pinned public Stow artifact and supplies its own decisions and harness integration.
No production CHS application is migrated by this plan.

The subsequent [protected-cache plan](protected-cache-plan.md) adds principal/resource
ACL requirements. Subscription/query scope and webhook dispatch must respect those
permissions, including rechecking retained deliveries after policy changes. Workers
adapters cannot assume native filesystem watchers or persistent process lifetimes.
