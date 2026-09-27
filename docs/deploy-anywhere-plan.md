# Deploy-Anywhere Plan

**Status:** Phases 0–5 done. Phases 6–7 are not started.
**Date:** 2026-09-27
**Baseline:** `main` at `4ca2c1a`
**Relationship:** additive to the accepted decisions in ADRs 0005–0011, `docs/compat-contract.md`, and `docs/workspace-contract.md`. This plan is the current execution order for the *portability and multi-writer* workstreams, and it sequences the MCP adapter that `docs/agent-workspace-plan.md` Phase 5 item 3 defers. It does not supersede that plan's workspace phases; the two run in parallel and this one is ordered first where they touch the same code.

## 1. Goal

Make stow the S3 implementation for anywhere it can be deployed, and make its
eventual-consistency model safe for more than one writer.

Three properties, in the order they bind:

- **Immediacy** — an object store that starts in tens of milliseconds and needs
  no process, no container, and no network hop to the first operation.
- **Deploy anywhere** — the same object model on a V8 isolate, a browser, a
  Raspberry Pi, a gateway, and a microcontroller.
- **Eventual consistency** — local is authoritative, upstream converges
  asynchronously, and *concurrent writers are detected rather than silently
  resolved*.

The third is the one currently missing, and it is the one that decides whether
the first two are safe to build on. A store that starts instantly and deploys
anywhere but loses another writer's update is a fast way to lose data.

## 2. What Phase 0 measured

`scripts/wasm-isolate-probe.mjs` boots the committed wasm artifact in a fresh V8
realm built with `vm.createContext`, containing only Web-standard APIs. No
`require`, `Buffer`, `module`, or `setImmediate`. It then drives a real workload.
`scripts/wasm-isolate-probe.test.mjs` asserts the properties that would change
the design if they stopped being true.

| Finding | Number | Source |
|---|---|---|
| Boots and serves operations with no Node APIs | yes | probe `booted: true`, all ops `ok` |
| 1 MiB object round-trips byte-identically | yes | probe `roundTripMatches: true` |
| Stored bytes match what was written | 1049576 B / 201 objects | probe `usage` |
| Host surface required | **22 functions, 1 module** (`gojs`) | binary import section |
| Import modules beyond `gojs` | none — no `fs`, no `net`, no `Date` | binary import section |
| Bundle size | **5.15 MiB** | committed artifact |
| Linear memory after boot, zero objects | **8 MiB** | probe `afterBootMiB` |
| 200 small objects | **+0 MiB** | probe `miBPer200SmallObjects: 0` |
| Store a 1 MiB object | **+4.5 MiB** | probe `miBPer1MiBObject` |
| Read that object back | **+7.5 MiB** | probe `afterReadMiB` |

Those two rows are the large-object case. The agent case is many small reads, and
it is not measured yet — Phase 2 measures it before the transport is changed on
the strength of numbers from the large-object case alone.
| Cold start to first operation (Node, 7 runs, median) | ready ~45 ms, first op ~16 ms, total ~62 ms | `wasm-coldstart.mjs` |

### Two premises that changed

**The bundle-size blocker is gone.** Cloudflare removed its compressed-bundle
limits on 2026-09-04; the gate is now 64 MiB uncompressed on free and paid, and
5.15 MiB is 8% of it. `docs/driver-facade-plan-status.md` records the older
"4 MB is irreducible" framing, which is now obsolete and is corrected there.

**The binding constraint is the transport, not the size.** The bridge carries
object bytes as base64 inside a JSON string. A 1 MiB object costs 4.5 MiB of
linear memory to store and 7.5 MiB more to read back, because the bytes are
encoded, copied across the JS boundary, and decoded again. On a 512 MB machine
that is noise. On a device with hundreds of megabytes it is the whole budget.

## 3. The defect that makes the consistency claim unsafe

