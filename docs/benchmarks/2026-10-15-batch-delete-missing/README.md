# Custom-store batch deletion confirms missing keys

Baseline `524ca63` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. This corrects **F4** from the
[codebase review](../../reviews/2026-10-15-codebase-map.md). F1/F2 corrections are
retained. No storage optimization, dependency, format, interface or public API
is added.

## Original defect

A caller-supplied store correctly returns public `ErrObjectNotFound` for an
absent key. The batch adapter checks the distinct internal sentinel before
translating the public error, stops immediately and never deletes later keys.
S3 returns `404 NoSuchKey` for a batch beginning with a missing key instead of
confirming that key and proceeding. This violates the existing internal Store
contract and documented idempotent S3 deletion.

The new regression reproduced the failure twice before production changes:
[original failures](raw/before-tests.log). Nine of eleven cases failed per run;
raw/wrapped internal-sentinel compatibility cases passed. The original review
had separately reproduced this twice normally and twice under the race detector;
its [probe source](../../reviews/2026-10-15-codebase-map/embedded_probe.go.txt) and
original logs remain available. The original probe now passes twice.

## Minimal correction

Two added production lines recognize the public missing-key error, including
wrapped errors, while retaining the existing internal-sentinel path. A committed
error containing the public missing-key sentinel still stops the batch and
preserves its error chain. Ordinary failures retain existing translation and
return the confirmed prefix; later keys are not touched. The batch still issues
one delete per visited key, in request order, and uses the existing result slice.
There is no extra HEAD, lookup, payload buffering, allocation or storage call in
the adapter. The tests' custom store uses HEAD to produce realistic missing-key
errors; that belongs to the test store, not the production correction.

Eleven regression cases exercise the real custom-store adapter and runtime:
public/internal sentinels, each raw and wrapped; mixed missing/existing keys and
duplicates; a real failure after a confirmed prefix, including storage error,
precondition, missing bucket, cancellation and a committed error; and S3 verbose
and quiet responses. They verify call order, unchanged later objects after
failure, exact committed bytes/object counts and quota reuse. Duplicate keys do
not double-release quota. S3 now returns `200` and confirms every requested key
in order, while quiet mode suppresses Deleted entries.

All eleven cases pass twice normally and five times under the race detector
(**55 race cases**). No S3 handler behavior for genuine backend failures is
changed; this correction restores the supported missing-key path.

## Measurements and rejected cost

An initial two-line implementation reused the existing single-delete adapter to
normalize every result before classification. It passed the functional tests,
but measurements showed needless work on an already-supported path. For a batch
of 1,000 fixed-result deletes, median adapter overhead rose from 11.415 µs to
65.968 µs (**5.8×**) when the store returned the internal missing-key sentinel.
Confirmed-success overhead rose from 5.288 µs to 7.097 µs. Allocations stayed at
one 16 KiB result slice. That implementation was replaced with the narrow public
sentinel check. [Intermediate measurements](raw/normalized-measurements.json)
and its four raw benchmark blocks are retained; those results describe an
implementation that is not shipped.

The final `BenchmarkCustomStoreBatchDelete` isolates adapter overhead using a
fixed-result store with no storage I/O. Each operation processes 1,000 keys;
setup is outside timing. Standalone baseline/final binaries use the identical
benchmark harness, Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64 and eight Go processors.
Serial blocks were baseline/final/final/baseline, three one-second samples each:
six samples per version and case. No agent tests or builds ran concurrently;
other host activity was not controlled. The intermediate comparison used the
same block order with three 300 ms samples per block.

| Final adapter-only measurement / 1,000 keys | Before | After |
|---|---:|---:|
| Confirmed success median | 5.301 µs | 6.115 µs (+15.4%) |
| Confirmed success range | 4.987–6.081 µs | 5.693–6.806 µs |
| Internal missing-key median | 11.660 µs | 10.368 µs (−11.1%) |
| Internal missing-key range | 9.678–15.882 µs | 9.835–23.703 µs |
| Both cases: allocated bytes | 16,384 B/op | 16,384 B/op |
| Both cases: allocation count | 1/op | 1/op |
| Fresh WASM | 4,616,726 bytes | 4,616,726 bytes; rebuilt hash differs |

The success median is **0.815 µs higher per 1,000 keys**, about 0.8 ns/key. Timing
ranges overlap, including an outlier in the final missing-key samples; these
measurements establish neither exact latency parity nor a speedup. They measure
adapter overhead, not end-to-end storage performance. Public missing-key batches
have no comparable successful baseline because the original adapter stops at the
first missing key. Completing all requested deletes necessarily does more work
than that defective early return.

[All final samples and binary hashes](raw/measurements.json), raw benchmark logs
and [WASM hashes](raw/wasm.json) are retained. The final correction adds error-chain
checks only after the existing nil/internal-missing fast paths. Those additional
checks affect public missing-key and genuine-error results. No new success-path
storage operation or allocation is introduced.

## Compatibility, validation and next finding

A batch containing a missing key now proceeds, so later requested objects are
actually deleted. This is the intended existing contract. Callers must not rely
on the old erroneous early stop to preserve later keys. Genuine refusals still
stop at their existing boundary and preserve the confirmed prefix, error mapping
and committed marker. This adds no atomic batch, rollback or durability guarantee.

Full Go tests, relevant runtime/storage/S3/run-through/embedded/public-API race
suites and all four conformance profiles pass. Relevant vet, quality/file-size
ratchets, formatting and diff checks pass. The WASM was freshly rebuilt at
the same size; generated-output verification, WASM smoke and seven package
integration tests pass. Logs are in `raw`.

Next: **F3's two internal runtime COPY adapters still drop known destination
identity on committed errors**. The public-runtime variant was corrected with
F2. The remaining two variants reproduce on the corrected branch and need their
own result-preservation change; they are not implemented here. Neighboring custom
buffered PUT/multipart result adapters remain outside this correction as well.
