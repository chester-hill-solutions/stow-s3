# Remediation plan: code evaluation findings

Current core correctness status: the [bounded closure register](reviews/2026-10-18-correctness-closure.md)
evaluates nine historical leads: two reproduced fixes remain, six leads are
closed, and one is optional strictness. Older follow-ups below are historical;
they do not imply the current queue is empty.

**Date:** 2026-10-14. **Baseline:** `main` @ `9cb61c2`, tag `v0.3.1`,
`internal/version/version.go` = `0.3.1`, `stow-s3 0.3.1`.
**Source of findings:** independent code evaluation (not the project's own
docs). Two findings were reproduced with throwaway `-race`/unit probes; the rest
are code-read findings. `go test ./...` is green at baseline, which is the point:
these defects live in paths the current suite does not exercise.

**Goal:** close the confirmed correctness and safety defects without regressing
the security core, and add the tests and gates that would have caught them.

**Non-goals:** new features; the accepted-but-unimplemented ADR work
(notifications, scoped ACLs, encrypted cache); rewriting any subsystem. Every
change here is a bug fix, a missing test, or a documentation correction.

---

## 0. Implementation status (2026-10-14)

Implemented on a working tree over `main` @ `9cb61c2`. Every fix has a
regression test that fails at the baseline; `go test ./...`, `go test -race` on
the changed packages, `go vet`, the file-size ratchet and the Go-quality ratchet
are green.

| ID | Status | Where |
|---|---|---|
| WS-1 | **Done** | `RLock` on `ListObjectsV2`/`Path`, `Lock` on bucket create/delete; `workspace/concurrency_test.go` fails under `-race` before the fix |
| WS-2 | **Done** | composite ETag recorded on the entry + `Multipart` marker; contract test asserts `HeadObject` equals the completion ETag on all three backends |
| WS-3 | **Done** | terminal entries no longer order a key; provenance refreshed after a deterministic failure; non-conflict failures are recorded, not surfaced, at the caller boundary |
| WS-4 | **Done** | `CopyOptions` carries the date conditions, evaluated with RFC 9110 precedence |
| WS-5 | **Done** | `ExpectedSHA256` on `CheckpointArchiveOptions`, `--expect-sha256` on `workspace adopt`, `expect_sha256` on the MCP tool |
| WS-6 | **Done** | `prewarm` refuses without an explicitly configured upstream |
| WS-7 | **Done** | OS advisory lock (`flock`/`LockFileEx`) held for the store's lifetime, replacing check-then-remove recovery; multiprocess regression test |
| WS-8 | **Done** | released operations no longer charged or counted; released entries pruned |
| WS-9 | **Done** | loopback admin trust withdrawn when a forwarding header is present |
| WS-10 | **Done** | list page size clamped to 1000 centrally |
| WS-11 | **Decision needed** | cross-language session backend unchanged (see below) |
| WS-12 | **Done** | XML declaration emitted on every XML response |
| WS-13 | **Done** | `resumeLimit` narrows an unlimited cap |
| WS-14 | **Done** | `registry_bound.remaining` clamped; import/handoff counts report the payload count |
| WS-15 | **Partially done** | four leads fixed (see below); the rest documented |

### What WS-3 actually was

The mechanical fix (stop terminal entries ordering a key) broke the conformance
test `TestMirrorWritesDoesNotTellTheCallerTheObjectIsMissing`, which encodes that
a *retry* after a terminal failure returns success. The two are reconcilable only
if a non-conflict deterministic propagation failure is recorded rather than
surfaced to a direct mutation call. So the fix is: un-block the key, refresh
provenance after the failure, and suppress the error at the mutation boundary
(`callerFacingPropagationError`) while the retry worker still reports it. A
second ordering predicate, `outboxState.firstForKey`, also counted terminal
entries and had to be fixed alongside `isPendingForKey` — two copies of one rule,
which is the pattern the codebase warns about elsewhere.

### Follow-up round: the three external findings

