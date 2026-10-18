# Single-key outbox lookup allocation review

Date: 2026-10-17 (Toronto; Railway receipts use UTC).
Baseline: `a56510419d06adc3f561a6e8c354aae22153d193` on `codex/core-copy-quality`; freshly fetched
`origin/main` is `219d1dcfff9807b04660a40313b32127e5be2933`. The worktree started clean.
The preceding [protobuf change](2026-10-17-outbox-protobuf.md) is included in both variants.

## Evidence before the change

`FileOutbox.UpstreamState` called `readState`, which acquired the process mutex and
filesystem lock, reloaded the full journal, then cloned provenance, pending and
prepared maps to return one value. This was an allocation inefficiency; the
lookup contract itself was correct.

Before editing the implementation, an allocation profile on Railway attributed
155,276.73 KiB (22.46% of all sampled allocated bytes) to `outboxState.clone`.
The profile includes fixture setup and benchmark calibration. It samples every
allocation and imposes substantial timing overhead; its latency is excluded from
the comparison below. Most allocated objects came from protobuf decoding, so
removing the clone should save bytes while leaving most allocation counts intact.
See the [original profile](2026-10-17-outbox-lookup/raw/baseline-profile/lookup-profile.log).

## Change and preserved contracts

The existing locked reload now accepts an internal read callback. A provenance
lookup copies its requested `UpstreamState` while both locks remain held.
`readState` still clones the full state for its existing snapshot callers.
All journal reads, decoding, format validation, sequence/token reconstruction,
write transactions and durability fences remain in their existing paths.

Regression tests cover independent file-outbox instances observing each other's
updates, known-empty versus missing state, expected absence and unknown-state
flags, bucket separation, repeated journal/lock failures with no stale successful
result, recovery after repair, and concurrent readers plus a writer sharing one
or two instances. The initial contract tests passed twice normally and twice
under race detection against the original implementation before replacement.

No dependency, public API or persistence format changes. JSON migration and
protobuf schema 4 behavior are unchanged. No journal caching, indexing or
incremental persistence was introduced.

## Railway comparison

The actual `BenchmarkFileOutboxLookup` operation is unchanged. Original binaries
overlay only `file_outbox.go` and `provenance.go` from the baseline commit; candidate
binaries use the final implementation. Both use identical final test/benchmark
fixtures and build flags. Source and binary hashes are retained in
[build metadata](2026-10-17-outbox-lookup/raw/lookup-comparison/build.json).

There are **160 observations**: five shapes × two variants × two CPU settings ×
eight observations per cell. For each CPU setting, ABBA and BAAB blocks each run
two repetitions, with a one-second minimum per benchmark observation. CPU labels
are `GOMAXPROCS`, not CPU affinity or concurrent clients. The runner has a four-CPU,
4 GiB cgroup limit. Benchmark temporary files use its ext4 `/results` volume;
measurements are warm steady-state local-file lookups, with no upstream requests.

Provenance depths count extra rows plus one target row. The mixed fixture has
1,024 extra provenance rows, one target row and 1,024 pending entries. Every
operation reloads and decodes the full file under the filesystem lock. Every
observation reports zero written bytes. Journal bytes are identical between
variants. B/op is cumulative allocated bytes, not RSS or retained heap.

All rows are medians, with before → after and relative change. Raw observations,
block order, resource counters, ranges and environment metadata are retained;
these medians do not establish statistical significance or cold-cache performance.

