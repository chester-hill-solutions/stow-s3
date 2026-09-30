# Working-storage foundation review

> **Consolidated on 2026-09-29; reference only.** The [new working-storage plan](storage-foundation-plan.md) is the sole work order. Findings, delegated defaults and dependencies are absorbed into the new plan. The retained review is historical and schedules no independent work. Former checklists, phase numbers and next steps below do not schedule work.

**Status:** planning review, 2026-09-29. Accepted directions are recorded in the
linked plans and ADRs. The public conditional-write prerequisite is now implemented
locally; the broader extensions remain planned. The
[canonical plan](plan.md#dependency-order-for-the-accepted-foundation-extensions)
owns work order. This review covers workspaces, objects, checkpoints, transfer,
notifications and recovery as well as caching.

## Decision boundary

The user has delegated routine technical choices and requested questions only for
consequential choices that cannot be resolved from their goals or this repo. No new
user decision is pending. API names, error shapes, bounds, codec generation, test
fixtures and adapter qualification are engineering work. Record those choices with
evidence instead of repeatedly asking for agreement.

The accepted foundation remains portable storage. Callers declare what they read,
quiesce writers, decide readiness and react to notifications. Stow checks declared
storage identities and owns the supported persistence/retention/publication boundary.
It does not grow an agent executor, arbitrary task queue or collaboration engine.

## Defaults to carry into implementation

- Start with shared native runtime/store contracts. Extend existing conditional writes,
  authority, archive verification and durable receipts rather than duplicating them
  in wrappers. Qualify coordination scope; process locks cannot govern all host edits.
- A supplied [input basis](guarded-storage-plan.md#agreed-declared-input-checks)
  defaults to current-input validation at ready admission. Explicit snapshot mode
  permits intentional historical inputs. No basis means no freshness claim. Preserve
  saved drafts on refusal; never recompute or publish stale output automatically.
- Optional full text patches require explicit content inclusion. Prefer changed-line
  ranges/counts without line text for the fallback, plus explicit unavailable/omitted
  outcomes. Bound comparison work as well as encoded output. Use the accepted single
  Protobuf schema, generated ProtoJSON and 64 KiB complete-body default.
- Admit bounded recovery bookkeeping alongside each pending operation. New data cannot
  use that reserve or another namespace's reserve. Namespace quotas are ceilings;
  they do not promise future payload space. No payload overcommit or reserve borrowing
  in the first supported profile. Physical headroom requires adapter evidence.
- Restored operations start inactive. Keep external effects paused when current
  ownership cannot be proved. Backup decryptability, source unreachability and copied
  local claims confer no authority to send. Do not introduce forced takeover.
- Keep encrypted persistence opt-in with explicit plaintext-workspace capabilities.
  Normal key rotation preserves required older data until verified re-protection or
  authorized removal. Recipient/recovery-key export uses the verified archive inside
  an authenticated envelope; key custody stays with the host/custodian.
- Protected origin reads distinguish authorization denial from network unavailability.
  Stale use needs an explicit qualified policy; expiry cannot silently reset on reopen.
  Read-only edge caching is the initial deployment profile. Native/Railway and Workers
  publish separate supported-operation matrices.

## Remaining qualification gaps

These are implementation gates, not claims of reproduced defects in the baseline.
Existing source observations are recorded in the detailed plans.

| Gap | Required resolution/evidence | Existing owner |
| --- | --- | --- |
| Fresh identity and publication races | Public Go/embedded options now expose existing conditional-write fields with support negotiation and typed refusal. Define richer comparison semantics and enforce guarded defaults/input checks inside supported core admission. A cached ETag or wrapper-side check is insufficient for arbitrary host edits. | S1-7, S1-10, S1-6 |
| Lost replies and replay | Define committed, refused and uncertain outcomes separately from conflict/denied/blocked. Reconcile by retained operation/request identity; expired or missing receipts do not prove an effect never happened. Reuse checkpoint-request receipts where applicable. | S1-10, S1-6, S1-9 |
| Cleanup and reserve | Current registry limits are not unified namespace budgets or pending-operation holds. Persist the smallest required dependency set, fail closed on unreadable protection records and test cleanup races. Prove completion/ack/release at a full logical budget and actual backing-store pressure. | S1-4, S1-9 |
| Source ownership after restore | Local outbox claims coordinate shared state, not independent copied stores. Bind release/activation to operations, snapshot and recipient; qualify shared coordination or receiver-enforced fencing against two restored hosts and stale backups. Until then external work stays paused. | S1-9 |
| Recovery payload completeness | Upload IDs do not carry remote part bytes; download offsets do not carry verified prefixes. Specify eligible transfer, upstream-outbox and notification sections with their owners. Unknown required/security-relevant sections refuse; optional-field compatibility must not discard required protections. | S0-4, S1-5, S1-9 |
| Encryption across the lifecycle | Inventory records, metadata, temp files, multipart payloads, checkpoints, outbox and retained notification versions. Qualify bounded range reads, rotation interrupted by crashes, emergency revocation and protected export/decrypt staging. No plaintext staging by implication. | S1-8, S1-9 |
| Authorization through delayed work | Recheck current grants for warm reads, list/copy/capture, both sides of a diff and pending delivery. Whole-report revocation pauses dispatch without changing identity. Restoring an old policy/key snapshot cannot grant current destination permission. | S1-7, S1-6, S1-9 |
| Durable delivery and backpressure | Persist observed baseline/event state consistently; expose observer gaps rather than dropping them silently. Delivery acceptance stops retries; explicit artifact release ends only its hold. Bound journal/history/retries and protect saved output until release/discard. | S1-6, S1-4 |
| Resource bounds and visibility | Bound upstream fills, comparisons, list/index work, concurrency and encrypted memory peaks, not only inbound body size. Provide paged, permission-safe inspection of blockers/holds/usage and actionable release/discard operations. Metrics must not disclose secrets or another namespace. | S1-5, S1-4, S1-6, S4-4 |
| Host capabilities and upgrades | Prove supported native/Linux runtime, Railway volume lifecycle and Workers execution/persistence independently. Define old-reader refusal, required capabilities, key/policy migration and rollback before claiming protected operation across upgrades. | S2, S4-4, S1-7, S1-8 |

## Smallest useful proof sequence

1. Use two real native consumers to read/edit/save one resource. Prove stale-save
   refusal, then save a draft against declared inputs and refuse ready publication
   after an input changes. Preserve drafts and permission-safe errors.
2. Publish an immutable ready output, delay delivery through later edits and restart,
   accept it durably, retrieve the saved version and explicitly release only that
   consumer's hold. Include revoked scope and encoded diff-size boundaries.
3. Fill a namespace with protected pending work. Refuse new payloads, finish admitted
   bounded bookkeeping and reclaim only released/disposable data. Verify a second
   namespace and actual adapter failure behavior.
4. Interrupt upload/download/upstream propagation separately. Reconcile lost replies
   on the original store before qualifying selected portable sections. Import without
   external effects; test source restart, duplicate recipients and old backups before
   admitting cross-host activation.
5. Exercise independent namespace keys, interrupted rotation and an authorized
   recipient/recovery-key bundle on a fresh host with the original provider absent.
   Decryption must not restore permissions or external-send ownership.
6. Run deployment-specific denial/outage/freshness/redeploy/size scenarios with
   synthetic data. Record revision, configuration and unsupported operations; the
   Workers spike can start early to expose shared-core/persistence limits.

Existing release, live-provider and independent-consumer gates remain separate from
these future extension proofs. A local test pass or deployment template is not a
production qualification. The next design work is to define the common identity and
admission contract, followed by these narrow slices in canonical dependency order.
