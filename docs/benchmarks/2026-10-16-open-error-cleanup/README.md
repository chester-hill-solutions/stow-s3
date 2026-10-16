# Close provider readers when runtime opening fails

Corrected on 2026-10-16 from `c0e43efbff9752ea7ae43915b64dde89ba416528`,
on `codex/core-copy-quality`. Freshly fetched `origin/main` remains
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor of this branch.
The prior [excess-body correction](../2026-10-16-excess-body/README.md) is preserved.
Only failed-open reader ownership changes; earlier COPY, publication, accounting
and read-completeness corrections are not repeated.
[Source/toolchain/binary identities](raw/environment.json) pin the comparison.

## Evidence before implementation edits

The saved file-backed probe was replayed on the unchanged baseline. All six routes
leave an actual file open after returning the provider's original opening error:
stream, buffered GET, guarded observation, storage adapter, fallback range and
native range. Each was run twice [normally](raw/original-before.log) and twice
[under race](raw/original-before-race.log): **12 expected failures per log**.
The original source is retained in the preceding
[report](../2026-10-16-excess-body/raw/next-open-error-probe.go.txt).

The new regression matrix also ran before implementation edits:
[normal](raw/before-tests.log), [race](raw/before-race.log).
There are **24 leaf cases**: 18 fail and six controls pass per repetition.
Final regression sources were replayed twice against the exact baseline production
files using an overlay: [normal](raw/final-before.log), [race](raw/final-before-race.log).
These reproduce the same 36 failed leaves and 12 passing controls per log.
No compilation error is counted as a repro.

The core tests retain an os.File and check its descriptor state, Close count,
zero payload reads, original error, unchanged accounting, healthy retry and
full-capacity guarded-save recovery. Public custom-store tests cover direct,
buffered, native-range and fallback-range paths, including Close returning a
committed/missing error. Controls preserve nil-reader errors, genuine opening
absence, marked missing errors, missing buckets, invalid ranges and failed skips.

The reproduction injects a provider returning a nonnil reader alongside an
error. It demonstrates what the runtime does with that combination, not that the
native filesystem backend naturally produces it. Retaining the file makes its
unclosed state deterministic; without retention, eventual finalization may close
an os.File, but it does not provide bounded cleanup for repeated failed requests.
Other reader implementations need not have finalizers at all.

## Small correction and compatibility

The runtime's existing failed-open branch closes a nonnil provider reader before
returning the original error and empty result. The public store's range fallback
now propagates the reader and metadata with its error so that this same owner can
close it. Previously that adapter discarded the reader before the runtime saw it.
The production diff is **three added lines and one changed return**.

Existing range-validation and range-skip failures already close and return a nil
reader; the runtime therefore does not close them twice. Successful streams remain
lazy and retain their existing reader type, Seek and WriterTo behavior. No extra
HEAD, read, wrapper, buffer, lock, dependency, public method or format is added.

Cleanup errors are deliberately ignored when an opening error already exists.
Joining or substituting a Close error could change missing-object classification
or introduce a false committed marker. Tests preserve the original error and the
existing ReadForSave absence/invalid-condition contracts. Custom-store guarded
saves remain unsupported where they were unsupported before.

A provider handing back a nonnil reader transfers cleanup responsibility to the
runtime even on failure. Providers that have already released it should return
nil. Failed opens now incur that reader's Close cost; a blocking provider Close
can delay the failed request. This does not introduce a timeout or close resources
a provider never returns. Cleanup failures are not retried or newly surfaced.
Healthy opens execute no added allocation or new cleanup operation.

## Validation

- [Final new regressions](raw/after-tests.log): **24 leaves twice = 48 passes**.
- [Focused race suite](raw/focused-race.log): **222 leaves five times = 1,110
  passes**, including prior read completeness, policy, version and stream tests.
- [Full Go suite](raw/full-go.log), [broad runtime/storage/S3/run-through/public
  race suite](raw/broad-race.log) and [vet](raw/vet.log) pass.
