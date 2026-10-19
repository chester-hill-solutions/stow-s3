# Bind outbox identities to entry IDs — 2026-10-18

## Baseline and defect

Baseline `fbe58f7` already rejects overlapping/repeated identities and lossy UTF-8
identity encoding. `origin/main` was refreshed before this work; exact refs are
in [baseline.json](2026-10-18-outbox-binding/raw/baseline.json).

Snapshots return each record's `OutboxEntry.ID`, while mutation/claim/recovery
operations locate it by the journal map key (protobuf `Identity`). The decoder
accepted a mismatch. A missing or dangling ID made returned work unaddressable.
An ID aliasing a different map key let an acknowledgement affect that other record.

The deterministic fixture has `outbox-1` for key `first` and `outbox-2` for key
`second`, with fixed creation timestamps. Changing the first record's ID to
`outbox-2` leaves both map keys distinct:

| Stored map identity | Returned entry ID | Intent key |
| --- | --- | --- |
| outbox-1 | outbox-2 | first |
| outbox-2 | outbox-2 | second |

Baseline snapshots advertise `outbox-2` for `first`; MarkSuccess using it deletes
the unprocessed `second` intent while
leaving `first` pending. For prepared records, Commit using that advertised ID
commits `second` with the version supplied for `first`. These are temporary-file
outbox mutations; the reproduction does not send requests upstream or establish
that a deployed journal has suffered loss. Normal public writers key entries by
ID; this finding concerns recovery from inconsistent journal records.

## Reproduce before implementation

The new tests run before changing production code, twice normally and twice with
race detection. They cover protobuf schema 4, typed JSON schemas 0–3 and flat
legacy snapshots, for both active and prepared records:

- 36 constructor cases: missing, dangling and aliased IDs in every format/queue.
- 36 reload cases: snapshots, completion/acknowledgement and Close.
- 12 valid controls: matching Unicode/NUL custom IDs, legacy attempt migration,
  correct acknowledgement, Close/reopen, retention of the unprocessed record and
  monotonic automatic ID allocation.

All **72 distinct regressions failed twice in each mode** (144 failures per
normal/race run), while all 12 valid controls passed twice in each mode. Each
mode captured **24 wrong-record acknowledgements/commits with rewritten file
bytes**. The [red receipt](2026-10-18-outbox-binding/raw/before/red-receipt.json)
asserts the exact failure set and passing controls, not an arbitrary failing exit.
The harness expects the two regression commands to exit 1; its successful job
status means that those expected failures occurred.

The initial uploaded test helpers were reduced to the existing five-parameter
quality limit before any test execution. The guarded
[test synchronization receipt](2026-10-18-outbox-binding/raw/initial-test-sync-receipt.json)
connects the upload-manifest hash to that source; the format stage and source
copies record the actual tested bytes. Production remained unchanged throughout
this preparation and the red run.

## Small change and contracts

Extend the existing per-entry attempt-normalization pass to require each map key
to equal its stored entry ID. Rename that private helper to `normalizeOutboxEntries`
and propagate its error through the shared decoded-state normalizer. Both native
protobuf and all supported legacy JSON paths use that same validation. The check
adds one string comparison per entry to a loop already visiting that entry; no
additional map, buffer, complete scan or public API is added.

Valid matching IDs retain their exact values, including custom Unicode and NUL
strings; no new canonical ID syntax is imposed. Legacy attempts still acquire
the Attempted marker so claims reconcile a potentially lost upstream reply.
Valid legacy snapshots remain readable and migrate to existing schema 4 on the
next write. Schema, dependencies, public interfaces, locking, snapshots,
ownership fences, atomic replacement and S3 request behavior are unchanged.

On failed reload, tests verify that corrupt bytes are not rewritten and that
errorless inspection retains the last acknowledged active/prepared queues.
Restoring valid bytes permits normal snapshots and correctly sequenced enqueue.
The shared normalization works on newly decoded private maps; partial migration
of those temporary maps is discarded on error, never published to a live handle.

## Verification

All **72 regressions and 12 valid controls pass twice normally and twice under
race detection**, 168 passing cases in the new matrix per mode, using byte-identical new regression
and benchmark source from the red run.
[Green receipt](2026-10-18-outbox-binding/raw/after/green-receipt.json) verifies exact cases and source hashes.

All **16 Railway stages pass**: formatting; focused outbox tests twice normally
and twice with race detection; full Go suite; broad storage/runtime/embedded/
run-through/S3/public-package race tests; four conformance profiles (memory,
filesystem, runtime-memory, runtime-filesystem); vet; site build/tests; documentation
links; quality/file-size/format gates; final benchmark build; and interleaved
comparison. [Final commands/results](2026-10-18-outbox-binding/raw/after/checks.json)
and [baseline commands/results](2026-10-18-outbox-binding/raw/before/checks.json)
retain every step. No baseline or tolerance was raised.

