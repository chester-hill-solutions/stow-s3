# Close refused propagation source readers

Corrected on 2026-10-16 from `877526a59da521239ab2fe688091e42a5fe4af52`, on
`codex/core-copy-quality`. Freshly fetched `origin/main` is
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor of this branch.
The preceding [captured-version correction](../2026-10-16-runthrough-version/README.md)
is preserved, including its disclosed changed-version durable fallback cost.
No completed storage, COPY, publication or quota work is repeated.

## Demonstrated defect and bounded correction

The retained propagation probe opens a real file as the authoritative local
source and injects an opening error alongside that reader. A mirror PUT commits
locally, then propagation returns the source error before its existing Close
defer is installed. No source bytes are read and no upstream PUT occurs, but
the file remains open. Retried failures can retain additional provider resources.
This is a provider fault injection over native MemoryStore descriptors and real
file readers, not evidence that native filesystem GET naturally returns a
reader together with an opening error.

Before implementation, the [original probe](../2026-10-16-runthrough-version/raw/next-propagation-probe.go.txt)
fails twice [normally](raw/original-before.log) and twice [under race](raw/original-before-race.log).
It passes twice [normally](raw/original-after.log) and twice [under race](raw/original-after-race.log)
after the correction.

The production diff adds **three lines** to propagateWrite's existing opening-error
branch: close a non-nil reader, then return the original error. Successful opens,
version-conflict cleanup and the existing deferred Close remain in place. No
helper, wrapper, additional source GET, journal write, dependency, format or public API is
introduced. The source-opening failure still prevents upstream propagation.

Close errors are deliberately ignored at this boundary. They must not replace
the source error, remove its committed marker or turn a transient source failure
into a terminal conflict. This does add the provider's Close latency to failed
opens; Close can block or fail, and no additional cleanup retry is introduced.
Existing successful operations already call Close. Nil readers require no cleanup.

## Regression coverage

The permanent matrix contains **33 cases**: four propagation entry paths (live
PUT and queued PUT/COPY/multipart), four opening results (reader, nil reader,
Close failure and nil metadata), each with and without the committed marker,
plus a healthy-open version-conflict control. The queued COPY/multipart cases
exercise delivery of their existing immutable destination versions, not their
separate mutation/initiation paths.

Per repetition on exact baseline production code, **24 cases fail and nine
controls pass**. The original pre-edit runs and final-source baseline-overlay
replays run twice normally and twice under race. No compile failure is counted
as a reproduced defect. All 33 pass after the correction.

Each non-nil refused reader closes exactly once, its real file becomes unusable,
and it consumes zero body bytes. Original source-error identity and the local
commit marker survive. No upstream PUT occurs on failed open. The FileOutbox
entry remains nonterminal with its version, failure and retry schedule intact,
including Close errors containing conflict/missing sentinels. After repairing
the fault and making the existing entry due through the existing scheduling API,
one retry delivers it exactly once and clears the queue. Version conflicts still
close once and remain terminal. No sleeps establish these outcomes.

Evidence: [pre-edit normal](raw/before-tests.log), [pre-edit race](raw/before-race.log),
[final-source baseline normal](raw/final-before.log), [final-source baseline race](raw/final-before-race.log),
and [corrected normal, 66 passing executions](raw/after-tests.log).
[Final source provenance](raw/final-provenance.json),
[focused/provenance runner](raw/final-checks.py.txt), and [final origin refresh](raw/fetch-final.log).

## Verification

The final implementation passes:

- [All Go tests](raw/full-go.log), [broad storage/runtime/S3/run-through race checks](raw/broad-race.log), and [vet](raw/vet.log).
- [Entire run-through race suite, five repetitions](raw/focused-race.log): **1,460 passing leaf executions**.
- [Conformance](raw/conformance-summary.log) on memory, filesystem, runtime-memory and runtime-filesystem; [full output](raw/conformance-full.log). The pre-existing mirror-bucket authority decision and opt-in live upstream tests remain skipped in each profile. No live AWS qualification is claimed.
- [Go/file-size gates](raw/quality.log), [WASM build](raw/build-wasm.log), [package build](raw/package-build.log), [generated-artifact check](raw/generated.log), [real WASM smoke test](raw/wasm-smoke.log), and [seven package integration tests](raw/package-tests.log).

The [rebuilt WASM identity](raw/wasm.json) is unchanged: **4,619,197 bytes**,
SHA-256 `a828baf915d451665700e039bf7a1895bdfcd2306c2298cca9ea9482668f1193`.
Native propagation is outside that artifact. [Verification runner](raw/validate.py.txt).

## Before/after measurements

