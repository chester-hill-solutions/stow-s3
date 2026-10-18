# Copy only the requested outbox snapshot — 2026-10-18

## Baseline, evidence and change

After `8278d32` removed repeat decoding of byte-identical journals, pending and
prepared snapshots still called `readState`, cloning provenance and both queues
before returning just one queue. Propagation ordering/retries, prepared recovery
and inspection use those existing paths. `origin/main` was refreshed before this
work; exact baseline commits are in [baseline.json](2026-10-18-outbox-snapshot/raw/baseline.json).

The Railway pilot demonstrated this remaining cost before production adoption.
For 16,384 provenance rows plus one target and one entry in each queue, a CPU-1
pending snapshot took 1.176 → 0.209 ms and allocated 2,502,064 → 926,656 B/op.
A one-entry prepared snapshot alongside 16,384 active entries took 5.897 → 0.557 ms
and allocated 8,281,312 → 1,639,360 B/op. The complete baseline and isolated
prototype file-outbox suites passed twice normally and twice with race detection.

The change splits the existing value-copy and sorting code into small private
helpers. Each filesystem snapshot copies only its requested queue while holding
the existing mutex and file lock, then sorts the owned slice after both locks
are released. The comparator is unchanged: CreatedAt, then numeric outbox ID for
equal timestamps. Empty results remain non-nil; failed snapshots return nil plus
the error. Errorless last-known-state fallbacks and memory-outbox behavior are
preserved. The full-state read helper is removed; its test-only caller now makes
its own clone through the existing lock helper. No API, dependency, storage
format, cache, pooling, index, checkpoint or storage abstraction is added.

Complete journal reads and validation still precede selection. Every operation
uses the existing fresh-read/byte-equality behavior, so corrupt or unsupported
unrequested state is not ignored. Writes, claim fencing and durability are
unchanged. A result still owns copied OutboxEntry values, rather than a map or
slice that future callers/writers can mutate.

## Correctness and validation

New black-box tests cover complete field preservation, numeric-ID tie ordering,
caller mutation of returned entries, a held result across peer writes, non-nil
empty results, nil results on journal errors, repair after corruption, and
concurrent readers/writers through shared and separate handles. They run against
both baseline and prototype, then the final production source. Existing tests
cover cross-process coordination, missing/truncated/null/unknown journals,
permissions, migration, recovery, ownership/tokens and directory-fence failures.

All 15 main Railway stages passed: formatting; focused file-outbox tests twice
normally and twice under race detection; the full Go suite; broad storage/runtime/
embedded/run-through/S3/public-package race tests; four conformance profiles; vet;
quality/file-size/format gates; site build/tests; documentation links; comparison.
The added matched-load diagnostic subsequently passed formatting, quality, the
complete run-through suite normally and under race detection, and vet. Its Go
production sources match the main verification exactly. Tail and phase studies
also completed successfully. [Main checks](2026-10-18-outbox-snapshot/raw/main/checks.json),
[follow-up checks](2026-10-18-outbox-snapshot/raw/follow/checks.json) and their environments record exact
commands/source hashes. No baseline was raised.

## Method and limits

All compilation, formatting, tests and measurements ran on Railway. Local work
was editing, Git, packaging, hashing and analysis of exported data. Pinned Go
1.25.6 and Node 25.9.0 used a 4-CPU/4-GiB cgroup and ext4 `/results`. CPU 1/4
mean GOMAXPROCS, not client count or affinity. Synthetic fixtures are seeded in
a separate process, excluding encoder construction from benchmark RSS. They use
fixed-size validators, numeric IDs and equal CreatedAt values (exercising the
numeric tie comparator). Counts and initial hashes are retained per cohort.
Large shapes are stress cases, not typical deployments or capacity guarantees.

Every fixture contains one target provenance row. `empty` has no other rows;
`small` has 2,048 extra provenance rows and 128 entries in each queue. Provenance
fixtures have their named extra provenance depth and one entry in each queue.
`active=16384`/`prepared=16384` have the named queue depth and one entry in the
other queue, without extra provenance. `mixed=2048` has 16,384 extra provenance
rows plus 2,048 entries in each queue. Snapshot contents/count/order are checked
outside timing. The prior returned slice is kept alive through the next read.

