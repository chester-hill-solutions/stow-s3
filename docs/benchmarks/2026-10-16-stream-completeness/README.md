# Reject premature EOF in runtime streams

Corrected on 2026-10-16 on `codex/core-copy-quality`, based on `336ea8d`.
Freshly fetched `origin/main` remains `219d1dcfff9807b04660a40313b32127e5be2933`,
an ancestor of this review branch. The preceding
[buffered-read correction](../2026-10-16-read-completeness/README.md) is retained.
[Measured source/executable identities](raw/environment.json) and
[final source hashes](raw/final-source-hashes.json) pin this change.

## Evidence before changing production

The retained six-case repro opens a real native memory object whose metadata says
three bytes, then injects a reader ending at zero, one or two. Both `OpenObject`
and `StoreAdapter.GetObject` return that incomplete body with nil `io.ReadAll`
error. This is provider-reader fault injection, not spontaneous MemoryStore damage.
The original [normal](raw/original-before.log) and [race](raw/original-before-race.log)
runs each contain twelve semantic failures (six cases, twice); no data race occurs.
The source is retained in the preceding
[report](../2026-10-16-read-completeness/raw/next-stream-probe.go.txt).

The initial 57 permanent cases were run before production edits:
[44 failing leaves](raw/before-tests.log) are 22 distinct defect cases repeated
twice; all 35 controls pass. These include direct/adapter streams, the public
wrapper, seekable truncated bodies and both native/fallback range paths.
The later explicit `io.Copy` coverage adds twelve cases: eight defects and four
controls. A [baseline source-overlay replay](raw/copy-before-tests.log) reproduces
all eight twice, leaving production files untouched. Overall: **69 new cases,
30 defect cases and 39 controls**. No guard bypass or persisted overwrite was
observed; failure/retry checks preserve the original object and quota accounting.

## Small correction and preserved contracts

The existing `objectStream` stores expected size and current position. `Read`
adds the returned byte count and translates only exact `io.EOF` before expected
end into `io.ErrUnexpectedEOF`, returning any bytes from that read alongside the
error. Full objects use captured metadata size; ranges use requested length.
Successful seeks update position from the provider's returned offset. This keeps
suffix reads, rewind, current/end-relative seeking and seeking beyond EOF valid.
The same per-stream mutex already protects reads, seeks and close.

Original non-EOF reader errors, including wrapped EOF and committed/missing
errors, remain unchanged. Complete/empty EOF, negative unknown-size hints,
zero-length reads and explicit close after partial consumption retain their
behavior. A terminal read closes the provider once; subsequent reads retain
`io.ErrClosedPipe`. Policy, cancellation, runtime lifecycle and captured-version
checks remain in the existing read path. The existing `WriteTo` path calls `Read`
and propagates the correction, including through `StoreAdapter`.

No added wrapper, lock, provider call, prefetch, payload buffer, dependency,
public API or storage format. Two int64 fields add 16 bytes to each allocated
stream in these native measurements, with unchanged allocation counts. Each read
adds position accounting and an EOF/length check; successful seeks assign the
new position. Providers declaring an inaccurate positive size can now surface an
error instead of successful truncation. This is EOF validation, not an upper
read bound: excess bodies and total whole-object buffering are unchanged.
Close-error handling and range-opening errors remain as before.

## Verification

- [Initial new cases and prior buffered/guarded regressions](raw/after-tests.log):
  140 leaves repeated twice, all pass.
- [Explicit WriterTo cases](raw/copy-after-tests.log): twelve leaves twice, all pass.
- [Focused race matrix](raw/focused-race.log): 145 leaves repeated five times,
  **725 passes**. [WriterTo race cases](raw/copy-after-race.log): another **60 passes**.
  Combined **785 focused race case runs**, no skips or races.
- Original repro passes twice [normally](raw/original-after.log) and
  [under race](raw/original-after-race.log).
- [Full Go suite](raw/full-go.log), [broad runtime/storage/S3 race suite](raw/broad-race.log)
  and [vet](raw/vet.log) pass. These ran before the final test-only WriterTo
  addition, which was separately checked normally and under race. Production
  source did not change afterward.