1. **Store lock (was WS-7).** The rename-based reclaim was still check-then-act.
   Replaced with an OS advisory lock held for the store's lifetime
   (`flock` on unix, `LockFileEx` on Windows, refused on targets with neither).
   The kernel releases it on exit, so there is no stale state to recover and no
   two-owner window. `internal/storage/fs/fs_lock_test.go` covers both
   single-process exclusion and a real second process.
2. **Upstream read memory (`refreshFromUpstream`).** The body was read with
   `io.ReadAll` before any cache accounting, so the cache limit was not a
   peak-memory bound. The upstream reader is now streamed straight into the cache
   store (disk-staged on the filesystem backend); when the caller needs the body
   it is read back from the cache, falling back to a re-fetch only if the copy was
   cached and then evicted.
3. **Filesystem listing.** `ListObjectsV2` ignored its context, so a cancelled
   listing walked the whole bucket. The context is now threaded through the walk
   and checked per entry. The larger concern — a walk per page and an unbounded
   prefix scan — needs an ordered index and genuinely incremental pagination,
   which is a design change rather than a fix and remains open.

### WS-15 leads: fixed and open

Fixed:

- **`Workspace.Path` symlink escape.** `locate` follows directory entries and
  `os.Stat`, so a key reached through a symlinked ancestor reported a path
  outside the workspace that `GetObject` refuses. `Path` now confirms the
  candidate through the same no-follow open the read paths use.
  `path_symlink_test.go` covers it.
- **`runtime.UploadPart` buffers before the quota check.** The whole part was
  read with `io.ReadAll` before the budget was consulted, so an oversized part
  could exhaust memory before being refused. The read is now bounded by the
  session byte budget.
- **`touchCache` could create a zero-valued index entry.** A cache hit for a key
  with no index entry recorded only `accessedAt`, so eviction planning from it
  could try to delete the empty key and undercount the bytes. The hit path now
  records bucket, key and size.
- **`embedded` batch delete failed on a missing key.** It now treats
  `ErrObjectNotFound` as a confirmed delete, matching S3 and the built-in
  backends.

Open (needs a decision or a larger change, not a mechanical fix):

- **`COPY … REPLACE` reads the whole source into memory** at the S3 layer, but the
  runtime's `PutObject` buffers non-owned readers anyway, so a real fix needs a
  streaming `PutObject` path rather than a change at one layer.
- **`policystore.Store.Policy(env)` ignores `env`**, so the documented read-side
  validation does not run. Authority cannot actually widen (`Allows` intersects
  with the environment), but the contract and the parameter disagree.
- **`RecoveryHold.ExpiresAt` is persisted but never enforced.**
- **`EffectiveWritePolicy` ignores the resolved `Authority`**, so status can
  report propagation a narrowing grant forbids.
- **SigV4 leniencies** (case-insensitive ETag and `UNSIGNED-PAYLOAD`, only the
  first value of a repeated signed header, canonical query sorted by decoded
  keys). All fail closed or are unreachable with ordinary SDK clients.
- **`ListObjectsV2` still walks per page**; the ordered index remains open.

### WS-11 decision

`packages/stow-s3` starts a session with `backend: memory`;
`packages/stow-s3-py` uses `backend: filesystem`. They must be made to agree, but
which is correct is a product choice, so it is left open rather than guessed.

### Ratchet note

The Go-quality ratchet gates total comment lines with a two-sided floor: a rise
is new debt and a fall is an unrecorded improvement. The fixes here therefore
carry only the comment lines needed to keep the count exactly at the baseline.
A maintainer who wants more prose on these changes must pay it down elsewhere or
record a new baseline deliberately.

---

## 1. Release strategy

| Class | Fixes | Target |
|---|---|---|
| Crash / data correctness | WS-1 (lock race), WS-2 (multipart ETag), WS-3 (outbox wedge), WS-4 (copy preconditions) | **0.3.2** (patch) |
| Safety / integrity hardening | WS-5 (handoff digest), WS-6 (prewarm guard), WS-7 (fs lock TOCTOU), WS-8 (capacity release) | **0.3.2** (patch) |
| Observable behavior change | WS-9 (admin trust), WS-10 (list caps), WS-11 (session backend parity) | **0.4.0** (minor) |
| Cosmetic / additive | WS-12 (XML prolog), WS-13 (resumeLimit), WS-14 (handoff/CLI nits) | **0.3.2** |
| Investigation | WS-15 (unreproduced leads) | unscheduled |

