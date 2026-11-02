# Reject excess provider bytes in runtime reads

Corrected on 2026-10-16 on `codex/core-copy-quality`, based on `9f33b37`.
Freshly fetched `origin/main` is `219d1dcfff9807b04660a40313b32127e5be2933`,
an ancestor of this branch. The previous
[stream EOF correction](../2026-10-16-stream-completeness/README.md) remains intact.
[Source and binary identities](raw/environment.json) pin the measured implementation.

## Before: contradictory bodies succeed and consume the entire payload

The retained probe advertises three bytes and supplies `old!`. `OpenObject`,
`GetObject` and `ReadForSave` succeed with four bytes. The guarded path fingerprints
those wrong bytes; the backend refuses that fingerprint. Six semantic failures
occur in each [normal](raw/original-before.log) and [race](raw/original-before-race.log)
run (three cases repeated twice). Source remains in the preceding
[report](../2026-10-16-stream-completeness/raw/next-excess-probe.go.txt).
This is injected provider inconsistency, not spontaneous native-store corruption.

Before production edits, all **41 new regression cases** were run twice:
[38 defect cases fail and three controls pass](raw/before-tests.log) per repetition.
The matrix covers raw/adapter/WriterTo/buffered/guarded/public reads, a claimed
zero-byte body, coalesced EOF, original provider errors, seek positions and
native/fallback ranges. A finite 1 MiB body advertised as three bytes is consumed
in full before the fix. Tests verify bounded failure, closure, invalid save
conditions, unchanged accounting and healthy full-capacity recovery afterward.

Incorrect size metadata alone does not demonstrate a guard bypass: a provider
returning the entire actual stored body can still produce its valid fingerprint.
The before tests explicitly attempt a save with such a condition; that requested
save can commit. The wrong-content fingerprint in the original four-byte probe
is refused. The correction prevents issuing either malformed observation.

## Correction and limits

The existing stream reader calculates remaining declared bytes. If a caller's
buffer could cross the end, it passes at most `remaining + 1` bytes to the provider.
An actual overrun returns only the valid prefix and a shared private size error.
An existing non-EOF provider error is preserved instead. The reader records actual
provider progress, closes once on error and retains subsequent `io.ErrClosedPipe`.
The extra byte is not returned in the read count. No additional scratch buffer,
reader wrapper, field, lock, open or HEAD call is introduced. The same check
propagates through the existing WriterTo path, buffered GET and guarded reads.

From position zero without seeks, a conforming io.Reader supplies at most declared
size plus one byte before failure. At logical EOF the next read can probe one
byte; a zero-length read does not force a probe. Slice arithmetic only adds one
when remaining is smaller than the caller's buffer, avoiding `Size + 1` overflow.
Provider buffers shrink near the declared end, which can change chunking or EOF
coalescing. Normal EOF, premature EOF, negative unknown-size hints, successful/failed seeks,
partial close, original errors, source versions and runtime/policy checks retain
their behavior. A seek beyond the actual end still returns normal EOF.

Ranges use their requested length. A native provider oversupplying the range is
refused; fallback range readers deliberately stop at the requested interval and
do not inspect payload beyond it. Consumers that stop after a partial/exact-size
read or close early do not validate unread trailing bytes. No eager prefetch or
new close-time validation is added.

Providers with inaccurate nonnegative sizes, including default zero for a nonempty
object, now fail rather than succeeding with contradictory data. Negative sizes
retain their existing unknown-size behavior. Whole-object APIs still materialize
valid complete bodies; the existing initial 32 MiB capacity clamp is unchanged.
This does not bound provider-internal prefetch or all unknown-size/whole-object
memory use. No dependency, public error/API, storage format or indexing machinery
is added. The private error is shared rather than allocated per overrun.

## Validation

- [New and prior read regressions](raw/after-tests.log): **193 leaves twice**, all pass.
- [Focused race matrix](raw/focused-race.log): **198 leaves five times = 990 passes**,
  including policy, captured-version and concurrent-stream tests; no skips or races.
- The same original three paths pass twice [normally](raw/original-after.log) and
  [under race](raw/original-after-race.log), with [equivalent post-fix assertions](raw/after-probe.go.txt)
  adapted to the zero-object/valid-prefix error contract.
- [Full Go suite](raw/full-go.log), [broad storage/runtime/S3 race suite](raw/broad-race.log)
  and [vet](raw/vet.log) pass.
- [Four conformance profiles](raw/conformance-summary.log) pass: memory,
  filesystem, runtime-memory and runtime-filesystem. Each retains two existing
  skips: opt-in live upstream and mirror bucket creation awaiting an authority decision.
