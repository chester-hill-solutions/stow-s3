# Conditional COPY source capture — October 15, 2026

**Withdrawn for performance regression.** The `959f3b5` implementation described
below is historical evidence, not the current implementation. The
[restoration](../2026-10-15-copy-cost-restoration/README.md) returns to the earlier
documented COPY contract and adds a guard against GET→PUT payload emulation.

This follow-up closes the custom-store source-version race identified in the
[preceding review](../2026-10-15-core-copy-quality/README.md). It also makes the
generic runtime fallback admit the size of the source it actually captured.
These are corrections to the same COPY path; no optimization is included.

## Baseline and deterministic failures

Baseline: `b56ee81`, the preceding condition-preservation fix, over
`origin/main` at `219d1dcfff9807b04660a40313b32127e5be2933`. A final fetch confirmed
main is still at that commit. The separate `codex/patch-0.3.2` work is excluded.

The earlier fix checked source conditions but left HEAD and COPY as separate
observations for stores lacking `ConditionalCopyStore`. A replacement between
them could publish a source version that failed the caller's conditions.

`TestConditionalCopyFallbackCapturesTheCheckedSource` replaces the source inside
the first metadata/read observation, before COPY continues. It uses a real memory
store behind a base-interface wrapper and exercises direct HTTP, run-through and
runtime-backed HTTP. Each profile covers all four source conditions and both
absent and existing destinations. Timestamps are fixed; there are no sleeps or
probabilistic goroutine schedules.

All 24 cases fail twice against the preceding commit: COPY returns 200 and
publishes the replacement bytes, content type, metadata and checksum, despite
the condition identifying or allowing only the captured original version.
The same final test source passes twice against the candidate. The permitted
outcomes are the captured original version or 412 with no destination mutation.
Source readers must be closed on either outcome.

The companion quota case starts with a four-byte source and a 16-byte runtime
limit. A source observation triggers replacement by an 18-byte object. The
preceding runtime admits four bytes, publishes 18 and reports 22 bytes of usage.
The corrected generic fallback captures before admission, so it can copy the
captured four-byte version or refuse a captured oversized version. It cannot
admit one size and publish the other. Unmanaged external changes themselves are
outside the runtime's quota ownership; the test checks COPY's own admission.

See [failing baseline](raw/defect-before.txt) and
[passing candidate](raw/defect-after.txt). The 24 race cases and one quota case
each run twice in these logs.

## Implementation and preserved contracts

- The internal storage helper delegates to existing conditional COPY support.
  Otherwise, a conditioned copy obtains one GET snapshot, checks its metadata and
  streams that same reader through the existing PUT interface with its size.
- Direct HTTP and run-through use that helper. Unconditional fallback copies
  retain ordinary backend COPY.
- The generic runtime fallback opens its snapshot before checking conditions and
  quota. It uses that metadata's size for admission, enforces the stream length
  with its existing sized reader, and publishes/accounting through the existing
  mutation result path. Native conditional stores retain their existing path.
- Content type, user metadata and checksum algorithm/value come from the captured
  source. Readers close on success, refusal and failures. PUT retains each backend's
  staging and atomic publication behavior, including copying onto the source key.
- The existing outbox still records the COPY operation and propagates its committed
  local version. No recovery format or live-write authorization is changed.

No dependency, public module API, storage format or new storage abstraction is
added. `internal/storage.CopyObject` is shared internal implementation over the
existing Store interface. The preceding HEAD-only adapter fallback is replaced.

Contract tests cover all three backends, cross-bucket and self-copy, all six
checksum profiles (none, CRC32, CRC32C, CRC64NVME, SHA1 and SHA256), condition/read/PUT
failures, source closure, size forwarding and destination preservation. A genuine
two-part source verifies that its composite ETag can be used as the condition.

