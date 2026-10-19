# Remove copies of the buffered read result

Baseline source is `11f3c9a` (production is unchanged from `c614a2b`); freshly
fetched origin/main remains `219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor.
The [earlier unresolved timing investigation](../2026-10-15-save-read-call-boundary/README.md)
and [body-error correction evidence](../2026-10-15-save-body-failure/README.md)
remain unchanged, including every earlier adverse result.

This change removes full Object argument/return copies at the existing private
read helper boundary. Tiny GET medians improve consistently in the prototype,
actual production same-binary comparison and direct API comparison. The latter
measures **872.35 → 839.40 ns (−3.78%)** at one processor and
**790.15 → 756.35 ns (−4.28%)** at eight. No allocation count increases.

This is a modest result-copy refinement. It does **not** establish broad latency
parity: direct measurements still include **+1.70% guarded 64 KiB reads** at one
processor and **+1.84% GET 64 KiB** at eight. Identical-code controls now quantify
variation wider than those changes, and GC CPU counters correlate with large-read
latency. Those observations improve attribution; they do not prove GC explains
every prior outlier or guarantee that a small regression is absent.

## Measurement loop and hypotheses, before production edits

The prior separate-binary timing differences were not stable. Three predictions
were tested: extra record/call movement should produce a roughly constant small
cost; GC-related variation should correlate with measured GC work; uncontrolled
host variation should also appear when comparing identical implementations.

A disposable harness places the exact original `6300249` methods and real current
methods in one ordinary-build test binary. It uses the same qualified native memory
runtime, one object, 1 MiB/one-object caps, caller-owned buffered APIs, one metadata
entry and text/plain. Both implementations warm up; provider counters verify one
opening per operation. Both versions still execute authorization, lifecycle,
streaming and, for guarded reads, fingerprinting. Setup is outside timing.

Twenty cycles alternate A-B-B-A and B-A-A-B. Each implementation has two labels
calling the exact same function, giving A/A controls. Tests repeat twice at each
of CPUs one/eight. Each case/configuration has 160 measured blocks; each cycle
compares geometric means of its two blocks per implementation. The reported
paired change is the median of 40 cycle ratios. The independent ns/op column
medians are a different statistic, so their quotient need not equal that ratio.

The initial comparison uses 10,000 operations/block for three-byte reads and
1,000 for 64 KiB. Identical-code absolute control medians range about 1.6–6.3%,
while original-vs-current guarded 64 KiB paired changes are −0.61%/+0.25%.
GET of three bytes shows +1.85%/+4.93% paired current cost. Its controls also vary,
so this alone is not a precise copy-cost estimate. [Initial results](raw/matched.log),
[summary](raw/initial-summary.json), [source](raw/probe.go.txt), [plan](raw/plan.json).

Runtime allocation, GC-cycle, pause and GC-CPU counters are read outside each
timed batch. These process-wide counters include runtime/background work and
boundary effects; they are diagnostic, not a per-call CPU profile. Blocks with
large latency often have more GC CPU, including strong correlations around 0.94
for the initial one-CPU guarded 64 KiB case. Correlation is not proof of causation.
The existing streaming copy buffer pool is unchanged; misses were not separately
instrumented, so pool behavior is not claimed as the sole cause.

## Prototype and retained implementation

Before changing production, a pointer prototype is tested in the harness. It fills
the already-opened Object and returns only error. Tiny-read blocks grow to 50,000
operations; 64 KiB remains 1,000. Tiny GET paired medians improve 2.1–2.6% in all
four per-run/CPU comparisons; every run favors the prototype. The combined paired
changes are −2.42%/−2.18%. Other cases include adverse values; none is removed.
[Prototype measurements](raw/pointer-matched.log), [summary](raw/prototype-summary.json),
[per-run results](raw/pointer-repeat-summary.json), [source](raw/pointer-probe.go.txt),
[plan](raw/pointer-plan.json).

The prototype also checks 24 combinations of raw/wrapped missing, unexpected EOF
and committed body errors, after zero/one/all bytes, together with bytes or on the
next read. Each combination is run twice. Exact errors, no returned partial object
or condition, one close per opening and healthy recovery pass:
[contract probe](raw/pointer-contract.log). This is a performance refinement of the
already-demonstrated body-error fix, not a newly alleged correctness defect.

The production change updates the existing readOpenedObject signature from
(reader, Object) returning (Object, error) to (reader, *Object) returning error.
GetObject and ReadForSave pass their local result by address. Callers still return
a zero object/condition on error. Only Data is filled after successful io.Copy.
Opening absence and committed-error classification remain before body reading.

The buffer sizing/clamp, io.Copy, reader lifetime, close behavior, exact errors,
authorization, quotas, metadata cloning, data ownership and fingerprint inputs
remain unchanged. No additional opening, body pass or payload allocation is added.
The 32 MiB initial-capacity clamp is still not a total read or memory limit.
Close errors remain ignored as before. No interface, public API, storage abstraction,
dependency or format is added; no filesystem/index/checkpoint work starts.

Compiler escape analysis confirms the Object pointer does not escape. The same
binary's static ARM64 instruction counts shrink: GetObject 104 → 96, helper
148 → 140, ReadForSave 384 → 352. These static counts support removal of record
movement, not an executed-instruction or cycle guarantee.
[Compiler excerpt](raw/production-compiler-excerpt.log),
[assembly](raw/production-assembly.log), [counts](raw/production-assembly-summary.json).
Diagnostic compiler binaries are never used for timing.

## Actual production comparison with same-code controls

The final matched binary compares the real new production methods with an exact
frozen copy of the previous methods; only private names differ in the reference.
[Reference verification](raw/reference-verification.json) and
[source/binary identities](raw/production-source.json) are retained. New production
code is not replaced by an independently optimized synthetic loop.
[Raw results](raw/production-matched.log), [summaries](raw/production-summary.json),
[source](raw/production-matched-probe.go.txt), [plan](raw/production-matched-plan.json).

| Case | CPUs | Value median ns/op | Pointer median ns/op | Paired-cycle change | Same-code absolute control median |
|---|---:|---:|---:|---:|---:|
| GET, 3 bytes | 1 | 874.81 | 852.62 | -2.90% | 3.10% |
| GET, 64 KiB | 1 | 8,571.00 | 8,625.69 | +1.05% | 3.97% |
| Guarded read, 3 bytes | 1 | 2,210.28 | 2,192.24 | -0.83% | 1.19% |
| Guarded read, 64 KiB | 1 | 39,977.56 | 39,988.65 | +0.10% | 1.35% |
| GET, 3 bytes | 8 | 808.05 | 789.40 | -1.88% | 2.82% |
| GET, 64 KiB | 8 | 8,910.75 | 8,914.92 | +0.43% | 7.65% |
| Guarded read, 3 bytes | 8 | 2,063.77 | 2,048.41 | -0.71% | 1.03% |
| Guarded read, 64 KiB | 8 | 39,745.12 | 39,585.77 | -0.06% | 1.79% |

Tiny GET paired improvements are −2.90%/−1.88%; guarded three-byte reads are
−0.83%/−0.71%. Adverse 64 KiB GET paired changes are **+1.05%/+0.43%**, with
same-code control absolute medians of 3.97%/7.65%. Guarded 64 KiB is +0.10%/−0.06%,
against control medians 1.35%/1.79%. These results calibrate measurement resolution;
they do not justify claiming every small cost is zero. No outliers are filtered.

## Existing direct API benchmarks

The unchanged eleven existing benchmark cases are compiled with ordinary go test
-c. Before is the retained, digest-checked value-helper binary; after is the actual
production build without temporary probes. Three samples/block in before/after/
after/before order give six/version/case/CPU. Tiny successes use 250,000 operations,
opening errors/absence 1,000,000, 64 KiB 50,000 and body errors 100,000.

All measurement phases are serial with no concurrent agent tests/builds. Other
host activity is uncontrolled. Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64.
These are native memory workloads, not end-to-end HTTP or disk results and not
concurrent-throughput tests. The three matched phases contain 1,280 blocks each;
direct benchmarks add 264 samples: **4,104 measured observations, all retained**.
[Direct raw log](raw/direct.log), [all values/ranges](raw/direct-measurements.json),
[plan](raw/direct-plan.json), [harness](raw/measure-production.py.txt).
SOURCE headings identify the individual direct-log files used by the summaries.

| Case | CPUs | Before ns/op | After ns/op | Median change |
|---|---:|---:|---:|---:|
| GET, 64 KiB | 1 | 8,368.00 | 8,343.50 | -0.29% |
| GET, body error | 1 | 854.15 | 827.50 | -3.12% |
| GET, 3 bytes | 1 | 872.35 | 839.40 | -3.78% |
| Guarded read, 64 KiB | 1 | 39,438.00 | 40,106.50 | +1.70% |
| Guarded read, body error | 1 | 918.20 | 893.35 | -2.71% |
| Opening error, marker first | 1 | 135.40 | 136.85 | +1.07% |
| Actual absence | 1 | 199.30 | 201.45 | +1.08% |
| Opening error, missing first | 1 | 140.10 | 139.25 | -0.61% |
| Guarded read, 3 bytes | 1 | 2,151.50 | 2,155.00 | +0.16% |
| Unknown opening error | 1 | 123.60 | 124.90 | +1.05% |
| Prepared wrapped absence | 1 | 137.00 | 138.15 | +0.84% |
| GET, 64 KiB | 8 | 8,847.00 | 9,009.50 | +1.84% |
| GET, body error | 8 | 785.50 | 755.00 | -3.88% |
| GET, 3 bytes | 8 | 790.15 | 756.35 | -4.28% |
| Guarded read, 64 KiB | 8 | 40,052.50 | 40,007.50 | -0.11% |
| Guarded read, body error | 8 | 836.35 | 808.25 | -3.36% |
| Opening error, marker first | 8 | 135.65 | 135.40 | -0.18% |
| Actual absence | 8 | 190.25 | 190.70 | +0.24% |
| Opening error, missing first | 8 | 140.50 | 139.60 | -0.64% |
| Guarded read, 3 bytes | 8 | 2,035.00 | 2,015.50 | -0.96% |
| Unknown opening error | 8 | 123.80 | 123.40 | -0.32% |
| Prepared wrapped absence | 8 | 136.80 | 136.65 | -0.11% |

Adverse direct results are explicit: guarded 64 KiB at one CPU is
**39,438 → 40,106.5 ns (+1.70%, +668.5 ns)**; GET 64 KiB at eight is
**8,847 → 9,009.5 ns (+1.84%, +162.5 ns)**. The corresponding matched results
and same-code controls above prevent attributing those full deltas solely to this
small helper change. They also prevent a universal speedup/parity claim.

Opening-error/absence medians range roughly −0.64% to +1.08%; the prior discarded
phase-flag prototype's 11–26% error-path cost is not introduced. Source still opens
and classifies errors directly in ReadForSave. Tiny direct GET improves 3.78%/4.28%,
body-error GET 3.12%/3.88%, and body-error guarded reads 2.71%/3.36% in these runs.
The tiny guarded case is +0.16%/−0.96%; that adverse one-CPU value also remains.

Candidate outliers remain: eight-CPU tiny GET reaches **1,313 ns**, GET 64 KiB
**10,923 ns**, and one-CPU guarded 64 KiB **43,588 ns**. Earlier 93,447 ns and
+13.11% results remain in their original report; this run does not replace them.

Direct allocation counts are unchanged in every sample: successful GET/body errors
14, guarded successes 19, actual absence one, prepared opening errors zero.
One-CPU B/op is unchanged: tiny GET 1,139; body failures 1,147; GET 64 KiB 66,675;
guarded tiny 1,619; guarded 64 KiB 67,158–67,159; absence 48; prepared errors zero.
Eight-CPU byte totals vary, with all values retained. For example guarded 64 KiB
spans 69,763–70,068 B before and 69,861–70,132 B after. Runtime-counter measurements
also include fractional per-operation GC/runtime overhead and are not rounded
into a claim of strict byte equality. These are cumulative allocations, not peak
heap/RSS or a new memory guarantee.

The rebuilt packaged WASM grows **4,617,893 → 4,618,016 bytes (+123)**.
[Artifact hashes](raw/wasm.json) retain this compatibility/distribution cost.
No WASM speedup is inferred from native measurements.

## Contracts, validation and next finding

Four new ownership cases exercise both GET/guarded-read pairings: mutating the
first returned bytes/metadata cannot change a second result or storage, and the
original guard remains valid with full byte capacity. They pass twice before
production edits and twice afterward: [before](raw/before-ownership.log),
[after with prior regressions](raw/after-contracts.log). They pin preserved behavior;
they are not falsely presented as failing pre-existing correctness tests.

All 56 focused leaf cases (four new and 52 existing) pass five race repetitions,
**280 passes, no skips**: [focused race](raw/focused-race.log). Full Go tests,
affected runtime/storage/S3/run-through/embedded/public race suites, all four
conformance profiles, relevant vet, unchanged quality/file-size ratchets,
generated output, WASM smoke and seven package tests pass.
[Full Go](raw/full-go.log), [broad race](raw/broad-race.log),
[conformance](raw/conformance-summary.log), [vet](raw/vet.log),
[quality](raw/quality.log), [generated](raw/generated.log),
[WASM smoke](raw/wasm-smoke.log), [package tests](raw/package-tests.log).
Conformance retains two existing skips/profile: opt-in live upstream coverage and
mirrored bucket creation awaiting an authority decision. No baseline is raised.

All temporary measurement probes and dependency links are removed before broad
validation. Exact production hashes are checked against measured sources, along
with formatting, documentation links and staged whitespace checks:
[final checks](raw/final-checks.log). Retained probe files are diagnostic text,
not permanent tests; reproduce one at a time using the recorded commands and
remove it afterward. Source frozen references are internal to those probes.

The next confirmed correctness finding remains premature EOF returning truncated
data as success. It is documented in the previous body-error report. The read
loop/length handling is unchanged here; no early-EOF fix is automatically implemented.
