# Direct abort retry reconciles confirmed upload absence

Baseline `18aab69` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. Previous corrections are
retained. This implements the direct-runtime finding from the
[multipart validation review](../2026-10-15-multipart-validation/README.md).
S3 prevalidation's separate early-return variant is reproduced below and unchanged.

## Original defect and correction

The native fault fixture removes a real memory-backend upload and its four-byte
part, then reports an unmarked error modeling loss of the abort acknowledgement.
The runtime conservatively retains reservations on that uncertain first response.
On direct retry, the actual backend returns authoritative ErrUploadNotFound,
but finishAbort rejects all unmarked non-nil errors. Four bytes, one upload session
and, for an absent target, one object slot remain reserved despite confirmed
absence. Full-capacity PUT and initiation are refused indefinitely on that handle.
The first conservative response is correct; ignoring the later confirmation is not.
This fault model does not claim that built-in memory aborts ordinarily lose replies.

The exact retained probe fails twice normally and twice under the race detector
before production edits: [normal](raw/original-before.log),
[race](raw/original-before-race.log), [source](raw/original-probe.go.txt).
It passes twice in each mode after correction:
[normal](raw/original-after.log), [race](raw/original-after-race.log).

There are two small production changes:

- Native finishAbort now accepts canonical ErrUploadNotFound, including wrapping,
  alongside success and the existing committed marker, then uses removeUpload.
  The caller still returns the original missing error. recordMutation still runs;
  no acknowledgement is fabricated and the first uncertain response stays charged.
- The public MultipartStore abort adapter converts ordinary raw/wrapped public
  ErrObjectNotFound into internal ErrUploadNotFound, matching the existing upload
  lookup and route-validation policy. Unknown errors and committed chains keep
  their exact identity, including a missing cause joined to the committed marker
  in either order. Its successful path avoids error-chain traversal.

No added lookup, storage mutation, scan, lock, interface, dependency, format or
public API is needed. Object bytes and Usage are preserved; only reservations for
the absent upload are released. Existing shared-target reference counts preserve
surviving uploads' byte/object capacity. Once removed from the ledger, repeated
or concurrent retries reject before a second provider abort or cleanup.

Compatibility effect: a direct caller receiving confirmed absence can reuse the
previously stranded capacity without reopening. The missing error still returns
to that caller; it is not turned into success. Custom-store ordinary missing errors
now use the canonical internal upload sentinel, intentionally dropping their
public wrapper in the same way as lookup/validation. Committed/unknown chains are
preserved. Providers must report the missing sentinel only for confirmed absence;
arbitrary uncertain failures remain conservative. No new durability guarantee is
made. ErrNoSuchUpload from route validation can also mean a bucket/key mismatch
and is not treated as an abort's authoritative absence confirmation.

## Regression and validation evidence

The native matrix covers missing/existing target objects with raw missing,
wrapped missing, continued uncertainty and explicit refusal. Refused/uncertain
retries keep actual part bytes and reservations until successful cleanup. A shared
missing target keeps its surviving two-byte part and object slot while releasing
only the absent upload's four bytes/session; it admits another upload to that
same target and releases the final reservation after remaining aborts.

The concurrent regression starts two direct retries together after one uncertain
abort. Both return missing; exactly one reaches the provider, cleanup happens once,
all counters reach zero, and full capacity is reusable. It also fails twice against
the baseline accounting implementation ([original failure](raw/concurrent-before.log)).

Eight adapter cases cover success, raw/wrapped public missing, internal missing,
unknown failure, refusal and both committed/missing join orders. Four public
custom-store cases combine absent/existing target with raw/wrapped absence. The
fixture truthfully checks existence on repeated abort; its lookup is test-only,
not an added production read. Tests assert conservative first-response admission,
absence/error mapping, exact chain preservation, unchanged object bytes/Usage,
no provider work on further repeats, and reusable full byte/object/session capacity.

The finalized pre-implementation matrix's [failure log](raw/before-tests.log)
shows native missing retries, shared-target recovery, public missing translation
and public capacity reuse failing. Refusal/unknown controls pass. The concurrent
red check separately restores only baseline accounting for that native test and
restores the candidate in a finally block before other validation.

All **22 new cases** and **18 previous abort-publication cases** pass twice
normally and five times under -race: **200 focused race cases**
([normal](raw/after-tests.log), [race](raw/focused-race.log)). Full Go tests,
relevant runtime/storage/S3/run-through/embedded/public race suites, all four
conformance profiles ([summary](raw/conformance-summary.log)), relevant vet,
existing quality and file-size ratchets, formatting/diff checks, documentation
links, generated-output verification, WASM smoke and seven package integration
tests pass. Intentionally failing next-finding probes are removed before broad
validation. Existing baselines are not raised.

## Comparative measurements and costs

Identical benchmark sources are compiled against baseline and candidate:

- BenchmarkAbortAccountingLifecycle measures native ledger construction,
  initiation, four-byte part registration and finishAbort with success/committed
  error. This checks existing accounting paths; no backend or payload is created.
- BenchmarkAbortRetryAccountingLifecycle constructs the same fresh ledger,
  records an initial uncertain abort, then calls finishAbort with canonical missing
  or another unknown error. The retained defective baseline ledger is discarded
  after each iteration. Construction is included; this is not a successful full
  recovery loop, store/network latency or an end-to-end abort measurement.
- BenchmarkCustomAbortSuccessLifecycle performs actual public-store initiation,
  upload of the four-byte body and successful runtime abort. One metadata entry
  is used; runtime setup/close are outside timing. This measures the normal
  lifecycle, including the public adapter.

