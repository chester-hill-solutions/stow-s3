# Return outbox durability failures and measure journal save cost

Follow-up: [protobuf outbox measurements and implementation](2026-10-17-outbox-protobuf.md).

Outbox persistence reported success after replacing its journal even when opening
or syncing the parent directory failed. A successful acknowledgement could therefore
describe a replacement whose directory entry had not been durably committed.
This change routes persistence through the existing `atomicfile.Write` helper and
returns those errors. Journal-depth measurements also establish the remaining
full-rewrite cost; no unchanged-save optimization is included.

The starting revision is `e95d66c` (the completed cache-admission optimization).
A fresh fetch reports `origin/main` at
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor.
[Exact baseline](2026-10-17-outbox-durability/raw/baseline.json).
Prior COPY, recovery, listing and cache work is retained. This is one correctness
change, discovered while measuring the next performance finding.

## Original defect and deterministic reproduction

The native test runs as UID 10001 on Railway. It seeds an active intent, a prepared
intent and provenance, then makes the journal directory writable and searchable
but unreadable (`0333`). File replacement still succeeds; opening the parent to
sync it returns a permission error. A changed inode proves the injected failure
occurs after replacement, rather than before the write. This reproduces a real
directory-open failure; it is not a simulated power-loss test or a direct injection
of a directory `Sync`/`Close` failure.

On the original implementation, provenance save, enqueue, prepare, commit,
acknowledge and close all return success. Adapter, runtime and HEAD reads also
return success without a completed provenance fence. The assertions fail twice
normally and twice under race before production changes:
[original normal failure](2026-10-17-outbox-durability/raw/fence-regression/reproduce-fence.log), [original race failure](2026-10-17-outbox-durability/raw/fence-regression/reproduce-fence-race.log).

The final regression additionally requires the acknowledged in-memory snapshot to
remain unchanged after the error. Once permissions recover, reopening must read
the visible replacement, preserve monotonically increasing IDs, retain `0600`
permissions and leave no temporary files. API tests require two consecutive reads
under the same fault to fail, including when the first replacement already contains
the same observed ETag. They verify both upstream readers close, no cache entry is
published, and a repaired retry succeeds. The second-attempt assertion and `0600`
assertion were strengthened after the original red run. All final cases pass twice
normally and twice under race without skips on Railway.

The initial investigation also retained a failing diagnostic that an unchanged
provenance save replaces the journal inode. That is a measured performance property,
not an existing no-rewrite contract. The diagnostic is absent from the final tests:
[initial rewrite diagnostic](2026-10-17-outbox-durability/raw/rewrite-diagnostic/reproduce.log). Existing outbox race controls passed on that baseline.

## Small implementation change and contracts

[file_outbox.go](../../internal/runthrough/file_outbox.go) preserves serialization
and delegates replacement to [atomicfile.Write](../../internal/atomicfile/atomicfile.go)
with mode `0600`. This removes the duplicated temporary-file/write/sync/rename/
directory-sync implementation. The shared helper propagates every stage's error
and preserves `errors.Is` causes. No public API, dependency, journal format,
index, checkpoint or storage abstraction is added.

Existing process/file locks, fresh reload under the file lock, cloning, state
validation, sequence/token behavior and assignment after successful persistence
remain in place. Every operation still performs its full journal rewrite.
The shared helper's existing Windows directory-sync policy remains unchanged;
the permission-fault tests intentionally skip Windows and root execution.

An error after rename is an uncertain persistence result: the new journal may
already be visible. The code does not claim to restore the old file. Subsequent
operations reload that visible journal under the lock; the recovery regression
checks this boundary. Callers previously accepting directory-fence failures now
receive an error, and provenance-dependent reads fail rather than acknowledge an
unfenced observation. Error strings gain the shared helper's context while wrapped
causes remain inspectable. Internal temporary filenames change from `.outbox-*` to
`.tmp-*`. The helper explicitly applies `0600` before file sync and also checks
parent existence. These are the added successful-path operations measured below.

## Railway measurements

All Go execution, formatting, benchmarks, tests, race/conformance, vet, quality,
site and documentation checks run only on the authorized Stow Railway scratch
runner. Local work is source editing, packaging, orchestration, exported-data
analysis and Git. Deployment `dd03f42a-20b3-49c4-8a8e-4ca846213227` reached SUCCESS.
It uses pinned Go 1.25.6, linux/amd64, a 4-CPU / 4-GiB cgroup and persistent ext4
results storage. Commands run as UID 10001 with the existing child reaper.