- [Quality/file-size gates](raw/quality.log), [generated comparison](raw/generated.log),
  [WASM smoke](raw/wasm-smoke.log), [seven package integration tests](raw/package-tests.log)
  and [final format/provenance/documentation checks](raw/final-checks.log) pass.

Reproducibly rebuilt WASM: 4,618,495 → 4,619,001 bytes, **+506 bytes**.
[Build output](raw/build-wasm.log), [package build](raw/package-build.log) and
[hashes](raw/wasm.json) are retained. Complete read-contract tests at this existing
seam would have prevented the missing upper-length check; no abstraction is needed.

## Measurements: safety gain, allocation parity, uncertain latency

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64, native memory backend. CPU labels mean
GOMAXPROCS, not OS affinity. [Compiler flags/experiments](raw/compiler-environment.json)
are default. No concurrent agent builds/tests ran during timing. Unrelated host
work was uncontrolled; [mid-run load averages](raw/midrun-host-load.json) were
20.35 / 28.11 / 22.07. That context does not prove the cause of any sample.
All **1,952 observations** remain: 1,280 interleaved blocks, 432 direct samples,
240 focused process samples. No adverse sample is filtered out.
These are runtime microbenchmarks, not HTTP/disk/throughput or peak-memory claims.
B/op is cumulative allocation, not RSS or peak heap.

### Interleaved complete-reader comparison

One ordinary binary includes frozen `9f33b37` stream methods, its WriterTo method
and original copy-buffer pool, plus unchanged GET/ReadForSave with private renaming.
Reader method names/count and WriterTo are checked before timing:
[source](raw/paired-probe.go.txt), [verification](raw/reference-verification.json),
[plan](raw/plan.json), [all 1,280 blocks](raw/paired.log), [summary](raw/paired-summary.json),
[parser](raw/analyze-paired.py.txt). Ten alternating ABBA/BAAB cycles run twice at
CPU 1 and 8, with duplicate same-code labels. Allocation and process-wide GC
counters are sampled outside timing; they include boundary/runtime work.

| CPU | Case | Median ns before → after | Median paired-cycle change | Same-code absolute median change |
| --- | --- | ---: | ---: | ---: |
| 1 | `tiny` | 2350.05 → 2206.70 | -2.17% | 37.08% |
| 1 | `64KiB-32KiB` | 3538.39 → 3119.20 | -2.54% | 27.97% |
| 1 | `64KiB-1KiB` | 5793.59 → 6158.89 | +11.85% | 27.05% |
| 1 | `seek` | 2603.57 → 2383.63 | -3.63% | 22.00% |
| 1 | `range` | 4644.37 → 4122.91 | +7.91% | 29.42% |
| 1 | `get-tiny` | 1532.87 → 1594.69 | +5.87% | 25.48% |
| 1 | `get-64KiB` | 17554.35 → 18718.67 | +6.30% | 38.14% |
| 1 | `save-64KiB` | 68246.46 → 65302.46 | -10.65% | 21.24% |
| 8 | `tiny` | 1037.58 → 925.86 | -7.07% | 22.08% |
| 8 | `64KiB-32KiB` | 2990.66 → 3268.03 | -4.15% | 20.76% |
| 8 | `64KiB-1KiB` | 5133.39 → 5644.35 | +4.99% | 23.74% |
| 8 | `seek` | 1797.82 → 1783.67 | -2.74% | 15.66% |
| 8 | `range` | 2702.71 → 2694.57 | -0.73% | 24.15% |
| 8 | `get-tiny` | 1243.80 → 1203.97 | -2.25% | 25.45% |
| 8 | `get-64KiB` | 16129.71 → 16153.39 | -3.48% | 17.60% |
| 8 | `save-64KiB` | 52384.58 → 53391.10 | +4.30% | 10.49% |

These changes are smaller than the large same-code variation; this run cannot
establish latency parity. Cycle medians are not quotients of the displayed ns medians.

### Direct before/after binaries: every result retained

Both ordinary binaries contain identical final tests and benchmarks. ABBA blocks
have three repetitions, six samples per case/CPU/version. See [plan](raw/plan.json),
[runner](raw/measure.py.txt), [raw](raw/direct.log),
[all parsed samples/ranges/allocations](raw/direct-measurements.json) and
[parser](raw/analyze-direct.py.txt). `ExcessRead` changes from incorrect success to
bounded failure: its large reductions are **not equivalent-result speedups**.

