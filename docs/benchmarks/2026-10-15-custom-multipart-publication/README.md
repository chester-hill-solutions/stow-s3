# Custom multipart completion preserves committed results and cleanup accounting

Baseline `26262a5` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. Earlier completed corrections
are retained. This addresses the custom multipart completion finding from the
[buffered PUT review](../2026-10-15-buffered-put-publication/README.md), separately
from the previously corrected workspace multipart backend.

## Reproduction and correction

A supported public custom Store publishes the actual object, then returns its
known identity with `ErrMutationCommitted` and a later failure. The multipart
adapter discarded that identity on every error. Runtime accounting dereferenced
nil metadata: the four-byte object was readable, but usage remained zero bytes /
zero objects, or three bytes / one object when overwriting `old`.

The deterministic regression ran twice before changing production. Eight
committed-outcome cases per run panic after publication; the HTTP case also
panics. The baseline also exposes a distinct missing-upload sentinel mismatch:
[original output](raw/before-tests.log). The final regression adds a conservative
lookup-error case beyond that initial matrix.

Preserving the completion result alone stops the panic but exposes the cleanup
accounting issue. When the custom store has removed the upload, its public
`ErrObjectNotFound` is not the internal `ErrUploadNotFound` expected by runtime
reconciliation. Removed parts remain charged and an eight-byte overwrite is
incorrectly refused at an eight-byte cap:
[isolated output with the finalized harness](raw/metadata-only-final-harness.log).
That isolation temporarily withholds only the missing-error mapping. Restoring
the exact implementation makes the same cases pass:
[final focused output](raw/final-focused.log). Earlier intermediate development
output is also retained in `raw`.

The production diff is five added lines and two removed lines in the existing
adapter. Completion now returns known metadata alongside a committed error;
ordinary failures still return nil metadata. Missing-upload lookup maps raw or
wrapped ordinary public `ErrObjectNotFound` to internal `ErrUploadNotFound`.
A lookup error carrying the committed marker retains its original chain and is
not treated as confirmation of cleanup. No runtime change is required.

Existing runtime accounting can now charge the published object, retain parts
and the upload slot when cleanup is unconfirmed, and release them when the store
confirms removal. Completion errors remain errors; the HTTP regression returns
500 / `InternalError` without panicking. A committed result does not add rollback
or durability guarantees. Public custom stores remain responsible for reporting
truthful metadata and the committed outcome.

## Regression coverage and validation

Fourteen native cases cover new objects and overwrites, each with success,
retained upload, removed upload, wrapped missing-upload error, unavailable lookup,
lookup with joined missing/committed markers, and ordinary refusal. A fifteenth
case enters through the S3 handler. The fixture reuses the existing public
multipart/base-store test fixtures and implements one valid single-part
publication with the existing `CompletionETag` convention.

Tests verify actual payload, returned identity, ETag, size, modification time,
content type, initiation metadata and independent ownership of the returned
metadata map. They prove that retained parts continue consuming byte/session
quota; confirmed cleanup releases it; refusal preserves the previous object or
its absence; and retry/abort recovery leaves correct accounting. Success performs
no provider upload lookup; committed errors perform the runtime's existing one
lookup. Internal version/checksum fields have no public equivalent and are not
invented by the adapter.

All fifteen cases pass twice normally and five times under the race detector
(**75 race cases**). The original probe from the preceding review passes twice
normally and twice under `-race`. The next finding's intentionally failing probe
was removed before the ordinary suites.

The full Go suite, relevant runtime/storage/S3/run-through/embedded/public-API race
suites, and all four conformance profiles pass. Relevant vet, Go quality and
file-size ratchets, formatting/diff checks, generated-output verification, WASM
smoke and seven package integration tests pass. Logs are retained in `raw`;
[conformance summary](raw/conformance-summary.log) records all four profiles.

## Comparative cost evidence