- [Four conformance profiles](raw/conformance-summary.log) pass: memory,
  filesystem, runtime-memory and runtime-filesystem. Each retains the two existing
  skips for opt-in live upstream and mirror bucket creation awaiting an authority decision.
- [Quality and file-size gates](raw/quality.log), [generated comparison](raw/generated.log),
  [WASM smoke](raw/wasm-smoke.log), [seven package integration tests](raw/package-tests.log)
  and [final checks](raw/final-checks.log) pass.

WASM was rebuilt with pinned Go 1.25.6 and reproducible flags:
4,618,255 → 4,618,495 bytes, **+240 bytes**.
[Build](raw/build-wasm.log), [package build](raw/package-build.log) and
[artifact hashes](raw/wasm.json) are retained. The added regression coverage belongs
at this existing stream seam; no new storage abstraction is required.

## Comparative costs

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64, native memory storage. No concurrent
agent build/test ran during timing; unrelated host activity was uncontrolled.
These are runtime microbenchmarks, not HTTP/disk/throughput or peak-memory claims.
**1,808 applicable observations** are retained: 288 direct, 240 initial manual-Read
label samples and 1,280 corrected balanced blocks. An additional 1,280-block
incomplete-reference run is retained separately and excluded from comparison.
Allocation measurements are cumulative bytes per operation, not RSS or peak heap.

### Balanced same-binary comparison with GC counters

The primary probe includes exact frozen baseline stream methods **and its existing
WriterTo method**, plus unchanged GET/ReadForSave methods with private renaming.
It uses the original shared copy-buffer pool and checks reader method names/count
and WriterTo support before timing. See [source](raw/paired-probe.go.txt),
[reference verification](raw/paired-reference-verification.json), [plan](raw/paired-plan.json),
[all blocks](raw/paired.log), [summary](raw/paired-summary.json) and
[parser](raw/analyze-paired.py.txt). Ten alternating ABBA/BAAB cycles per case run
twice at CPU 1 and 8; duplicate labels execute identical code as A/A controls.
Streaming large blocks use 10,000 operations, tiny blocks 50,000, buffered 64 KiB
GET 5,000 and guarded 64 KiB reads 1,000. Allocation and process-wide GC CPU,
cycle and pause counters are sampled outside timing; they include boundary/runtime
work and are diagnostic context rather than per-call profiling.

| CPU | Case | Median ns before → after | Median paired-cycle change | Same-code absolute median change |
| --- | --- | ---: | ---: | ---: |
| 1 | `tiny` | 818.24 → 847.03 | +2.62% | 6.87% |
| 1 | `64KiB-32KiB` | 2141.91 → 2168.16 | +5.36% | 8.83% |
| 1 | `64KiB-1KiB` | 3784.07 → 3881.65 | +4.45% | 7.59% |
| 1 | `seek` | 1487.04 → 1554.86 | +1.56% | 9.36% |
| 1 | `range` | 2119.59 → 2136.39 | -0.19% | 4.67% |
| 1 | `get-tiny` | 973.89 → 1026.03 | +3.94% | 11.20% |
| 1 | `get-64KiB` | 11329.16 → 11026.46 | -1.61% | 11.54% |
| 1 | `save-64KiB` | 47229.94 → 46578.46 | -0.20% | 9.56% |
| 8 | `tiny` | 796.44 → 772.29 | -0.98% | 10.86% |
| 8 | `64KiB-32KiB` | 2058.78 → 2068.85 | -5.15% | 8.96% |
| 8 | `64KiB-1KiB` | 3760.26 → 3705.31 | +0.21% | 7.77% |
| 8 | `seek` | 1416.99 → 1364.40 | +0.35% | 7.00% |
| 8 | `range` | 2079.50 → 2094.95 | +1.42% | 8.74% |
| 8 | `get-tiny` | 876.03 → 896.61 | +4.67% | 7.93% |
| 8 | `get-64KiB` | 15677.44 → 13510.23 | -1.81% | 19.27% |
| 8 | `save-64KiB` | 48788.94 → 50372.12 | -0.29% | 13.99% |

