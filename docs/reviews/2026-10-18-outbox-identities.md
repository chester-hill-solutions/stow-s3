# Preserve protobuf outbox identities — 2026-10-18

## Baseline and demonstrated failures

Baseline `bfe5b90` already rejects IDs shared between active and prepared maps.
`origin/main` was refreshed before this work; exact refs are in
[baseline.json](2026-10-18-outbox-identities/raw/baseline.json).
This change addresses loss before those maps reach the shared normalizer.

The protobuf decoder accepted repeated records with the same identity within
active entries, prepared entries or provenance. Assigning each to a Go map kept
only the last record. Both record orders were accepted. Opening such a journal
and then calling Close permanently rewrote it without the earlier record.
Identical duplicates were silently deduplicated too.

The writer could itself create ambiguous identities: `outboxProtoText` replaced
invalid UTF-8 before serialization. Distinct map keys `\xfe` and `\xff` both
became U+FFFD. Even a single invalid identity was changed, making a subsequent
lookup by its original bytes miss. A public SaveUpstreamState call acknowledged
such a changed identity. These are in-memory string inputs, not valid UTF-8 S3
keys. No deployed data loss or valid-writer production incident is established.

The deterministic tests were run before production edits, twice normally and
twice with race detection. **52 distinct regression cases failed in each mode**
(104 failures with count=2): 12 direct duplicate decodes, 12 duplicate constructor
cases, 21 reload operations, six invalid-identity encode cases and one public
failed-write case. Both differing/identical duplicates and both record orders
are exercised. Each mode recorded 24 accepted-and-rewritten opens. A control
preserving distinct empty, ASCII, NUL, Unicode and replacement-character
identities passed twice in each mode.
[Red receipt](2026-10-18-outbox-identities/raw/before/red-receipt.json) verifies the exact case set.

The baseline job's final benchmark build failed because its output path was in
the root-owned `/results` directory. The original failure and log are retained.
Only that build was repeated with output in writable `/app/benchmark`; the
[repair job](2026-10-18-outbox-identities/raw/baseline-build/checks.json) passed with source hashes
identical to the red run. No production edit preceded the repaired baseline build.
This infrastructure failure is not counted as an integrity regression.

## Small change and preserved contracts

Require each serialized record to increase the size of its private decode map by
one, for provenance and the shared active/prepared entry decoder. A repeated
identity cannot grow the map, so decoding returns an error immediately. These
temporary maps never replace acknowledged state until decoding succeeds. There
is no auxiliary set, extra hash lookup or new state allocation. Keep identity keys
unchanged when creating protobuf messages. The existing protobuf marshaler then rejects
invalid UTF-8 rather than persisting changed identities. Marshal still completes
before atomic replacement, so an encoder error preserves the existing journal.

Ordinary text values retain their existing UTF-8 replacement behavior. The old
JSON-parity test now tests that behavior with valid identity keys; the new red
regressions replace its previous expectation of lossy identity replacement.
Valid UTF-8 identity strings, including NUL and actual U+FFFD characters, are
unchanged. No schema, dependency, public API, mutex, file lock, durability fence
or S3 operation changes.

Tests also verify that corrupt reloads do not rewrite bytes or replace the last
acknowledged in-memory queues. Restoring valid bytes permits reads and a new
correctly sequenced enqueue. Rejected public writes preserve the preceding file
and provenance and permit a later valid write. Existing migration, recovery,
claim fencing and conformance tests remain in the full verification.

## Verification

All 52 originally failing cases now pass twice normally and twice under race
detection, plus the distinct-identity control twice in each mode: **106 passing
cases per mode** using byte-identical regression/benchmark source from the red
run. The [final green receipt](2026-10-18-outbox-identities/raw/final/green-receipt.json)
verifies exact cases and source hashes.

All **17 final Railway stages** pass: formatting; focused outbox tests twice
normally and twice with race detection; full Go suite; broad storage/runtime/
embedded/run-through/S3/public-package race tests; four conformance profiles
(memory, filesystem, runtime-memory, runtime-filesystem); vet; site build/tests;
documentation links; quality/file-size/format gates; two benchmark builds; and
interleaved comparison. The initial lookup implementation also passed all 16 of
its stages. [Final commands/results](2026-10-18-outbox-identities/raw/final/checks.json)
and [initial commands/results](2026-10-18-outbox-identities/raw/after/checks.json)
are retained alongside the original red job and its separate build repair.

All formatting, compilation, code checks, tests and measurements ran on the same
authorized Railway scratch deployment. Local work was reading, editing, Git,
packaging, hashing and analysis of exports. Go 1.25.6 and Node 25.9.0 used a
4-CPU/4-GiB cgroup with ext4 `/results`. Validation runs as UID 10001. Initial
upload hashes, formatted source copies, exact commands, byte/hash-verified
exports and all logs are retained. Final report/documentation checks use the
same validated production/test hashes. No baseline is raised.

