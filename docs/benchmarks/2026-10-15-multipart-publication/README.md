# Multipart completion preserves a committed publication

Baseline `2c100c3` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. This corrects **F1** from the
[codebase review](../../reviews/2026-10-15-codebase-map.md). F2–F4 remain open.

## Original defect and reproduction

On a real workspace, initiate an upload and store the four-byte part `body`.
Replace `.stow/index.json` with a directory before completing. Content publication
succeeds, but index persistence fails. Workspace completion discarded the metadata
returned by PUT with `ErrMutationCommitted`. Runtime accounting then dereferenced
nil metadata, panicking while the destination existed and usage remained zero.
The [original review probe](../../reviews/2026-10-15-codebase-map/runtime_probe.go.txt)
reproduced this twice normally and twice under the race detector before this fix.

The new regression also reproduced the panic twice before implementation:
[baseline failures](raw/before-tests.log). A second fixture makes the upload
directory non-writable, forcing cleanup failure after successful publication;
completion previously returned nil metadata and an ordinary error for that
committed object. This permission fixture ran on this host; it skips for root or
hosts that do not enforce directory permissions.

Preserving metadata alone was insufficient: an intermediate implementation
prevented the panic but released all part reservations while upload files remained
on disk. Both failure fixtures demonstrated that under-accounting:
[backend-only failures](raw/backend-only-tests.log).

## Small correction

Workspace completion now returns known metadata and a committed error for
post-publication failures. It preserves the composite multipart ETag in the live
index, including when index persistence fails. Cleanup failures also carry the
committed marker. Pre-publication errors retain their existing behavior.

Runtime accounts for the published object, then releases the upload's part and
session reservations only on success or confirmed upload absence. A committed
error triggers one existing `GetMultipartUpload` call. A present upload retains
its reservations; an unavailable lookup retains them conservatively and preserves
both errors. There is no new storage interface, format, dependency or public API.
Normal completion adds no storage operation; its two existing index saves and
upload cleanup remain unchanged. COPY paths are unchanged.

Six regression cases cover index failure, cleanup failure, repair/close/reopen,
completion retry, abort, confirmed cleanup, unavailable cleanup lookup and an S3
`500 InternalError` response without a panic. They check payload, content type,
user metadata, ETag, version identity, object usage, retained part bytes, session
limits and quota admission. The six cases pass twice normally and five times
under the race detector (30 race cases). The original F1 probe also passes twice.

## Costs and compatibility

`BenchmarkWorkspaceMultipartCompletion` measures the **entire successful 4 KiB
multipart lifecycle**: initiate, upload one part and complete to the same target.
Setup and final close are outside timing. Standalone binaries used Go 1.25.6 on
Apple M1 Pro, macOS 14.5 ARM64. The benchmark body and setup were identical;
additional regression helpers were added after the baseline binary was built.
Serial blocks were baseline, candidate, candidate, baseline, each collecting
three samples of five iterations. No builds or other tests ran concurrently.

| Measurement | Before | After |
|---|---:|---:|
| Median lifecycle | 58.71 ms | 61.22 ms |
| Timing range | 55.87–63.36 ms | 57.87–70.47 ms |
| Bytes allocated | 140,555–142,272 B/op | 140,606–142,672 B/op |
| Allocation count | 1,044–1,046/op | 1,043–1,047/op |
| Packaged WASM | 4,615,872 bytes | 4,615,917 bytes |

**The measured median is 4.3% higher.** The filesystem timing ranges overlap;
this small sample does not establish latency parity or a statistically reliable
slowdown. It is no speedup claim. Allocation ranges overlap. Production adds
branches on success and one upload lookup plus error handling only on committed
failure. There is no new payload buffering. All samples and binary hashes are in
[measurements](raw/measurements.json); [WASM hashes](raw/wasm.json) record the
45-byte artifact increase.

Post-publication cleanup errors now report `ErrMutationCommitted` and known
metadata, rather than implying refusal. Callers must verify the object before
retrying. Retained or uncertain uploads continue to consume quota, so admission
can refuse writes previously admitted through incorrect under-accounting.
After repairing the obstruction, the live workspace handle can persist metadata
on successful close; reopen, completion retry and abort are covered. Abort removes
the upload and preserves the published destination. This does not add crash-atomic
content/index/cleanup publication or reconstruction of metadata after loss of a
live handle. It uses the existing authoritative upload lookup and does not add
orphan-file discovery for a partially failed directory removal. Invalid-index
reopen continues to fail closed.

## Validation

Full Go tests, relevant runtime/storage/S3/run-through/embedded/public-API race
suites and all four conformance profiles pass. Focused regressions, relevant vet,
quality/file-size ratchets, formatting and diff checks pass. The rebuilt WASM,
generated-output verification, WASM smoke test and seven package integration
tests pass. Evidence is retained in `raw`, including the original failures and
the intermediate quota failure.

Next priority: **F2**, the supported public custom-store COPY fallback drops a
successful PUT result when accompanied by a committed error, causing another
nil-metadata panic and stale usage. It requires a separate result/error adaptation
change and regression coverage. It is not implemented here.
