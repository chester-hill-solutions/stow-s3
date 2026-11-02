# Distinguish opening absence from body-read failure

Baseline `6300249` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. This addresses the separate
[partial-body finding](../2026-10-15-save-read-uncertainty/README.md#next-confirmed-finding--partial-body-failure-still-becomes-absence).
Earlier marked-error, HEAD, multipart and COPY corrections remain intact.
No storage abstraction, interface, public API, dependency or format is added.

## Evidence and smallest correction

The retained probe opens an existing three-byte object's real reader and metadata,
reads one byte, then injects raw or wrapped ErrObjectNotFound from the reader.
Ordinary GetObject returns the read error. ReadForSave instead sees that same final
error as lookup absence, discards it and issues a bound absence condition despite
the object still existing. The backend guard refuses the subsequent save, so this
is an incorrect read/condition rather than a demonstrated overwrite or guard bypass.
The body error is injected at the native io.ReadCloser seam; built-in memory
readers do not naturally emit it.

The [retained probe](raw/original-probe.go.txt) fails twice normally and twice
under -race before production edits: [normal](raw/original-before.log),
[race](raw/original-before-race.log). It passes twice in each mode after the fix:
[normal](raw/original-after.log), [race](raw/original-after-race.log).

ReadForSave now uses the existing OpenObject interface and classifies only that
opening result as absence. Once opening succeeds, all body failures return through
the normal error path with no object or usable condition. The existing committed-
marker guard remains on opening errors, preserving the previous correction.

To reuse the identical buffering/closure behavior, GetObject's existing read body
is moved into the private readOpenedObject function and both callers use it. The
production diff adds **ten lines and removes two**, across two existing files.
The helper keeps the same buffer sizing, io.Copy, returned ownership, exact body
errors and deferred close; close errors remain ignored as before. The 32 MiB
initial-allocation clamp is unchanged and is not a total object/read limit.
No extra opening, HEAD, body pass, metadata clone or payload copy is introduced.
Successful fingerprinting, context/authority checks and stream snapshot/lifetime
behavior retain their existing paths. This is a phase correction, not a buffering
optimization or a new memory guarantee.

## Regression and validation evidence

The new suite contains **22 cases**:

- Twelve native defect cases combine raw/wrapped missing body errors, failure
  before any bytes/after one byte/after all three bytes, and data+error returned
  together or on successive reads. They assert exact read error, empty object and
  unbound condition, one GET/close per operation, observed byte counts, unchanged
  usage/revision, refusal of the invalid condition, original identity/properties,
  healthy retry, full byte capacity and stale-condition rejection. Each case also
  verifies that ordinary GetObject keeps its existing body-error contract.
- Four body-error controls cover unexpected EOF, unknown failure, missing-bucket
  error and marked missing. Two successful controls check complete and empty
  objects, valid fingerprints and full-capacity guarded publication.
- Four public embedded-wrapper cases cover raw/wrapped missing and both reader
  error-return forms. They check the existing public missing-error translation,
  no absence condition, closure, invalid-condition refusal and healthy recovery.
  Custom public stores remain unqualified for managed saves; the wrapper tests use
  an internal qualified native memory provider rather than adding that capability.

All final new regression sources were run twice against unchanged production code
before implementation: [baseline](raw/before-tests.log). Sixteen defect cases fail
per repetition; the controls and ordinary GET checks already pass. The 22 new and
29 prior marked-read/absence/access/public-wrapper cases all pass twice afterward:
[after](raw/after-tests.log). The prior marked-error fix is not weakened.

Five focused race repetitions pass **255 cases** (110 new and 145 prior cases)
without skips: [focused race](raw/focused-race.log). Full Go tests, relevant
runtime/storage/S3/run-through/embedded/public race suites, all four conformance
profiles, relevant vet, unchanged quality/file-size ratchets, formatting,
documentation links, generated-output verification, WASM smoke and seven package
integration tests pass. [Full Go](raw/full-go.log), [broad race](raw/broad-race.log),
[conformance summary](raw/conformance-summary.log), [quality](raw/quality.log) and
[package tests](raw/package-tests.log) retain results. Each conformance profile
keeps two existing skips: opt-in live upstream coverage and mirrored bucket
creation awaiting an authority decision. No ratchet baseline is raised.

## Comparative performance and allocation measurements

The original BenchmarkReadForSaveObservation is reused unchanged for complete
three-byte objects, real absence and prepared opening errors. It includes actual
runtime checks, locking, a real memory store, body copying and fingerprinting on
success. Inputs contain one metadata entry and text/plain; setup/error construction
are outside timing. The provider counter verifies one GET per iteration.

BenchmarkOpenedObjectRead measures ordinary GET at three bytes and 64 KiB,
guarded reads at 64 KiB, and ordinary/guarded reads failing after one byte with
ErrObjectNotFound. It uses real memory storage and explicit 1 MiB/one-object caps;
runtime/bucket/object creation and close are outside timing. Faulted readers are
created inside the timed read, and counts confirm one GET/close and one byte per
failed operation. Successful calls include actual body copying and, when guarded,
fingerprinting. This measures complete buffered APIs, not HTTP/disk latency or
peak heap/RSS. The unchanged read buffer remains proportional to returned data.

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64. Identical benchmark sources are compiled
against baseline and candidate. Serial baseline/candidate/candidate/baseline blocks
contain three samples each, first at one processor, then eight: six samples per
version/case/configuration. Tiny successful reads use 250,000 iterations; opening
errors/absence 1,000,000; 64 KiB reads 10,000; body failures 100,000. These are serial
loops even with GOMAXPROCS=8. No agent builds/tests run concurrently; other host
activity is uncontrolled. Every command, sample/outlier, binary digest, B/op and
allocation count is retained in [measurements](raw/measurements.json), raw benchmark
logs and [environment](raw/benchmark-environment.json).

| Case | Processors | Median ns/op before → after | Change | Before range | After range |
|---|---:|---:|---:|---:|---:|
| GET, 3 bytes | 1 | 934.050 → 900.500 | -3.6% | 826.400–1,221.000 | 859.800–1,288.000 |
| GET, 64 KiB | 1 | 9,043.000 → 8,777.500 | -2.9% | 8,523.000–13,011.000 | 8,336.000–9,588.000 |
| GET, partial body error | 1 | 861.500 → 867.350 | +0.7% | 826.100–900.400 | 838.800–920.500 |
| Guarded read, 3 bytes | 1 | 2,549.500 → 2,181.500 | -14.4% | 2,163.000–5,142.000 | 2,153.000–2,216.000 |
| Guarded read, 64 KiB | 1 | 40,091.000 → 40,943.000 | +2.1% | 39,272.000–41,303.000 | 40,297.000–93,447.000 |
| Guarded read, partial body error | 1 | 944.950 → 939.950 | -0.5% | 910.200–1,062.000 | 903.800–1,008.000 |
| Actual lookup absence | 1 | 220.900 → 200.700 | -9.1% | 220.300–264.700 | 196.200–201.500 |
| Prepared wrapped absence | 1 | 152.800 → 137.100 | -10.3% | 151.800–162.700 | 136.300–145.000 |
| Unknown opening error | 1 | 155.500 → 124.300 | -20.1% | 154.500–161.000 | 123.100–164.000 |
| Opening error, marker first | 1 | 163.600 → 145.950 | -10.8% | 162.600–165.200 | 134.700–167.900 |
| Opening error, missing first | 1 | 160.300 → 142.750 | -10.9% | 159.200–161.300 | 138.100–154.300 |
| GET, 3 bytes | 8 | 777.450 → 779.600 | +0.3% | 734.700–788.200 | 760.100–870.300 |
| GET, 64 KiB | 8 | 8,756.000 → 8,848.000 | +1.1% | 8,130.000–9,856.000 | 8,159.000–15,306.000 |
| GET, partial body error | 8 | 782.600 → 800.350 | +2.3% | 748.700–1,323.000 | 757.700–850.400 |
| Guarded read, 3 bytes | 8 | 2,014.000 → 2,018.000 | +0.2% | 2,001.000–2,429.000 | 2,009.000–2,044.000 |
| Guarded read, 64 KiB | 8 | 40,340.500 → 41,277.500 | +2.3% | 39,743.000–41,808.000 | 40,230.000–43,880.000 |
| Guarded read, partial body error | 8 | 962.200 → 867.250 | -9.9% | 833.000–3,415.000 | 850.700–913.700 |
| Actual lookup absence | 8 | 210.800 → 189.450 | -10.1% | 209.300–297.500 | 187.600–202.700 |
| Prepared wrapped absence | 8 | 151.500 → 137.000 | -9.6% | 151.200–157.900 | 135.800–137.200 |
| Unknown opening error | 8 | 155.350 → 123.300 | -20.6% | 154.200–156.300 | 122.900–124.500 |
| Opening error, marker first | 8 | 162.050 → 135.000 | -16.7% | 161.600–163.700 | 134.800–135.300 |
| Opening error, missing first | 8 | 160.150 → 138.600 | -13.5% | 158.500–161.900 | 138.200–140.200 |

Ordinary tiny GET medians change **−3.6% / +0.3% (−33.55 / +2.15 ns)** at one/eight
processors; 64 KiB GET changes **−2.9% / +1.1% (−265.5 / +92 ns)**. Guarded 64 KiB
reads measure **40,091 → 40,943 ns (+2.1%, +852 ns)** and **40,340.5 → 41,277.5 ns
(+2.3%, +937 ns)**. These adverse results are retained, including the candidate
**93,447 ns** outlier and the eight-processor GET sample at **15,306 ns**. Tiny
GET also has a candidate 1,288 ns sample. No successful-read speedup or exact
latency parity is claimed.

Tiny guarded reads measure −14.4% / +0.2%; body-failing GET +0.7% / +2.3%; corrected
body-failing guarded reads −0.5% / −9.9%. Prepared/opening cases measure lower
medians, from −9.1% to −20.6%, in these runs. The structure removes the intermediate
GetObject call from guarded opening failures, but the measurements do not isolate
all timing changes to call overhead. Baseline tiny guarded reads include a 5,142 ns
sample and baseline body-failing guarded reads reach 3,415 ns. All favorable and
adverse samples remain; earlier reviews' separate performance concerns are not
considered resolved and no further optimization is inferred.

Allocation **counts are unchanged**: ordinary successful GET and body failures
report 14; successful guarded reads 19; actual absence one; prepared opening errors
zero. At one processor B/op is identical: tiny GET 1,139; GET/guarded body failure
1,147; 64 KiB GET 66,675; tiny guarded read 1,619; guarded 64 KiB read 67,158–67,159;
actual absence 48; prepared opening errors zero. Eight-processor byte totals vary
and overlap; all individual values/ranges are retained in JSON. In particular,
guarded 64 KiB samples range 69,835–70,196 B before and 69,729–70,773 B after. These
are cumulative allocations, not peak memory, and do not establish byte-for-byte
allocation parity for every run. No payload-sized allocation/copy is added by the
helper extraction, and no buffer optimization is proposed.

Compatibility behavior: a missing sentinel from an opened body's reader now
remains an error instead of manufacturing absence. The condition is empty and
SaveObject refuses it as invalid; reliable opening absence still yields a valid
creation condition. Ordinary GetObject retains its original error/close contract.
The shared helper adds a function call on ordinary successful GET; measured costs
above cover that affected path. No extra provider read or lock hold for I/O is added.

The refreshed packaged WASM grows **4,617,224 → 4,617,893
bytes (+669 bytes)**. [Artifact metadata](raw/wasm.json) records both digests.
Generated-output verification, WASM smoke and package integration tests validate
the rebuilt artifact.

## Next confirmed finding — premature EOF returns a truncated successful object

A separate probe returns EOF after zero or one byte from a reader whose real
metadata reports the existing three-byte object. Both GetObject and ReadForSave
return success with Data shorter than Size; the guarded read also issues a bound
condition derived from incomplete bytes. The reader closes once and the actual
stored object remains intact. This is a separate length/completeness defect: the
existing buffered read accepts successful io.Copy without checking the copied
length against known metadata size. No size/EOF validation is added here.

All four method/length cases fail twice normally and twice under -race on both
baseline and candidate: [source](raw/next-truncated-read-probe.go.txt),
[baseline normal](raw/next-truncated-read-before.log),
[baseline race](raw/next-truncated-read-before-race.log),
[candidate normal](raw/next-truncated-read.log),
[candidate race](raw/next-truncated-read-race.log). No data race is reported.
The baseline replay temporarily restores the two original production files after
all broad tests and package checks finish, then restores the candidate in a
finally block. [Source digests](raw/next-baseline-comparison.json) confirm exact
restoration. This independently confirms the truncation defect predates the
shared-reader extraction.

The backend refuses a save based on that incomplete fingerprint, preserving the
original bytes/version. A subsequent healthy read/save succeeds. The ordinary
GET result still demonstrates truncated data reported as successful. This is not
a demonstrated guard bypass or overwrite, nor a claim that the built-in memory
reader naturally truncates; the probe deliberately supplies an early-EOF reader
over real memory storage and unchanged metadata through existing interfaces.

Reproduce by temporarily copying the source to
internal/runtime/short_read_review_probe_test.go and running only
TestReviewShortObjectReadReportsSuccess with -count=2, optionally -race, then
removing it. Probes are removed after each replay; broad validation ran without
them. The later baseline replay restores the identical validated candidate.