Two optimized ordinary test binaries contain identical final tests and benchmarks.
Only outbox_adapter.go is replaced by its exact baseline in the reference binary.
The [runner](raw/measure.py.txt) records source/compiler/binary identities in
[environment.json](raw/environment.json) and [compiler-environment.json](raw/compiler-environment.json).
Apple M1 Pro, Darwin arm64, Go 1.25.6. CPU settings select GOMAXPROCS, not parallel
benchmark workers.

Eight cases cover successful delivery, version conflict, nil-reader error and
reader-plus-error, each with first-attempt and reconciliation entry. The fixture
uses a native MemoryStore object and MemoryOutbox, a three-byte source, and a
stub upstream that consumes bytes and reports absence for reconciliation. It
isolates propagateWrite; it does not time queue claims, durable scheduling,
FileOutbox I/O or network storage. The failed reader is a reused synthetic
ReadCloser, allowing the leaking reference to run without exhausting real descriptors.
Its Close increments a counter; it does not model real provider cleanup latency.

Two alternating ABBA/BAAB cycles at CPU 1 and 8, two repetitions per block and
100 ms per measurement produce **256 retained observations**, eight per case,
version and CPU: [plan](raw/plan.json), [raw](raw/measurements.log),
[samples](raw/samples.json), [summary with ranges](raw/summary.json).
All samples, adverse medians and both cycle comparisons are retained.

| CPU | Case | Median ns/op before → after | Change | Both cycle changes | Same-code absolute change, median |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | `conflict/first` | 479.85 → 448.10 | -6.62% | -50.28%, +28.77% | 22.20% |
| 1 | `conflict/reconcile` | 555.95 → 429.55 | -22.74% | -51.46%, -14.20% | 15.51% |
| 1 | `nil-error/first` | 16.11 → 13.91 | -13.63% | -18.82%, -18.59% | 18.84% |
| 1 | `nil-error/reconcile` | 15.63 → 13.87 | -11.29% | -3.52%, -35.93% | 10.52% |
| 1 | `reader-error/first` | 14.68 → 14.24 | -3.03% | -12.78%, -3.08% | 13.03% |
| 1 | `reader-error/reconcile` | 14.62 → 13.79 | -5.71% | -48.41%, +2.77% | 15.15% |
| 1 | `success/first` | 605.80 → 508.50 | -16.06% | -45.98%, +280.59% | 15.05% |
| 1 | `success/reconcile` | 541.20 → 531.25 | -1.84% | -36.15%, -6.05% | 19.08% |
| 8 | `conflict/first` | 391.95 → 469.55 | +19.80% | +3.75%, +115.42% | 16.22% |
| 8 | `conflict/reconcile` | 514.15 → 374.20 | -27.22% | -30.61%, -62.96% | 17.26% |
| 8 | `nil-error/first` | 15.95 → 16.56 | +3.82% | -6.70%, +15.35% | 11.53% |
| 8 | `nil-error/reconcile` | 13.99 → 15.15 | +8.29% | +28.44%, +30.59% | 19.59% |
| 8 | `reader-error/first` | 13.39 → 16.12 | +20.34% | +28.02%, +10.62% | 11.39% |
| 8 | `reader-error/reconcile` | 14.32 → 13.98 | -2.31% | -22.13%, +2.30% | 5.46% |
| 8 | `success/first` | 505.35 → 446.35 | -11.68% | -36.29%, +43.84% | 18.42% |
| 8 | `success/reconcile` | 469.15 → 551.15 | +17.48% | +62.07%, +18.90% | 44.92% |

**These wall timings do not establish latency parity or a speedup.** The adverse
CPU-8 conflict/first and success/reconcile medians increase **19.80% and 17.48%**.
CPU-8 nil-reader errors increase 3.82%/8.29%, and reader-error/first increases
20.34%. CPU-1 success/first has a **+280.59%** second-cycle comparison despite its
lower pooled median: its candidate samples range from **481.3 to 4,428 ns/op**.
The corresponding reference ranges from 475.9 to 1,178 ns/op. Same-code variation
and cycle disagreement are substantial. They are not grounds for silently
removing slower observations or claiming all overhead is noise.

### Allocations and cleanup

Allocated bytes and allocation counts are identical in every observation at both
CPU settings, including first and reconciliation paths:

| Result | B/op before → after | Allocs/op before → after |
| --- | ---: | ---: |
| Successful delivery | 616 → 616 | 7 → 7 |
| Version conflict | 600 → 600 | 6 → 6 |
| Nil-reader opening error | 0 → 0 | 0 → 0 |
| Reader-plus-error opening failure | 0 → 0 | 0 → 0 |

The failed-close/op metric is **0 → 1** for reader-plus-error and remains zero
otherwise. This counter concerns the synthetic refused reader only; successful
native readers still close through their existing path. The repaired error path
does additional required cleanup, so comparisons are correctness costs rather
than equivalent-result speedups. A real Close can incur a syscall, network work,
blocking or failure beyond this synthetic measurement.