`internal/runthrough/outbox_adapter.go` `propagateWrite` performs two checks and
then overwrites unconditionally:

1. the **local** version still matches the outbox entry, else
   `ErrOutboxVersionConflict`;
2. on reconcile, `HeadObject` upstream, and if the ETag **equals** the local
   ETag, treat the write as already propagated and skip it.

The ETag comparison is a crash-dedup test, not a precondition. If upstream has
moved on, the write proceeds and the upstream object is replaced with no signal
to either party.

Concretely, for an agent and a device sharing keys: a device writes K directly
upstream; stow holds a stale local copy; an agent writes K; propagation sees an
ETag that differs from the agent's new one, concludes the write has not landed,
and overwrites the device's object.

`docs/compat-contract.md` §6.2 is honest that a merged listing is a union of two
separately-timed reads and that a key may appear in two pages or neither. It says
nothing about lost writes, because there is no story for them yet. The workspace
contract states last-writer-wins per key for concurrent writers on one host
(`docs/workspace-contract.md` §9); that is a different situation from two
independent writers converging, and the two should not be conflated.

**The primitive already exists.** `storage.PutOptions` carries `IfMatch` and
`IfNoneMatch` (`internal/storage/types.go`), and the S3 surface honors them
(`internal/s3api/handlers_bucket.go`). `outbox_adapter.go` references neither.
Phase 1 is therefore a use of existing capability, not a new mechanism.

## 4. Phases

### Phase 1 — Detect concurrent writes on propagation (P0)

1. Record the upstream ETag observed at enqueue time in the outbox entry, using
   the existing per-key ordered record. Do not add a second index.
2. Propagate with `IfMatch` set to that ETag. A precondition failure becomes a
   terminal outbox entry carrying a distinct conflict code, not a retryable
   error: retrying an `If-Match` failure cannot succeed without a new decision.
3. Define what a conflict means for each policy. Under `mirrorWrites` the
   caller's local write is authoritative and must not be silently reverted, so
   the conflict is recorded and surfaced rather than resolved. State this in the
   compatibility contract; §6.2's consistency paragraph is amended, not
   replaced.
4. Keep the existing reconcile equality check for crash dedup. It is correct for
   what it does and is not the conflict mechanism.
5. Cover: unchanged upstream propagates; upstream changed by another writer
   conflicts; the same write replayed after a crash does not conflict; a delete
   racing a write conflicts.

**Reuse before building.** The browser persistence path already implements
compare-and-swap conflict detection, and it is the model to conform to rather
than a mechanism to reinvent alongside:

- `packages/stow-s3/src/browser-types.ts` — `PersistenceAdapter` is an interface
  whose `commit` takes `(namespace, expectedGeneration, changes)`.
- `packages/stow-s3/src/indexeddb-store.ts` — the generation check runs inside a
  `readwrite` IndexedDB transaction, so the compare-and-swap is atomic, and a
  mismatch throws `BrowserPersistenceError("persistence_error", ...)`. It is a
  refusal, not a merge.
- Quota validation and a lock manager sit alongside it.

That surface detects a conflict at write time on the local store, by refusing a
stale write. The run-through path overwrites *upstream* with no check at all, so
this is a different position and Phase 1 is not redundant. But the shape is the
same problem solved once already, on the surface closest to the edge, and the two
must be reconciled rather than coexisting. Concretely: prefer the generation-
counter pattern where a store can carry one, keep `If-Match` where the target is
a foreign bucket that cannot, and say in the contract which mechanism applies
where. Two unreconciled conflict vocabularies is the failure mode ADR 0010
already names for permissions — a set that can grow without anyone noticing.

**Exit:** a second writer's update is never silently overwritten; the conflict is
visible in the outbox and mapped to a stable error code, using the same refusal
vocabulary as browser persistence. ADR 0005's consent rules and ADR 0011's
local-default rule are untouched.

### Phase 2 — Shrink the runtime floor, not the bridge (P0, revised by measurement)