`BenchmarkCustomMultipartCompletionLifecycle` measures the same successful 4 KiB
initiate/upload/complete lifecycle before and after. It uses the public custom
Store adapter, native runtime, real stored bytes, one metadata entry and repeated
destination overwrites. Setup, body creation and close are outside timing. The
benchmark function and fixture are identical in both binaries; regression-only
assertions were refined between builds. Tests are disabled during measurement.
The original committed-error path panics, so it has no comparable successful
completion latency baseline.

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64. Baseline/candidate/candidate/baseline
blocks contain three one-second samples each at the default eight processors.
A preliminary run overlapped later test compilation; all its `pilot-*` samples
are retained but excluded from the comparisons. The clean runs have no concurrent
agent tests or builds. Other host activity is uncontrolled.

| Comparison | Median before → after | Timing change | Allocations before → after |
|---|---:|---:|---:|
| Default eight processors, one-second samples | 35.577 → 73.184 µs | **+105.7% (2.06×)** | 59 → 59 |
| Fixed 50,000 operations, one processor | 18.991 → 19.164 µs | +0.9% | 59 → 59 |
| Fixed 50,000 operations, eight processors | 28.163 → 20.901 µs | −25.8% | 59 → 59 |

The initial clean eight-processor timing ranges are 18.316–178.501 µs before and
19.736–171.618 µs after, with strong variation within each version. Those medians
alone cannot establish either parity or an attributable regression. The fixed
workload repeats use candidate/baseline/baseline/candidate order, three samples
per block. At one processor, ranges are 17.948–20.946 µs before and
17.364–19.600 µs after. These overlapping observations support no speedup claim.

A final fixed-workload repeat at the original eight-processor setting, after all
suites finished, reverses the direction: 28.163 → 20.901 µs (−25.8%). Ranges are
20.970–32.374 µs before and 18.780–39.918 µs after. Allocated bytes range from
51,906–51,907 before and 51,906–51,908 after, with 59 allocations throughout.
This does not reproduce the initial 2.06× median slowdown; the direction reversal
and overlapping distributions preclude attributing a latency change to the fix.
Neither the slower nor faster measurement set is omitted.

Successful allocation counts remain 59 in every clean sample. At one processor,
both versions allocate exactly 51,882 B/op; at eight processors, the initial
ranges are 51,904–51,908 before and 51,903–51,908 after. These are cumulative
allocation totals, not peak heap or RSS. All samples, commands, environment and
binary hashes are retained in [measurements](raw/measurements.json) and
[environment metadata](raw/benchmark-environment.json), alongside raw logs.

The newly working committed-error path converts and clones the known metadata
and reaches the runtime's existing reconciliation lookup. Those operations were
previously bypassed by the panic and have a cost. Metadata size affects that
allocation cost; no additional payload-sized copy is introduced by this fix.
Non-nil errors incur marker checks. Success adds no storage operation, lock,
lookup or payload buffer. No resource-limit relaxation, optimization, indexing,
checkpointing, dependency, storage format, abstraction or public API is added.

The rebuilt packaged WASM remains **4,617,032 bytes**, with a changed digest:
[before/after artifact hashes](raw/wasm.json).

## Next confirmed finding — committed parts escape byte quota

The public custom part-upload adapter discards a known Part on a committed error.
Runtime `finishPart` also rejects all errors when updating part reservations.
An existing public multipart fixture really retains `part` (four bytes) and
returns its Part with a committed marker. Runtime returns nil Part and the marker.
A subsequent four-byte PUT to the same target succeeds at `MaxBytes: 4`, leaving
four object bytes plus four retained part bytes: **eight actual bytes under a
four-byte cap**, with reported object usage four bytes / one object.

It reproduces twice normally and twice under `-race`, without a reported data
race: [probe](raw/next-part-probe.go.txt), [normal](raw/next-part-probe.log),
[race](raw/next-part-probe-race.log). Copy the text temporarily to
`pkg/stow/part_review_probe_test.go` and run only
`TestReviewCustomCommittedPartQuota` with `-count=2` (optionally `-race`), then
remove it. It reuses this change's single-part fixture, enters through the public
custom Store seam, and requires neither an external writer nor a race. The
part-upload implementation is deliberately unchanged in this completion fix.
