# Storage optimization — October 13, 2026

The filesystem record and upload-buffer fixes passed the cloud race tests, both allocation regressions, a deployed smoke suite and the repeated comparison. Full repository standards, all conformance lanes, Node client tests and WASM tests also passed on Railway.

## Measured change

Same S3 workload configuration: 1 MiB files, four workers, 16 retained fixtures, three 20-second windows and five-second warm-ups. Targets have a 2 vCPU / 2 GiB ceiling; the separate generator has 4 vCPU / 4 GiB. Both batches reported AMD EPYC 9655P CPUs and used private HTTP without TLS. Filesystem targets used the persistent benchmark volume. These are exploratory measurements.

| Backend / workload | Before | After | Ratio |
|---|---:|---:|---:|
| stow-memory / put | 182.75 MiB/s | 194.70 MiB/s | 1.07× |
| stow-filesystem / put | 7.05 MiB/s | 66.45 MiB/s | 9.43× |
| stow-memory / get | 466.95 MiB/s | 480.65 MiB/s | 1.03× |
| stow-filesystem / get | 119.40 MiB/s | 371.35 MiB/s | 3.11× |
| stow-memory / quarantine | 105.75 files/s | 109.95 files/s | 1.04× |
| stow-filesystem / quarantine | 2.00 files/s | 47.50 files/s | 23.75× |

All 18 S3 runs and 18 selected native samples completed. Measured and warm-up workflows had zero failures or drops. Reads passed SHA-256 and length verification, and quarantine left no worker objects. The independent smoke suite passed all 15 implementation/workflow combinations and 126 native samples.

See [latency, ranges, RPS and sampled server RSS](regression/s3/report.md), [raw S3 index](regression/s3/suite.json), [all selected native cases](regression/embedded-report.md) and [completion receipt](regression/run.json). The [earlier comparison](../2026-10-13-railway-performance/README.md) includes the unchanged competitor baselines. Competitors were not remeasured in the optimization batch.

## Allocation and native throughput

Native table: 1 MiB files, CPU setting 4, three repetitions. Earlier windows were 300 ms; later windows were 500 ms. These short native cases measure implementation overhead and include verified reads.

| Backend | PUT MiB/s before → after | GET MiB/s before → after | Quarantine files/s before → after | PUT allocated MiB/op before → after |
|---|---:|---:|---:|---:|
| stow-memory | 481.08 → 770.03 | 6,617.37 → 7,195.11 | 421.23 → 625.55 | 5.04 → 1.03 |
| stow-filesystem | 25.03 → 336.35 | 177.49 → 3,775.67 | 9.77 → 225.20 | 23.58 → 2.09 |

Memory PUT allocation fell about 80%; filesystem PUT allocation fell about 91%. These are allocated bytes per operation, not retained memory or process RSS.

## Confirmed causes and fix

The [before CPU profile](profiles-before/stow-filesystem.cpu.txt) placed most filesystem PUT CPU time in JSON/base64 work. Its allocation profile showed repeated record reads and decoding, plus growing `io.ReadAll` buffers. The [memory allocation profile](profiles-before/stow-memory.alloc.txt) attributed nearly all allocated space to `io.ReadAll`.

New filesystem records have a protobuf metadata header followed by raw payload bytes in one atomic file. HEAD, listing and quota refresh parse the header and check its declared length against the file. GET streams through the opened file. Managed atomic replacement and deletion leave an already opened reader on its captured version. Operations requiring a content fingerprint still load and verify content.

Known-length readers now provide a bounded allocation hint. The store makes its own buffer, still reads to EOF, preserves read errors and retains the stream-length checks. Caller buffers remain independent.

The [after filesystem CPU profile](profiles-after/stow-filesystem.cpu.txt) is dominated by MD5 and system calls, with JSON/base64 absent from its top nodes. The [after memory profile](profiles-after/stow-memory.alloc.txt) reflects one owned payload allocation. Profile runs are diagnostic and excluded from the throughput medians.

## Compatibility and guarantees

Object paths and sharding remain unchanged. Publication retains temporary-file creation, file sync, rename and directory sync. Guards, journal recovery, conditional writes and quota refresh remain in place. Namespace accounting charges the actual encoded record size.

Existing version 1 JSON records remain readable and can coexist with new records. Only overwritten records change format. Their compatibility reader still decodes payloads; untouched legacy data does not receive the new metadata-read performance benefit. Older Stow binaries cannot read version 2 records. The workspace layout and S3 protocol are unchanged. Always close streamed readers to release their file handles.

See [ADR 0020](../../adr/0020-binary-object-records.md), [protobuf schema](../../../internal/storage/fs/recordpb/record.proto), [record reader](../../../internal/storage/fs/record_io.go) and [owned input reader](../../../internal/storage/owned_reader.go). The Go protobuf library/generator is pinned to 1.36.12; generation used protoc 3.21.12 on Railway.

## Validation and remaining limits

The [new tests fail on the old binary](red-regressions.json) and [pass on the fixed binary](green-regressions.json). Cloud race tests also cover legacy reads, raw-byte persistence, reopen, snapshot readers, malformed records, ownership, guards and shared admission. Permission-failure tests run without root privileges. Generated protobuf source is compared with a fresh build from its schema.

The benchmark roles have matching source, binary and harness hashes. Their image digests differ; image layers also contain build-time validation and caches. The measured source payload is `8d8a17c837b17f83681ab9c8c874c7cab6dca80aa67da4b502de7d610fa34ef1`. The focused batch ran from 2026-10-13T17:37:34.676Z to 2026-10-13T17:45:28.412Z.

No generator sample reported quota throttling. Some target GET runs hit the two-core ceiling; the highest run throttled in 94% of cgroup periods. That fraction is not lost wall time. Generator CPU reached about 151%, and fixed concurrency/one Node generator can still constrain observed throughput. See [resource analysis](regression/analysis.json) and each raw sample.

These are warmed, bounded-set workflows. Native filesystem cases use ephemeral overlay storage and S3 cases use a persistent volume. Lane ratios do not isolate HTTP cost. There is no real scanner, DPI parsing, flagged-file promotion, TLS, cold-cache test, power-loss test or sustained-arrival qualification here. Quarantine counts incoming payload once per completed workflow.

The [full validation receipt](verification/receipt.json) and [log](verification/standards.log) record `make standards`, all four conformance lanes, `make test-node test-wasm` and protobuf schema drift verification. The verifier checked the complete uploaded source hash before running tests. Tests ran without root privileges; native DNS used Debian's localhost resolver. The [generated-file manifest](verification/generated-change.json) confirms only the SDK WASM changed; the rebuilt artifact is saved in the tracked SDK output.

The protobuf dependency increased the SDK WASM from 6,432,495 to 9,800,798 bytes
(6.13 to 9.35 MiB). Agent cold-start measurements for the rebuilt Node/WASM host
are part of the subsequently requested competitor comparison. Network download
and browser compilation costs are not established by the S3 throughput results.

Verification deployment `b4d2e2fa-6a58-4081-ae89-b4748392f749` reached `SUCCESS`. The target deployment was stopped after evidence export and read back as `REMOVED`. The runner was retained for the subsequently requested expanded competitor measurements and website charts. No Go or Docker build ran locally.