Rationale: the ETag and copy-precondition fixes change what a client observes,
but in both cases the new behavior is the correct one and the old behavior was a
bug, so they belong in a patch. Anything that changes an access decision or a
cross-language contract is a minor.

---

## 2. Finding register

| ID | Finding | Severity | Reproduced | Evidence | Batch |
|---|---|---|---|---|---|
| WS-1 | Workspace store read paths take no lock → data race / fatal map access | High | Yes (`-race`) | `workspace/workspace.go:34`; `objects.go:273,311,366`; `list_rooted.go:60`; `manifest.go:186-203` | 1 |
| WS-2 | Multipart completion returns composite ETag but persists MD5 | High | Yes (unit) | `workspace/multipart.go:240`; `objects.go:82`; `put_body.go:36` | 1 |
| WS-3 | Terminal outbox entry wedges its key permanently | High | No | `runthrough/outbox.go:297-309`; `outbox_adapter.go:338,466-476`; `adapter.go:350,354` | 1 |
| WS-4 | Copy-source date preconditions dropped when ETag condition present | Medium | No | `s3api/copy.go:85-95,165-170`; `storage/store.go:65-68` | 1 |
| WS-5 | Handoff integrity self-referential; no out-of-band digest | Medium | No | `pkg/stow/handoff.go`; absent from `cmd/stow-s3/`, `mcpstorage/` | 2 |
| WS-6 | `prewarm` builds client against real AWS with no upstream configured | Medium/Low | No | `runthrough/transport_policy.go:14-16`; `upstream.go:34-70`; `cmd/stow-s3/prewarm.go:111-119` | 2 |
| WS-7 | Stale store-lock recovery TOCTOU | Medium | No | `storage/fs/fs_lock.go:44-49` | 2 |
| WS-8 | Released capacity operations never reclaimed | Medium | No | `capacity/operations.go:89-108`; `accounting.go:86-96` | 2 |
| WS-9 | Admin read-only routes trust `RemoteAddr` loopback (proxy) | Medium | No | `s3api/server.go:318-329,343-350` | 3 |
| WS-10 | `max-keys`/`max-uploads` uncapped | Low/Medium | No | `handlers_bucket.go:94-99`; `storage/util.go:180-185` | 3 |
| WS-11 | TS session = memory, Python session = filesystem | Medium | No | `packages/stow-s3/src/session.ts:120`; `packages/stow-s3-py/src/stow_s3/session.py:260` | 3 |
| WS-12 | XML prolog dead code, never emitted | Low | No | `s3api/errors.go:258` | 4 |
| WS-13 | `resumeLimit` rejects narrowing from unlimited | Low | No | `pkg/stow/resume.go:134-145` | 4 |
| WS-14 | CLI/reporting nits (negative `remaining`, count messages) | Nit | No | `cmd/stow-s3/workspace.go:56-64`; `checkpoint_import.go:143` | 4 |
| WS-15 | Unreproduced leads (see §6) | — | No | sweep | 5 |

---

## 3. Workstreams

Each item: **Defect → Root cause → Fix → Tests → Acceptance → Risk/effort.**

### WS-1 — Workspace store locking (High, crash)
**Defect.** `Store.mu` is an `RWMutex` that is only ever taken with `Lock`; no
read path takes `RLock`. `ListObjectsV2 → listAll → escapedObjects` iterates
`objectIndex.Buckets` (`objects.go:366`) and `list_rooted.go:60` calls
`Index.entry`, while `putLocked` writes the same map (`objects.go:95`). A
concurrent list+put is a Go `fatal error: concurrent map read and map write`.