Cycle-change medians are not quotients of the two ns medians. Median changes range
from −5.15% to +5.36%, within each case's same-code absolute median variation.
GC CPU/elapsed correlations reach 0.980; that does not establish causality or
clear every direct adverse reading. No general speedup or parity is claimed.

### Direct before/after binaries

Both ordinary binaries contain identical tests and final benchmark sources at
measurement time. The final WriterTo regression file was added later; measured
and final hashes are retained separately. ABBA blocks contain three samples each,
six samples per case/CPU/version: [plan](raw/plan.json), [raw](raw/direct.log),
[parsed samples/ranges/allocations](raw/direct-measurements.json),
[runner](raw/measure.py.txt) and [parser](raw/analyze-direct.py.txt).
`short` changes from EOF accepted as success to `io.ErrUnexpectedEOF`; its timing
compares different results and is not a like-for-like successful-read cost.

| CPU | Benchmark (prefix `Benchmark` omitted) | Median ns before → after | Change |
| --- | --- | ---: | ---: |
| 1 | `OpenedObjectRead/get-64KiB` | 10853.50 → 9928.00 | -8.53% |
| 1 | `OpenedObjectRead/get-body-missing` | 923.40 → 892.05 | -3.40% |
| 1 | `OpenedObjectRead/get-tiny` | 901.25 → 948.70 | +5.26% |
| 1 | `OpenedObjectRead/save-64KiB` | 45992.50 → 44312.00 | -3.65% |
| 1 | `OpenedObjectRead/save-body-missing` | 1017.45 → 951.00 | -6.53% |
| 1 | `StreamCompleteness/64KiB-1KiB` | 3741.50 → 3756.00 | +0.39% |
| 1 | `StreamCompleteness/64KiB-32KiB` | 1955.50 → 2344.00 | +19.87% |
| 1 | `StreamCompleteness/adapter` | 992.30 → 981.65 | -1.07% |
| 1 | `StreamCompleteness/range` | 2029.50 → 2185.00 | +7.66% |
| 1 | `StreamCompleteness/seek` | 1525.00 → 1507.00 | -1.18% |
| 1 | `StreamCompleteness/short` | 708.60 → 879.05 | +24.05% |
| 1 | `StreamCompleteness/tiny` | 749.25 → 806.10 | +7.59% |
| 8 | `OpenedObjectRead/get-64KiB` | 12026.00 → 13467.00 | +11.98% |
| 8 | `OpenedObjectRead/get-body-missing` | 861.95 → 964.70 | +11.92% |
| 8 | `OpenedObjectRead/get-tiny` | 925.70 → 893.75 | -3.45% |
| 8 | `OpenedObjectRead/save-64KiB` | 49512.00 → 51692.50 | +4.40% |
| 8 | `OpenedObjectRead/save-body-missing` | 1014.95 → 979.55 | -3.49% |
| 8 | `StreamCompleteness/64KiB-1KiB` | 3660.00 → 3770.00 | +3.01% |
| 8 | `StreamCompleteness/64KiB-32KiB` | 2118.50 → 2167.50 | +2.31% |
| 8 | `StreamCompleteness/adapter` | 990.80 → 978.90 | -1.20% |
| 8 | `StreamCompleteness/range` | 2219.50 → 2282.50 | +2.84% |
| 8 | `StreamCompleteness/seek` | 1380.00 → 1434.00 | +3.91% |
| 8 | `StreamCompleteness/short` | 679.15 → 741.35 | +9.16% |
| 8 | `StreamCompleteness/tiny` | 721.95 → 801.65 | +11.04% |

