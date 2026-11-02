# Reject premature EOF in buffered reads

Reviewed and corrected on 2026-10-16, based on `32168f8` on
`codex/core-copy-quality`. Freshly fetched `origin/main` remains
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor of this review branch.
[Source and executable identities](raw/environment.json) pin the measured change.
The preceding [pointer refinement](../2026-10-16-read-result-pointer/README.md)
and [body-error correction](../2026-10-15-save-body-failure/README.md) are retained.

## Demonstrated defect and correction

After successful open with metadata advertising three bytes, an injected reader
ending at zero, one or two bytes caused `GetObject` and `ReadForSave` to succeed
with incomplete data. `ReadForSave` also issued a fingerprint of those incomplete
bytes. The backend refused that fingerprint: no guard bypass or overwritten bytes
was demonstrated. This is reader fault injection against real native memory
storage, not evidence that MemoryStore spontaneously truncates.

The retained [original repro before](raw/original-before.log) and
[race repro before](raw/original-before-race.log) each contain eight semantic
failures (four cases repeated twice). Its source remains in the preceding
[report](../2026-10-15-save-body-failure/raw/next-truncated-read-probe.go.txt).
The final regression tests were added and run before changing production:
[32 failing leaves](raw/before-tests.log) are the 16 defect cases repeated twice;
all 11 control cases already passed each repetition.

The private `readOpenedObject` helper now retains `io.Copy`'s byte count and,
only after a nil copy error, returns `io.ErrUnexpectedEOF` when fewer bytes were
read than the original metadata size. Both buffered APIs already use this helper.
The caller receives a zero object and no save condition, and closes the reader
once. Actual reader errors, including wrapped EOF, retain their original identity.
Empty bodies, complete EOF with bytes, negative size hints, legitimate opening
absence, authorization, quota and stale-save behavior retain their contracts.
The initial 32 MiB buffer-capacity clamp is unchanged; it is not a read limit.
Extra bytes and streaming/range readers are outside this correction.

There is no new storage interface, public API, dependency or format. Successful
reads add one integer comparison, with no extra open, HEAD, read pass or payload
allocation. Callers relying on a successful short body now receive an error;
providers with inaccurate positive sizes may therefore surface failures. Negative
sizes keep their existing unknown-size behavior. Close errors remain handled as
before. This change does not impose a total buffering bound.

## Regression and contract validation

The new runtime and public-wrapper tests contain 27 cases: 16 short-read defect
cases and 11 complete/empty/unknown-size/error-identity controls. They cover EOF
returned with bytes and on the following read. Failure checks include zero results,
invalid save-condition refusal, reader closure, unchanged accounting and healthy
full-capacity retry. Existing fixtures and recovery checks are reused.

- [New and prior focused tests](raw/after-tests.log): 83 leaves repeated twice pass.
- [Focused race tests](raw/focused-race.log): 83 leaves repeated five times,
  **415 passes**, no skips or race reports.
- Original repro now passes [normally](raw/original-after.log) and
  [under race](raw/original-after-race.log), twice each.
- [Full Go suite](raw/full-go.log), [broad storage/runtime/S3 race suite](raw/broad-race.log),
  and [vet](raw/vet.log) pass.
- [Four conformance profiles](raw/conformance-summary.log) pass: memory,
  filesystem, runtime-memory and runtime-filesystem. Each retains the two existing
  skips for opt-in live upstream and mirror bucket creation awaiting an authority decision.
- [Go quality/file-size gates](raw/quality.log), [generated-output comparison](raw/generated.log),
  [WASM smoke](raw/wasm-smoke.log) and [seven package integration tests](raw/package-tests.log) pass.
- [Final format, documentation, provenance and cleanup checks](raw/final-checks.log).

The packaged WASM was rebuilt with the pinned toolchain and reproducible flags:
4,618,016 to 4,618,255 bytes, **+239 bytes**.
[Build output](raw/build-wasm.log) and [artifact hashes](raw/wasm.json) are retained.

## Comparative measurements

Go 1.25.6, macOS 14.5 ARM64, Apple M1 Pro, native memory backend; 3-byte and
64 KiB objects. These are runtime microbenchmarks, not disk, HTTP, throughput or
peak-memory guarantees. Timing phases ran serially without concurrent agent
builds/tests; unrelated host activity was uncontrolled. All **1,688 observations**
are retained, including adverse results and outliers. Allocation figures are
cumulative per operation, not RSS or peak memory.

### Successful reads with same-code controls

