# Missing custom-store uploads retain the S3 error contract

Baseline `8f35607` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. This follows the separate
[committed-abort correction](../2026-10-15-abort-publication/README.md), preserving
all previous corrections. It implements only that report's missing-upload finding.

## Demonstrated defect and smallest correction

After a successful abort, a public custom MultipartStore returns public
`ErrObjectNotFound` from upload lookup. The validation adapter returned it directly,
while its sibling GetMultipartUpload already translates it to internal
`storage.ErrUploadNotFound`. The S3 route validator understands the internal error,
so the mismatch produced **500 / InternalError instead of 404 / NoSuchUpload**.
The existing conformance corpus requires the latter for aborted multipart uploads.

Before production modification, permanent regressions reproduce this across
UploadPart, CompleteMultipartUpload, AbortMultipartUpload and ListParts, with both
raw and wrapped missing errors: eight HTTP combinations. Two direct adapter cases
also fail, while the other seven direct cases pass. Each runs twice normally and
twice under the race detector: [normal](raw/before-tests.log),
[race](raw/before-race.log). These are the same HTTP scenarios retained as a probe
in the preceding report, now permanent regression tests.

The production change adds three lines to ValidateMultipartUpload's error branch,
matching the existing GetMultipartUpload conversion. It handles wrapped missing
errors, preserves unknown failures and committed error chains exactly, and leaves
bucket/key mismatch behavior unchanged. A committed marker joined to a missing
cause is preserved in either order. Successful validation still performs one
provider lookup and compares the two routing fields directly. It does not call
the allocating metadata-conversion method or clone the upload's metadata.

Compatibility effect: ordinary absent uploads now receive the established S3
404 / NoSuchUpload response through the public custom-store path. No storage
mutation, extra lookup, interface, dependency, format, public API, new abstraction,
locking change or payload buffering is introduced. Unknown/committed lookup
failures are not reclassified as absence. Custom stores must still return the
public missing sentinel for confirmed absence; arbitrary provider errors remain
errors. This does not make validation and the following mutation atomic.

## Validation

Seventeen regression cases pass twice normally and five times under the race
detector (**85 focused race cases**). Direct tests cover successful lookup,
bucket/key mismatch, raw/wrapped public missing, internal missing, unknown error,
and both joined committed/missing orderings. They check exact error preservation,
one provider lookup and unchanged metadata. HTTP tests use real initiation, part
upload and successful abort through the existing public custom-store fixture,
then submit each of the four missing-upload requests.

[Passing regressions](raw/after-tests.log), [focused races](raw/focused-race.log),
[full Go tests](raw/full-go.log), [relevant race suites](raw/broad-race.log), all
four memory/filesystem/runtime conformance profiles
([summary](raw/conformance-summary.log)), relevant vet, quality/file-size ratchets,
formatting and diff checks, generated-output verification, WASM smoke and seven
package integration tests pass. The intentionally failing next-finding probe is
removed before broad validation. Detailed logs are retained in `raw`.

## Comparative costs

The identical BenchmarkMultipartValidation source is compiled against the
baseline and candidate. It calls the real private validation adapter over a
lookup-only fixture; provider lookup increments a counter and returns a stored
upload/error. Setup and metadata creation are outside timing. Successful cases
contain zero, one or 512 metadata entries; the missing case wraps the public
sentinel once. This isolates adapter overhead, not HTTP/store/network latency or
runtime authorization/locking. It is serial even when GOMAXPROCS is eight.

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64. Serial baseline/candidate/candidate/
baseline blocks, each with three samples at 10,000,000 fixed iterations, give six
samples per version/case/processor count. No agent builds or tests run concurrently
with measurements; other host activity is uncontrolled. All samples and commands,
including outliers, are retained in raw benchmark logs and
[measurements](raw/measurements.json); binary digests and host/toolchain metadata
are in [environment](raw/benchmark-environment.json).

| Case | Processors | Median ns/op before → after | Change | Before range | After range |
|---|---:|---:|---:|---:|---:|
| Success, zero metadata | 1 | 11.870 → 11.515 | −3.0% | 11.68–12.03 | 11.31–11.98 |
| Success, one entry | 1 | 12.015 → 11.395 | −5.2% | 11.73–12.26 | 11.26–11.50 |
| Success, 512 entries | 1 | 11.995 → 11.430 | −4.7% | 11.76–12.40 | 11.29–11.56 |
| Wrapped missing | 1 | 12.305 → 28.065 | **+128.1%** | 11.79–16.54 | 27.26–30.09 |
| Success, zero metadata | 8 | 11.895 → 11.465 | −3.6% | 11.78–15.85 | 11.23–11.65 |
| Success, one entry | 8 | 12.015 → 11.410 | −5.0% | 11.86–12.17 | 11.26–11.81 |
| Success, 512 entries | 8 | 12.070 → 11.445 | −5.2% | 11.74–12.31 | 11.24–11.96 |
| Wrapped missing | 8 | 11.995 → 27.745 | **+131.3%** | 11.76–12.37 | 27.43–28.19 |

**Every sample in every case remains 0 B/op and 0 allocations/op.** This is
cumulative allocation evidence, not peak RSS or a whole-object buffering study.
Successful timing differences are sub-nanosecond; no speedup or exact latency
parity is claimed. The error path now costs approximately **16 ns more** (about
2.3 times its former adapter-only time) to traverse the error chain and classify
absence while respecting the committed marker. Deeper custom error chains can
cost more. No payload-sized work is added. Earlier reviews' separate performance
concerns are not considered resolved by this adapter comparison.

The packaged WASM is rebuilt and verified: it remains **4,617,032 bytes**, with
an updated digest ([comparison](raw/wasm.json)). No optimization is proposed.

## Next recovery finding — confirmed absence does not release an uncertain abort

A separate native fault probe models an abort whose confirmation is lost: the
memory backend removes the upload/part, but a wrapper returns an ordinary error
without a committed marker because the caller has no confirmation. Retaining the
reservation on that first uncertain reply is conservative. A retry then returns
an authoritative internal ErrUploadNotFound from the actual memory backend.
Despite that new confirmation, finishAbort retains the old four-byte reservation,
upload session and, for a missing target, object slot. Further full-capacity PUT
and initiation are refused at the configured limits.

Both missing and existing targets reproduce twice normally and twice under the
race detector, without reported data races: [probe source](raw/next-abort-retry-probe.go.txt),
[normal](raw/next-abort-retry-probe.log), [race](raw/next-abort-retry-probe-race.log).
The probe reuses the native abort fault fixture, deliberately supplies an unmarked
unknown-outcome error first, and verifies backend absence before retrying. This
is a fault-injected recovery scenario, not evidence that a built-in memory abort
normally loses its acknowledgement. Known committed outcomes remain governed by
the preceding correction and must still carry their marker.

The retry/reconciliation policy needs its own contract review and regression
coverage for ordinary refusal, unavailable lookup, retained parts and shared
target reservations before any correction. Its implementation is unchanged here.
Reproduce by temporarily copying the text to
`internal/runtime/abort_retry_review_probe_test.go`, running only
`TestReviewUnknownAbortRetry` with `-count=2`, optionally `-race`, then removing it.