Pilot: four shapes × two views × CPU 1, ABBA, 100-ms minimum: 32 observations.
Main timing: eight shapes × two views × CPU 1/4, ABBA then BAAB, 500-ms minimum:
256 observations, four per variant/cell. Each observation has a separate process;
CPU 4 reverses fixture/view order. These are serial, warm-file snapshots, not
end-to-end S3 or concurrency/cold-cache guarantees. Fixed-count memory probes
add 48 observations: three shapes × two views × CPU 1/4, ABBA, 32 timed reads.

The main contention probe adds 16 observations: a single unthrottled snapshot
reader alongside 16 serial changed provenance saves, two large-queue shapes,
CPU 1/4 and ABBA. The follow-up adds 64 observations, using 32 writes, ABBA/BAAB,
and both unthrottled and exactly one-snapshot-per-write modes. A final targeted
cohort adds eight CPU-1 active-queue observations with 64 writes and byte-identical
binaries to the follow-up (SHA equality is required before execution).

There are **424 retained benchmark observations**. Cohorts and workload modes
are not pooled or substituted. B/op in pure-read cases is cumulative allocation
per snapshot; contention B/op includes writer and reader allocations divided by
writer count and must not be treated as isolated write allocation. Contention
ns/op includes observer startup/drain; per-write maxima time the Save call itself.
Lower p50/p95 order statistics are retained as diagnostics, not latency SLAs.

Process HWM is lifetime VmHWM through measured work, including constructor,
warm-up and calibration. Live heap is HeapAlloc after verification and forced GC
while the handles and last returned slice remain live. Both include runtime/test
overhead, not just Stow data. RSS excludes parent-runner overhead and buffered
kernel file cache. Fixed-count probes supplement timing probes because faster
code completes more work per second. No bounded process-memory claim is made.

## Snapshot results

All values are before → after medians. Allocated bytes are B/op. Both views include
all their requested entries; other categories and provenance are not returned.

CPU 1:

| Fixture | Pending ms | Prepared ms | Pending B/op | Prepared B/op |
| --- | ---: | ---: | ---: | ---: |
| empty | 0.007 → 0.007 | 0.007 → 0.007 | 1,600 → 1,104 | 1,600 → 1,104 |
| small | 0.203 → 0.093 | 0.194 → 0.090 | 489,568 → 189,408 | 489,568 → 189,408 |
| provenance=16384 | 1.151 → 0.181 | 1.156 → 0.189 | 2,502,064 → 926,656 | 2,502,064 → 926,656 |
| provenance=65536 | 6.550 → 0.955 | 6.795 → 1.001 | 9,977,136 → 3,679,168 | 9,977,136 → 3,679,168 |
| provenance=262144 | 55.937 → 4.839 | 50.411 → 4.730 | 39,877,552 → 14,689,216 | 39,877,552 → 14,689,216 |
| active=16384 | 25.773 → 17.784 | 7.326 → 0.549 | 13,786,384 → 7,144,432 | 8,281,312 → 1,639,360 |
| prepared=16384 | 5.613 → 0.452 | 24.599 → 16.512 | 8,035,552 → 1,393,600 | 13,540,624 → 6,898,672 |
| mixed=2048 | 4.430 → 2.087 | 4.123 → 1.930 | 5,217,984 → 1,983,472 | 5,217,984 → 1,983,472 |

CPU 4:

| Fixture | Pending ms | Prepared ms | Pending B/op | Prepared B/op |
| --- | ---: | ---: | ---: | ---: |
| empty | 0.007 → 0.007 | 0.007 → 0.007 | 1,600 → 1,104 | 1,600 → 1,104 |
| small | 0.238 → 0.083 | 0.243 → 0.086 | 489,570 → 189,408 | 489,572 → 189,409 |
| provenance=16384 | 1.373 → 0.241 | 1.434 → 0.228 | 2,502,066 → 926,656 | 2,502,065 → 926,656 |
| provenance=65536 | 7.052 → 1.030 | 7.221 → 0.938 | 9,977,136 → 3,679,168 | 9,977,138 → 3,679,168 |
| provenance=262144 | 49.344 → 4.000 | 48.582 → 4.065 | 39,877,552 → 14,689,216 | 39,877,556 → 14,689,216 |
| active=16384 | 19.199 → 16.015 | 6.411 → 0.457 | 13,786,389 → 7,144,434 | 8,281,342 → 1,639,360 |
| prepared=16384 | 5.918 → 0.359 | 18.714 → 14.146 | 8,035,557 → 1,393,600 | 13,540,630 → 6,898,672 |
| mixed=2048 | 4.179 → 1.630 | 4.054 → 1.475 | 5,217,990 → 1,983,474 | 5,217,989 → 1,983,474 |

