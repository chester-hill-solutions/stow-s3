# Committed abort releases multipart quota exactly once

Baseline `893e2e4` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. Earlier completed corrections
are retained. This addresses the committed-abort finding from the
[initiation review](../2026-10-15-initiation-publication/README.md). Missing-upload
S3 response adaptation remains a separate finding.

## Original defect and correction

A truthful public custom Store removes its upload and four-byte part, then
returns `ErrMutationCommitted` with a subsequent failure. Runtime `finishAbort`
records that mutation but rejects every non-nil error before releasing its byte,
missing-object and session reservations. The provider has no uploads or part
bytes left, yet a new four-byte PUT and a new upload are refused at four-byte /
one-object / one-session caps. Object Usage correctly stays zero; the stale
reservations prevent capacity reuse.

The original probe reproduces twice normally and twice under the race detector
before production changes: [normal](raw/original-before.log),
[race](raw/original-before-race.log). It now passes twice in each mode:
[normal](raw/original-after.log), [race](raw/original-after-race.log).

The finalized native/public regressions also ran twice before implementation:
[original failures](raw/before-tests.log). Four native committed cases, the native
shared-target case, four public committed cases and the S3 case fail per run;
success and ordinary refusal pass. Native assertions directly show the stale
four-byte reservation, missing-target object slot and outstanding session. Repeated
abort reaches the provider again; shared-target cleanup cannot admit a replacement
session even after the actual upload is gone.

The production diff replaces two lines with one: `finishAbort` now uses the
existing `recordMutation` result. Success and the existing committed marker both
reach the existing `removeUpload`; ordinary errors retain their reservations.
The runtime caller already returns the original error, so no adapter or public
wrapper changes. There is no new helper, interface, API, dependency, format,
scan, storage operation or locking mechanism.

Error chains remain intact, including a missing-upload cause joined with the
committed marker. Known committed cleanup is accounted for without reporting
success. The S3 regression still returns 500 / `InternalError` for the later
failure, while the released quota is usable immediately. This adds no rollback
or durability guarantee and depends on truthful committed outcomes.

Intentional compatibility effect: capacity stranded by a known committed abort
is usable again. Ordinary refusals keep the actual upload and part charged until
successful recovery. Unmarked errors are not interpreted as successful cleanup.
The separate handling of ordinary missing-upload errors is unchanged.

## Regression and validation evidence

Eight native and eight public cases cover absent/existing target objects with
success, committed error, committed error containing a missing cause, and ordinary
refusal. A ninth native case has multiple uploads sharing one missing target:
removing one releases only its bytes/session, keeps the surviving part and shared
object reservation, admits a replacement session, and releases the final
reservation only after all uploads are gone. Another case enters through S3.

Tests verify error preservation, actual upload/part removal or retention, unchanged
object bytes and Usage, exact native reservation totals, reuse of full byte/object/
session capacity, and recovery after ordinary refusal. Repeated abort is rejected
before another provider abort call, avoiding a second reservation release. Public fixtures
reuse the existing multipart operations with distinct upload IDs and real part
bytes. The native fixture wraps the real memory backend.

All eighteen cases pass twice normally and five times under `-race`
(**90 focused race cases**). Full Go tests, relevant runtime/storage/S3/run-through/
embedded/public-API race suites, all four conformance profiles, relevant vet,
existing quality/file-size ratchets, formatting/diff checks, generated-output
verification, WASM smoke and seven package integration tests pass. Actual logs are
in `raw`; the [conformance summary](raw/conformance-summary.log) records each profile.
Intentionally failing next-finding probes are removed before the broad suites.

## Comparative measurements and new costs

Two complementary benchmarks use identical source before and after:

- `BenchmarkCustomAbortSuccessLifecycle` performs real public custom-store
  initiation, upload of `part` (four bytes), and successful runtime abort. One
  metadata entry and the same test helper are used in both versions. Runtime
  setup and close are outside timing. This measures the normal lifecycle.
- `BenchmarkAbortAccountingLifecycle` constructs a fresh native accounting state,
  registers a missing-target upload and four part bytes, then calls `finishAbort`
  with success or a fixed committed error. Construction is included in timing.
  It has no backing store, payload allocation or I/O and is not an end-to-end abort
  measurement. A fresh state gives each iteration the same valid starting state;
  the defective committed-error state is discarded after the iteration. This
  isolates the cost of executing the previously skipped cleanup without inventing
  a working baseline recovery loop.

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64. Serial baseline/candidate/candidate/
baseline blocks contain three samples, first with one processor and then eight:
six samples per version/case/processor count. Each accounting sample has 500,000
iterations; each real lifecycle sample has 50,000. No agent tests or builds run
concurrently with measurements. Other host activity is uncontrolled. All samples,
commands and binary hashes are retained in [measurements](raw/measurements.json)
and [environment metadata](raw/benchmark-environment.json).