**Root cause.** The mutex was added for the write/adopt path (which mutates the
index) and read paths were assumed single-threaded. The runtime S3 facade hides
this because `Instance.mu` serializes its ops; the embedded `Workspace.Path`
(`pkg/stow/workspace.go:225`) and direct store callers do not.

**Fix.**
- Take `RLock` in `ListObjectsV2` (around `listAll`), in `Path`, and in
  `CreateBucket`/`DeleteBucket` (use `Lock` for the two mutating ones).
- Add `locateLocked` and use it from `DeleteObject`/`resolveLocked` so no
  lock-holding caller re-enters.
- Keep `resolve`/`record`/`peek` as write-locked (they may adopt and persist).
- Audit every `s.objectIndex` / `s.folded` access and classify read vs write.

**Tests.** A new `workspace/concurrency_test.go`: parallel `PutObject` +
`ListObjectsV2` + `Path` + `DeleteObject` under `-race` (the probe that caught
this is the seed). Add to `make test-race` coverage implicitly (already run by
`standards`).

**Acceptance.** `go test -race ./internal/storage/workspace/` clean with the new
concurrency test; no new `-race` reports anywhere.

**Risk/effort.** Low risk, small. The one trap is the non-reentrant mutex — the
existing `resolveLocked`/`peekLocked` split shows the pattern to follow.

### WS-2 — Multipart completion ETag (High)
**Defect.** `CompleteMultipartUpload` persists `prepared.ETag` = MD5 of the
whole assembled body (`objects.go:82`) and only overwrites the *returned* copy
with `CompletionETag(etags)` (`multipart.go:240`). Reproduced:
completion `"30dc…-2"`, `HeadObject` `"de62…"`.

**Root cause.** The composite ETag was added to the return value, not to the
stored entry, and the shared contract test only checks the returned ETag.

**Fix.**
- Thread an optional ETag override into the persist step (e.g.
  `putLocked(..., etagOverride string)` or a small `putLockedWithETag`), and have
  completion pass `storage.CompletionETag(etags)` so the **index entry** carries
  the composite.
- **Critical interaction:** `resolveLocked` re-derives an entry whenever
  `entry.ETag != derived.ETag` (`resolve.go:123`). A composite ETag legitimately
  differs from the derived MD5, so without a marker the next head would clobber
  the composite. Add a marker to `ManifestEntry` (e.g. `Multipart bool` or
  `CompositeParts int`) and make `resolveLocked` keep the recorded composite when
  the entry is multipart and not stale (`entry.stale(size, mtime)` is the
  existing staleness test).

**Tests.**
- Unit: after completion, `HeadObject` and `GetObject` return the same ETag the
  completion returned (the probe that caught this).
- Contract: extend the shared multipart completion contract test to assert
  `HeadObject().ETag == completion.ETag` for memory, fs, and workspace — the
  assertion that would have caught the divergence in all three.
- Conditional write: `If-Match: <completion ETag>` succeeds; a wrong ETag fails.

**Acceptance.** All three backends agree; contract test fails before the fix and
passes after.

**Risk/effort.** Medium — the re-derivation interaction is the subtle part.
Add a migration note only if `ManifestEntry` gains a field the loader must
tolerate (it is JSON with `omitempty`; old entries simply lack the marker, which
is correct since pre-fix multipart entries were wrong anyway).

### WS-3 — Terminal outbox entries wedge a key (High)
**Defect.** A deterministic failure sets `Terminal=true` and keeps the entry
(`outbox.go:303-307`). Terminal entries block ordering (`outbox_adapter.go:338,
466-476`), so a later `PutObject` commits locally, `completeIntentLocked`
no-ops (`adapter.go:350`), and `PutObject` returns success (`adapter.go:354`).
The key never propagates again; only an admin discard clears it.

**Root cause.** "Stop auto-retrying" and "block the key" were conflated. A
terminal entry is a dead letter; it should stop retrying and stop being an
ordering gate, while remaining visible for inspection.