| CPU | Benchmark (prefix `Benchmark` omitted) | Median ns before → after | Change |
| --- | --- | ---: | ---: |
| 1 | `ExcessRead/size=1048576/mode=get` | 589821.50 → 1406.50 | -99.76% |
| 1 | `ExcessRead/size=1048576/mode=guarded` | 1204436.50 → 1084.50 | -99.91% |
| 1 | `ExcessRead/size=1048576/mode=stream` | 2017068.00 → 1351.50 | -99.93% |
| 1 | `ExcessRead/size=3/mode=get` | 1812.50 → 2600.00 | +43.45% |
| 1 | `ExcessRead/size=3/mode=guarded` | 3471.00 → 1587.50 | -54.26% |
| 1 | `ExcessRead/size=3/mode=stream` | 1894.00 → 1947.50 | +2.82% |
| 1 | `OpenedObjectRead/get-64KiB` | 15639.00 → 26725.00 | +70.89% |
| 1 | `OpenedObjectRead/get-body-missing` | 2197.00 → 1376.50 | -37.35% |
| 1 | `OpenedObjectRead/get-tiny` | 1248.50 → 2648.50 | +112.13% |
| 1 | `OpenedObjectRead/save-64KiB` | 81226.00 → 69463.50 | -14.48% |
| 1 | `OpenedObjectRead/save-body-missing` | 1984.50 → 1438.00 | -27.54% |
| 1 | `StreamCompleteness/64KiB-1KiB` | 5069.50 → 6441.50 | +27.06% |
| 1 | `StreamCompleteness/64KiB-32KiB` | 3527.00 → 3544.50 | +0.50% |
| 1 | `StreamCompleteness/adapter` | 1377.00 → 1821.00 | +32.24% |
| 1 | `StreamCompleteness/range` | 2608.50 → 3632.00 | +39.24% |
| 1 | `StreamCompleteness/seek` | 2549.50 → 2320.00 | -9.00% |
| 1 | `StreamCompleteness/short` | 947.90 → 1605.00 | +69.32% |
| 1 | `StreamCompleteness/tiny` | 1221.50 → 1374.00 | +12.48% |
| 8 | `ExcessRead/size=1048576/mode=get` | 652014.00 → 1268.50 | -99.81% |
| 8 | `ExcessRead/size=1048576/mode=guarded` | 1421636.00 → 1146.50 | -99.92% |
| 8 | `ExcessRead/size=1048576/mode=stream` | 1755339.00 → 1588.00 | -99.91% |
| 8 | `ExcessRead/size=3/mode=get` | 2939.00 → 1042.00 | -64.55% |
| 8 | `ExcessRead/size=3/mode=guarded` | 4255.50 → 1110.00 | -73.92% |
| 8 | `ExcessRead/size=3/mode=stream` | 2013.00 → 1059.00 | -47.39% |
| 8 | `OpenedObjectRead/get-64KiB` | 26797.00 → 18620.50 | -30.51% |
| 8 | `OpenedObjectRead/get-body-missing` | 4338.00 → 1049.50 | -75.81% |
| 8 | `OpenedObjectRead/get-tiny` | 1471.00 → 1481.50 | +0.71% |
| 8 | `OpenedObjectRead/save-64KiB` | 90728.00 → 61482.50 | -32.23% |
| 8 | `OpenedObjectRead/save-body-missing` | 2105.00 → 1037.50 | -50.71% |
| 8 | `StreamCompleteness/64KiB-1KiB` | 5196.00 → 4233.50 | -18.52% |
| 8 | `StreamCompleteness/64KiB-32KiB` | 3813.00 → 2621.00 | -31.26% |
| 8 | `StreamCompleteness/adapter` | 1568.50 → 1265.50 | -19.32% |
| 8 | `StreamCompleteness/range` | 3015.50 → 2393.50 | -20.63% |
| 8 | `StreamCompleteness/seek` | 4133.50 → 1663.50 | -59.76% |
| 8 | `StreamCompleteness/short` | 942.10 → 906.65 | -3.76% |
| 8 | `StreamCompleteness/tiny` | 890.60 → 929.50 | +4.37% |

Adverse results include CPU 1 tiny GET **+112.13%** (1,248.5 → 2,648.5 ns),
64 KiB GET **+70.89%**, 1 KiB-chunk streams **+27.06%**, range reads **+39.24%**,
adapter reads **+32.24%**, prior short-EOF reads **+69.32%**, and small malformed
GET **+43.45%** (different result). Candidate outliers include 9,474 ns for small
malformed GET, 43,809 ns for CPU 1 large GET, 11,139 ns for 1 KiB-chunk streams,
114,899 ns for CPU 8 guarded large reads and 10,639 ns for CPU 8 32 KiB-chunk
streams. All favorable results and baseline outliers remain too. No general
speedup, zero-cost or latency-parity conclusion is drawn from these wall timings.

### Focused binary A/A and process CPU follow-up