The benchmark seeds journals in one setup write, outside timing. Depth is the
number of additional provenance rows plus one target row. The mixed workload has
1,024 additional provenance rows and 1,024 pending intents, with fixed timestamps.
Changed saves alternate two equal-length ETags; unchanged saves repeat one value.
The final saved value is checked after timing. `B/op` is cumulative allocation,
not retained heap or peak RSS. `written-B/op` is Linux `/proc/self/io` `wchar`
for the process during the measured loop, not a physical-disk write counter.
CPU labels mean GOMAXPROCS, not affinity or concurrent clients.

### Original cost and identical-binary control

[Plan](2026-10-17-outbox-durability/raw/baseline-identical/plan.json), [identical binary hashes](2026-10-17-outbox-durability/raw/baseline-identical/build.json), [samples](2026-10-17-outbox-durability/raw/baseline-identical/samples.json), [all ranges](2026-10-17-outbox-durability/raw/baseline-identical/summary.json).
The 80 observations use a 100-ms minimum and forward/reverse ordering. Both labels
copy exactly the same binary (identical SHA-256); each row below pools the four
observations. Shared-storage latency varies substantially: the largest CPU-4 case
includes a 425.105-ms observation, retained in the range and all raw results.
There is no discarded adverse control and no claimed fixed storage latency.

| CPU | Unchanged save | Journal bytes | Median ms (range) | Median B/op | Median allocations/op |
|---|---|---:|---:|---:|---:|
| 1 | mixed=1024/unchanged | 508,920 | 14.691 (13.368–16.119) | 3,841,362.5 | 21,617.5 |
| 1 | provenance=0/unchanged | 153 | 3.658 (2.707–4.099) | 4,731.0 | 54.0 |
| 1 | provenance=128/unchanged | 11,545 | 3.972 (3.477–4.448) | 99,624.0 | 835.0 |
| 1 | provenance=16384/unchanged | 1,458,329 | 36.126 (33.094–38.898) | 13,889,908.0 | 98,583.0 |
| 1 | provenance=2048/unchanged | 182,425 | 8.465 (7.958–9.746) | 1,603,616.5 | 12,381.0 |
| 4 | mixed=1024/unchanged | 508,920 | 13.856 (13.277–14.826) | 4,292,487.5 | 21,634.0 |
| 4 | provenance=0/unchanged | 153 | 6.303 (3.547–23.129) | 4,731.0 | 54.0 |
| 4 | provenance=128/unchanged | 11,545 | 3.472 (2.951–3.735) | 100,550.0 | 835.0 |
| 4 | provenance=16384/unchanged | 1,458,329 | 35.374 (34.204–425.105) | 16,862,676.5 | 98,611.0 |
| 4 | provenance=2048/unchanged | 182,425 | 7.535 (7.122–10.910) | 1,730,000.5 | 12,390.0 |

Every unchanged operation writes the full serialized journal: from 153 bytes for
the smallest case to 1,458,329 bytes at depth 16,384. At CPU 1 the latter allocates
about 13.9 MB/op and 98,583 objects. The mixed case writes 508,920 journal bytes per
save. A process-write sample may include a small extra process write; the metric
does not include filesystem metadata amplification. Full reload, clone and JSON
serialization remain size-dependent; this experiment does not isolate their
individual CPU shares or measure lock contention. Those hypotheses remain open.

### Correctness fix: before/after costs

[Plan](2026-10-17-outbox-durability/raw/final-timings/plan.json), [source/binary hashes](2026-10-17-outbox-durability/raw/final-timings/build.json), [all samples](2026-10-17-outbox-durability/raw/final-timings/samples.json), [all ranges](2026-10-17-outbox-durability/raw/final-timings/summary.json), [resources/load](2026-10-17-outbox-durability/raw/final-timings/block-resources.json).
There are 320 final observations: ABBA and BAAB cycles, two repetitions per block,
200-ms minimum, eight observations per workload/version/CPU. Both binaries use
identical final tests; the baseline overlays only `file_outbox.go` from the starting
revision. The candidate uses the exact source that passed validation. The initial
identical-binary cohort is not pooled into this comparison.

