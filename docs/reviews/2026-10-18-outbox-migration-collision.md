# Reject overlapping prepared-record migration — 2026-10-18

## Defect and baseline

`normalizeDecodedOutboxState` moved an active-map entry marked `Prepared` into the
prepared map before checking for overlapping IDs. With the same ID already in
that map, the assignment replaced its record; removing the active key then hid
the collision from validation. Opening the journal succeeded. A subsequent Close
rewrote the journal with only the surviving record.

This is rejection of ambiguous/corrupt input. This investigation does not show
that valid Stow writers create overlapping IDs, or establish that a deployed
journal has lost records. The normalizer already rejected overlap when the
active-map entry was not marked Prepared.

`origin/main` was refreshed before this work. The working baseline is `d5782d2`,
which already includes the completed COPY, recovery and outbox performance work;
exact refs are in [baseline.json](2026-10-18-outbox-migration-collision/raw/baseline.json).
No completed remediation is repeated.

## Reproduction before modification

A deterministic fixture stores two records under `outbox-7`: a Prepared-marked
active-map record with key `first-local-write`, owner `first-owner`, token 41, and
an existing prepared record with key `second-local-write`, owner `second-owner`,
token 42. Opening the baseline retained the first record. Close permanently
rewrote the temporary journal without the second. Identical cross-map duplicate
records were also accepted, despite the existing global-ID overlap rule.

The regression tests were uploaded and run before changing production code.
The same tests run twice normally and twice with race detection reproduced
**47 distinct failing cases in each mode** (94 failures with count=2):

- Two direct-normalizer cases, including identical records and input immutability.
- Ten constructor cases across JSON schemas 0–3 and framed protobuf schema 4.
- Thirty-five reload cases: five formats × pending/prepared snapshots, provenance
  lookup, enqueue, prepare, provenance save and Close.

The constructor test deliberately closes an unexpectedly accepted outbox to
expose permanent writeback. Each mode recorded 20 accepted-and-rewritten opens.
Six non-overlapping migration controls passed twice in each mode, including the
flat-map legacy format. The two unmarked-overlap controls also passed twice.
The [red verification receipt](2026-10-18-outbox-migration-collision/raw/before/red-receipt.json)
asserts the exact failing case set, repetitions and passing controls; it does not
merely check for an arbitrary nonzero exit.

## Change and preserved behavior

Move the existing overlap-validation loop before record migration. Reject every
cross-map ID collision before mutating either map. No new error type, dependency,
format, public API or storage abstraction is introduced.

Valid Prepared-marked active records still migrate when their ID is absent from
the prepared map. Tests preserve the active queue, prepared order and fields,
read-without-rewrite behavior, successful Close/reopen, sequence advancement and
ownership fencing. On corrupt reload, error-returning operations reject the
journal without rewriting it. Errorless inspection keeps the last acknowledged
prepared record; restoring the original bytes permits normal operation again.
Existing mutexes, shared file locks, atomic replacement and durability fences
are unchanged. No S3 request path changes.

## Results and verification

After the five-line validation loop was moved, all **55 migration cases passed
twice normally and twice with race detection** (110 passing cases per mode),
using byte-identical regression source from the red run. This includes all 47
originally failing cases and eight controls. The
[green receipt](2026-10-18-outbox-migration-collision/raw/after/green-receipt.json)
verifies case counts and exact production/test source hashes.

All 14 Railway stages passed: formatting; focused outbox tests twice normally
and twice with race detection; full Go suite; broad storage/runtime/embedded/
run-through/S3/public-package race tests; memory, filesystem, runtime-memory and
runtime-filesystem conformance; vet; site build/tests; documentation links; and
Go quality/file-size/format gates. No baseline was raised.
[Exact commands and results](2026-10-18-outbox-migration-collision/raw/after/checks.json)
and [baseline commands/results](2026-10-18-outbox-migration-collision/raw/before/checks.json)
are retained. Report/documentation checks are rerun after adding this report.

All formatting, compilation, tests and checks ran on the existing authorized
Railway scratch runner, using Go 1.25.6, Node 25.9.0, a 4-CPU/4-GiB cgroup and
ext4 `/results`. Validation runs as UID 10001 so filesystem permission tests are
meaningful. Local actions were reads, edits, Git, packaging, hashing and analysis
of exported evidence. The baseline's expected failing test exit codes are
explicit in its plan; a successful harness receipt does not mean those tests
passed. Raw logs, plans, harnesses, source copies and export hashes are retained.

## Cost, compatibility and limits

This reorders two existing passes. It adds no validation pass, allocation site,
retained buffer or per-handle state. Both versions check map membership and scan
active entries on valid native journals; migrated records no longer needlessly
participate in the later prepared-map overlap scan. No performance improvement
is claimed and no benchmark was run for this validation-order correction.

Ambiguous journals that previously overwrote or silently deduplicated records now
fail to open/reload, including identical records in both maps. Restore a valid
journal before retrying. Stow cannot reconstruct a record already lost through a
previous rewrite. Valid JSON and protobuf formats remain readable; legacy writes
still migrate to the existing protobuf schema. This fix does not constitute a
complete semantic-integrity validator for every journal field.

## Next correctness check

Investigate duplicate identities within a single protobuf repeated collection.
`decodeOutboxProtoEntries` assigns each decoded entry into a map by identity, so
a repeated identity may overwrite an earlier record before normalization can
see it. Reproduce the behavior before deciding the appropriate rejection rule;
consider provenance collections and identity/entry-ID consistency in that review.
This is the next investigation, not another implemented fix in this change.