**Fix (preferred).**
- Move deterministically-failed entries to a dead-letter set (`state.dead`) and
  remove them from the ordering set (`state.entries`). `Pending()` returns live
  entries only; add `Failed()`/`DeadLetters()` for inspection.
- New writes to a key therefore propagate with freshly recorded upstream
  provenance (a fresh `recordUpstreamState` read already happens on enqueue), and
  may conflict again — which is correct and visible.
- Expose dead letters in `/_stow/inspect` and the existing admin `discard` route;
  keep them durable so nothing is silently forgotten.
- Bump the outbox schema version and migrate: on load, terminal entries in the
  current version move to dead letters. Reject newer versions as today.

**Alternative (smaller, weaker).** Keep terminal entries in place but exclude
them from `isFirstPendingForKey`/`dueForPropagation`, and surface a warning on
the next write. Rejected because it silently leaves the failure invisible.

**Tests.** After a forced conflict: a subsequent `PutObject` to the same key
either propagates or conflicts again, and never returns success while staying
local; the dead letter is listed and discardable; migration of an old terminal
entry is covered.

**Acceptance.** No write returns success while its intent is silently blocked;
`upstream_conflict_test.go` gains the "next write propagates" assertion it lacks.

**Risk/effort.** Medium — schema bump + migration; the outbox already versions
its files, so the mechanism exists.

### WS-4 — Copy-source date preconditions (Medium)
**Defect.** `copySourceConditions` reads only if-match/if-none-match
(`copy.go:165-170`). When the store is a `ConditionalCopyStore` and an ETag
condition exists, the date headers are never consulted (`copy.go:85-95`) and
`storage.CopyOptions` has no date fields (`store.go:65-68`), so a date
precondition sent alongside an ETag condition is silently ignored.

**Fix.** Add `SourceIfModifiedSince`/`SourceIfUnmodifiedSince` to
`storage.CopyOptions`; populate them in `copySourceConditions`; evaluate them in
`CheckCopySourceConditions` with the RFC 9110 precedence already used for reads
(If-Match before If-Unmodified-Since; If-None-Match before If-Modified-Since).
The fallback path keeps working unchanged.

**Tests.** Copy with (a) only a date condition, (b) only an ETag condition,
(c) both, each for pass and fail; assert the date condition is honored when an
ETag condition is present. Add a wire-level case in `internal/s3api`.

**Acceptance.** No precondition is dropped on any copy path; the new cases fail
before the fix.

**Risk/effort.** Low.

### WS-5 — Handoff out-of-band digest (Medium)
**Defect.** `workspace adopt`/`ImportHandoff` verify the archive digest against
the digest stored in the same document, so a whole-pair replacement is accepted.
Delta already has `--expect-sha256`; handoff has no equivalent.

**Fix.** Add `--expect-sha256` (name TBD: archive digest) to `workspace adopt`,
an `ExpectedArchiveDigest` option to `ImportHandoff`, and an optional parameter
to the MCP `stow_handoff_adopt` tool. Recompute and compare; refuse on mismatch
with a message that names both digests (mirror the delta refusal). Empty stays
"no check" but the command help and the docs state the workflow. Consider also
binding the expected `checkpoint_id`/`workspace_id`.

**Tests.** Tampered archive refused when the expected digest is supplied;
accepted when absent (documented); tampered document+archive refused when the
caller carries the sender's digest.

**Acceptance.** The transfer path can be verified against a digest the receiver
did not take from the same document.

**Risk/effort.** Low, additive.

### WS-6 — Prewarm upstream guard (Medium/Low)
**Defect.** `validateUpstreamEndpoint` returns nil for an empty endpoint
(`transport_policy.go:14-16`), so `NewS3Client(cfg.Upstream)` succeeds with no
`BaseEndpoint` (`upstream.go:52-67`) and `prewarm` later resolves
`s3.amazonaws.com`. The comment at `prewarm.go:111-119` claims the opposite.

**Fix.** In `runPrewarm`, require an explicitly configured upstream endpoint
(and mode) before building the client, and return a clear error otherwise —
mirroring `buildStore`'s guard (`cmd/stow-s3/main.go`). Correct the comment.
Optionally add a dedicated constructor that refuses an empty endpoint, so other
callers cannot repeat the mistake.