- [Four conformance profiles](raw/conformance-summary.log) pass: memory,
  filesystem, runtime-memory and runtime-filesystem. Each retains the existing
  opt-in live upstream and mirror bucket-creation skips.
- [Go quality/file-size gates](raw/quality.log), [generated comparison](raw/generated.log),
  [WASM smoke](raw/wasm-smoke.log), [seven package integration tests](raw/package-tests.log)
  and [final measured-source checks](raw/final-source-check.log) pass.
- [Documentation checks](raw/docs.log) pass. Validation commands are retained in
  [the runner](raw/validate.py.txt) and [final checks](raw/final-checks.py.txt).

Rebuilt shipped WASM: **4,619,001 → 4,619,197 bytes (+196)**.
[Hashes](raw/wasm.json), [build](raw/build-wasm.log) and
[package build](raw/package-build.log) are retained. A failed-open ownership test
at the existing runtime/custom-store seam would have prevented this defect;
no new abstraction was needed.

## Costs: resource cleanup, allocation counts and noisy timing

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64. CPU labels specify GOMAXPROCS, not
processor affinity. No other agent tests or builds ran during measurements;
unrelated host activity was uncontrolled. Load averages at the end of the direct
comparison were **17.875 / 16.950 / 19.359**. This is context, not proof of the
cause of any result. No throughput, peak-memory or RSS claim is made.

The error microbenchmark supplies a reusable in-memory reader, so Close dispatch
can be measured without accumulating file descriptors on the old implementation.
The nil-error case means a nil reader plus an opening error; neither case reads
a body. Cleanup goes from **0 to 1 Close/op** when a reader is returned, remaining
**0 B/op and 0 allocs/op**. A real provider's syscall/network cleanup cost is
additional and is not estimated by this synthetic benchmark.

### Separate before/after binaries: all results retained

Both binaries use identical final benchmarks; only baseline production files are
overlaid. An ABBA sequence runs three repetitions per block at CPU 1 and 8:
**192 observations**, six per case/version/CPU. See [plan](raw/measurement-plan.json),
[runner/parser](raw/measure.py.txt), [raw results](raw/measurements.log),
[all samples](raw/samples.json) and [summary with ranges and allocations](raw/performance-summary.json).
Negative changes are reductions in sampled time; error results now include cleanup
and therefore do additional work. Large improvements are not claimed as speedups.

| CPU | Benchmark (prefix omitted) | Median ns/op before → after | Change |
| --- | --- | ---: | ---: |
| 1 | `OpenedObjectRead/get-64KiB` | 24,696.00 → 14,224.00 | -42.40% |
| 1 | `OpenedObjectRead/get-tiny` | 1,682.50 → 2,361.00 | +40.33% |
| 1 | `OpenedObjectRead/save-64KiB` | 54,867.50 → 47,878.50 | -12.74% |
| 1 | `OpeningError/nil` | 47.32 → 108.15 | +128.55% |
| 1 | `OpeningError/reader` | 51.44 → 122.53 | +138.22% |
| 1 | `StreamCompleteness/adapter` | 1,203.00 → 1,248.00 | +3.74% |
| 1 | `StreamCompleteness/range` | 2,353.00 → 2,258.50 | -4.02% |
| 1 | `StreamCompleteness/tiny` | 986.20 → 878.45 | -10.93% |
| 8 | `OpenedObjectRead/get-64KiB` | 45,893.00 → 21,576.50 | -52.99% |
| 8 | `OpenedObjectRead/get-tiny` | 2,267.50 → 1,588.50 | -29.94% |
| 8 | `OpenedObjectRead/save-64KiB` | 83,146.00 → 99,182.50 | +19.29% |
| 8 | `OpeningError/nil` | 52.44 → 50.05 | -4.56% |
| 8 | `OpeningError/reader` | 61.64 → 51.62 | -16.26% |
| 8 | `StreamCompleteness/adapter` | 1,851.50 → 1,168.50 | -36.89% |
| 8 | `StreamCompleteness/range` | 3,770.50 → 2,394.50 | -36.49% |
| 8 | `StreamCompleteness/tiny` | 1,194.00 → 1,010.45 | -15.37% |

