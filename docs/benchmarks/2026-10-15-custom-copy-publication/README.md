# Custom-store COPY preserves a committed result

Baseline `f8c8951` on `codex/core-copy-quality`, with freshly fetched `origin/main`
`219d1dcfff9807b04660a40313b32127e5be2933`. This corrects **F2** from the
[codebase review](../../reviews/2026-10-15-codebase-map.md), including the public
COPY wrapper needed to deliver that result. F1's multipart correction is retained.
No indexing, checkpointing or storage optimization is included.

## Evidence before the correction

The regression opens the public Go runtime over a supplied base `Store`. Its
PUT really publishes to a separate memory runtime, then returns the destination
object with `ErrMutationCommitted` and a subsequent failure. The adapter's
GET→PUT COPY fallback discards that metadata. Runtime accounting recognizes the
committed marker, dereferences nil metadata and panics. A new destination exists,
but usage remains four bytes/one object instead of eight bytes/two objects.
An overwrite similarly leaves seven bytes charged instead of eight.

The 16-case regression ran twice before production changes; eight cases failed
per run. See [original failures](raw/before-tests.log). It also demonstrated two
necessary result/error boundaries:

- The public COPY wrapper discarded the known object even when the underlying
  optional `CopyStore` returned its identity and accounting was correct.
- A committed error joined with a public missing-object failure was translated
  into an ordinary missing-object error. The committed marker and underlying
  error chain were lost; accounting skipped the published object, and an
  over-budget write was then admitted.

The success and ordinary refusal cases passed before the fix. The original
review independently reproduced the generic fallback panic twice normally and
twice under the race detector; its [probe source](../../reviews/2026-10-15-codebase-map/embedded_probe.go.txt)
and original logs remain in that review directory.

## Small correction and preserved contracts

The fallback now retains known metadata alongside a committed error. The public
COPY wrapper follows the public PUT convention: return the known object and
error when publication committed, while ordinary refusals still return an empty
object. Error translation preserves a committed error chain before normalizing
ordinary refusal sentinels. The marker already aliases the internal sentinel;
no new error type or translation machinery is needed.

This changes eight production lines across two files, including the translation
comment. No interface, dependency, format or public API is added. COPY still
checks the source and destination through the existing runtime. Its fallback
performs exactly one GET and one PUT; optional `CopyStore` performs one COPY and
no fallback GET or PUT. No snapshot emulation, extra HEAD, payload copy or read
is introduced. The generic store's existing buffered COPY and conditional-source
consistency limits are unchanged.

The public-API regression covers both capability routes, new destinations and
overwrites, and four outcomes: success, generic committed failure, committed
missing-object failure and ordinary precondition refusal. It verifies payload,
content type, metadata, ETag, modification time, exact returned identity,
`errors.Is` for both causes, exact usage and storage call counts. A five-byte
source overwrite is refused at an eight-byte combined cap after COPY. Deleting
the copied destination releases its actual bytes and object slot; an eight-byte
source overwrite then succeeds. Ordinary refusal preserves the old destination
or its absence. Sixteen cases pass twice normally and five times under the race
detector (**80 race cases**). The test assertions were split into small helpers
to satisfy existing complexity and parameter limits; no baseline was raised.

The original custom-store panic probe now passes twice. The original real
workspace public-COPY probe also passes twice, confirming that the public-wrapper
part of F3 is corrected. F3's two internal StoreAdapter variants remain open.

## Comparative measurements and costs

`BenchmarkCustomStoreCopyPublication` measures successful public COPY through the
base-store fallback, backed by a real memory runtime, at 4 KiB and 1 MiB. Source
creation and close are outside timing; repeated destination overwrites are timed.
Standalone baseline/candidate binaries used Go 1.25.6, Apple M1 Pro, macOS 14.5
ARM64, default eight Go processors. The benchmark body/setup were identical;
regression payload assertions and helper refactoring were added after binary
creation. No agent build or test suite ran concurrently with measurements;
other host activity was not controlled.

First, baseline/candidate/candidate/baseline blocks each collected three 300 ms
samples. Candidate medians were **2.5% higher at 4 KiB and 10.2% higher at 1 MiB**;
one candidate 1 MiB sample reached 6.83 ms. These noisy results prompted longer
samples, retained rather than discarded in [short measurements](raw/measurements.json).

The same binaries then ran candidate/baseline/baseline/candidate blocks, three
one-second samples per block: six samples per version and payload.

| Longer-run measurement | Before | After |
|---|---:|---:|
| 4 KiB median | 12.676 µs | 11.697 µs (−7.7%) |
| 4 KiB range | 11.961–16.979 µs | 11.329–14.656 µs |
| 4 KiB allocations | 36/op; 12,036–12,121 B/op | 36/op; 12,082–12,159 B/op |
| 1 MiB median | 1.976 ms | 1.994 ms (+0.9%) |
| 1 MiB range | 1.901–3.983 ms | 1.869–2.355 ms |
| 1 MiB allocations | 38–39/op; 2,123,421–2,130,876 B/op | 38–39/op; 2,122,983–2,132,863 B/op |
| Packaged WASM | 4,615,917 bytes | 4,616,726 bytes (+809) |

Timing and allocation ranges overlap. This is no speedup or exact-parity claim.
[Longer samples](raw/long-measurements.json), raw benchmark logs and baseline/
candidate binary hashes are retained. The measured success path adds no allocation
count or storage operation. Committed failure now converts and clones known
metadata instead of discarding it; those error-path allocations are necessary to
return the result. Additional `errors.Is` checks traverse non-nil error chains.
A baseline runtime latency comparison for that generic committed-failure path is
not meaningful because the original path panics.

Compatibility changes for committed failures: callers now receive the published
identity and original error chain, and usage/quota admission reflects the actual
mutation. A nested missing-object error no longer masks a committed result.
Ordinary refusal translation is unchanged. Custom stores remain responsible for
reporting an accurate destination object with the committed marker; this does not
infer publication when a store returns an ordinary error or promises rollback.
No new durability or crash-recovery guarantee is introduced.

## Validation and next finding

Full Go tests, the relevant runtime/storage/S3/run-through/embedded/public-API race
suites and all four conformance profiles pass. Final focused ordinary/race tests
pass after assertion-helper refactoring. Relevant vet, quality/file-size ratchets,
formatting and diff checks pass. The WASM was rebuilt; generated-output checking,
WASM smoke and seven package integration tests pass. Logs are retained in `raw`.

Next priority: **F4**, a custom store's public missing-key sentinel stops batch
deletion before later keys. The original probe still fails twice on this corrected
branch: [confirmation](raw/next-batch-delete-probe.log). This common idempotent-delete
failure remains a separate change; it is not implemented here. The remaining F3
adapter identity loss and neighboring buffered PUT/multipart result adapters also
remain outside this correction.