| CPU | Save | Median ms before → after | Change | Median B/op before → after | Median allocations/op before → after |
|---|---|---:|---:|---:|---:|
| 1 | mixed=1024/changed | 14.359 → 13.654 | -4.91% | 3,887,255.0 → 3,912,196.0 | 21,618.5 → 21,618.5 |
| 1 | mixed=1024/unchanged | 14.042 → 15.062 | +7.27% | 3,890,320.5 → 3,966,329.0 | 21,619.0 → 21,620.0 |
| 1 | provenance=0/changed | 3.646 → 3.353 | -8.05% | 4,655.0 → 4,687.0 | 54.0 → 53.0 |
| 1 | provenance=0/unchanged | 3.487 → 3.229 | -7.41% | 4,729.5 → 4,687.0 | 54.0 → 53.0 |
| 1 | provenance=128/changed | 3.800 → 3.418 | -10.04% | 99,601.0 → 99,558.5 | 835.0 → 834.0 |
| 1 | provenance=128/unchanged | 3.448 → 3.484 | +1.07% | 99,636.5 → 99,631.0 | 835.0 → 834.0 |
| 1 | provenance=16384/changed | 36.212 → 35.827 | -1.06% | 14,064,749.5 → 13,615,291.5 | 98,585.0 → 98,580.5 |
| 1 | provenance=16384/unchanged | 38.754 → 38.489 | -0.68% | 13,365,607.0 → 13,365,570.0 | 98,579.5 → 98,577.5 |
| 1 | provenance=2048/changed | 8.574 → 7.888 | -8.00% | 1,603,607.0 → 1,603,558.5 | 12,381.0 → 12,380.0 |
| 1 | provenance=2048/unchanged | 8.897 → 8.217 | -7.64% | 1,603,619.0 → 1,603,626.5 | 12,381.0 → 12,380.0 |
| 4 | mixed=1024/changed | 14.997 → 14.871 | -0.84% | 4,354,542.0 → 4,445,673.0 | 21,637.5 → 21,639.0 |
| 4 | mixed=1024/unchanged | 14.658 → 15.157 | +3.40% | 4,292,495.5 → 4,312,801.5 | 21,634.5 → 21,635.0 |
| 4 | provenance=0/changed | 3.324 → 3.057 | -8.01% | 4,662.5 → 4,697.0 | 54.0 → 53.0 |
| 4 | provenance=0/unchanged | 3.560 → 3.049 | -14.34% | 4,731.0 → 4,692.5 | 54.0 → 53.0 |
| 4 | provenance=128/changed | 3.596 → 3.449 | -4.08% | 101,275.0 → 101,011.5 | 836.0 → 835.0 |
| 4 | provenance=128/unchanged | 3.430 → 3.646 | +6.30% | 100,862.0 → 101,307.5 | 836.0 → 835.0 |
| 4 | provenance=16384/changed | 35.882 → 35.190 | -1.93% | 16,213,433.0 → 15,564,114.0 | 98,605.5 → 98,598.5 |
| 4 | provenance=16384/unchanged | 34.283 → 36.309 | +5.91% | 15,464,286.5 → 15,913,705.0 | 98,599.0 → 98,601.5 |
| 4 | provenance=2048/changed | 7.951 → 7.667 | -3.57% | 1,765,776.5 → 1,740,016.0 | 12,392.0 → 12,389.5 |
| 4 | provenance=2048/unchanged | 7.289 → 7.681 | +5.39% | 1,680,029.5 → 1,673,934.5 | 12,386.5 → 12,384.5 |

All timing changes, including slower controls, are retained. Measured slower medians include CPU-1 mixed/unchanged
(14.042 → 15.062 ms, +7.27%), CPU-4 depth-128/unchanged (+6.30%),
CPU-4 depth-2,048/unchanged (+5.39%) and CPU-4 depth-16,384/unchanged
(34.283 → 36.309 ms, +5.91%). CPU-1 depth-128/unchanged is also +1.07%,
and CPU-4 mixed/unchanged is +3.40%. These are real observations, retained
without attributing them conclusively to code or shared-storage variation.
At small depth the helper avoids the old deferred unlink of an already renamed
temporary path: 54 → 53 allocations per save in the smallest case, and 835 → 834
at depth 128 on CPU 1. Large-journal pooling/GC variation prevents treating every
median allocation difference as a fixed per-call saving. These short fsync-heavy
observations quantify this runner's measured cost; they do not establish a universal
latency improvement or absence of regressions on other storage. There is no proposed
performance win from this correctness patch. Both versions still write the same
journal payload on each save, and allocation growth with journal size remains.
The change adds no object-sized buffering and makes no peak-memory claim.

### Longer comparison of unchanged saves

The initial comparison's six slower medians warranted a longer run. This separate
cohort uses identical source and ABBA/BAAB ordering, a one-second minimum and
two repetitions per block, retaining 160 additional observations. It covers all
five unchanged-save workloads at both CPU settings and is not pooled with the
shorter cohort: [Plan](2026-10-17-outbox-durability/raw/long-unchanged/plan.json), [source/binary hashes](2026-10-17-outbox-durability/raw/long-unchanged/build.json), [all samples](2026-10-17-outbox-durability/raw/long-unchanged/samples.json), [all ranges and allocations](2026-10-17-outbox-durability/raw/long-unchanged/summary.json), [resources/load](2026-10-17-outbox-durability/raw/long-unchanged/block-resources.json).

