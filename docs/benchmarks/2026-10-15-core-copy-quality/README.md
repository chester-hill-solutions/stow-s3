# Core COPY correctness review — October 15, 2026

One change: preserve source preconditions when the run-through adapter wraps a
custom store without optional conditional COPY support. No optimization ships in
this change. The implementation adds nine lines and reuses the shared checker.

## Baseline and completed work

Fetched `origin/main`: `219d1dcfff9807b04660a40313b32127e5be2933`.
The review uses a separate checkout and excludes the pre-existing
`codex/patch-0.3.2` branch (`e03efc9`).

Read the remediation register, compatibility contract, backend COPY contract,
runtime quota and staging paths, workspace COPY, run-through mutations, and ADRs
0002 and 0020. Relevant baseline tests passed for S3, runtime, run-through and all
storage backends before implementation.

Already completed on main:

- Source date conditions reach `ConditionalCopyStore` even without ETag headers.
  `TestCopySourceDateConditionIsEvaluatedOnTheCopiedVersion` exercises a stale
  HEAD deterministically. Date/ETag cross-pairing tests also exist.
- Memory and filesystem COPY check conditions against the captured version under
  their publication lock. Workspace COPY shares its managed read/write lock.
- REPLACE uses one GET and checks that GET's metadata before handing its sized
  reader to PUT. It no longer reads the whole source at the HTTP layer.
- PR #71 (`b16aab1`, merged as `219d1dc`) streams ordinary filesystem REPLACE.
  The older remediation register's whole-object HTTP buffering lead is stale.
  Existing tests cover allocation bounds, quota refusal before staging and
  copying onto the source key.
- Recent remediation includes multipart ETag persistence, advisory store locking,
  outbox ordering and bounded multipart admission. These were not reimplemented.