No pure-snapshot pilot/main median regressed. All ranges and individual samples,
including contention regressions, remain in [main observations](2026-10-18-outbox-snapshot/raw/main/samples.json),
[main analysis](2026-10-18-outbox-snapshot/raw/main/analysis.json), [pilot observations](2026-10-18-outbox-snapshot/raw/pilot/samples.json)
and [pilot analysis](2026-10-18-outbox-snapshot/raw/pilot/analysis.json). Small diagnostic sample counts do
not establish statistical significance or universal latency improvements.

## Contended writes, adverse results and follow-up

This table retains the original unthrottled probe. Completion ms/write includes
reader startup/drain; snapshot counts show the changed amount of background work.
Max write time is the largest observed Save latency across those cells.

| CPU | Queue | Completion ms/write | Snapshot count (median) | Max write ms |
| ---: | --- | ---: | ---: | ---: |
| 1 | active=16384 | 88.241 → 94.386 | 11.0 → 15.0 | 142.647 → 177.797 |
| 1 | prepared=16384 | 77.038 → 92.751 | 11.0 → 14.5 | 120.872 → 139.639 |
| 4 | active=16384 | 60.802 → 50.045 | 15.5 → 13.5 | 82.113 → 65.756 |
| 4 | prepared=16384 | 54.475 → 48.111 | 14.5 → 13.5 | 76.183 → 63.892 |

The CPU-1 prepared case's +20.40% completion median was not discarded. Its reader
completed 11 → 14.5 snapshots per 16 writes, so read work was not held constant.
That observation motivates the follow-up; it does not prove a sole cause.

Follow-up with 32 writes per observation:

| Mode | CPU | Queue | Completion ms/write | Snapshot count (median) | Max write ms |
| --- | ---: | --- | ---: | ---: | ---: |
| writer-matched | 1 | active=16384 | 105.402 → 96.687 | 32.0 → 32.0 | 149.342 → 436.146 |
| writer-matched | 1 | prepared=16384 | 102.240 → 96.620 | 32.0 → 32.0 | 120.470 → 146.885 |
| writer-matched | 4 | active=16384 | 69.418 → 60.002 | 32.0 → 32.0 | 75.724 → 77.928 |
| writer-matched | 4 | prepared=16384 | 58.055 → 60.378 | 32.0 → 32.0 | 68.693 → 71.362 |
| writer-unthrottled | 1 | active=16384 | 93.540 → 85.147 | 25.5 → 25.0 | 169.850 → 147.323 |
| writer-unthrottled | 1 | prepared=16384 | 79.807 → 80.880 | 19.5 → 25.0 | 147.030 → 133.745 |
| writer-unthrottled | 4 | active=16384 | 52.677 → 52.064 | 20.5 → 26.0 | 78.916 → 80.272 |
| writer-unthrottled | 4 | prepared=16384 | 51.052 → 50.156 | 24.0 → 21.0 | 88.664 → 90.231 |

The matched CPU-1 medians improved 5.50–8.27%; CPU-4 prepared completion was +4.00%.
The follow-up also contained an after maximum of 436.15 ms. The longer targeted
64-write cohort, using byte-identical binaries, measured 107.295 → 107.577 ms/write
(+0.26%), with observed maxima 144.315 → 378.833 ms.
That includes another after outlier; neither tail nor the original +20.40%
median is replaced by a favorable later measurement. Timing ranges overlap;
no storage/scheduling-versus-code statistical attribution is claimed.

[Writer follow-up](2026-10-18-outbox-snapshot/raw/follow/samples.json), [follow-up ranges](2026-10-18-outbox-snapshot/raw/follow/analysis.json),
[targeted observations](2026-10-18-outbox-snapshot/raw/tail/samples.json), [targeted ranges](2026-10-18-outbox-snapshot/raw/tail/analysis.json)
and [binary hashes](2026-10-18-outbox-snapshot/raw/tail/comparison-build.json) retain the complete evidence.

## Phase attribution diagnostic