The adverse CPU-1 error samples exceed 2×, while CPU-8 samples decrease. Tiny GET
also rises 40.33% at CPU 1; guarded 64 KiB rises 19.29% at CPU 8. These results are
retained, not dismissed or replaced by the follow-up. They do not establish
latency parity or a stable regression magnitude.

All allocation counts match: stream 10, range 11, adapter 13, GET 14 and guarded
read 19. CPU-1 allocated bytes match exactly: 1,072 / 1,104 / 1,568 / 1,155 /
66,691 / 67,175 for tiny stream, range, adapter, tiny GET, 64 KiB GET and guarded
64 KiB respectively. At CPU 8, the buffered-read B/op medians differ:
1,192.5 → 1,200, 68,176 → 68,569 and 69,570 → 69,843. Allocated-byte parity is
not claimed there. Existing pooled copy buffers and GC contribute to benchmark
accounting, but this run does not isolate the cause of those differences.

### Same-binary comparison of the changed opening method

A second probe embeds the exact baseline openObject method with only its name
changed. Both versions use the unchanged openStoreRange and **the same concrete
stream types**, including WriterTo. The probe checks type/method preservation.
No replacement reader implementation or missing fast path can explain a difference.
See [reference verification](raw/paired-reference.json), [source](raw/paired-probe.go.txt),
[runner/parser](raw/paired.py.txt), [240 blocks](raw/paired.log),
[samples](raw/paired-samples.json) and [summary including every cycle range](raw/paired-summary.json).

Ten alternating ABBA/BAAB cycles per case/CPU contain duplicate labels for the
same code to measure within-run variation. Fixed iterations are 200,000 per error
block and 20,000 per healthy open+Close block. Both wall time and process user+
system CPU are recorded; process CPU includes background runtime work. This
isolates opening, not complete buffered GET or disk/network service latency.

| CPU | Case | Median paired CPU change | Same-code absolute CPU change | Median paired wall change |
| --- | --- | ---: | ---: | ---: |
| 1 | `nil-error` | -1.94% | 3.59% | -3.50% |
| 1 | `reader-error` | +1.16% | 3.26% | +0.48% |
| 1 | `healthy-open` | -0.75% | 3.22% | +0.94% |
| 8 | `nil-error` | -1.20% | 1.60% | -1.25% |
| 8 | `reader-error` | +0.78% | 1.45% | +1.86% |
| 8 | `healthy-open` | +1.00% | 7.42% | +1.86% |

Median paired CPU changes are smaller than the corresponding same-code variation.
Individual wall cycles still vary greatly (reader-error CPU 1 reaches +224.26%).
This follow-up does not support assigning the earlier 2× error timings to a stable
CPU regression from this correction. It also does not prove latency parity. The
reliable conclusions are explicit Close ownership, unchanged allocation counts,
zero additional error-path allocations and the measured small WASM increase.

## Next confirmed finding — left unchanged

Run-through still discards a provider reader on an opening error in its local
read and upstream refresh paths, before the runtime can close it. A separate
file-backed overlay probe exercises the real Adapter → runtime composition:
[source](raw/next-runthrough-probe.go.txt),
[baseline normal](raw/next-before.log), [baseline race](raw/next-before-race.log),
[candidate normal](raw/next-after.log), [candidate race](raw/next-after-race.log).
Both cases fail twice in every log: Close count zero and file still usable.
Original errors, accounting and repaired retries retain their behavior.

This is the next confirmed resource-correctness finding in this review, with the
same injected-provider limitation as the current repro. Native S3Client's own
metadata-conversion failure already closes its response body. The remaining
adapter ownership paths need their own regression matrix and independent change;
no run-through production code is modified here.
