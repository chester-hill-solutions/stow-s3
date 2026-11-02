# Runtime COPY adapters preserve committed identity

Baseline `b531f15` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. This corrects the two remaining
internal-adapter variants of **F3** in the
[codebase review](../../reviews/2026-10-15-codebase-map.md). The public-runtime
variant was corrected with F2; it is not reimplemented here.

## Demonstrated defect

On a real workspace, write a source and obstruct `.stow/index.json` before COPY.
The destination publishes successfully. The workspace returns known destination
metadata plus `ErrMutationCommitted`, and the runtime correctly charges eight
bytes/two objects. Both `StoreAdapter.CopyObject` and `CopyObjectCond` then discard
that metadata on any error. Their caller receives nil instead of the exact known
version, ETag, checksums and properties. This is demonstrated identity loss;
these direct adapter cases did not lose payload bytes or miscount usage.

The new regression failed six publication cases per run, repeated twice before
implementation: [original failures](raw/before-tests.log). It covers plain COPY,
matching ETag conditions and date-only conditions, each to a new destination and
an existing destination. Three ordinary-refusal cases already passed.
The original review independently reproduced both adapter variants twice normally
and twice under the race detector, with the same real index obstruction.

## Four-line correction and contracts

Both adapters now use the existing PUT-adapter rule: preserve known metadata with
a committed error, and return nil for ordinary failures. They return the original
error. There is no new interface, dependency, format, API, helper or production
comment. Backend COPY, condition evaluation, runtime admission/accounting and S3
handlers are unchanged. No additional GET, HEAD, PUT, lock or payload buffer is
introduced. In particular, this does not revive generic GET→PUT snapshot emulation.

Nine regression cases verify the committed result against the stored destination,
including version ID, ETag, size, content type, user metadata, modification time
and CRC32 checksum. They check payload, usage, quota refusal and independent
ownership of the returned metadata map. After repairing the obstruction, successful
close and reopen retain the failed call's known identity. A subsequent COPY then
succeeds and returns its own publication metadata. Ordinary missing-source,
ETag-mismatch and date-only refusal still return nil metadata, preserve the old
destination and leave usage unchanged.

The nine cases pass twice normally and five times under the race detector
(**45 race cases**). All six original map probes now pass twice normally and twice
under the race detector: [normal](raw/original-probes-after.log),
[race](raw/original-probes-after-race.log). All four findings from that original
focused review are now corrected locally; this is not an exhaustive correctness
claim for the codebase.

The committed error remains an error. Existing S3 COPY handling checks it before
writing success, retaining its existing failure response. Returning identity does
not add rollback, atomic content/index persistence or crash-safe metadata recovery.
After an index obstruction, the live handle can persist intended metadata on a
successful close after repair; loss of that handle still has the documented
metadata-recovery limitations. Callers must verify a committed failure before retry.

## Comparative costs

`BenchmarkStoreAdapterCopyPublication` measures both adapter methods over real
immutable memory COPY with a 1 MiB source and one user-metadata entry. The benchmark
store optionally returns a fixed committed error **after** a real successful local
copy, allowing the original and corrected paths to perform the same mutation.
The original benchmark does not require non-nil metadata on committed errors,
because that missing result is precisely the defect. Setup and close are outside
timing. Memory COPY shares immutable owned payloads; these are runtime/metadata
costs, not a filesystem throughput measurement.

Standalone binaries used the identical harness, Go 1.25.6, Apple M1 Pro,
macOS 14.5 ARM64 and eight Go processors. Serial blocks were baseline/candidate/
candidate/baseline, with three one-second samples per block: six samples per
version and case. No agent builds or tests ran concurrently; other host activity
was not controlled.

| Case | Median before → after | Bytes/op before → after | Allocs/op before → after |
|---|---:|---:|---:|
| Plain success | 2.148 → 2.347 µs (+9.3%) | 3,408 → 3,408 | 25 → 25 |
| Conditional success | 2.503 → 2.343 µs (−6.4%) | 3,424 → 3,424 | 26 → 26 |
| Plain committed error | 2.093 → 2.292 µs (+9.5%) | 2,912 → 3,408 | 22 → 25 |
| Conditional committed error | 2.414 → 2.279 µs (−5.6%) | 2,928 → 3,424 | 23 → 26 |

Timing ranges overlap in every case. Plain-success ranges are 2.124–2.478 µs
before and 2.196–3.810 µs after; conditional-success ranges are 2.263–2.959 µs
before and 2.244–3.790 µs after. These samples establish neither exact latency
parity nor a speedup. [Every sample and binary hash](raw/measurements.json) and
raw benchmark blocks are retained, including outliers.

Successful COPY has unchanged allocation counts and bytes. A committed error now
costs **496 additional bytes and three allocations per call** with this metadata
fixture: the existing conversion returns an ObjectMeta and clones its map instead
of discarding the result. This is the same conversion already used on success;
no separate optimization is proposed. Non-nil errors also incur a marker check.
Other metadata sizes may have different allocation-byte costs. There is no new
storage operation or payload-size allocation. The freshly rebuilt WASM remains
4,616,726 bytes, with a new [recorded hash](raw/wasm.json).

## Validation and next finding

Full Go tests, relevant runtime/storage/S3/run-through/embedded/public-API race
suites and all four conformance profiles pass. The S3 suite includes existing
COPY source-condition, source-version and backend payload-I/O cost contracts.
Relevant vet, Go quality/file-size ratchets, formatting and diff checks pass.
Generated-output verification, WASM smoke and seven package integration tests
pass. Evidence is retained in `raw`.

Next confirmed finding: the **buffered custom-store PUT adapter also discards a
known committed object**. A supported public Store really publishes `body`, then
returns the object with the committed marker. The outer public PUT returns an
empty identity, while payload and usage correctly show four bytes/one object.
This reproduced twice normally: [probe](raw/next-buffered-put-probe.go.txt),
[output](raw/next-buffered-put-probe.log). It is not a reproduced panic and has
not yet been qualified under the race detector. To rerun, copy the probe into
`pkg/stow/put_review_probe_test.go` and run only
`TestReviewBufferedPutPreservesCommittedIdentity`; it uses the existing
[custom-store test fixture](../../../pkg/stow/store_copy_publication_test.go).
Remove that temporary file afterward. The failing probe is retained as text to
keep the normal suite green. This neighboring result-preservation change and
custom multipart adapters remain outside this correction.