One ordinary test binary includes frozen baseline methods (private names changed)
and the actual candidate methods. [Reference verification](raw/reference-verification.json),
[probe source](raw/matched-probe.go.txt), [plan](raw/matched-plan.json),
[raw 1,280 blocks](raw/matched.log), [summary](raw/matched-summary.json),
[runner](raw/measure.py.txt) and [parser](raw/analyze-matched.py.txt) are retained.
There are 20 balanced ABBA/BAAB cycles per case, repeated twice at CPU 1 and 8;
two labels execute identical baseline or candidate code to calibrate A/A variation.
Tiny blocks use 50,000 operations; 64 KiB blocks use 1,000.
Runtime allocation, GC CPU, pause and cycle counters are captured outside timing.
These process-wide deltas include boundary/runtime work, not per-call profiling.

| CPU | Read | Median ns before → after | Median paired-cycle change | Same-code absolute median change |
| --- | --- | ---: | ---: | ---: |
| 1 | get-3 | 910.23 → 904.22 | -0.25% | 3.46% |
| 1 | get-64KiB | 10029.67 → 10200.12 | +0.76% | 8.30% |
| 1 | save-3 | 2265.42 → 2264.10 | +0.86% | 3.91% |
| 1 | save-64KiB | 40270.17 → 40079.33 | -0.53% | 1.94% |
| 8 | get-3 | 806.25 → 814.51 | +0.27% | 2.73% |
| 8 | get-64KiB | 9601.25 → 9424.38 | -0.45% | 9.22% |
| 8 | save-3 | 2068.38 → 2068.16 | -0.16% | 1.70% |
| 8 | save-64KiB | 40041.06 → 40244.31 | +0.31% | 2.59% |

The change column is the median of cycle ratios, not the ratio of the two ns
medians. Changes here are smaller than same-code median variation. GC CPU and
elapsed-time correlations reach 0.996; that is diagnostic context, not causal
proof or a promise of parity. No general speedup is claimed.

### Direct before/after benchmarks

Ordinary before/after binaries contain the identical final benchmark/test sources.
Each case/CPU/version has six samples in ABBA blocks; fixed iteration counts are
in the [plan](raw/direct-plan.json). [All 408 samples](raw/direct.log),
[parsed samples/ranges/allocations](raw/direct-measurements.json) and
[parser](raw/analyze-direct.py.txt) retain the original eleven cases and six new
EOF cases. `after=0/1` below changes from incorrect success to a correct error;
its lower guarded-read cost is **not an equivalent-result speedup**.

| CPU | Benchmark (prefix `Benchmark` omitted) | Median ns before → after | Change |
| --- | --- | ---: | ---: |
| 1 | `BufferedEOFRead/guarded=false/after=0` | 814.75 → 822.80 | +0.99% |
| 1 | `BufferedEOFRead/guarded=false/after=1` | 839.40 → 813.75 | -3.06% |
| 1 | `BufferedEOFRead/guarded=false/after=3` | 827.40 → 848.15 | +2.51% |
| 1 | `BufferedEOFRead/guarded=true/after=0` | 2161.50 → 895.20 | -58.58% |
| 1 | `BufferedEOFRead/guarded=true/after=1` | 2222.00 → 888.50 | -60.01% |
| 1 | `BufferedEOFRead/guarded=true/after=3` | 2183.00 → 2190.00 | +0.32% |
| 1 | `OpenedObjectRead/get-64KiB` | 8562.50 → 9095.00 | +6.22% |
| 1 | `OpenedObjectRead/get-body-missing` | 840.85 → 828.10 | -1.52% |
| 1 | `OpenedObjectRead/get-tiny` | 879.90 → 858.35 | -2.45% |
| 1 | `OpenedObjectRead/save-64KiB` | 40427.00 → 40407.50 | -0.05% |
| 1 | `OpenedObjectRead/save-body-missing` | 909.35 → 898.35 | -1.21% |
| 1 | `ReadForSaveObservation/marker-first` | 136.90 → 141.15 | +3.10% |
| 1 | `ReadForSaveObservation/missing` | 200.25 → 202.80 | +1.27% |
| 1 | `ReadForSaveObservation/missing-first` | 139.10 → 139.30 | +0.14% |
| 1 | `ReadForSaveObservation/present` | 2236.00 → 2192.00 | -1.97% |
| 1 | `ReadForSaveObservation/unknown` | 124.40 → 123.00 | -1.13% |
| 1 | `ReadForSaveObservation/wrapped-missing` | 137.80 → 138.90 | +0.80% |
| 8 | `BufferedEOFRead/guarded=false/after=0` | 804.15 → 883.35 | +9.85% |
| 8 | `BufferedEOFRead/guarded=false/after=1` | 765.30 → 816.65 | +6.71% |
| 8 | `BufferedEOFRead/guarded=false/after=3` | 782.10 → 781.20 | -0.12% |
| 8 | `BufferedEOFRead/guarded=true/after=0` | 2036.50 → 842.85 | -58.61% |
| 8 | `BufferedEOFRead/guarded=true/after=1` | 2015.00 → 851.55 | -57.74% |
| 8 | `BufferedEOFRead/guarded=true/after=3` | 2010.00 → 2022.00 | +0.60% |
| 8 | `OpenedObjectRead/get-64KiB` | 10697.50 → 10582.00 | -1.08% |
| 8 | `OpenedObjectRead/get-body-missing` | 784.75 → 768.55 | -2.06% |
| 8 | `OpenedObjectRead/get-tiny` | 838.30 → 751.40 | -10.37% |
| 8 | `OpenedObjectRead/save-64KiB` | 42259.00 → 41774.00 | -1.15% |
| 8 | `OpenedObjectRead/save-body-missing` | 843.85 → 908.65 | +7.68% |
| 8 | `ReadForSaveObservation/marker-first` | 151.80 → 138.75 | -8.60% |
| 8 | `ReadForSaveObservation/missing` | 190.65 → 189.60 | -0.55% |
| 8 | `ReadForSaveObservation/missing-first` | 152.90 → 143.45 | -6.18% |
| 8 | `ReadForSaveObservation/present` | 2049.50 → 1985.50 | -3.12% |
| 8 | `ReadForSaveObservation/unknown` | 133.15 → 123.35 | -7.36% |
| 8 | `ReadForSaveObservation/wrapped-missing` | 141.65 → 137.40 | -3.00% |

