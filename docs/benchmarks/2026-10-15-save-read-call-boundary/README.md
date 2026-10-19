# Read timing investigation — no optimization retained

The body-error correction in `c614a2b` reported adverse 64 KiB guarded-read medians
of +2.1–2.3% and a 93,447 ns candidate outlier. Calling it finished without first
establishing repeatability was premature. The [original evidence](../2026-10-15-save-body-failure/README.md)
remains unchanged. This investigation preserves every new measurement and rejects
both implementation experiments: **production source and shipped WASM are restored
byte-for-byte to c614a2b**. No performance improvement or resolution is claimed.

Original means `6300249`, before the body-error correction. Committed means
`c614a2b`. Freshly fetched origin/main remains
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor of the branch.
No dependency, format, API, storage abstraction, indexing or checkpoint work is added.
The body-error fix remains intact; the separate early-EOF finding is unimplemented.

## Repeatability before implementation changes

The exact original/committed binaries are checked against their retained SHA256
identities. Sixteen serial blocks per CPU use ABBA order repeated four times,
eight samples/version/case at CPUs one and eight. Both 64 KiB GET and guarded
reads use 50,000 iterations/sample, five times the earlier 10,000.
[Plan](raw/repeat-plan.json), [all results](raw/repeat.log),
[summary](raw/repeat-summary.json).

Committed vs original medians are +3.93%/+0.84% for guarded reads and
−4.04%/+13.11% for ordinary GET at one/eight CPUs. Individual adjacent-pair ratios
vary considerably. These retain the adverse results and do not establish that
the earlier guarded +2.1–2.3% is a stable implementation cost. The summary's simple
paired t intervals are diagnostic approximations, not independent-host confidence
bounds or proof of equivalence. No allocation-count increase appears.

## Rejected experiments

Retained patches use zero context; apply with git apply --unidiff-zero when
reconstructing a prototype against c614a2b.

The committed helper receives and returns a complete Object record. That adds a
record/call boundary relative to the original combined open/read routine. This
was a plausible cost to investigate; timings do not isolate it from compiler
layout, allocation/GC behavior or host variation.

The first prototype passes only reader + size into the body helper and returns
bytes + error, keeping all read semantics unchanged. Its 64 KiB GET medians are
+0.87%/+4.62% vs original and −0.40%/+0.66% vs committed at one/eight CPUs. Guarded
64 KiB medians are −0.48%/−2.21% vs original. It does not demonstrate a dependable
improvement. [Patch](raw/narrow-prototype.patch.txt), [all results](raw/narrow-comparison.log),
[measurements/ranges](raw/narrow-measurements.json), [plan](raw/narrow-plan.json).

The second prototype retains the original combined open/read routine in a private
method returning an additional opening-success flag. ReadForSave distinguishes
lookup absence from body failure through that flag; the public GET wrapper inlines.
This avoids passing the Object through a second body helper and preserves the
corrected body-error semantics. [Patch](raw/phase-prototype.patch.txt),
[compiler evidence](raw/phase-compiler-excerpt.log), [source identity](raw/phase-source.json).

An intermediate compiler-check build used diagnostic -m=2 flags and an additional
explanatory comment. Its diagnostic flags do not change optimization settings,
but its generated data addresses differ. It is not substituted for an identical-
flags comparison: [all intermediate results](raw/diagnostic-comparison.log),
[measurements](raw/diagnostic-measurements.json), [plan](raw/diagnostic-plan.json),
[source identity](raw/diagnostic-source.json). That intermediate run placed success
medians within 1% of original; **the later identical-flags run does not**. The
added comment also hit the existing comment-line ratchet and was removed without
raising any baseline before the final prototype build.

## Final prototype comparison, with identical build flags

The finalized phase prototype is rebuilt with ordinary go test -c, matching the
original and committed benchmark build flags. All six permutations of the three
versions give each six samples/case/CPU. Eleven existing benchmark cases remain
unchanged, including actual runtime checks, real memory storage, copying and
fingerprinting, genuine absence, prepared opening errors and one-byte body failures.
Setup/error construction is outside timing; counts verify one provider opening and
one underlying close/byte for the body failures.

Three-byte successes use 250,000 iterations, opening errors/absence 1,000,000,
64 KiB reads 50,000 and body failures 100,000. CPU one precedes eight; these are
serial loops, not concurrent-load tests. Go 1.25.6, macOS 14.5 ARM64, Apple M1 Pro.
No agent tests/builds run during timed comparisons; other host activity is
uncontrolled. All four rounds total 1,252 individual case measurements; none is
excluded. SOURCE headers in combined logs preserve filenames referenced by JSON.

[All final results](raw/phase-comparison.log), [all samples/ranges](raw/phase-measurements.json),
[plan and binary identities](raw/phase-plan.json), [harness](raw/compare.py.txt),
[summary parser](raw/summarize.py.txt). The scripts use temporary binary paths;
rebuild identical benchmark sources against the recorded versions to repeat elsewhere.
Original body-failing guarded reads incorrectly succeed; those timings describe
the buggy path rather than an equivalent error result. Corrected versions return
the body error. The table gives medians, not proof of speedup or parity:

| Case | CPUs | Original ns/op | Committed ns/op | Phase prototype ns/op | Prototype vs original | Prototype vs committed |
|---|---:|---:|---:|---:|---:|---:|
| GET, 64 KiB | 1 | 8,303.50 | 8,404.00 | 8,184.50 | -1.43% | -2.61% |
| GET, body error | 1 | 814.45 | 843.40 | 820.10 | +0.69% | -2.76% |
| GET, 3 bytes | 1 | 825.05 | 843.30 | 814.30 | -1.30% | -3.44% |
| Guarded read, 64 KiB | 1 | 39,145.50 | 39,519.50 | 39,273.50 | +0.33% | -0.62% |
| Guarded read, body error | 1 | 908.45 | 912.15 | 890.20 | -2.01% | -2.41% |
| Opening error, marker first | 1 | 163.50 | 136.00 | 163.80 | +0.18% | +20.44% |
| Actual absence | 1 | 218.85 | 198.15 | 220.65 | +0.82% | +11.36% |
| Opening error, missing first | 1 | 160.05 | 138.25 | 162.70 | +1.66% | +17.69% |
| Guarded read, 3 bytes | 1 | 2,155.50 | 2,139.50 | 2,140.50 | -0.70% | +0.05% |
| Unknown opening error | 1 | 154.65 | 123.65 | 155.00 | +0.23% | +25.35% |
| Prepared wrapped absence | 1 | 152.05 | 136.30 | 152.20 | +0.10% | +11.67% |
| GET, 64 KiB | 8 | 8,378.00 | 8,309.00 | 8,999.50 | +7.42% | +8.31% |
| GET, body error | 8 | 731.90 | 789.85 | 720.75 | -1.52% | -8.75% |
| GET, 3 bytes | 8 | 735.20 | 757.30 | 768.55 | +4.54% | +1.49% |
| Guarded read, 64 KiB | 8 | 39,434.00 | 39,251.00 | 39,743.50 | +0.78% | +1.25% |
| Guarded read, body error | 8 | 829.30 | 829.00 | 817.60 | -1.41% | -1.38% |
| Opening error, marker first | 8 | 163.25 | 134.90 | 164.15 | +0.55% | +21.68% |
| Actual absence | 8 | 212.20 | 188.95 | 213.40 | +0.57% | +12.94% |
| Opening error, missing first | 8 | 159.85 | 138.60 | 162.40 | +1.60% | +17.17% |
| Guarded read, 3 bytes | 8 | 1,989.00 | 1,978.50 | 1,995.50 | +0.33% | +0.86% |
| Unknown opening error | 8 | 156.10 | 122.85 | 155.10 | -0.64% | +26.25% |
| Prepared wrapped absence | 8 | 153.40 | 136.25 | 152.05 | -0.88% | +11.60% |

The finalized prototype's **eight-CPU GET is +7.42% vs original, +8.31% vs
committed at 64 KiB**. Tiny GET is +4.54%/+1.49%, respectively. Guarded 64 KiB is
+0.33%/+0.78% vs original at one/eight CPUs, and −0.62%/+1.25% vs committed.
Opening-error/absence medians cost **11.36–26.25% more than committed**, roughly
16–32 ns/op. That explicit cost rules out treating this as an unqualified fix.

Adverse outliers remain: phase-prototype guarded 64 KiB reaches **62,356 ns** at
one CPU (original reaches 52,337 ns); its eight-CPU GET reaches 9,682 ns at 64 KiB
and 863.1 ns at three bytes. The original report's 93,447 ns sample and the first
repeat's +13.11% committed GET median remain in their respective records. No
later favorable result replaces them.

Allocation counts remain unchanged in every case of these comparisons: ordinary
GET/body errors 14, successful guarded reads 19, actual absence one, prepared
opening errors zero. One-CPU B/op remains unchanged in the final run: tiny GET
1,139; body failures 1,147; 64 KiB GET 66,675; tiny guarded read 1,619; guarded
64 KiB approximately 67,158; absence 48; prepared errors zero. Eight-CPU byte
counts vary; every individual value is retained. These are cumulative allocations,
not peak heap/RSS or proof of per-run byte equality. No resource guarantee changes.

The rejected phase prototype's WASM is 4,617,455 bytes, 438 bytes smaller than
committed, but still 231 bytes larger than original. [Prototype artifact metadata](raw/wasm.json)
is retained. The shipped artifact is restored to committed's **4,617,893 bytes**.
A smaller prototype artifact does not justify its unproven/adverse runtime costs.

## Decision, validation and remaining uncertainty

No prototype is retained. The committed correctness correction remains in place.
Its original reported slowdown has not been explained as
a repeatable causal regression; these runs also do not certify performance parity.
A more controlled host or sufficiently precise attribution is needed before
selecting an optimization. Further speculative tuning is stopped.

Both prototypes preserve the correction's existing regressions. The opening-phase
prototype passes the 22 body-failure cases plus prior controls; the broader focus
includes read authority, giving 52 leaf cases repeated five times under -race
(**260 passes**, no skips): [focused race](raw/focused-race.log). Candidate
[full Go](raw/full-go.log), [broader race](raw/broad-race.log),
[four conformance profiles](raw/conformance-summary.log), [vet](raw/vet.log),
[generated output](raw/generated.log), [WASM smoke](raw/wasm-smoke.log) and
[seven package integration tests](raw/package-tests.log) pass. These are prototype
validation evidence, not a reason to retain the prototype. Conformance keeps two
existing skips/profile: opt-in live upstream coverage and mirrored bucket creation
awaiting an authority decision. No ratchet baseline is increased.

After all experiments, source/artifact digests prove exact restoration to c614a2b:
[restoration](raw/restoration.json). The original correction's broader validation
remains linked in its evidence report; focused tests are also rerun on the restored
source: [restored tests](raw/restored-tests.log). Final formatting, unchanged
quality/file-size ratchets, documentation links and diff checks pass:
[final checks](raw/final-checks.log). Temporary links and probes are absent.

The next confirmed correctness finding is still premature EOF returning truncated
data as success. Its deterministic baseline/candidate reproduction remains in the
previous report; no early-EOF correction is implemented during this investigation.