## Comparative cost

The final guard adds one size comparison per decoded provenance/queue record.
The encoder avoids the former identity text-normalization scan. An initial
implementation added membership lookups against the destination maps instead.
Its separate 96-observation cohort measured a 16.8% CPU-1 decode increase at depth
128 and an 8.0% CPU-4 increase at depth 16,384. Allocation counts were unchanged.
Those adverse results prompted the simpler growth check and remain retained in
[initial comparison](2026-10-18-outbox-identities/raw/after/comparison-analysis.json).
The final cohort is reported separately; its measurements do not replace the
initial results or pool different implementations. There is no new
retained cache, map, allocation site on valid input, storage format or abstraction.
Fresh full journal reads and byte-identical validated-state reuse are unchanged;
this measurement isolates codec work, not end-to-end S3 latency or cached reads.

Separate baseline/final test binaries were built on Railway. For the final
cohort, the original baseline was rebuilt through a Go source overlay pointing
to the hash-verified red-run source copies; shipping source files were not swapped.
The rebuilt baseline binary SHA-256 matches the initial cohort exactly.
The same benchmark source constructs deterministic unique identities with equal counts in each of
three collections: provenance, active and prepared. Depth 16,384 therefore means
49,152 total records and is a stress case, not an asserted typical workload.
Fixtures and initial encoding are outside the timer. Encode/decode are measured
separately; counts are checked after timing. Each binary process runs all cases
at GOMAXPROCS 1 and 4 with a 500-ms minimum per case. Eight processes alternate
ABBA then BAAB: four observations per variant/cell, **96 observations per cohort,
192 total** across the initial and final checks.
Allocation bytes/counts are cumulative per codec operation, not retained heap or
RSS. No process-memory or storage-capacity bound is inferred.

Values are per-cell medians. Each cohort has its own baseline; initial and final samples are not pooled.

| Depth per collection | Operation | CPU | Initial lookup guard Δ | Final µs before → after | Final Δ |
| ---: | --- | ---: | ---: | ---: | ---: |
| 0 | encode | 1 | +2.03% | 0.169 → 0.161 | -4.88% |
| 0 | encode | 4 | +0.06% | 0.187 → 0.193 | +3.20% |
| 0 | decode | 1 | -0.29% | 0.211 → 0.206 | -2.72% |
| 0 | decode | 4 | +2.64% | 0.217 → 0.225 | +4.06% |
| 128 | encode | 1 | +2.44% | 139.323 → 133.211 | -4.39% |
| 128 | encode | 4 | -2.29% | 153.570 → 149.698 | -2.52% |
| 128 | decode | 1 | +16.80% | 131.279 → 133.751 | +1.88% |
| 128 | decode | 4 | +5.77% | 182.149 → 174.967 | -3.94% |
| 16,384 | encode | 1 | +2.84% | 30,061.057 → 29,331.341 | -2.43% |
| 16,384 | encode | 4 | +2.21% | 24,846.359 → 25,875.236 | +4.14% |
| 16,384 | decode | 1 | -0.69% | 31,848.610 → 30,731.267 | -3.51% |
| 16,384 | decode | 4 | +7.99% | 24,408.183 → 24,943.806 | +2.19% |

Allocation counts are unchanged in every final cell. The largest absolute
difference between median allocated bytes is 2.5 B/op; exact values
and ranges for every cell remain in the analysis.

All individual samples, timing/allocation ranges and binary hashes are retained
in [comparison analysis](2026-10-18-outbox-identities/raw/final/comparison-analysis.json) and
[commands/order](2026-10-18-outbox-identities/raw/final/comparison-runs.json).
Small sample counts do not establish statistical significance. Benchmark binaries
are removed after measurements and hashes are saved; the runner is stopped only
after verified exports and final documentation checks.

## Compatibility, limits and next check

Duplicate protobuf identities now fail to open/reload, even for identical records.
Restore a valid snapshot before retrying. Invalid UTF-8 map identities now fail
encoding without replacing the journal. Valid UTF-8 data remains readable in the
existing format. Legacy JSON decoding is unchanged: information already lost
through earlier JSON normalization or an earlier protobuf rewrite cannot be
reconstructed here. This does not prove complete semantic integrity of every
journal field or demonstrate that valid Stow writers emit repeated records.

The next correctness check is identity/entry-ID consistency: these are separately
encoded values, while queue operations return the entry ID and locate records by
map identity. Establish the behavior and consequence of mismatches before making
another correction. No such change is included in this commit.