**Tests.** `prewarm` with no upstream configured fails without creating a cache
directory and without any provider call (assert on a call counter).

**Acceptance.** No outbound provider call is possible from `prewarm` without an
explicitly configured upstream.

**Risk/effort.** Low.

### WS-7 — Store-lock recovery TOCTOU (Medium)
**Defect.** `fs/fs_lock.go:44-49` removes the lock path unconditionally after a
staleness check; two processes can both remove/recreate and both believe they
own the directory.

**Fix.** Reclaim the stale lock by identity: atomically `rename` the stale lock
to a unique name (only one renamer wins), then remove the renamed file and
retry acquisition. Alternatively re-read the lock after removal and verify it
still matches the observed stale id before proceeding.

**Tests.** Two concurrent acquirers against a stale lock: exactly one ends up
owning the directory; add a fault-injected interleaving.

**Acceptance.** No interleaving yields two owners.

**Risk/effort.** Low/medium.

### WS-8 — Released capacity operations never reclaimed (Medium)
**Defect.** `ReleaseOperation` sets `Released=true` and keeps the entry
(`operations.go:89-108`); `operationCharge` still adds `ReserveBytes` to
`RecoveryBytes` for released ops (`accounting.go:86-96`), and nothing prunes
them. The recovery reserve fills with finished work over a host's lifetime.

**Fix.** Stop charging `ReserveBytes` for released operations (they no longer
need a reserve to record their own release), and add a pruning step so the state
file does not grow without bound. Confirm nothing references a released
operation before pruning (`CheckDependencies` already skips released). Decide
whether to keep a bounded audit trail or drop on release; if kept, cap it.

**Tests.** Begin+release many operations and assert new operations are still
admitted and `RecoveryBytes` does not grow monotonically.

**Acceptance.** Released operations do not consume the recovery reserve
indefinitely.

**Risk/effort.** Low/medium — clarify the reserve's intended lifetime first.

### WS-9 — Admin loopback trust behind a proxy (Medium, 0.4.0)
**Defect.** `authorizeAdmin` grants token-free access to
`/_stow/status|inspect|metrics` from any loopback `RemoteAddr`
(`server.go:343-350,318-329`); behind a local reverse proxy every remote caller
looks loopback and can read bucket names, keys and counters.

**Fix (choose one).**
- **A:** Refuse loopback trust when a forwarding header (`X-Forwarded-For`,
  `Forwarded`, `X-Real-IP`) is present — fail closed, since that indicates a
  proxy the trust model did not anticipate.
- **B:** Require the admin token for read-only routes whenever the server is
  bound to a non-loopback interface.
- **C:** Add `--admin-trust-proxy` and document that loopback trust assumes no
  proxy.

Recommendation: **A + C**, keeping `stow doctor` working on a direct loopback
connection.

**Tests.** Read-only routes: allowed from a direct loopback connection; refused
when a forwarding header is present; unchanged for a remote address.

**Acceptance.** A proxy in front of the server cannot turn a remote caller into
a token-free admin reader by default.

**Risk/effort.** Medium; it is a deliberate access-policy change, hence 0.4.0.

### WS-10 — List limit caps (Low/Medium, 0.4.0)
**Defect.** `max-keys`/`max-uploads` accept any positive value
(`handlers_bucket.go:94-99`; `util.go:180-185`), so one request can force an
unbounded listing.

**Fix.** Clamp to S3's 1000 (values above are treated as 1000), for `max-keys`,
`max-uploads` and `max-parts`. Keep the "malformed is refused, not defaulted"
behavior for non-numeric values.

**Tests.** `max-keys=100000` returns at most 1000 and truncates; existing
pagination tests unchanged.

**Acceptance.** No request can request an unbounded page.

**Risk/effort.** Low.