| CPU | Fixture | Latency, µs/op | B/op | Allocations/op |
| --- | --- | --- | --- | --- |
| 1 | mixed=1024 | 1,082.34 → 876.88 (-18.98%) | 1,882,549 → 1,368,960 (-27.28%) | 13,375 → 12,338 |
| 1 | provenance=0 | 7.79 → 7.62 (-2.19%) | 2,416 → 1,920 (-20.53%) | 22 → 18 |
| 1 | provenance=128 | 31.02 → 26.65 (-14.09%) | 50,800 → 39,736 (-21.78%) | 417 → 411 |
| 1 | provenance=16384 | 4,113.12 → 3,325.60 (-19.15%) | 6,875,952 → 5,301,664 (-22.90%) | 49,321 → 49,253 |
| 1 | provenance=2048 | 394.21 → 308.47 (-21.75%) | 844,400 → 647,488 (-23.32%) | 6,194 → 6,182 |
| 4 | mixed=1024 | 1,636.31 → 1,318.85 (-19.40%) | 1,882,572 → 1,368,964 (-27.28%) | 13,375 → 12,338 |
| 4 | provenance=0 | 7.84 → 7.80 (-0.59%) | 2,416 → 1,920 (-20.53%) | 22 → 18 |
| 4 | provenance=128 | 33.39 → 30.34 (-9.16%) | 50,800 → 39,736 (-21.78%) | 417 → 411 |
| 4 | provenance=16384 | 6,151.47 → 4,881.49 (-20.65%) | 6,875,961 → 5,301,668 (-22.90%) | 49,321 → 49,253 |
| 4 | provenance=2048 | 517.44 → 427.28 (-17.42%) | 844,402 → 647,490 (-23.32%) | 6,194 → 6,182 |

Adverse latency medians: none in this cohort.

[All observations](2026-10-17-outbox-lookup/raw/lookup-comparison/samples.json),
[medians and ranges](2026-10-17-outbox-lookup/raw/lookup-comparison/analysis.json),
[per-block resources](2026-10-17-outbox-lookup/raw/lookup-comparison/block-resources.json),
and [final allocation profile](2026-10-17-outbox-lookup/raw/final-profile/lookup-profile.log).
The final profile attributes only 288 bytes to cloning during fixture setup;
lookup calls no longer clone the maps.

## Validation and costs

All 14 final stages passed on Railway: formatting; focused tests twice normally
and twice under race detection; the complete Go suite; broad storage/runtime/
embedded/run-through/S3/public-package race tests; all four conformance profiles
(memory, filesystem, runtime-memory and runtime-filesystem); vet; quality,
file-size and formatting gates; site build, site tests and documentation links.
See [final checks](2026-10-17-outbox-lookup/raw/source-checks/checks.json).

The initial quality check rejected complex new test functions. Test setup and
repeated read/write loops were simplified; production source did not change, and
no quality baseline was raised. The rejected cohort is retained under
`source-checks-initial`. The final complete matrix includes the simplified tests.
Local source hashes match all five Railway-validated source files.

The new internal callback runs under both existing locks. Its current callers
retain only a value or an owned clone; future callers must keep that ownership
contract. Most allocation counts and the full-journal read/parse cost remain.
The measured lookup cost is still linear in journal size. This change makes no
bounded-memory, write-throughput or whole-product performance claim. All source
compilation, formatting, tests and measurements ran on Railway.

The final report and retained evidence are also checked on Railway against exact
source and document hashes; see the
[report receipt](2026-10-17-outbox-lookup/raw/source-checks/report-receipt.json).
The scratch runner is stopped after evidence export.

## Next highest-priority finding

A zero-byte journal bypasses decoder validation in `reloadLocked`. After a durable
enqueue and forced truncation, opening the file, reading provenance and taking a
pending snapshot all succeed with empty state. A subsequent enqueue also succeeds
and reuses `outbox-1`. This turns corrupt persistence into apparently valid empty
recovery state and resets intent identity.

An isolated test overlay reproduced all four cases twice normally and twice under
race detection on Railway. Failures are assertion failures, with no compile failure
or race report. See the [normal reproduction](2026-10-17-outbox-lookup/raw/next-finding/empty-journal-normal.log)
and [race reproduction](2026-10-17-outbox-lookup/raw/next-finding/empty-journal-race.log).
The reproduction source is retained as evidence; it is not added as a failing test
to the passing suite. This finding is **not fixed by the lookup change** and takes
priority over avoiding unchanged-save rewrites. Missing-path initialization must
remain valid when correcting it.
