# Buffered custom-store PUT preserves committed identity

Baseline `7172a99` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. This corrects the buffered PUT
finding confirmed by the preceding
[COPY-adapter review](../2026-10-15-copy-adapter-publication/README.md).
The earlier COPY, multipart-workspace and batch-deletion corrections are retained.

## Demonstrated defect

A supported caller-supplied base Store really publishes `body`, then returns the
known object with `ErrMutationCommitted` and a later failure. Its buffered PUT
adapter discards that object on every error. Runtime accounting uses the declared
write size, so usage correctly reports four bytes/one object; the public caller
receives an empty identity despite the stored object being readable. This was
identity loss, not a reproduced panic or payload loss on this PUT path.

Four of eight regression cases failed per baseline run, repeated twice before
implementation: [original failures](raw/before-tests.log). Both new-object and
overwrite cases fail with a generic committed error and with a committed error
containing public `ErrObjectNotFound`. Success and ordinary refusal cases passed.
The original probe from the preceding review also reproduced the missing identity
twice before this fix; it now passes twice normally and twice with `-race`.

## Two-line correction and preserved contracts

The buffered adapter now retains known metadata on a committed error and returns
it with the translated original error. Ordinary failures still return nil metadata.
This matches the existing streamed PUT and corrected COPY behavior. The public
wrapper and runtime accounting already preserve committed results; they need no
change. Error translation still prioritizes the committed marker over a joined
missing-object cause.

There is no new interface, dependency, format, API, storage abstraction or helper
in production. The existing `io.ReadAll` path and buffering limits are unchanged.
No new storage operation, lock, GET, HEAD, PUT or payload buffer is introduced by
the correction. Tests reuse the existing base-Store fault-injection fixture.

Eight public-API cases cover new destinations and overwrites, each with success,
generic committed error, committed missing-object error and ordinary refusal.
They verify stored payload, exact returned public identity, content type, metadata,
modification time, ETag and size; error markers and causes; usage; and independent
ownership of the returned metadata map. A five-byte overwrite is refused at a
four-byte cap before another store PUT. Deleting the published destination releases
capacity for a new four-byte object. A refused write preserves the old object or
its absence and leaves usage unchanged. The adapter still performs one store PUT,
without a fallback GET or COPY.

All eight cases pass twice normally and five times under the race detector
(**40 race cases**). The original probe passes normally and under the race detector:
[normal](raw/original-probe-after.log), [race](raw/original-probe-after-race.log).
The returned object remains metadata-only, consistent with existing PUT success;
the payload is retrieved through GET. Callers still receive an error and must
verify a committed outcome before retrying. This adds no rollback or durability
guarantee and does not infer publication from an ordinary failure.

## Comparative costs

`BenchmarkCustomStoreBufferedPutPublication` exercises public PUT through the base
Store's buffered adapter over a real memory runtime, with a 4 KiB or 1 MiB body,
content type and one user-metadata entry. For committed-error cases, the fixture
returns its fixed error only after the underlying PUT succeeds. Setup, body
creation and close are outside timing. Repeated destination overwrites are timed.
The original benchmark intentionally tolerates the empty committed result so both
versions perform the same storage operation.

Standalone binaries use the identical harness, Go 1.25.6, Apple M1 Pro,
macOS 14.5 ARM64 and eight Go processors. Serial blocks were baseline/candidate/
candidate/baseline, three one-second samples each: six samples per version and
case. No agent tests or builds ran concurrently; other host activity was not
controlled. The measurements are highly variable, with overlapping timing ranges
in every case. They support no speedup or exact latency-parity claim.

| Case | Median before → after | Allocation count before → after |
|---|---:|---:|
| 4 KiB success | 36.093 → 20.837 µs (−42.3%) | 69 → 69 |
| 4 KiB committed error | 24.323 → 22.409 µs (−7.9%) | 62 → 69 |
| 1 MiB success | 4.006 → 3.056 ms (−23.7%) | 92–93 → 92–93 |
| 1 MiB committed error | 3.292 → 3.508 ms (+6.6%) | 85–86 → 92–93 |

Success timing ranges are 24.458–49.047 µs before versus 17.910–27.182 µs after
at 4 KiB, and 2.585–5.001 ms before versus 2.559–3.545 ms after at 1 MiB.
The 1 MiB committed-error samples range from 2.368–22.254 ms before and
2.866–7.396 ms after, including a large baseline outlier. Every sample and binary
hash is retained in [measurements](raw/measurements.json) and the raw benchmark logs.
These host/GC-sensitive samples cannot attribute the observed lower success
medians to this correction.

Successful-write allocations are unchanged: about 30,190 B/op at 4 KiB and
6,307,000 B/op at 1 MiB. Committed errors now execute existing metadata conversions
instead of returning an empty object. At 4 KiB that is **29,021 → 30,189–30,190 B/op**,
about **1,168 additional bytes and seven allocations** with this one-entry metadata
fixture. The 1 MiB allocation-count ranges also shift by seven; allocated-byte
ranges are 6,305,715–6,305,955 before and 6,306,980–6,307,101 after. Those numbers
are cumulative allocation per operation, not peak heap or RSS. Metadata size can
change the byte cost. Non-nil errors also incur a committed-marker check.
No new payload-sized allocation is introduced. No buffering optimization is proposed.

The freshly rebuilt packaged WASM grows from 4,616,726 to 4,617,032 bytes
(**306 bytes**). Both [artifact hashes](raw/wasm.json) are retained.

## Validation and next finding

Full Go tests, relevant runtime/storage/S3/run-through/embedded/public-API race
suites and all four conformance profiles pass. Relevant vet, Go quality/file-size
ratchets, formatting and diff checks pass. Generated-output verification, WASM
smoke and seven package integration tests pass. Logs are in `raw`.

Next confirmed high-priority finding: **the public custom multipart completion
adapter discards a committed result and triggers a runtime accounting panic**.
A single-part fault-injection fixture, built from existing public-store test
fixtures, really publishes `body`, retains the upload, then returns the known object
with a committed error. `multipartAdapter.CompleteMultipartUpload` drops its
metadata. Runtime dereferences nil while accounting: bytes remain readable, but
usage stays at zero bytes/zero objects. This is distinct from the already corrected
workspace multipart implementation.

It reproduces twice normally and twice under `-race`, without a reported data race:
[probe](raw/next-multipart-probe.go.txt), [normal](raw/next-multipart-probe.log),
[race](raw/next-multipart-probe-race.log). The probe exercises one valid single-part
completion and a synthetic post-publication failure; it is not a new backend or
full qualification of arbitrary custom multipart behavior. To rerun, copy it to
`pkg/stow/multipart_review_probe_test.go` and run only
`TestReviewCustomMultipartCompletionPreservesCommittedResult`. It reuses the existing
`multipartStore` and `memoryStore` test fixtures and enters the native runtime
through the existing public custom-store adapter. Remove the temporary test
afterward. It remains text so the ordinary suite stays green. The multipart
implementation is deliberately unchanged in this independently reviewable fix.