| Benchmark | Processors | Median ns/op before → after | Timing change | Allocations before → after |
|---|---:|---:|---:|---:|
| Accounting, success | 1 | 483.85 → 489.85 | +1.2% | 5 → 5 |
| Accounting, committed | 1 | 322.40 → 500.75 | **+55.3%** | 4 → 5 |
| Real successful lifecycle | 1 | 3,212 → 3,278.5 | +2.1% | 32 → 32 |
| Accounting, success | 8 | 410.15 → 419.60 | +2.3% | 5 → 5 |
| Accounting, committed | 8 | 254.75 → 424.30 | **+66.6%** | 4 → 5 |
| Real successful lifecycle | 8 | 2,784 → 3,018.5 | **+8.4%** | 33 → 33 |

One-processor accounting-success ranges are 470.7–491.1 ns before and
470.7–1,519.0 ns after; committed ranges are 311.6–339.0 ns before and
490.1–576.6 ns after. Real-lifecycle ranges are 3.185–3.341 µs before and
3.156–5.083 µs after. Eight-processor accounting-success ranges are 401.3–421.7 ns
before and 405.0–468.9 ns after; committed ranges are 251.9–269.0 ns before and
415.7–456.7 ns after. Real-lifecycle ranges are 2.702–3.655 µs before and
2.776–4.170 µs after. Successful timing distributions overlap. Adverse medians
and outliers are retained; no speedup or exact latency-parity claim is made.

After validation finishes, the reversed-order eight-processor repeat yields
accounting success **450.25 → 455.35 ns (+1.1%)**, committed accounting
**293.10 → 477.10 ns (+62.8%, +184 ns)**, and real successful lifecycle
**3.321 → 3.149 µs (−5.2%)**. Accounting-success ranges are 442.8–473.7 ns before
and 441.0–518.7 ns after; committed ranges are 281.4–306.9 ns before and
454.9–604.9 ns after. Real-lifecycle ranges are 3.005–3.820 µs before and
3.000–3.291 µs after. Accounting bytes/counts remain 368/five on success and
352/four → 368/five on committed errors; the real lifecycle remains 3,753 B/op and
33 allocations in both. The initial +8.4% and repeat −5.2% real-lifecycle medians
are both retained. Their direction differs, so no attributable latency change is
established for successful operations. The additional committed-accounting work
is consistently measured and reported.

Accounting success remains **368 B/op, five allocations**. Committed accounting
changes **352 → 368 B/op, four → five allocations**: **16 additional allocated
bytes and one allocation**, with about **170–185 ns** added to the measured ledger
lifecycle. The error path now performs the existing target-key construction,
reference-count updates, map deletions and reservation subtraction. Target lengths
can affect byte cost. No payload-sized allocation is added.

The real successful lifecycle remains 3,752 B/op and 32 allocations at one
processor. At eight, it is 3,753 B/op before and 3,753–3,754 after, with 33
allocations in both. These are cumulative allocation totals, not peak heap or RSS.
The accounting benchmark's additional work corrects the retained-capacity defect;
it must not be extrapolated to network/store latency or a full committed-error
lifecycle, which the old runtime cannot complete repeatedly at the configured cap.
Earlier reviews' separate timing concerns are not considered resolved by this
abort comparison.

No buffering optimization, filesystem indexing, incremental checkpointing or new
storage abstraction is proposed. The rebuilt packaged WASM remains
**4,617,032 bytes**, with a changed digest: [artifact comparison](raw/wasm.json).

## Next confirmed S3 finding — missing upload IDs become server errors

The custom-store validation adapter directly returns public `ErrObjectNotFound`
from `GetMultipartUpload`. It bypasses the existing public-to-internal
missing-upload conversion, so S3 route validation responds 500 / `InternalError`
for an upload that was successfully aborted. The existing conformance corpus
requires 404 / `NoSuchUpload` for aborted/repeated multipart operations.

A separate probe exercises UploadPart, CompleteMultipartUpload,
AbortMultipartUpload and ListParts after successful abort, with raw and wrapped
public missing errors: **eight combinations fail**, twice normally and twice
under `-race`, without reported data races. The probe deliberately uses an
ordinary successful abort and isolates validation behavior from committed errors.
[Source](raw/next-missing-upload-probe.go.txt),
[normal](raw/next-missing-upload-probe.log),
[race](raw/next-missing-upload-probe-race.log).

Copy the text temporarily to `pkg/stow/missing_upload_review_probe_test.go`, run
only `TestReviewCustomMissingUploadS3Status` with `-count=2`, optionally `-race`,
then remove it. It reuses this change's public fixture and the existing S3 adapter.
The validation implementation remains unchanged in this abort-accounting fix.
