# Reject null legacy journal roots

Date: 2026-10-18 (Toronto; receipts use UTC).
Baseline: `2839fb83b778c41117bf61d2af1a9beb98e32711` on `codex/core-copy-quality`.
Freshly fetched `origin/main`: `219d1dcfff9807b04660a40313b32127e5be2933`. Worktree initially clean.
The [observed-journal review](2026-10-17-outbox-missing.md) first reproduced this case.

## Defect and deterministic before evidence

The migration reader decoded JSON `null` into both a zero-valued header and a nil
legacy entry map without an error. Normalization treated that nil map as a valid
empty snapshot. Replacing an acknowledged journal with `null` therefore silently
reset state and counters; writes could replace the bad input with a new journal.
`RetryPending` could report recovery success after dropping its known work.

Before implementation changes, the final regressions failed twice normally and
twice under race detection. There are 17 assertion cases: eight operations for
plain `null`, the same eight for whitespace-wrapped `null`, and the recovery
caller. The operations are open, provenance lookup, pending/prepared snapshots,
enqueue, owned prepare, provenance save and close. These were assertion failures,
not compile failures or race reports. All other-root, valid empty snapshot and
JSON-to-protobuf migration controls passed twice in both modes. The new tests
also passed quality, file-size and formatting gates before the fix.

[Normal failures and controls](2026-10-18-outbox-null/raw/before/regression-normal.log),
[race failures and controls](2026-10-18-outbox-null/raw/before/regression-race.log),
[before checks](2026-10-18-outbox-null/raw/before/checks.json).

## Small correction and protected behavior

Reject a nil legacy map immediately after the existing JSON decode. An object
root, including an empty object, yields a non-nil map. No additional parser, raw-text
special case or new representation is needed. This adds three production lines.

Regressions require repeated errors, identical invalid file contents and retention
of the last acknowledged pending/prepared state. Restoring the exact valid
snapshot permits existing and new handles to resume, retains provenance and
advances intent IDs and fencing tokens. The recovery caller returns an error
with zero upstream calls. Boolean, number, string and array roots remain rejected
for all eight operations, with no input replacement or cached-state reset.

Valid legacy object snapshots, empty snapshots, migration, future-version errors
and the protobuf format remain supported. Native protobuf reads do not enter the
legacy reader. Existing locks, fresh reloads, journal-lifetime tracking, atomic
replacement and the durability fence are unchanged.

No dependency, public API, error type, storage format or abstraction was added.

## Validation, costs and compatibility

All 14 final stages passed on Railway: formatting; focused tests twice normally
and twice under race detection; the complete Go suite; broad storage/runtime/
embedded/run-through/S3/public-package race tests; all four conformance profiles
(memory, filesystem, runtime-memory and runtime-filesystem); vet; quality,
file-size and formatting gates; site build, site tests and documentation links.
See [final checks](2026-10-18-outbox-null/raw/source-checks/checks.json).
The completed report is verified against exact source and document hashes in the
[report receipt](2026-10-18-outbox-null/raw/source-checks/report-receipt.json).
All code compilation, formatting, tests and quality checks ran on Railway using
the existing scratch service and pinned Go 1.25.6/Node 25.9.0 toolchain. No quality
baseline was raised. The runner is stopped after verified evidence export.

Compatibility change: JSON `null` can no longer initialize or reset an outbox.
Use a genuinely new missing path or a valid empty snapshot for initialization;
restore a valid snapshot when repairing damaged persistence. The correction
refuses unsafe acknowledgement; it does not reconstruct lost bytes.

Cost: a nil comparison in the existing legacy fallback and an error allocation
for the invalid null case. There is no additional I/O or valid-input allocation
mechanism. No benchmark was run for this correctness-only change, and no timing
or memory improvement is claimed.

## Next measured priority

Unchanged provenance saves still reload, clone, encode and replace the complete
journal. The [existing paired Railway measurements](2026-10-17-outbox-protobuf.md#actual-native-saves)
recorded **17.264 ms/op** and **9,523,870 allocated B/op** at 16,384 extra provenance
rows plus the target row, CPU 1, after protobuf. That cohort predates these
correctness corrections; it is evidence of the remaining full-rewrite cost, not
a measurement of this commit.

The next step is a fresh Railway comparison for avoiding unchanged-save work,
using the current implementation as the baseline. It must preserve fresh reloads
under the file lock, distinguish a missing row from a known empty value, and
complete the directory durability fence even if an earlier rename succeeded but
its fence failed. Equality with visible disk state alone cannot acknowledge
successful persistence. No unchanged-save optimization is included here; no
indexing, checkpointing or storage abstraction has begun.

Follow-up: [unchanged-save measurements and implementation](2026-10-18-outbox-unchanged.md)
avoid healthy native rewrites while retaining both file and directory fences.