| CPU | Unchanged save | Median ms before → after | Change | Before range ms | After range ms |
|---|---|---:|---:|---:|---:|
| 1 | mixed=1024/unchanged | 14.034 → 14.685 | +4.64% | 13.453–15.816 | 13.085–26.914 |
| 1 | provenance=0/unchanged | 3.522 → 3.351 | -4.84% | 3.171–7.032 | 3.126–6.695 |
| 1 | provenance=128/unchanged | 4.049 → 3.382 | -16.48% | 3.639–7.235 | 3.091–3.815 |
| 1 | provenance=16384/unchanged | 35.966 → 36.959 | +2.76% | 33.648–69.018 | 35.024–38.850 |
| 1 | provenance=2048/unchanged | 8.049 → 8.040 | -0.12% | 7.091–14.333 | 7.373–10.964 |
| 4 | mixed=1024/unchanged | 14.732 → 15.585 | +5.79% | 13.888–24.962 | 14.046–23.644 |
| 4 | provenance=0/unchanged | 3.361 → 3.736 | +11.17% | 3.041–6.380 | 2.860–7.397 |
| 4 | provenance=128/unchanged | 3.884 → 4.198 | +8.09% | 3.246–6.872 | 3.649–11.287 |
| 4 | provenance=16384/unchanged | 36.634 → 38.253 | +4.42% | 33.344–72.746 | 32.730–55.037 |
| 4 | provenance=2048/unchanged | 8.266 → 9.647 | +16.71% | 7.564–10.378 | 7.549–15.570 |

Seven of ten longer-run medians are slower, including all five CPU-4 workloads
(+4.42% to +16.71%). CPU-4 depth 2,048 rises from 8.266 to 9.647 ms; CPU-1 mixed
rises from 14.034 to 14.685 ms (+4.64%). The two cohorts have identical source and
binary hashes. Overlapping ranges and the identical-binary control show substantial
storage variation, but do not prove these observed costs are noise. This change
is accepted as a correctness fix with a measured latency cost, not as a performance
optimization or a claim of neutrality. Isolating the added parent check/permission
update from storage variation remains unmeasured. The complete 560 observations
(80 control, 320 short comparison, 160 longer comparison) are retained.

## Validation and evidence

The complete final validation matrix passed on Railway:
[Commands/results](2026-10-17-outbox-durability/raw/final-checks/checks.json), [exact source hashes](2026-10-17-outbox-durability/raw/final-checks/environment.json), [export receipt](2026-10-17-outbox-durability/raw/final-checks/export-receipt.json).

| Check | Exit | Seconds |
|---|---:|---:|
| format | 0 | 0.02 |
| focused-normal | 0 | 7.41 |
| focused-race | 0 | 12.02 |
| full-go | 0 | 59.38 |
| broad-race | 0 | 64.74 |
| conformance-memory | 0 | 4.63 |
| conformance-filesystem | 0 | 5.25 |
| conformance-runtime-memory | 0 | 4.91 |
| conformance-runtime-filesystem | 0 | 3.98 |
| vet | 0 | 7.55 |
| quality | 0 | 0.40 |
| site-build | 0 | 0.49 |
| site-tests | 0 | 0.57 |
| doc-links | 0 | 0.08 |

The normal/race regressions cover directory-fence failure and reopening. The broad
race pass covers storage, runtime, embedded, run-through, S3 and the public package.
Conformance runs memory, filesystem, runtime-memory and runtime-filesystem profiles.
The full Go suite includes the existing shared atomic-writer tests. Quality and
file-size baselines are unchanged. Final report/document checks also run on Railway:
[report receipt](2026-10-17-outbox-durability/raw/final-checks/report-receipt.json).

Raw logs, plans, formatted source snapshots, build/source/binary hashes, resource
snapshots and length/SHA-256-verified export receipts are retained. Test binaries
are not committed. The initial tracked-source manifest records the upload before
the staged source edits; each later job records its own final changed-source hashes.
The evidence includes expected failed regression commands, not suppressed failures.
The runner is stopped after export; the benchmark target remains untouched:
[verified service status](2026-10-17-outbox-durability/raw/runner-stop-status.json).

## Next finding, not implemented

An unchanged `SaveUpstreamState` still reloads, clones, serializes and durably
replaces the entire journal. The measurements justify evaluating a small unchanged-
save path before any indexing or incremental checkpointing. No such optimization
is implemented in this change.

Any candidate must compare the complete value and key presence after a fresh reload
under the existing file lock, preserve stale-instance/concurrent-writer safeguards,
and still complete a durability fence before acknowledging a value that may have
been left visible by a previous failed replacement. The repeated-read regression
guards that last requirement. Equality with an on-disk ETag alone is not evidence
of a completed fence. Measure that candidate comparatively on Railway before
accepting it; do not weaken the newly enforced failure behavior to save a rewrite.
