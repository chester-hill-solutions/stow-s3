# Reject zero-byte outbox journals

Date: 2026-10-17 (Toronto; receipts use UTC).
Baseline: `f23f56ea40932085af582577583bf08624f23f2c` on `codex/core-copy-quality`.
Freshly fetched `origin/main`: `219d1dcfff9807b04660a40313b32127e5be2933`. Worktree initially clean.
The preceding [lookup review](2026-10-17-outbox-lookup.md) first reproduced this defect.

## Original defect and evidence

`reloadLocked` treated an existing zero-byte journal as a new empty outbox.
It bypassed the decoder, replaced acknowledged in-memory state, and reset intent
sequence and fencing-token counters. Reads returned empty results and subsequent
writes could reuse intent IDs. `RetryPending` could report successful recovery
while the durable record of its pending work had been truncated.

Before implementation changes, deterministic file-backed regressions failed twice
normally and twice under race detection for all eight operations: open, provenance
lookup, pending snapshot, prepared snapshot, enqueue, owned prepare, provenance
save and close. Opening a real snapshot truncated at byte zero also succeeded.
The recovery-caller test separately demonstrated `RetryPending` returning nil,
twice normally and twice under race detection. These are assertion failures,
not compilation errors or race reports. Both pre-fix test versions passed quality,
file-size and formatting checks.

The positive controls passed before the fix: missing-path initialization, a valid
empty protobuf snapshot and a valid empty legacy JSON object all remain supported.
See [loader failures](2026-10-17-outbox-empty/raw/loader-before/regression-normal.log),
[loader race failures](2026-10-17-outbox-empty/raw/loader-before/regression-race.log),
[caller failure](2026-10-17-outbox-empty/raw/caller-before/caller-normal.log) and
[caller race failure](2026-10-17-outbox-empty/raw/caller-before/caller-race.log).

## Change and preserved contracts

Delete the four-line empty-file shortcut. Every existing file now reaches the
existing format decoder, which already rejects empty input. Decoding completes
before cached state changes. A zero-byte journal cannot authorize a new write,
acknowledge a successful close or turn recovery into an empty success.

The regressions check repeated refusals, unchanged corrupt file contents and
retention of the last acknowledged pending/prepared entries. Restoring the exact
valid snapshot permits existing and new handles to resume, preserves provenance,
assigns the next IDs (`outbox-3`, `outbox-4`) and advances the owned-prepare fencing
token. An additional test rejects every proper byte prefix of a real protobuf
snapshot through the file loader, including the previously bypassed zero prefix.
The recovery-caller regression requires an error with zero upstream calls and
no replacement of the corrupt file.

No new dependency, public API, error type, format or storage abstraction.
File locking, fresh reloads, protobuf/JSON decoding, atomic replacement and the
durability fence remain in their existing paths. Missing-path initialization and
valid empty snapshots are preserved.

## Validation and costs

All source compilation, formatting and checks ran on Railway, using the existing
scratch service and pinned Go 1.25.6/Node 25.9.0 toolchain. The completed 14-stage
matrix covers focused tests twice normally and twice under race detection, the
full Go suite, broad storage/runtime/embedded/run-through/S3/public-package race
tests, all four conformance profiles (memory, filesystem, runtime-memory and
runtime-filesystem), vet, quality/file-size/format gates, site build, site tests
and documentation links. See [final checks](2026-10-17-outbox-empty/raw/source-checks/checks.json).
The source and document hashes are verified again for the finished report in the
[report receipt](2026-10-17-outbox-empty/raw/source-checks/report-receipt.json).

Compatibility change: an existing zero-byte file now causes an error instead of
being accepted as empty. A deliberately precreated empty file is also rejected;
use a missing path or a valid empty snapshot for initialization. The fix detects
truncation and refuses unsafe acknowledgement; it cannot reconstruct lost data.
Restore a valid journal before retrying.

The valid nonempty read/decode and write paths perform the same operations.
Invalid empty input now produces the existing decoder error. No benchmark was
run for this correctness-only change and no performance improvement is claimed.
No new persistent state or allocation machinery was introduced, and no baseline
was raised. The Railway scratch runner is stopped after verified evidence export.

## Why the earlier tests missed it

The codec tests already rejected every truncated protobuf payload. The loader
short-circuited before calling that decoder for zero bytes. These regressions
exercise the file boundary and recovery caller, where success or failure affects
acknowledged work. The correction removes the bypass rather than adding another
validation mechanism.

## Next highest-priority finding

The missing-file initialization shortcut also applies to a live instance that has
already loaded an acknowledged journal. If that journal disappears, provenance
lookup, pending snapshot, enqueue, prepare and close all succeed instead of
reporting loss of existing persistence. The loader resets the last known state;
mutations may create a new journal from that empty state.

An isolated overlay reproduced all five cases twice normally and twice under
race detection after the zero-byte correction. See the
[normal reproduction](2026-10-17-outbox-empty/raw/next-finding/missing-journal-normal.log)
and [race reproduction](2026-10-17-outbox-empty/raw/next-finding/missing-journal-race.log).
This finding is not fixed here. The next correction must distinguish missing-path
initialization from losing a journal already observed by the same live handle,
while preserving fresh reads and cross-process locking. These tests do not claim
that a new process can infer a missing journal's prior history.

Follow-up: [observed-journal loss correction](2026-10-17-outbox-missing.md) distinguishes
fresh initialization from missing-file reloads after a live handle knows a journal.