Adverse readings remain explicit: CPU 1 successful 64 KiB GET rises **6.22%**
(+532.5 ns), while CPU 8 short-EOF GET rises **9.85%** (+79.2 ns at zero bytes)
and **6.71%** at one byte. CPU 8 existing guarded body-error reads rise **7.68%**.
Candidate outliers include 5,877 ns for zero-byte EOF GET, 1,258 ns for one-byte
EOF GET, 1,298 ns for complete EOF GET, 1,546 ns for guarded body-error,
14,223 ns for 64 KiB GET and 47,111 ns for guarded 64 KiB reads. Favorable baseline
outliers are also retained (e.g. 4,701 ns for complete guarded EOF at CPU 1).
The matched controls help interpret variation; they do not prove every adverse
direct reading is noise or clear all performance concerns.

Allocation counts stay unchanged for existing cases and complete EOF: buffered
GET/body failures 14, successful guarded reads 19, opening absence one, prepared
opening observations zero. Short guarded EOF drops 19 to 14 by returning before
fingerprinting incomplete data; short GET remains 14. At CPU 1, tiny GET uses
1,139 B/op, 64 KiB GET 66,675 B/op, guarded tiny 1,619 B/op and guarded 64 KiB
67,158–67,159 B/op. EOF fixtures add their wrapper: complete GET 1,147 B/op,
complete guarded 1,627 B/op, short guarded 1,627 → 1,147 B/op. CPU 8 byte totals
vary with runtime overhead: guarded 64 KiB before 69,834–70,182 and after
69,843–70,348 B/op. All per-sample values are retained; exact parity is not claimed.

## Next separate correctness finding: raw streaming EOF

`OpenObject` and `StoreAdapter.GetObject` still return a raw reader that can end
at zero, one or two bytes while metadata advertises three; `io.ReadAll` returns
nil error. Six injected cases reproduce twice normally and twice under race,
on both the baseline and candidate. Every log has twelve semantic failures and
no race report. Readers close once, accounting remains unchanged, healthy retry
returns the original version, and a full-capacity save succeeds.

[Probe source](raw/next-stream-probe.go.txt),
[baseline normal](raw/next-stream-before.log), [baseline race](raw/next-stream-before-race.log),
[candidate normal](raw/next-stream-after.log), [candidate race](raw/next-stream-after-race.log),
and [provenance](raw/next-stream-provenance.json) are retained. Baseline replay used
a Go source overlay with the [frozen baseline helper](raw/baseline-objects.go.txt);
production files were never replaced. Stream and adapter sources are unchanged.
To reproduce, copy the probe into `internal/runtime/stream_read_review_probe_test.go`
and run `go test ./internal/runtime -run '^TestReviewStreamingReadCompleteness$'
-count=2 -v`, optionally with `-race`, then remove it. It deliberately fails.

This establishes the Go raw-stream behavior, not S3 HTTP client acceptance:
transport Content-Length checks may detect truncation. Range-reader behavior has
not been qualified. Streaming error propagation, closure and partial-consumption
contracts need separate treatment. **That finding is not implemented here.**