Both accounting models use newUsageState(Options{}) and invoke ledger transitions
directly, bypassing admission checks and option normalization. They measure these
updates rather than qualification of quota admission; the configured-limit tests
above provide that qualification. The reservation updates exercised are the same,
but this microbenchmark must not be read as a complete admitted runtime lifecycle.

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64. Serial baseline/candidate/candidate/
baseline blocks contain three samples each, first at one processor and then eight:
six samples per version/case/processor count. Native samples use 500,000 fixed
iterations, public samples 50,000. No agent builds/tests run concurrently with the
measurements; other host activity is uncontrolled. These benchmarks are serial
even with GOMAXPROCS=8. Commands, all samples and outliers are retained in raw logs,
[measurements](raw/measurements.json) and [binary/host metadata](raw/benchmark-environment.json).

| Benchmark | Processors | Median ns/op before → after | Change | Before range | After range |
|---|---:|---:|---:|---:|---:|
| Accounting, success | 1 | 487.60 → 479.45 | −1.7% | 465.0–526.0 | 472.2–484.2 |
| Accounting, committed | 1 | 494.45 → 496.95 | +0.5% | 484.8–520.0 | 486.1–601.9 |
| Retry accounting, confirmed missing | 1 | 321.25 → 519.25 | **+61.6%** | 306.5–363.7 | 514.2–526.1 |
| Retry accounting, still unknown | 1 | 312.30 → 335.90 | **+7.6%** | 308.7–315.6 | 328.4–437.5 |
| Real successful lifecycle | 1 | 3,154.5 → 3,288.0 | **+4.2%** | 3,146–3,412 | 3,109–3,474 |
| Accounting, success | 8 | 420.35 → 410.10 | −2.4% | 402.2–491.5 | 401.7–440.1 |
| Accounting, committed | 8 | 434.05 → 419.95 | −3.2% | 417.9–495.7 | 414.3–434.8 |
| Retry accounting, confirmed missing | 8 | 268.85 → 450.55 | **+67.6%** | 258.6–449.4 | 437.3–459.4 |
| Retry accounting, still unknown | 8 | 268.15 → 283.35 | **+5.7%** | 265.6–277.7 | 278.7–322.9 |
| Real successful lifecycle | 8 | 2,817.5 → 2,848.0 | **+1.1%** | 2,745–2,891 | 2,724–3,019 |

Confirmed-missing retry accounting changes **352 → 368 B/op and four → five
allocations**: **16 allocated bytes and one allocation**, with about 182–198 ns
added to this ledger lifecycle. It now executes existing target-key construction,
reference counts, map deletion and reservation subtraction that the old code
skipped. Target length can affect byte cost. No payload-sized allocation is added.

Still-unknown retry accounting remains 352 B/op and four allocations. Its measured
median cost increases by 15.2–23.6 ns across the two configurations; two failed
abort attempts now additionally classify their errors as not missing. Deeper
provider error chains can cost more. Existing successful/committed accounting
stays 368 B/op and five allocations.

The real successful lifecycle initially remains **3,752 B/op and 32 allocations**
at one processor and **3,753 B/op and 33 allocations** at eight. Allocation totals
are not peak heap or RSS. Successful timing distributions overlap; adverse medians
and outliers are retained.

After all validation finishes, a reversed candidate/baseline/baseline/candidate
repeat checks successful lifecycle timing, with the same binaries, three samples
per block and 50,000 iterations. One-processor medians are **3,581.5 → 3,907 ns
(+9.1%)**, with ranges 3,450–6,129 before and 3,435–6,017 after. Eight-processor
medians are **3,489 → 3,042 ns (−12.8%)**, with ranges 2,917–4,998 before and
2,967–3,269 after. All repeat samples are retained in raw logs and
[repeat measurements](raw/repeat-measurements.json). Allocations stay 32/3,752 B
at one processor and 33/3,753 B before versus 33/3,753–3,754 B after at eight.

The one-processor median is higher in both runs (+4.2%, +9.1%); the eight-processor
results change direction (+1.1%, −12.8%). Host variability remains substantial;
the measurements do not isolate latency attribution. No speedup, exact parity,
or resolved performance concern is claimed. The public adapter's normal path adds
a nil check, with no error traversal or allocation. Earlier reviews' separate
performance concerns are not considered resolved. No buffering/indexing/checkpoint
optimization is proposed.

The rebuilt packaged WASM remains **4,617,032 bytes**, with a changed digest:
[artifact comparison](raw/wasm.json).

## Next confirmed finding — S3 validation bypasses this recovery path

A separate HTTP probe performs a truthful custom-store abort that removes the
upload/parts but returns an uncertain acknowledgement error: the first DELETE
returns 500. A second DELETE returns the correct 404 / NoSuchUpload from route
validation, so the handler returns before invoking runtime AbortMultipartUpload.
The direct recovery code above is never entered. Four combinations (missing or
existing target, raw or wrapped lookup absence) still strand byte/session capacity
and, for a missing target, its object slot. The probe records one provider abort
entry, verifies actual upload/part data is gone, and demonstrates that full-capacity
PUT and new initiation remain refused.

This reproduces twice normally and twice under -race, without reported data races:
[source](raw/next-s3-abort-retry-probe.go.txt),
[normal](raw/next-s3-abort-retry-probe.log), [race](raw/next-s3-abort-retry-probe-race.log).
The current correction is deliberately limited to direct abort results and their
public adapter; it does not claim HTTP retry reconciliation is solved.

A separate correction must distinguish confirmed absence from a route mismatch,
preserve resource authorization, and avoid releasing retained parts on unavailable
lookups. No implementation of that validation/reconciliation policy is included.
To reproduce, temporarily copy the source to
`pkg/stow/s3_abort_retry_review_probe_test.go`, run only
TestReviewS3AbortRetryAccounting with -count=2, optionally -race, then remove it.
