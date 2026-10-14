# Read, quarantine, workspace and browser hot paths — 2026-10-14

All builds, tests and measurements run on Railway. Website benchmark data remains unchanged.

## Changes

Whole-object reads use the authorized streaming lifecycle. Independent object captures
and reads use shared runtime locks. Each payload chunk checks current authority and
runtime liveness; close waits for an active read. The returned payload is caller-owned.
The embedded facade transfers that owned result instead of copying its bytes again.
Reported sizes guide initial allocation, capped at 32 MiB; larger reads may grow.

Ordinary unbound filesystem uploads stage outside the runtime publication lock.
Small bodies use a bounded prefix buffer. Large streams use temporary files with
incremental MD5 and requested checksum computation. Internally owned HTTP buffers
retain their existing handoff. Commit rechecks conditions, recovery holds and quotas.
Aborted or refused stages clean up their files; reopen removes interrupted staging.
Capacity binding requires staged work to finish or abort. Namespace admission,
content guards and save receipts retain their buffered paths.

Ordinary copies stream one captured payload into a new record. File-to-file copy
uses the bounded payload span and permits the platform's kernel transfer path.
The record format, legacy readers and durable publication sequence remain intact.
Windows retains its existing captured-payload materialization.

Native workspace index updates use compact JSON with the same version and fields.
Indented indexes remain readable. Every update still rewrites the complete index.
Incremental indexing needs a separate durability and migration design.

Browser WASM startup uses streaming instantiation for supported application/wasm
responses, with the existing buffered path for other hosts. Stream read buffers are
reused only after returned bytes have been copied into caller-owned JavaScript arrays.

## Measurement protocol

The original source archive reproduces the preceding SDK WASM digest. Six balanced,
alternating fresh processes per variant measure native GET, generated-body filesystem
PUT/COPY and workspace index replacement. These are in-process benchmarks; they do
not measure HTTP, DPI scanners or S3-compatible competitors. Index fixtures contain
1,000 and 10,000 entries with identical metadata. Filesystem tests use temporary
storage in the Railway container, not the target's mounted data volume.

The final run executes directly on the benchmark runner with a four-CPU quota and
4 GiB memory limit. Earlier read and disk probes ran on Railway build hosts. The final executor receipt records the actual instance execution.

WASM observations use six alternating fresh Node processes, five warmup pairs and
40 verified 1 MiB PUT/GET pairs. Readiness includes reading, compilation,
instantiation and bridge publication. First write adds open, bucket creation and a
4 KiB write. Process startup, JavaScript SDK loading, sockets, host persistence,
actual browser download and scanner work are excluded.

Medians are exploratory observations on one Railway host. They do not establish
production capacity, p99 latency or generalized startup gains.

## Results

Six-process medians on the four-CPU Railway runner:

| Operation | Before allocated bytes/op | After allocated bytes/op | Reduction |
| --- | ---: | ---: | ---: |
| Embedded GET, 1 MiB | 6,289,953 | 1,064,854.5 | 83.071% |
| Filesystem PUT, 1 MiB | 5,251,135 | 215,275.5 | 95.900% |
| Filesystem PUT, 32 MiB | 201,113,757 | 215,290.5 | 99.893% |
| Filesystem COPY, 32 MiB | 33,562,058.5 | 7,637.5 | 99.977% |
| Index replacement, 10,000 entries | 13,863,344 | 7,615,144.5 | 45.070% |

Allocated bytes are cumulative allocation per operation, not peak process memory.
The 32 MiB copy median remained about 26.5 ms versus 26.7 ms before; its speed
change is small and not a demonstrated capacity gain. Index replacement decreased
from about 27.8 to 16.5 ms. Full 10,000-entry index size decreased from 3,440,060
to 2,260,037 bytes. Every update still performs a complete durable replacement.

The SDK WASM changed from 4,610,006 to **4,610,009 bytes**. The validated SHA-256
is `99a8688b9f04c07f986846db77cfc8ae8ab148e6c27b22c3c24202c62e3ec47f`.
Gzip level 9 is 1,256,892 bytes; Brotli quality 11 is 925,382 bytes.
The Node first-write medians were both about 33.8 ms. This run does not establish
an improvement in cold first write. Binary-bridge steady PUT/GET medians increased
from 249.15 to 299.34 pairs/s; these exclude the SDK, browser, sockets and persistence.

## Validation and evidence

Final qualification passed repository standards, all conformance lanes, Node/WASM
tests, real browser tests, pinned protobuf regeneration and private chart checks.
Browser results: 34 passed, two optional cases skipped, no failures or flaky cases.
Coverage measured 74.63% (11,645/15,604 statements). No quality or coverage floor
was lowered. The validated SDK assets and Go reference were synchronized after a
checksum-verified export. Benchmark compute was stopped after synchronization.

- [Initial read probe](read/read-performance/samples.json), including raw native samples and the failing original regression.
- [Initial disk probe](disk/disk-performance/samples.json), including the slower first copy candidate.
- [Final source manifest](final/source.json), [source transfer receipt](final/source-upload.json) and [final overlay receipt](final/overlay-upload.json).
- [Pinned instance tools](final/instance.Dockerfile), [verification controller](final/instance-verify.mjs) and [process supervisor](final/subreaper.py).
- [Complete qualification receipt](final/verification/receipt.json), [qualification log](final/qualification.log) and [browser report](final/verification/sdk-browser-report.json).
- [Measured samples and analysis](final/verification/hot-paths/analysis.json), [unchanged-production proof](final/verification/hot-paths/reuse.json), [executor receipt](final/executor.json) and [export checksum](final/export.json).
- [Validated Go source](final/validated-go-source.tar.gz) and [archived generated reference and previews](final/verification/rendered-documentation.tar.gz).

The initial copy candidate reduced allocation but regressed latency. The retained
version permits a bounded file-to-file transfer and restored comparable copy time. Failed full-source deployment uploads created no new deployment. A verified
archive and declared overlay were transferred to the volume, then qualification
ran with the same checks on the existing benchmark instance.

The first complete attempt exposed additional browser source maps in generated
output. Later gates caught an explicit reader error discarded by the small-body
prefix path and complexity in the concurrency regression test. The prefix now
retains underlying errors, including when a full prefix masks them in ReadFull.
The test worker was extracted without changing its assertions. Container orphan
reaping is supplied by the verification supervisor; the lifecycle test remains
intact. Final measurements were rerun after the functional correction. A verified
source comparison permits their reuse after the test-only quality refactor.

Final executor deployment: `90f1a2b9-696c-43c6-8985-b94643bf335d` (`SUCCESS`
before shutdown). Qualification completed at 2026-10-14T17:36:15.885Z with exit 0.
Retained root: `/results/hot-paths-90f1a2b9-696c-43c6-8985-b94643bf335d`.
Native executables and before/after WASMs remain on the Railway volume.
The [environment receipt](../railway-environment.json) records stopped compute.