The [AWS COPY reference](https://docs.aws.amazon.com/AmazonS3/latest/API/API_CopyObject.html)
was checked for source-condition semantics; no new interpretation or API is added.

## Demonstrated defect and change

`s3api.copyVerbatim` trusts `ConditionalCopyStore`. The run-through adapter always
implements that interface, but its `localCopy` previously delegated straight to
unconditional `CopyObject` when its local store lacked the optional extension.
The outer HTTP layer therefore performed no check, and the inner adapter silently
dropped all four source conditions.

The deterministic HTTP regression wraps a real memory backend in a base-interface
wrapper, then in the real run-through adapter. This models a supported custom
store without optional conditional COPY. There are no sleeps or timing windows.

The 72-case matrix covers both local-store capabilities, COPY/REPLACE, all four
conditions passing and failing, unconditional requests, and absent/existing
destinations. Successful COPY also verifies source content type and metadata.

Before: eight cases fail in each of two runs. Failing conditions return 200,
create absent destinations and overwrite existing destination bytes.
After: all cases pass in both runs; the same requests return 412 and preserve
existing destinations or leave absent destinations absent. See
[baseline reproduction](raw/defect-before.txt) and
[candidate result](raw/defect-after.txt).

The adapter now obtains source metadata and calls the existing
`storage.CheckCopySourceConditions` before the fallback copy. It forwards metadata
read failures. Unconditional fallback copies perform no additional read. Stores
implementing conditional COPY still receive the original request directly.

No dependencies, public APIs, formats, indexing, checkpoint changes or new storage
abstractions are introduced.

## Costs and compatibility limits

A conditioned fallback copy adds one local HEAD. Rejected conditions now return
412 instead of incorrectly succeeding; callers relying on the bug will observe
this correction. A custom backend with slow or fallible HEAD also pays that read.
Source metadata maps may add cloning costs beyond the empty-metadata microbenchmark.

The existing fallback contract remains weaker: HEAD and COPY can observe different
source versions. This fix prevents conditions being dropped; it does not make an
arbitrary custom backend atomic. Built-in conditional stores retain their capture
semantics. No new lock can exclude writers inside an arbitrary custom backend.

## Comparative measurements

Host: Apple M1 Pro, 16 GiB RAM, macOS 14.5 ARM64, Go 1.25.6.
Serial baseline/candidate/candidate/baseline blocks; no concurrent build or test
jobs. Identical benchmark source was compiled against both versions. The candidate
contains only the implementation above, regression tests and measurement code.
Binary identities and method are in [metadata](raw/metadata.json), with parsed
samples in [summary](raw/summary.json) and complete per-process logs in `raw/`.

Adapter microbenchmarks use a 4 KiB source and repeatedly overwrite one target.
Each block has three 300 ms samples per case. Values below are pooled medians of
six samples per version. Allocation counts are identical within each profile.

| Adapter path | Before ns/op | After ns/op | Before B/op; allocs/op | After B/op; allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Custom base store, conditioned | 990.35 | 1144.50 | 464; 5 | 688; 8 |
| Custom base store, unconditional | 838.95 | 908.55 | 464; 5 | 464; 5 |
| Conditional store, conditioned | 913.25 | 914.85 | 480; 6 | 480; 6 |
| Conditional store, unconditional | 810.40 | 904.50 | 464; 5 | 464; 5 |

The corrected path adds 224 B and three allocations per copy in this fixture.
Nanosecond differences on unchanged paths demonstrate timing variability; these
samples do not establish a speed improvement or a general latency bound.

REPLACE measurements exercise HTTP dispatch through the runtime into each backend,
with a 512 MiB logical quota and 16-object limit. Each fresh process has a single
source and repeatedly overwrites one destination. It takes three timed copies,
plus the Go benchmark calibration invocation. Source setup is excluded from
allocation timing. Filesystem setup uses a known-length stream; memory setup
transfers an owned source buffer. GC runs before timing.

Peak RSS is the whole standalone test process, including source setup, destination,
allocator retention and benchmark harness. It is not a per-copy heap delta or a
concurrent capacity guarantee. `/usr/bin/time -l` reports bytes on this host.

Ranges are the two observed block results per version. MiB means 1,048,576 bytes.

| Backend; object | Before allocated MiB/op | After allocated MiB/op | Before peak RSS MiB | After peak RSS MiB |
| --- | ---: | ---: | ---: | ---: |
| Filesystem; 1 MiB | 0.260 | 0.260 | 13.97–14.50 | 13.86–14.05 |
| Filesystem; 16 MiB | 0.263 | 0.260–0.263 | 13.88–14.12 | 13.88–14.06 |
| Filesystem; 64 MiB | 0.260–0.263 | 0.260–0.266 | 13.42–13.72 | 13.03–13.55 |
| Memory; 1 MiB | 1.021–1.023 | 1.021 | 17.62 | 16.92–17.61 |
| Memory; 16 MiB | 16.022 | 16.022 | 77.17–92.69 | 92.81–93.00 |
| Memory; 64 MiB | 262.73 | 262.73 | 525.33–559.66 | 501.50–539.78 |

Filesystem allocation counts range from 303–306 before and 303–307 after. Memory
counts range from 85–101 before and 87–99 after. Raw logs retain exact counts,
latency and throughput. Filesystem timings vary widely (for example baseline
64 MiB copy averages 409–649 ms); no throughput improvement is claimed.

The filesystem result supports retaining the merged streaming implementation:
allocation and process memory do not scale with object size in this range.
The existing 8 MiB oversized-copy regression also passes: quota rejection precedes
whole-source staging. No new resource-contract violation is demonstrated here.

Memory REPLACE must retain destination bytes, but its 64 MiB allocation cost is
about 4.1 times the payload. `storage.readOwnedBytes` caps the initial allocation
hint at 32 MiB; subsequent buffer growth accounts for excess allocation beyond
that threshold. This is a material cost for supported large-object memory
workloads, not a newly introduced regression. Logical quota admission is not a
hard process-RSS ceiling. Smaller copies and filesystem streaming have different
costs. Neither RSS variability nor these samples justify an architectural rewrite.

An initial run accidentally used an unsized filesystem setup reader, so fixture
construction buffered the source and polluted peak RSS. Those samples were
discarded. Every published memory sample uses the corrected fixture above.

Reproduce the operation measurements with the added benchmarks:

```sh
go test ./internal/runthrough -run '^$' -bench '^BenchmarkCopyConditions$' -benchtime=300ms -count=3 -benchmem
go test ./internal/s3api -run '^$' -bench '^BenchmarkCopyReplace$' -benchtime=3x -count=1 -benchmem
```

For RSS, compile `go test -c` and run that standalone binary under
`/usr/bin/time -l`, selecting one backend/size per process. The retained
[measurement runner](raw/measure.py) records the four-block order and exact flags;
place baseline/candidate S3 and run-through test binaries alongside it with the
names shown in `metadata.json`. Test binaries are not committed.

## Verification and next priority

Passed:

- Baseline: `go test ./internal/s3api ./internal/storage/... ./internal/runtime ./internal/runthrough`.
- Candidate: `go test ./...`; [full output](raw/go-tests.txt).
- `make test-conformance`: memory, filesystem, runtime/memory and runtime/filesystem;
  [full output](raw/conformance.txt).
- `go test -race ./internal/s3api ./internal/storage/... ./internal/runtime ./internal/runthrough`.
- Final regression and existing date/REPLACE tests under the race detector;
  [output](raw/final-copy-race.txt).
- `go vet ./internal/runthrough ./internal/s3api`, formatting/diff checks,
  Go-quality and file-size ratchets without baseline changes.

Next correctness priority: the documented source-version gap for custom stores
without conditional COPY. A writer can replace the source after the fallback HEAD
and before the backend COPY. A separate change should first reproduce that exact
interleaving and assess a captured-read fallback against COPY metadata, ETag,
checksum and resource contracts. This review does not implement it.

The measured memory-backend allocation growth is a separate optimization candidate
with evidence now available. No follow-up optimization is automatically started.