A separate diagnostic uses test-only source overlays, with no shipping-source
changes. Four ABBA processes perform 128 writes each, one concurrent snapshot per
write, CPU 1: **512 phase-traced writes**. Validators are monotonic equal-length
values in this diagnostic (rather than alternating values), and timing hooks add
overhead, so its timings are not pooled with benchmark observations. The hooks
record lock wait, reload, operation, encoding and atomic-write durations.
Operation includes encoding/atomic write; these nested durations are not additive.

| Variant | Writes | Median Save ms | Max Save ms | Atomic-write time in that max | Lock wait in that max |
| --- | ---: | ---: | ---: | ---: | ---: |
| before | 256 | 68.950 | 189.806 | 128.883 ms | 0.048 ms |
| after | 256 | 71.703 | 494.769 | 448.903 ms | 0.047 ms |

The 494.77-ms after event spent 448.90 ms in the unchanged atomic-write phase
and 0.047 ms waiting for the locks. It was not a long snapshot-lock wait. That
phase includes file operations/fences and any runtime/scheduling delays within
it; no syscall-level attribution separates them. This does not establish a
filesystem-only cause or a universal absence of writer-tail costs. Durability
fences are preserved. [Every phase record](2026-10-18-outbox-snapshot/raw/trace/write-phases.json),
[phase analysis](2026-10-18-outbox-snapshot/raw/trace/phase-analysis.json), [commands](2026-10-18-outbox-snapshot/raw/trace/trace-commands.json)
and [overlay/binary hashes](2026-10-18-outbox-snapshot/raw/trace/trace-build.json) are retained. No debug hook
is included in the production implementation.

## Memory, cost and compatibility

There is no new retained cache or per-handle field. The existing journal buffer,
full-file read/validation costs and requested result slice remain. Copying the
requested result slice now happens under the existing locks, replacing the
larger full-state map clone there; sorting remains outside. This changes allocation
and scheduling timing, so contended-write results are reported above rather than
claiming a universal writer-latency improvement.

Fixed-count memory probes below show maximum observed whole-process RSS and median
post-GC live heap, MiB. They are different quantities and are not deployment limits.

| CPU | Fixture/view | Max HWM RSS MiB | Live heap MiB |
| ---: | --- | ---: | ---: |
| 1 | active=16384/pending | 70.3 → 56.1 | 14.76 → 14.76 |
| 1 | active=16384/prepared | 56.5 → 41.1 | 9.52 → 9.52 |
| 1 | prepared=16384/pending | 52.4 → 38.9 | 8.91 → 8.91 |
| 1 | prepared=16384/prepared | 70.3 → 56.2 | 14.16 → 14.16 |
| 1 | provenance=16384/pending | 30.0 → 22.2 | 3.64 → 3.64 |
| 1 | provenance=16384/prepared | 29.9 → 24.0 | 3.64 → 3.64 |
| 4 | active=16384/pending | 74.2 → 56.0 | 14.78 → 14.78 |
| 4 | active=16384/prepared | 54.3 → 49.8 | 9.53 → 9.53 |
| 4 | prepared=16384/pending | 53.2 → 49.1 | 8.92 → 8.92 |
| 4 | prepared=16384/prepared | 72.0 → 55.7 | 14.17 → 14.17 |
| 4 | provenance=16384/pending | 25.2 → 27.0 | 3.65 → 3.65 |
| 4 | provenance=16384/prepared | 25.2 → 25.1 | 3.65 → 3.65 |

No public fields/order/error/empty-result/storage-format changes are intended.
Native versus memory profiles retain the same value-copy/sort logic. Snapshot
ordering/copying still scales with requested queue size; journal reads still
scale with all retained state. Cache object/byte limits do not bound journal
history. [Fixture sizes](2026-10-18-outbox-snapshot/raw/main/fixtures.json), [runtime settings](2026-10-18-outbox-snapshot/raw/main/runtime-settings.json)
and per-cohort source/binary hashes record the measurement context and limits.
All raw observations, harnesses and overlays are retained. Binaries and fixture
directories are removed after hashes are saved, and the scratch runner is stopped
after verified export and final report/documentation checks on Railway.

## Next correctness check

Reproduce ambiguous prepared-record migration before further performance work.
`normalizeDecodedOutboxState` currently assigns a Prepared-marked active-map
record into the prepared map before checking overlap. Check whether a conflicting
record with the same ID can be silently replaced, then demonstrate the failure
with a deterministic test before changing that implementation. No recovery
normalization change is included here.