The fallback PUT computes a destination ETag according to single-part write
semantics. For a multipart source this may differ from the composite source ETag;
the condition still checks the source ETag. This is an observable change for custom
fallback stores and is covered by a regression. S3 also allows a multipart source
to become a single-part COPY destination; see the
[AWS CopyObject reference](https://docs.aws.amazon.com/AmazonS3/latest/API/API_CopyObject.html).
Native backend COPY behavior is retained.

## Comparative cost

Correctness requires the generic fallback to read and write the captured bytes.
It can no longer use an unconditional backend shortcut after checking another
version. This is more expensive, especially when a memory backend's ordinary COPY
would share immutable bytes. Custom backends and wrappers can retain optimized
conditional COPY by implementing/forwarding the existing optional interface.

Host: Apple M1 Pro, 16 GiB RAM, macOS 14.5 ARM64, Go 1.25.6. Runs were serial in
baseline/candidate/candidate/baseline order, with no concurrent builds or tests.
Identical benchmark source was compiled against both implementations. Metadata,
binary hashes, raw process logs and parsed samples are retained under `raw/`.

The 4 KiB adapter benchmark takes three 300 ms samples per case in each block.
The values below are medians of six samples per version.

| Adapter path | Before ns/op | After ns/op | Before B/op; allocs/op | After B/op; allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Base store, conditioned | 899.25 | 8599.50 | 688; 8 | 6002; 18 |
| Base store, unconditional | 701.45 | 699.25 | 464; 5 | 464; 5 |
| Conditional store, conditioned | 778.50 | 776.20 | 480; 6 | 480; 6 |
| Conditional store, unconditional | 674.50 | 679.95 | 464; 5 | 464; 5 |

The corrected adapter path adds 5,314 B and ten allocations in this fixture, and
takes about 9.6 times as long. Unaffected profiles retain their allocation counts.
Small timing changes on them do not establish a speedup or a general latency bound.

Large-object benchmarks dispatch conditioned COPY through HTTP and the runtime.
The fallback profile wraps the backend in the base Store interface. Its quota is
512 MiB with a 16-object cap. Filesystem setup streams a known length; memory setup
transfers an owned buffer. Setup and GC are excluded from allocation timing.
Each process runs three timed copies plus calibration, overwriting one target.

Ranges below are the two block observations per version. MiB = 1,048,576 bytes.

| Backend/path; object | Before allocated MiB/op | After allocated MiB/op | Before peak RSS MiB | After peak RSS MiB |
| --- | ---: | ---: | ---: | ---: |
| Filesystem/native; 64 MiB | 0.059–0.062 | 0.059–0.061 | 13.92–13.94 | 13.66–14.02 |
| Filesystem/fallback; 1 MiB | 0.059 | 0.257 | 13.53–13.58 | 13.86–13.89 |
| Filesystem/fallback; 16 MiB | 0.059 | 0.257 | 13.86–14.14 | 14.08–14.27 |
| Filesystem/fallback; 64 MiB | 0.062 | 0.260 | 13.70–13.95 | 13.98–14.11 |
| Memory/native; 64 MiB | 0.012 | 0.012 | 140.72–140.73 | 141.05–141.12 |
| Memory/fallback; 1 MiB | 0.012 | 1.021–1.023 | 14.38–14.39 | 17.91–18.03 |
| Memory/fallback; 16 MiB | 0.012 | 16.021–16.024 | 44.55–44.59 | 92.80–93.38 |
| Memory/fallback; 64 MiB | 0.012 | 262.73 | 140.73–140.94 | 541.09–541.16 |

Peak RSS is the whole standalone benchmark process, including setup, calibration
fixture lifetimes and allocator retention. It is not an isolated single-copy heap
delta, a promise about concurrent capacity or a hard RSS bound implied by logical
quota admission. `/usr/bin/time -l` reports bytes on this host.

The generic filesystem fallback retains bounded payload scratch in these samples,
but performs more allocation and integrity work: its 64 MiB mean copy times rise
from 110–136 ms to 360–522 ms. Native filesystem timing varies substantially,
despite effectively unchanged allocations; no native speedup is claimed.

Native memory COPY retains shared immutable bytes and approximately 12 KiB of
measured HTTP/runtime overhead per operation, independent of payload size.
The corrected generic memory fallback must retain destination bytes. At 64 MiB it
also inherits the existing `readOwnedBytes` growth cost beyond its 32 MiB initial
hint cap, allocating approximately 4.1 times the payload. Its sample mean latency
rises from 33–43 microseconds to 142–146 ms. This material cost is disclosed;
neither a new copy optimization nor a buffer-growth optimization is included.

The bundled WASM runtime is rebuilt: 4,615,854 → 4,625,923 bytes (+10,069,
approximately 0.22%). Generated output matches a fresh package build. JavaScript
and TypeScript APIs are unchanged.

Reproduce the allocation measurements with:

```sh
go test ./internal/runthrough -run '^$' -bench '^BenchmarkCopyConditions$' -benchtime=300ms -count=3 -benchmem
go test ./internal/s3api -run '^$' -bench '^BenchmarkConditionalCopy$' -benchtime=3x -count=1 -benchmem
```

For process memory, compile standalone test binaries for each source version and
run one profile/backend/size per process under `/usr/bin/time -l`.
[The measurement runner](raw/measure.py) retains exact flags and ordering; its
four binary names and hashes are in [metadata](raw/metadata.json). The
[summary](raw/summary.json) contains 48 adapter samples and 48 large-object samples.
Binaries are not committed.

## Validation and next correctness investigation

Passed:

- Final `go test ./...`: [output](raw/go-tests.txt).
- `make test-conformance`, all four profiles: [output](raw/conformance.txt).
- `go test -race ./internal/s3api ./internal/storage/... ./internal/runtime ./internal/runthrough`:
  [output](raw/race.txt).
- Final captured-copy backend and HTTP regressions under race detection:
  [backend output](raw/final-contract.txt), [HTTP output](raw/final-wire.txt).
- Vet for changed packages; formatting and whitespace checks; Go-quality and
  file-size ratchets without baseline changes.
- Rebuilt real WASM runtime test: [output](raw/wasm.txt).
- Seven embedded, WASM-host, stream and asset tests: [output](raw/node.txt).
- Reproducible generated package output: [output](raw/generated.txt). Existing
  matching TypeScript/tsx dependencies were reused; none were added.

Next correctness investigation: quota admission when a native conditional backend
or adapter can observe a source changed by an independent writer. Those paths,
and ordinary unconditional runtime COPY, still obtain size from HEAD before the
backend captures its source. This is a code-read risk needing its own deterministic
reproduction and profile qualification; this change does not claim shared-writer
quota guarantees. No next fix or substantial optimization is automatically started.