The +112% result justified a predeclared follow-up: the same original before/after
binaries run ten alternating ABBA/BAAB cycles per selected case/CPU, 40 fresh
processes per row. Duplicate labels execute the identical binary. Each process
records benchmark time and RUSAGE_CHILDREN user+system CPU deltas. The latter
includes startup and untimed fixture setup, so it is diagnostic process CPU per
benchmark iteration, not an isolated operation profile. No other agent test/build
ran concurrently. [Plan](raw/focused-plan.json), [runner](raw/focused-costs.py.txt),
[raw](raw/focused-costs.log), [all 240 samples](raw/focused-samples.json),
[summary](raw/focused-summary.json) and [parser](raw/analyze-focused.py.txt).

| CPU | Case | Wall benchmark cycle change | Wall same-code variation | Process CPU cycle change | CPU same-code variation |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | `get-tiny` | -11.63% | 23.74% | +0.08% | 4.10% |
| 1 | `get-64KiB` | +22.85% | 30.15% | -0.24% | 8.66% |
| 1 | `stream-64KiB-1KiB` | -19.89% | 29.90% | -1.92% | 2.26% |
| 8 | `get-tiny` | -18.72% | 22.70% | -0.18% | 4.94% |
| 8 | `get-64KiB` | -5.04% | 25.74% | +2.11% | 2.74% |
| 8 | `stream-64KiB-1KiB` | -27.28% | 35.86% | -1.06% | 4.87% |

Process CPU changes range −1.92% to +2.11%, within same-code median variation
2.26–8.66%. They do not reproduce a twofold processing-cost increase. Wall latency
is still inconclusive: CPU 1 large GET is **+22.85%** in this follow-up, with a
168,487 ns candidate outlier; CPU 8 chunked streams have a 115,024 ns candidate
outlier and 73,180 ns baseline outlier. This calibrates uncertainty without
proving every adverse result is host noise. A quiet host is needed for a precise
latency-cost estimate. All results remain reviewable; no optimization was added
in response to timing variation.

### Allocation and consumption results

Healthy allocation counts are unchanged in every direct sample: streams ten,
range eleven, adapter thirteen, buffered GET/body errors fourteen, guarded large
reads nineteen. At CPU 1 B/op is identical: stream 1,072, range 1,104, adapter
1,568, tiny GET 1,155, large GET 66,691, guarded large 67,174–67,175 and prior
body errors 1,163. CPU 8 pooled/runtime bytes vary; all values are retained.
The reader gains arithmetic, bounds/error checks and a slice adjustment, with no
new per-read/per-object allocation. Each stream Read still invokes one provider
Read; error construction is shared.

For a 1 MiB body falsely advertised as three bytes, regression byte counters
change **1,048,576 → 4 consumed**, returning at most the three-byte prefix and
an error. CPU 1 cumulative allocation changes: stream **5,242,160 → 1,584 B/op**,
buffered GET about **1,968,606 → 1,156–1,157**, guarded read about
**1,969,158 → 1,156–1,157**. Counts drop 37 → 11, 18–20 → 14 and 24–27 → 14.
These quantify corrected failure, not successful-workload performance. At CPU 8
cold pooled buffers can raise small-N failure totals to about 2,478 B/op;
no exact-byte parity or peak-memory guarantee is claimed.

## Next highest-priority finding in the inspected read paths

When a provider returns a live reader alongside an opening error, the runtime
discards access to it without closing it. Six file-backed injected paths reproduce:
raw stream, adapter, buffered GET, guarded read, fallback range and native range.
Each [baseline normal](raw/next-open-before.log), [baseline race](raw/next-open-before-race.log),
[candidate normal](raw/next-open-after.log) and [candidate race](raw/next-open-after-race.log)
run contains twelve expected ownership failures (six cases twice): Close count
zero and the descriptor still usable by Stat. Original error and accounting are
preserved, and a healthy retry works. Fixtures manually close every file afterward.

[Probe source](raw/next-open-error-probe.go.txt), [runner](raw/next-probe.py.txt) and
[frozen baseline](raw/baseline-streams.go.txt) are retained. Baseline replay uses
a Go source overlay; production files were never replaced. To reproduce, copy the
probe into `internal/runtime/opening_error_review_probe_test.go`, run
`go test ./internal/runtime -run '^TestReviewOpeningErrorDropsReader$' -count=2 -v`
(optionally with `-race`), then remove it. It uses this change's observation helper
and deliberately fails. This shows an injected combined-result cleanup gap, not
an observed native filesystem failure. Ownership for reader-plus-error is not
explicitly documented in Store; establish that contract in the separate correction.
**No next implementation is included.**