**Measured on 2026-09-27, and the plan's original premise was wrong.**
`scripts/wasm-agent-workload.mjs` runs an agent-shaped workload against the
committed artifact in the same isolate harness: 24 files totalling 597 KB, seeded
the way `workspace prepare` seeds a repository, then read back, then six
read-modify-writes. `scripts/wasm-agent-workload.test.mjs` pins the properties.

| | Measured |
|---|---:|
| Read, whole working set | 33.5 ms total, **1.4 ms/file**, p50 0.77 ms, p95 3.6 ms |
| Linear memory committed by reading | **0 MiB** |
| Seed cost, high-water | **7.9× the payload** |
| Post-boot floor | **8 MiB, 64% of total footprint** |
| Final footprint | 12.5 MiB for a 597 KB working set |

What this changes:

1. **Reads are not the problem, so a transport change would not help them.** A full
   pass over the working set commits zero new pages — the memory is already there
   from the seed — and a read costs about a millisecond. An agent's compounding
   per-call cost is therefore not dominated by the bridge. The original P0, "add a
   binary transport", was ranked on two 1 MiB-object numbers that do not describe
   this workload.
2. **The fixed floor is the largest single term**, at 64% of the footprint. That
   makes runtime size the first lever, not the bridge.
3. **Writes do pay the transport**, at 7.9× the payload. That is the one place a
   binary path helps, and it is paid at `prepare` time rather than per turn, so it
   is a seeding cost rather than a latency one.

So the phase is reordered around what the numbers say:

1. Attack the floor. A Go build of the embedded path with the reflect allocator
   and without the parts of the standard library it does not use is the obvious
   first attempt; 8 MiB is the number to beat and the artifact is 5.15 MiB on disk,
   so the floor is not simply the file size. Measure before assuming the win.
2. Keep the binary transport, scoped to the **write** path, where the 7.9× is
   real. `workspace prepare` seeding a large repository is the case that hurts.
3. Leave the read path alone. It commits no memory and costs about a millisecond;
   changing it would be work against a number that does not justify it.

Two honest limits on the figures. Linear memory is a high-water mark and does not
shrink, so 7.9× measures the peak the write path reached, not a steady-state cost
per stored byte; the read phase is what bounds the steady state, and it commits
nothing. And these are Node timings, not isolate timings — a real isolate bills
CPU, and the free Cloudflare plan allows 10 ms, which this workload's 33.5 ms read
pass would exceed. A paid plan allows five minutes and has no issue. That is a
deployment-cost fact rather than a runtime defect, but it decides who can use this.

**Exit:** the post-boot floor is below 8 MiB or the reason it is not is written
down; the seed path's multiple of payload is reported after any transport change;
the read path is measured again to confirm it still commits nothing.

### Phase 3 — Make the cache an incremental eviction index (P0)

**The cache is the deploy-anywhere story, and it is already built.** This phase
exists because the thing that makes the cache worth configuring is also what makes
it expensive, and the measurement is in `internal/runthrough/cache_scaling_test.go`.

The cache is bounded by bytes, object count and TTL, with eviction that does not
touch local writes, and revalidation is on by default
(`internal/runthrough/config.go` `Revalidate: true`). On any upstream error a
cached read falls back to the cached copy
(`internal/runthrough/adapter.go` `revalidateCachedObject`), so a host with no
connectivity keeps serving. That is the "execute on nothing" property, and it does
not need a local persistence backend: upstream is durable, the device holds a
bounded working set in RAM, and a disconnected host still reads.

The cost is in the eviction scan. `evictCache` calls `collectCacheCandidates`,
which lists every bucket and walks every object in the cache
(`internal/runthrough/cache_policy.go`). Its only caller is `trackCacheObject`,
whose only caller is `refreshFromUpstream` — so the scan runs on every
changed-object refresh, not on every write. Measured, with a byte limit
configured:

| Refreshes | Cache depth | Cache rows examined |
|---:|---:|---:|
| 200 | 200 | **0** — no limit configured, `evictCache` returns early |
| 40 | 40 | 1600 |
| 40 | 160 | 6400 |

That is exactly `refreshes × depth`: every refresh walks the whole cache, so
refreshing N changed objects costs O(N²), and a wall-clock run put it at 4.95× for
a 4× deeper cache. The limits are what turn the scan on, so **configuring the
cache is what introduces the cost** — on precisely the constrained host where the
cache is most wanted.

1. Track cached bytes and object count incrementally, updated on insert, on
   eviction and on delete, rather than rediscovered by listing. The adapter
   already keeps a `cacheEntries` map with access and expiry times
   (`cache_policy.go:12`); the accounting belongs beside it.
2. Evict from that index. The scan then costs nothing per read, and the
   `rowsExamined` assertions in the test become the regression guard: they should
   fall to zero, and the test is written to fail loudly when they do not.
3. Keep the scan as a reconciliation path, not the hot path — a debug or repair
   entry point, so a counter that has drifted can be rebuilt.
4. Do not change the eviction *policy* while fixing the *mechanism*. Oldest-access
   first, TTL, and never-evict-local are existing tested behavior.

**Exit:** a refresh examines zero cache rows; the byte and count limits are still
enforced; the existing eviction and TTL tests stay green unchanged.

**Done.** A refresh reads nothing from the store, down from refreshes x depth. The
test that pinned the defect asserted the count *grew* with depth, so it failed when
the count fell to zero — which is the intended direction, and the reason the
assertion was rewritten to pin "plans from the index" rather than "reads a
predictable amount". `ReconcileCacheIndex` and `CacheIndexEntries` exist so the
index-versus-store agreement is a test rather than a claim, including the drift case
where an object is removed behind the adapter's back.

### Phase 4 — A persistence seam, only if a target is named (P2, demoted)

Three backends exist: `memory` (ephemeral), `fs` (POSIX), and `workspace` (needs a
directory). A microcontroller has none of them, and `storage.Store` is the right
seam with no implementation behind it for that case.

This was Phase 3 in the first draft of this plan, on the assumption that a device
needed local durability. Phase 3 above is the correction: the cache makes local
durability optional, because upstream is the durable store and a disconnected host
still serves from RAM. So this is demoted and conditional.

1. Name the target before writing code — flash-backed KV, a block device, or a
   caller-supplied persistence callback are different designs with different
   failure modes.
2. Whatever is chosen must pass the same behavioral contract suite as the other
   backends, including the same-bytes gate, with no backend-specific exemption.
3. Record the durability and ordering it can actually keep. A store that loses the
   last write on power loss is legitimate if it says so.

**Exit:** a named target, a backend implementing `storage.Store`, green against
the shared contract suite, and its guarantees written down.

### Phase 4b — Name the browser path as the edge deployment (P1)

The tension between the workspace flagship and a filesystem-less host is real but
already has an answer, and it is shipping. The workspace backend stores objects as
real files at key-derived paths
(`internal/storage/workspace/workspace.go` `New` requires a `Root` and creates
it), so it needs a filesystem. A Worker isolate has none.

The browser path already demonstrates a durable agent object store with no
filesystem: `BrowserEmbeddedStow` with an `IndexedDbPersistenceAdapter`, a
generation-checked commit inside a readwrite transaction, quota validation, and a
lock manager. That is the "execute on nothing" agent surface, and it is tested.

1. State it plainly in the docs: for a filesystem-less host, the browser/persistent
   embedded path is the deployment, and the workspace backend is not available.
   This is currently implied and never said.
2. Reconcile the vocabulary. Two persistence models, two conflict vocabularies,
   and two capability reports is how a caller cannot tell which guarantee they
   have. Decide what a capability means on a host with no filesystem rather than
   reporting `false` for something that is merely unavailable.