Adverse direct readings remain explicit: successful 64 KiB/32 KiB-chunk streams
at CPU 1 rise **19.87%** (+388.5 ns), CPU 8 buffered 64 KiB GET **11.98%**
(+1,441 ns), CPU 8 GET body errors **11.92%**, tiny streams **7.59% / 11.04%**,
and CPU 1 range reads **7.66%**. Short-error results rise **24.05% / 9.16%**.
The CPU 1 large-stream candidate block has 5,255, 2,574 and 3,786 ns samples,
followed by 1,930, 2,027 and 2,114 ns; baseline ranges 1,869–2,022 ns.
Other candidate outliers include 5,330 ns for 1 KiB-chunk streams, 17,046 ns for
CPU 8 buffered 64 KiB GET and 54,322 ns for guarded 64 KiB reads. Favorable
outliers and every baseline sample are also retained. The balanced comparison
narrows uncertainty but does not justify discarding these direct results.

### Allocation and secondary timing evidence

At CPU 1, direct B/op changes exactly +16: ordinary streams 1,056 → 1,072,
range 1,088 → 1,104, adapter 1,552 → 1,568, short 1,064 → 1,080,
buffered tiny GET 1,139 → 1,155, buffered 64 KiB GET 66,675 → 66,691,
guarded 64 KiB about 67,159 → 67,175. Allocation counts remain respectively
10, 11, 13, 10, 14, 14 and 19. Existing body errors remain 14 allocations,
1,147 → 1,163 B/op at CPU 1. CPU 8 streaming totals also show +16, while
buffered totals vary with runtime overhead; all sample values remain in the JSON.
The corrected balanced run confirms the CPU 1 +16-byte result with equivalent
allocation counts. This correction adds a small fixed allocation cost; it is not
a buffering optimization or a claim of zero overhead.

The initial same-binary label experiment used manual Read loops (no WriterTo
invocation): [source](raw/matched-probe.go.txt), [raw](raw/matched.log),
[summary](raw/matched-measurements.json), [parser](raw/analyze-labels.py.txt).
Its frozen reader omitted WriterTo, so this cohort applies only to those explicit
Read loops. CPU 8 seek is +2.89%; all other case medians decrease, with large
same-code label variation (up to −16.67%). The balanced complete-method-set run
above is the primary comparison.

A subsequent probe incorrectly reused that incomplete frozen type for buffered
GET/ReadForSave. io.Copy therefore took a different path: e.g. baseline 64 KiB
GET allocated about 197,712 B/op versus the actual baseline's about 66,675.
Its apparent large improvements are invalid. That **entire 1,280-block run** is
excluded from conclusions and retained as [raw](raw/paired-incomplete-reference.log),
[source](raw/paired-incomplete-reference-probe.go.txt),
[plan](raw/paired-incomplete-reference-plan.json) and
[summary](raw/paired-incomplete-reference-summary.json). The corrected run restores
WriterTo, uses the original pool and checks method sets. No production change
was made in response to the invalid timing result.

## Next separate read-integrity finding: excess provider bytes

A captured provider reader can return four bytes while advertising three;
`OpenObject`, `GetObject` and `ReadForSave` still accept those bytes without error.
The guarded path fingerprints the excess body, but the backend refuses that
condition. The three fault-injection cases reproduce twice normally and twice
under race on both baseline and candidate. Each log contains six semantic failures;
no data race, persisted overwrite or accounting mutation is demonstrated.
Healthy retry and full-capacity save still succeed.

[Probe source](raw/next-excess-probe.go.txt), [runner](raw/next-probe.py.txt),
[baseline normal](raw/next-excess-before.log), [baseline race](raw/next-excess-before-race.log),
[candidate normal](raw/next-excess-after.log), [candidate race](raw/next-excess-after-race.log)
and [frozen baseline source](raw/baseline-streams.go.txt) are retained. The baseline
uses a Go source overlay; production files were never replaced. To reproduce,
copy the probe into `internal/runtime/excess_body_review_probe_test.go`, run
`go test ./internal/runtime -run '^TestReviewExcessObjectBody$' -count=2 -v`
(optionally with `-race`), then remove it. It uses this change's test fixtures
and deliberately fails. HTTP client behavior and natural native-store corruption
are not established by this injection. Excess-byte handling and total buffering
bounds require a separate review. **No next implementation is included.**