### WS-11 — Session backend parity (Medium, 0.4.0)
**Defect.** TS session starts `backend: "memory"` (`session.ts:120`); Python
starts `--backend filesystem` (`session.py:260`). Same "disposable session",
different storage, and `capabilities.backend` differs.

**Fix.** Decide the intended backend for a disposable session (recommend
`memory`: no directory to leak, matches the TS test and the "cannot outlive you"
story), align the other client, and add a cross-language assertion so they cannot
drift again. If filesystem is intended, align TS and update its test.

**Tests.** A conformance-style check that both clients report the same
`capabilities.backend` for a default session.

**Acceptance.** The two clients agree on `backend` for a default session.

**Risk/effort.** Low, but it is a product decision — record it.

### WS-12 — XML prolog (Low)
**Defect.** `errors.go:258` asserts `v.(struct{ XMLName xml.Name })`, which no
response type is, so no `<?xml …?>` is ever written.

**Fix.** Write the declaration for XML responses unconditionally (or via a
small helper), matching real S3. Verify no existing test parses around its
absence.

**Tests.** Error and list responses begin with `<?xml version="1.0" …?>`.

**Acceptance.** Prolog present on all XML responses.

**Risk/effort.** Trivial.

### WS-13 — `resumeLimit` narrowing (Low)
**Defect.** `if requested > persisted` errors when `persisted == 0`, but `0`
means unlimited for `MaxCheckpointBytes`/`MaxCheckpoints` (`resume.go:134-145`).

**Fix.** `if persisted != 0 && requested > persisted`. Keep the negative and
zero handling. Correct the error text if needed.

**Tests.** Resume with a stricter cap over an unlimited workspace succeeds;
widening a finite cap still fails.

**Acceptance.** A host can tighten retention on resume.

**Risk/effort.** Trivial.

### WS-14 — CLI/reporting nits (Nit)
- `registry_bound.remaining` can be negative when no bound is declared
  (`cmd/stow-s3/workspace.go:56-64`); make it zero when `max_workspaces == 0`, or
  correct the field's contract.
- `checkpoint_import.go:143` reports `len(manifest.Files)` in a count-limit
  message that counts payloads; report the count actually compared.
- `handoff.go:68` sets `Archive.Files = len(manifest.Files)`, omitting portable
  object payloads.

**Tests.** Field/JSON assertions for the corrected values.

**Risk/effort.** Trivial.

---

## 4. Cross-cutting test and gate work

These are what would have caught the batch-1 defects, and they matter more than
any single fix:

1. **Concurrency coverage for the workspace backend.** Add parallel
   list/put/path/delete tests under `-race` (§WS-1). `make standards` already
   runs `test-race`; the gap was the absence of a concurrent test, not the gate.
2. **Contract test: completion ETag is readable.** Extend the shared multipart
   contract so all three backends assert `HeadObject().ETag == completion.ETag`
   (§WS-2).
3. **Outbox liveness test.** Assert a write after a terminal failure does not
   return success while staying local (§WS-3).
4. **Copy-condition matrix.** Date/ETag/both × pass/fail (§WS-4).
5. **No-egress assertions.** Prewarm (and the offline paths) assert on provider
   call counters, not just results (§WS-6).
6. **Conformance drivers.** Add the multipart-ETag and copy-condition cases to
   `conformance/workspace/cases.json` (or the S3 conformance suite) so the Go,
   TS and Python drivers each catch a regression.
7. **Cross-language parity check.** One test asserting the two session clients
   agree on `capabilities.backend` (§WS-11).

---

## 5. Sequencing

| Milestone | Contents | Exit criteria |
|---|---|---|
| M0 — Red tests | Add the failing regression tests for WS-1…WS-4 (and WS-6/7 where cheap) | Tests fail at `v0.3.1`, each for the intended reason |
| M1 — Crash/correctness | WS-1, WS-2, WS-4 | `go test -race ./...` clean; contract tests pass |
| M2 — Outbox | WS-3 (schema bump + migration) | Liveness test passes; migration covered |
| M3 — Hardening | WS-5, WS-6, WS-7, WS-8 | Each acceptance met; no new egress; no double-owner |
| M4 — Behavior | WS-9, WS-10, WS-11 (0.4.0) | Access/cap/parity tests pass; docs updated |
| M5 — Nits + docs | WS-12, WS-13, WS-14; update comments/docs | `make standards` green |
| M6 — Verify | Full gate run, release notes | See checklist |

