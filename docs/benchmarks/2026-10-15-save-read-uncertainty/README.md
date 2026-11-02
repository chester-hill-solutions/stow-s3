# Preserve marked read uncertainty in ReadForSave

Baseline `720a9ea` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. This addresses the separate
[guarded-read finding](../2026-10-15-object-head-uncertainty/README.md#next-confirmed-finding--readforsave-fabricates-absence-on-ambiguous-get).
Previously completed HEAD, COPY, multipart and adapter corrections remain intact.
No new interface, public API, dependency, format or optimization is introduced.

## Original defect and correction

The retained probe creates a real existing object, then reports an unavailable
GET joined with ErrMutationCommitted and ErrObjectNotFound. ReadForSave treats
any missing cause as authoritative absence, discards the error and issues a bound
absence condition. Both joined orders and their wrapped forms reproduce. The
backend still refuses replacing that existing object under an absence guard;
this is a false observation/condition, not a demonstrated guard bypass or data loss.

The [original probe](raw/original-probe.go.txt) fails twice normally and twice
under -race before production changes: [normal](raw/original-before.log),
[race](raw/original-before-race.log). It now passes twice in both modes:
[normal](raw/original-after.log), [race](raw/original-after-race.log).
The native wrapper deliberately injects the read uncertainty over real memory
storage; built-in memory GET does not naturally emit this marked error.

The existing public mapper already preserves these marked errors and the backend
already enforces absence guards. The defect is isolated to the runtime's read
classification. The production diff changes **one condition in guarded_save.go**:
only missing without a committed marker becomes observed absence. Other errors
return through the existing error path with an empty object and unbound condition.
The original error is unchanged, including its wrapper and all joined causes.
No new lookup, body read, clone, lock, buffer or error wrapper is added.

Confirmed absence, complete-object fingerprinting, stale-generation detection,
owner/resource binding, current authority/policy and quota enforcement retain their
existing contracts. A condition from an uncertain marked read is now unusable:
SaveObject rejects it as ErrInvalidSaveCondition / SaveNotCommitted before writing.
A healthy subsequent observation can produce a valid condition and reuse full
capacity. This is the behavior already documented for empty conditions in
[portable workspace usage](../../portable-workspace-usage.md).

## Regression and validation evidence

The final matrix contains **29 cases**:

- Twelve native defect cases combine both joined orders and wrapping with GET
  failure on existing/absent objects and failure after bytes are read from an
  existing object's body. They check exact error identity, empty object/condition,
  one GET, body closure, unchanged quota/revision, invalid-condition refusal,
  preserved actual object identity/properties, healthy retry, full capacity and
  stale-condition rejection.
- Eight controls cover unknown and marker-only open/body errors, raw/wrapped
  ordinary missing, a complete object and a missing bucket. Ordinary absence
  remains a usable creation condition; missing buckets remain errors.
- Five refusal cases cover cancellation, authority, policy, closed state and
  unsupported stores, with no provider GET and no condition issued.
- Four cases exercise the public embedded wrapper in both joined orders and
  wrapping. They verify exact error preservation, invalid-condition translation
  and healthy read/save recovery over a qualified native memory provider. This
  does not enable guarded saves for custom public stores, which remain unsupported.

All final sources were run twice against unchanged production code before the
one-condition edit: [baseline](raw/before-tests.log). Sixteen defect cases fail
in each repetition; the controls already pass. All 29 pass twice afterward:
[after](raw/after-tests.log). The direct and public seams exercise actual reads,
condition validation, guarded publication and quota enforcement; no new test-only
production hook is introduced.

Five focused race repetitions pass **145 cases**, without skips:
[focused race](raw/focused-race.log). Full Go tests, relevant runtime/storage/S3/
run-through/embedded/public race suites, all four conformance profiles, relevant
vet, unchanged quality/file-size ratchets, formatting, documentation links,
generated-output verification, WASM smoke and seven package integration tests pass.
[Full Go](raw/full-go.log), [broad race](raw/broad-race.log),
[conformance summary](raw/conformance-summary.log), [quality](raw/quality.log) and
[package tests](raw/package-tests.log) retain the results. Each conformance profile
keeps two existing skips: opt-in live upstream coverage and mirrored bucket
creation awaiting an authority decision. No ratchet baseline is raised.

## Comparative measurements and new costs

BenchmarkReadForSaveObservation calls the actual runtime method over a real memory
store, with eight-byte/one-object limits. Present objects contain three bytes,
text/plain and one metadata entry; successful reads include body copying,
metadata handling and fingerprinting. Actual absence reaches the real provider.
Wrapped missing and uncertain errors are preconstructed and injected at the native
GET seam, with no underlying body opened. Setup, initial object creation,
error construction and close are outside timing. Runtime checks, locking, GET and
observation classification are inside. A provider counter verifies one GET per
iteration. This is not a disk/HTTP benchmark and does not measure peak memory.

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64. Identical benchmark sources are compiled
against original and corrected source. Serial baseline/candidate/candidate/baseline
blocks contain three samples each at one processor, then eight: six samples per
version/case/configuration. Present-object samples have 250,000 iterations; other
samples 2,000,000. Loops are serial even with GOMAXPROCS=8. No agent builds/tests
run concurrently with measurements; other host activity is uncontrolled.
Every command, sample/outlier, binary digest and allocation value is retained in
[measurements](raw/measurements.json), the raw benchmark logs and
[environment](raw/benchmark-environment.json).

| Case | Processors | Median ns/op before → after | Change | Before range | After range |
|---|---:|---:|---:|---:|---:|
| Present object | 1 | 5,295.500 → 5,043.000 | -4.8% | 4,303.000–6,283.000 | 4,531.000–6,095.000 |
| Actual missing object | 1 | 454.900 → 495.150 | +8.8% | 378.300–515.600 | 414.000–713.500 |
| Prepared wrapped missing | 1 | 302.650 → 342.150 | +13.1% | 280.400–367.200 | 274.200–376.100 |
| Unknown error | 1 | 319.900 → 366.000 | +14.4% | 243.100–383.800 | 330.200–410.900 |
| Ambiguous, marker first | 1 | 327.450 → 370.600 | +13.2% | 232.000–529.800 | 343.700–403.400 |
| Ambiguous, missing first | 1 | 249.300 → 359.450 | +44.2% | 162.000–359.000 | 320.600–408.000 |
| Present object | 8 | 2,187.500 → 2,264.000 | +3.5% | 2,114.000–2,330.000 | 2,091.000–2,593.000 |
| Actual missing object | 8 | 215.350 → 222.300 | +3.2% | 212.500–223.800 | 218.200–250.700 |
| Prepared wrapped missing | 8 | 161.100 → 156.450 | -2.9% | 153.300–194.900 | 152.300–171.500 |
| Unknown error | 8 | 160.950 → 163.000 | +1.3% | 157.200–170.100 | 157.300–166.500 |
| Ambiguous, marker first | 8 | 158.100 → 170.500 | +7.8% | 154.400–162.400 | 169.600–184.100 |
| Ambiguous, missing first | 8 | 152.900 → 168.200 | +10.0% | 148.600–166.000 | 166.600–169.800 |

Present-object medians measure **5,295.5 → 5,043 ns (−4.8%)** at one processor and
**2,187.5 → 2,264 ns (+3.5%, +76.5 ns)** at eight. Distributions overlap; no
successful-read speedup or exact parity is claimed. The marker check is skipped
when the missing sentinel does not match, including successful reads.

Actual absence measures **454.9 → 495.15 ns (+8.8%, +40.25 ns)** and
**215.35 → 222.3 ns (+3.2%, +6.95 ns)**. Prepared wrapped missing measures
**302.65 → 342.15 ns (+13.1%, +39.5 ns)** and **161.1 → 156.45 ns (−2.9%)**.
Marker-first ambiguity measures **+13.2% / +7.8% (+43.15 / +12.4 ns)**; missing-first
measures **+44.2% / +10.0% (+110.15 / +15.3 ns)**. The new marker traversal adds real
work when a missing cause matches. The one-processor run varies substantially,
including an unknown-error increase of +14.4% even though that path skips the new
marker scan; its ambiguous baseline samples span 162–529.8 ns. The measurements
do not isolate every timing difference to the new condition. All adverse samples
are retained, including candidate actual-absence 713.5 ns. No new optimization or
resolution of earlier reviews' separate performance concerns is inferred.

Uncertain and prepared wrapped-missing samples remain **0 B/op, zero allocations**.
Actual provider absence stays **48 B/op, one allocation**. Healthy reads stay
**19 allocations**: at one processor both versions report 1,619 B/op; at eight,
baseline samples report 1,695–1,716 B/op and candidate samples 1,689–1,707 B/op.
All per-sample byte totals are retained. Prepared errors bypass provider work, so
their allocation totals must not be equated with real absence. These are cumulative
allocations, not RSS or peak heap. Body-read errors are covered by tests rather
than this open-error benchmark; their existing read/close/buffer path is unchanged.

Compatibility behavior: a marked missing read now fails and issues no usable
condition. Callers need a reliable fresh observation before saving. The original
error chain remains visible, and normal absent-object creation is unchanged.
No extra provider request, fingerprint or payload-sized buffer is introduced.

The rebuilt packaged WASM remains **4,617,224 bytes (zero size change)**,
with a new digest. [Artifact metadata](raw/wasm.json) records both digests;
generated-output verification, WASM smoke and package tests validate the refreshed
artifact. No managed-save API is added to WASM or S3.

## Next confirmed finding — partial body failure still becomes absence

A separate native probe opens the real three-byte object and its metadata, reads
one byte successfully, then returns raw or wrapped ErrObjectNotFound from the
reader without a committed marker. ReadForSave still converts that body failure
into success with an absence condition, losing the read error. The stream closes
once, but there was no complete read and the actual object remains present.
Both forms reproduce twice normally and twice under -race:
[source](raw/next-partial-read-probe.go.txt), [normal](raw/next-partial-read.log),
[race](raw/next-partial-read-race.log). No data race is reported.

The backend's absence guard rejects the subsequent save, preserving the real
bytes/version; a healthy observation and guarded save then succeed. This is a
separate defect in distinguishing lookup errors from body failures: a missing
error after a successful open is
being treated like lookup absence. It is not a demonstrated overwrite or a claim
that the built-in memory reader naturally emits missing errors after partial data.
The deliberately faulted reader uses the existing io.ReadCloser contract. No
change to body-read error handling is included in this marked-error correction.

Reproduce by temporarily copying the text to
internal/runtime/partial_save_read_review_probe_test.go and running only
TestReviewReadForSavePartialBodyMissing with -count=2, optionally -race, then
removing it. The probe is removed before broad validation.