All compilation, formatting, tests, checks and measurements ran on the same
authorized Railway scratch deployment, using Go 1.25.6, Node 25.9.0, a 4-CPU/
4-GiB cgroup and ext4 `/results`. Checks run as UID 10001. Local actions were reads,
edits, Git, packaging, hashing and exported-data analysis. Upload and synchronization
receipts, formatted source copies, plans, raw logs, source/binary hashes and
byte/hash-verified exports are retained. The final report and documentation
checks run against the exact tested production/test bytes. No baseline is raised.

## Measured cost

Reuse the existing `BenchmarkOutboxIdentityValidation` without changing its source.
Separate baseline/final binaries run on the same Railway deployment in ABBA then
BAAB order: eight processes, GOMAXPROCS 1 and 4, a 500-ms minimum per case and
four observations per variant/cell, **96 observations total**. Fixtures contain
equal counts of provenance, active and prepared records at depths 0, 128 and
16,384 per collection. The largest therefore has 49,152 total records and is a
stress case, not a typical-workload or capacity claim. Fixed ASCII fields and
fixture construction are outside timing; encode/decode are timed separately and
record counts are verified afterward. Allocation counts/bytes are cumulative per
codec operation, not live heap or RSS. The new comparison concerns queue records;
provenance records have no nested entry ID.

All values are per-cell medians; CPU means GOMAXPROCS.

| Depth per collection | Operation | CPU | µs before → after | Time change | Allocs/op before → after |
| ---: | --- | ---: | ---: | ---: | ---: |
| 0 | encode | 1 | 0.165 → 0.171 | +3.43% | 3 → 3 |
| 0 | encode | 4 | 0.179 → 0.193 | +7.92% | 3 → 3 |
| 0 | decode | 1 | 0.201 → 0.212 | +5.47% | 4 → 4 |
| 0 | decode | 4 | 0.221 → 0.229 | +3.85% | 4 → 4 |
| 128 | encode | 1 | 130.777 → 130.570 | -0.16% | 393 → 393 |
| 128 | encode | 4 | 147.608 → 149.946 | +1.58% | 393 → 393 |
| 128 | decode | 1 | 131.400 → 136.979 | +4.25% | 2,341 → 2,341 |
| 128 | decode | 4 | 179.721 → 181.670 | +1.08% | 2,341 → 2,341 |
| 16,384 | encode | 1 | 30,205.365 → 29,498.062 | -2.34% | 49,161 → 49,161 |
| 16,384 | encode | 4 | 25,531.571 → 26,783.599 | +4.90% | 49,161 → 49,161 |
| 16,384 | decode | 1 | 31,741.556 → 32,990.368 | +3.93% | 295,171 → 295,171 |
| 16,384 | decode | 4 | 25,097.069 → 25,277.335 | +0.72% | 295,171 → 295,171 |

The largest absolute difference between median allocated bytes is 3.0 B/op.
Exact byte/allocation values and timing ranges for every cell remain in the analysis.

Individual samples, timing/allocation ranges, binary hashes and commands remain
in [comparison analysis](2026-10-18-outbox-binding/raw/after/comparison-analysis.json)
and [comparison order](2026-10-18-outbox-binding/raw/after/comparison-runs.json).
All results, including regressions, are retained; small sample counts do not
establish statistical significance. These are codec measurements, not end-to-end
S3 or cached-read latency promises. Existing full-journal reads and validated-byte
reuse remain unchanged. No process-memory or storage-capacity bound is inferred.
Benchmark binaries are removed after recording hashes; the scratch runner is
stopped only after export verification and final documentation checks.

## Compatibility, limits and next investigation

Journals whose map identity differs from their stored entry ID now fail to
open/reload, including a missing ID. Restore a valid snapshot before retrying.
Stow does not guess which of conflicting identities was intended or reconstruct
an intent already removed by an earlier acknowledgement. Equality validation is
not a full semantic validator for every record field or a proof that all journal
corruption is detectable. No deployed incidence or valid-writer mismatch is claimed.

Next, investigate caller-supplied ID reuse in Enqueue/Prepare. The shared state
currently assigns into the destination map by entry ID without checking whether
that ID already belongs to another live intent. Reproduce whether a normal API
call can replace queued work or create a journal rejected on the next reload,
and establish intended idempotency before changing that behavior. No such fix is
included here. The measured whole-journal write/fence costs remain the subsequent
performance priority; no indexing or checkpointing is introduced.