### Fixed-iteration process CPU diagnostic

The adverse wall samples justify a second, separately retained diagnostic, rather
than excluding results. With the same binaries, each case runs one million
iterations for native successful/conflict paths or 20 million for tiny opening
errors. Python records child-process user plus system CPU time divided by the
reported iteration count. This includes executable startup, Go test setup,
benchmark setup and teardown; it is not per-function instrumentation. The fixed
large counts reduce that fixed overhead, but they do not eliminate host CPU
frequency or scheduling effects. These are serial runs at GOMAXPROCS 1 and 8.
No other validation/build is run concurrently by this review.

Each case uses a full ABBA then BAAB sequence: **128 retained process samples**,
four per version/case/CPU. [Runner](raw/cpu-measure.py.txt),
[raw](raw/cpu-measurements.log), [all observations](raw/cpu-samples.json),
[summary/ranges/cycle comparisons](raw/cpu-summary.json).

| CPU | Case | Median process CPU ns/op before → after | Change | Same-code absolute change, median |
| --- | --- | ---: | ---: | ---: |
| 1 | `conflict/first` | 475.58 → 499.03 | +4.93% | 7.49% |
| 1 | `conflict/reconcile` | 447.19 → 424.58 | -5.05% | 7.44% |
| 1 | `nil-error/first` | 13.37 → 13.48 | +0.87% | 1.57% |
| 1 | `nil-error/reconcile` | 13.51 → 14.00 | +3.62% | 2.54% |
| 1 | `reader-error/first` | 13.53 → 13.09 | -3.25% | 2.80% |
| 1 | `reader-error/reconcile` | 13.79 → 13.72 | -0.45% | 5.83% |
| 1 | `success/first` | 502.72 → 480.46 | -4.43% | 7.77% |
| 1 | `success/reconcile` | 534.37 → 532.75 | -0.30% | 11.06% |
| 8 | `conflict/first` | 441.04 → 445.26 | +0.96% | 7.04% |
| 8 | `conflict/reconcile` | 426.32 → 425.79 | -0.12% | 8.37% |
| 8 | `nil-error/first` | 14.46 → 13.58 | -6.10% | 4.40% |
| 8 | `nil-error/reconcile` | 13.57 → 13.59 | +0.13% | 0.79% |
| 8 | `reader-error/first` | 13.73 → 13.22 | -3.75% | 1.13% |
| 8 | `reader-error/reconcile` | 13.60 → 13.84 | +1.75% | 3.03% |
| 8 | `success/first` | 486.65 → 495.17 | +1.75% | 2.58% |
| 8 | `success/reconcile` | 536.19 → 495.41 | -7.61% | 3.40% |

The CPU diagnostic does not reproduce the 17–20% adverse healthy-path wall
medians: healthy CPU changes range from **-7.61% to +4.93%**. It also contains
increases, including CPU-1 conflict/first +4.93% and nil-error/reconcile +3.62%.
It does not prove exact performance parity or establish that any particular wall
sample was caused by load. All original adverse wall results remain above.

No HTTP throughput, peak heap or RSS claim follows from these small fixture
measurements. This correction adds no allocation, source read, upstream request
or provenance persistence operation. The previous changed-version durable
fallback's **85–97% measured timing increase remains unchanged and unresolved**;
these source-opening measurements do not remeasure or repair that separate cost.

## Next highest-priority confirmed finding — left unchanged

Prepared write recovery accepts any error matching ErrObjectNotFound as proof
that the local mutation did not commit. If HEAD instead returns a missing error
joined with ErrMutationCommitted, recovery silently discards the prepared intent
rather than preserving uncertainty. This is the same existing marked-error
convention that earlier runtime fixes retain; it is not reliable absence.

The [separate deterministic probe](raw/next-prepared-probe.go.txt) prepares an
unowned PUT intent in a real FileOutbox, commits a replacement in native
MemoryStore, then injects that ambiguous HEAD result. RecoverPrepared returns
nil and removes the intent. The removal survives closing and reopening the
FileOutbox. The local replacement still exists, but repairing HEAD and retrying
never propagates it because its only recovery intent is gone. A plain uncertain
HEAD error is a passing control: it preserves the intent and later propagates
exactly once.

The marked-missing case fails twice and the ordinary uncertainty control passes
twice in each of [baseline normal](raw/next-before.log),
[baseline race](raw/next-before-race.log), [candidate normal](raw/next-after.log)
and [candidate race](raw/next-after-race.log). [Probe runner](raw/prepared-probe.py.txt).
This models an existing recovery state and provider error result; it does not
claim a physical process crash or a naturally occurring native filesystem HEAD
failure. The journal is reopened, while the native memory object remains in the
same process. No prepared-recovery implementation or permanent test is added in
this reader-ownership correction.

[Documentation/site gate output](raw/docs.log) accompanies this report.