Each milestone keeps `make standards` green and adds its regression test before
the fix (the repo's existing discipline).

---

## 6. WS-15 — leads to confirm (not yet reproduced)

From the broader sweep; confirm each before treating as a defect. None are
scheduled until reproduced:

- `Workspace.Path` can return a symlink-resolved path outside the root
  (`resolve.go`/`locate` use `os.ReadDir`/`os.Stat`), disagreeing with the
  no-follow read paths.
- `runtime.UploadPart` reads the whole part into memory before the quota check.
- `COPY … REPLACE` reads the whole source into memory (`s3api/copy.go:145`).
- `policystore.Store.Policy(env)` ignores `env`; read-side validation never runs.
- `EffectiveWritePolicy`/`validateLiveWriteBackend` ignore the resolved
  `Authority`, so status/banner can claim propagation a grant forbids.
- `RecoveryHold.ExpiresAt` is persisted but never enforced.
- `touchCache` can insert a zero-valued index entry (empty bucket/key/size).
- SigV4 leniencies: case-insensitive ETag comparison and `UNSIGNED-PAYLOAD`;
  only `values[0]` of a multi-valued signed header; canonical query sorted by
  decoded keys.
- Chunked uploads (`Transfer-Encoding: chunked`) are rejected.
- `embedded.storeAdapter.DeleteObjects` can turn a missing key into a batch
  failure for a caller store that returns `ErrObjectNotFound`.

For each: write the smallest failing test that demonstrates impact; if it does
not reproduce, record why and close.

---

## 7. Documentation and release work

- Correct the `prewarm.go` comment (§WS-6) and any other comment that states a
  guarantee the code does not provide (the evaluation found the `writepolicy`
  comment in the same class — confirm under WS-15).
- README "Known issue" style notes where behavior is deliberately left weak
  (e.g. handoff digest optional), and update the install/release notes for the
  target version.
- Add `docs/releases/v0.3.2.md` and, if M4 lands, `v0.4.0.md`.
- Link this plan from `docs/planning-index.md` and record its disposition there.
- Run the doc gates: `check-doc-commands`, `check-doc-links`, `check-site-claims`,
  `check-adr-index`.

---

## 8. Verification checklist (before tagging)

- [ ] `go test ./...` and `go test -race ./...` green, including the new
      concurrency and contract tests.
- [ ] The M0 red tests pass; each traces to a finding ID.
- [ ] `make test-conformance` green for memory, filesystem and runtime backends.
- [ ] `make test-node`, `make test-python`, `make test-wasm`, `make test-agent`
      green.
- [ ] `make standards` green (format, vet, race, quality ratchets, file size,
      coverage, version, site claims, install surface, doc commands, ADR index,
      doc links, API reference, scripts, TS quality, generated artifacts, MCP
      browser).
- [ ] No new `-race` reports; the workspace concurrency test is present.
- [ ] Outbox schema bump migrates an existing file; newer-version rejection
      still holds.
- [ ] No provider call from `prewarm` without an explicit upstream (counter
      assertion).
- [ ] Handoff adopt refuses a mismatched expected digest.
- [ ] Release notes written; planning index updated.

---

## 9. Open decisions

1. **Outbox terminal handling:** dead-letter set (recommended) vs. exclude-from-
   ordering. Decide before WS-3.
2. **Admin proxy trust:** refuse on forwarding headers (recommended) vs. require
   token off-loopback vs. opt-in flag.
3. **Session backend:** memory (recommended) vs. filesystem as the canonical
   disposable-session backend.
4. **Capacity reserve lifetime:** whether a released operation's record is kept
   for audit (bounded) or dropped.
5. **Version split:** confirm patch-vs-minor assignment in §1 before tagging.