3. Note the honest limits: an isolate is ephemeral, so a durable workspace is not
   possible there, and the workspace contract already rules out a network relay.
   Do not let "deploy anywhere" imply a workspace that cannot exist.

**Exit:** the documentation says which surface runs where, and a caller can tell
from the capability report what guarantee they actually hold.

**Done**, in the README, the workspace contract's delivery table, and on the
capability in Go, TypeScript and Python. The boundary is stated rather than
smoothed: a workspace needs a real directory, so a filesystem-less host gets the
embedded profile with a persistence adapter; an isolate is ephemeral, so durability
there is the platform's storage and a process-outliving workspace is unavailable.

No wire shape changed, because `backend` already carried the distinction — the fix
is telling a caller which field to read. A tri-state capability would put
"unavailable" and "disabled" in the payload itself, and that is a protocol
decision left open rather than made silently here.

### Phase 5 — Delta sync as a first-class operation (P1)

The checkpoint diff already computes add/change/delete
(`stow-s3 workspace diff`). That is most of a sync primitive, currently framed
as producing a reviewable artifact. For an agent and a device sharing a working
set, sending a delta instead of a bucket is what makes bandwidth cost irrelevant.

1. Promote the diff's output into a reusable delta document with a version, so it
   is not specific to checkpoints.
2. Define the apply direction and its conflict rule, reusing Phase 1's mechanism.
   A delta applied to a diverged target is the same problem as Phase 1, and must
   not grow a second answer.
3. Bound it: the same size and file caps the archive path already enforces.

**Exit:** a caller can compute a delta between two checkpoints and apply it to a
third, with conflicts surfaced rather than resolved.

**Done**, for checkpoints in one registry — the diff already computed the three
change kinds, so what was missing was the content and a verified base. The conflict
rule reuses Phase 1's mechanism rather than inventing one: every precondition is
checked before any of them is written, because a delta applied part way leaves a
target matching neither end and nothing reports it. An "added" path asserts absence,
the same shape as an `If-None-Match` of `*`, so `ErrDeltaConflict` is the same
refusal in the same words.

What is not done is the wire half. A delta today is produced and applied inside one
registry. Carrying one between machines means the archive transport, and
`EncodeDelta`/`DecodeDelta` exist for it but are not yet wired to a command, so
nothing moves a delta off the machine yet.

### Phase 6 — SSE-S3 wire semantics (P1)

Independent of the above and useful on every backend, including a local server.

1. Accept `x-amz-server-side-encryption: AES256` and record it; echo it on read.
   The contract currently rejects SSE headers outright
   (`docs/compat-contract.md` §unsupported, `internal/s3api/dispatch.go`), and
   many SDKs, Terraform and boto3 send them by default, so stow answers `400`
   to ordinary traffic.
2. Decide whether stow encrypts. Accepting the header and recording it without
   encrypting is defensible if the documentation says so plainly; claiming
   encryption stow does not perform is not. **Label this as wire compatibility,
   not as a security control.**
3. This does not protect a stolen device or an imaged flash card. At-rest
   encryption of the persistent backends is a separate piece of work with a
   separate threat model, and is explicitly out of scope for this phase.

**Exit:** an SDK that sends `AES256` is answered rather than refused, the
contract states exactly what was and was not done, and no document implies a
guarantee that does not exist.

### Phase 7 — The surfaces this unblocks (P2)

Sequenced last, and deliberately:

1. **The MCP stdio adapter**, which `docs/agent-workspace-plan.md` Phase 5 item 3
   already defers. It is pushed, not dropped. An adapter built before Phase 1
   would expose multi-writer semantics that silently lose writes, and would just
   be a faster route to that failure. It is also the surface agents actually
   drive, so it is last in build order and first in importance — which is the
   tension this plan resolves by putting correctness ahead of surface area.
