# Refuse loss of an observed outbox journal

Date: 2026-10-17 (Toronto; receipts use UTC).
Baseline: `7720c749ddc9120c7a381d7f7f93ebc771af72e5` on `codex/core-copy-quality`.
Freshly fetched `origin/main`: `219d1dcfff9807b04660a40313b32127e5be2933`. Worktree initially clean.
The [zero-byte review](2026-10-17-outbox-empty.md) separately reproduced this case.

## Defect and original evidence

Every `os.IsNotExist` result in `reloadLocked` was treated as first initialization,
even for a handle that had already loaded or successfully written the journal.
Unlinking an acknowledged file reset cached state and counters. Reads returned
empty success; writes and close could create a new journal from that empty state.
Recovery could report success instead of reporting missing persistence.

The final regressions were run against the unchanged implementation before editing
production code. Eighteen assertions failed twice normally and twice under race
detection: seven operations through the writing handle and a newly reopened handle
(14 cases), first enqueue without a subsequent reload, observation of a peer's
write, an empty snapshot and the recovery caller. Failures show nil where
`os.ErrNotExist` was required. These were assertion failures, with no compile
failure or race report. The unpublished-first-write retry control passed twice
in both modes, and the quality/file-size/format gates passed before the fix.

[Normal failures](2026-10-17-outbox-missing/raw/before/regression-normal.log),
[race failures](2026-10-17-outbox-missing/raw/before/regression-race.log),
[original checks](2026-10-17-outbox-missing/raw/before/checks.json).

## Change and protected contracts

A private per-handle marker records a successful journal read or durable write.
Missing-path initialization is allowed only while that marker is false. Once a
handle knows a journal, a missing-file reload returns the original filesystem
error before replacing cached state or attempting a mutation.

The marker is set after a successful decode and after a successful write's
existing durability fence. A failed unpublished first write leaves the handle
eligible to retry initialization. Empty snapshots count as observed journals;
this does not depend on an entry count, sequence or token being nonzero.

Regressions require repeated errors, no recreation of a lost file, retained
acknowledged pending/prepared entries, and restored operation after replacing the
exact valid snapshot. Resumption preserves provenance, assigns the next intent
IDs and advances fencing tokens. A reader opened before a peer creates the file
learns its existence through a normal fresh reload. `RetryPending` must report
file loss with zero upstream calls.

Existing process/file locks, fresh reads, decoding, atomic replacement and directory
fences remain in their existing paths. No dependency, public API, error type,
format, persistent marker or new storage abstraction was added.

## Validation, costs and limits

All 14 final stages passed on Railway: formatting; focused tests twice normally
and twice under race detection; the complete Go suite; broad storage/runtime/
embedded/run-through/S3/public-package race tests; all four conformance profiles
(memory, filesystem, runtime-memory and runtime-filesystem); vet; quality,
file-size and formatting gates; site build, site tests and documentation links.
See [final checks](2026-10-17-outbox-missing/raw/source-checks/checks.json).
The finished report and source bytes are verified in the
[report receipt](2026-10-17-outbox-missing/raw/source-checks/report-receipt.json).
All compilation, formatting and verification ran on the existing Railway scratch
runner using the pinned Go 1.25.6/Node 25.9.0 toolchain. No baseline was raised.

Cost: one boolean per handle, plus assignments during existing successful reads
and writes. There is no additional I/O or per-operation allocation mechanism.
This is a correctness change; no benchmark was run and no performance improvement
is claimed. Source formatting returned from Railway is used unchanged locally.
The runner is stopped after verified evidence export.

Compatibility change: deleting an observed journal no longer resets an existing
handle. Restore a valid snapshot to resume that handle. Genuinely new missing paths,
valid empty snapshots and retry after an unpublished first-write failure remain
supported.

The marker exists only for the live handle. A new process cannot infer a missing
file's prior history, and this change does not detect deletion followed by valid
replacement before a handle observes the absence. It does not reconstruct lost
bytes, authenticate snapshots or prevent deliberate rollback by a filesystem
owner. Those are not guarantees of this correction.

## Next highest-priority finding

The legacy migration decoder accepts JSON `null` as an empty snapshot, despite
journal roots being objects. After replacing a real snapshot with `null`, opening,
provenance lookup, pending/prepared snapshots, enqueue, prepare, provenance save and
close all succeed. The decoded empty state can again reset acknowledged work.
Other tested non-object roots (boolean, number, string and array) are rejected.

The isolated overlay reproduced all eight `null` cases twice normally and twice
under race detection after the live-file-loss correction; the other-root controls
passed. See the [normal reproduction](2026-10-17-outbox-missing/raw/next-finding/null-journal-normal.log)
and [race reproduction](2026-10-17-outbox-missing/raw/next-finding/null-journal-race.log).
This finding is not fixed here. The next correction should reject a null root
while preserving valid legacy object snapshots and the protobuf format.

Follow-up: [null-root correction](2026-10-18-outbox-null.md) rejects JSON `null`
without changing valid legacy object snapshots or protobuf snapshots.