2. **In-process TypeScript and Python workspace handles**, which the workspace
   contract's delivery table records as not yet shipped.
3. **A virtual-hosted corpus case**, which is the missing half of
   `docs/remediation-plan.md` open item 5. The client half of that item landed
   in `59568ca`; the server-side `STOW_BASE_HOST` and the corpus case remain.

## 5. Acceptance

- A second writer's update is never silently overwritten, and the conflict is
  reachable by a caller rather than only visible in a log. **Done** in `6f86e92`;
  the boundary is that a key stow has never read carries no precondition.
- The wasm runtime boots and serves operations in a realm with no Node APIs,
  asserted in CI rather than measured once. **Done** in `4ca2c1a`.
- An agent-shaped workload has a measured latency and memory profile against the
  committed artifact, and the transport decision follows it. **Done**; the answer
  was that reads are cheap and the floor dominates, so the phase was reordered.
- Every persistence backend, including a new one, passes the same behavioral
  contract suite as the memory and filesystem backends.
- A changed-object cache refresh examines zero cache rows, and the byte and count
  limits are still enforced. **Done.**
- A caller can tell, from the capability report alone, which of the workspace and
  persistent-embedded surfaces their host supports and what durability each gives.
  **Done**, by naming `backend` as the field to branch on and stating why
  `persistent` cannot answer it.
- `make standards` and `make test-all` green; no ratchet baseline grows.
- Every consistency claim in `docs/compat-contract.md` matches a test.

## 6. Risks

- **The wasm protocol bump is the expensive step, not the transport work.** The
  retired facade plan measured this at 0.73 confidence and sequenced it early
  for that reason. Two releases were previously broken by the `check-generated`
  byte-diff for reasons unrelated to the source. It is now scoped to the write
  path, so it is smaller work than the original phase implied — but the bump is
  still the risk, not the code around it.
- **The floor may not be reducible.** 8 MiB of linear memory for a runtime whose
  artifact is 5.15 MiB suggests the difference is allocator arena and Go runtime
  overhead rather than reachable code. If a reflect-allocator build does not move
  it, the honest answer is a documented floor rather than an optimisation, and
  that would change which devices are viable targets.
- **Adding `If-Match` to propagation changes retry behavior.** A precondition
  failure is terminal by nature; an entry that retries forever against a diverged
  upstream is worse than one that reports a conflict. The outbox's durable
  reconciliation must be extended, not bent.
- **"Deploy anywhere" is a spectrum, not a target.** The probe proves the V8
  isolate. A bare-metal microcontroller needs Phase 3's backend and a smaller
  Phase 2 floor than the 8 MiB measured here. Treat the two ends as separate
  claims with separate evidence.
- **Phase 3's fix changes eviction from list-driven to index-driven, and the
  index can drift.** A counter that disagrees with the store would enforce the
  wrong limit, which fails silently. The reconciliation path in step 3 exists for
  that, and the eviction and TTL tests must stay green through the change.
- **Phase 4 is a design decision wearing an implementation task's clothes, and it
  is now conditional.** The named target has to be chosen first; a backend built
  for the wrong flash abstraction is wasted. It is demoted because Phase 3 makes
  local durability optional, not because it stopped mattering.
- **SSE is easy to overclaim.** The wire header is not the security property, and
  a document that blurs the two is worse than no SSE at all.

## 7. Not in this plan

- The remote workspace service, shared daemon, network relay, automatic upstream
  promotion, arbitrary agent execution, OS-level sandboxing, global internet
  denial, and Git replacement. All are out of scope in
  `docs/agent-workspace-plan.md` and stay out of scope here.
- At-rest encryption of the persistent backends. Named in Phase 5 as explicitly
  separate, with a different threat model.
- A kernel filesystem or FUSE mount. The workspace contract already rules this
  out: the S3 facade is the integration point, and a kernel mount would be a
  second one.
