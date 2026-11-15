# Bounded same-store transfer continuation

W06.B failure conformance and W06.C exploratory cost evidence, collected on
November 14, 2026 (America/Toronto; batch November 15, 00:36:06–00:36:09 UTC).
All 24 cloud samples passed. Production code and the consumer-owned example are
unchanged. This is a small-fixture accounting result, not a capacity claim.

## Source and environment

- Tested source: `f8c0b2c5893a37ea97117614394fc53c261b80e0`, clean when packaged.
- Source payload SHA-256: `49fd7f8c2a53880d8b5ce73d6279e5fb411b541b1e46c5c15cba083d750a325c`.
- Test binary SHA-256: `ed3f4f1ff863d492f442de5a9b5d165aedf4f4b8188ed41a297f6f702a96ae0f`.
- Go 1.25.6, Linux amd64, CGO disabled; Docker bases are pinned in the
  [recipe](../../../benchmark/docker/resume-transfer.Dockerfile).
- Existing Railway benchmark runner, deployment
  `e4516806-40d1-459c-b798-10e86c443295`; observed `SUCCESS` before execution.
  Effective cgroup ceilings: 4 vCPU (`400000 100000`), 4 GiB RAM. Host model:
  AMD EPYC 9655P; host CPU count does not describe this container's allocation.
- The S3 server, SDK client, fixtures and verifier share each Go test process.
  Each of the 24 samples starts a fresh process. Storage is the container's
  ephemeral overlay filesystem, not the Railway persistent result volume.
- Raw [host metadata](raw/host.json), [resource counters](raw/batch-resources.json)
  and [summary with every sample](raw/summary.json) retain the observations.
  There was no increase in CPU throttling or memory pressure/OOM counters during
  the batch. Source/binary/deployment and export receipts are in
  [execution.json](execution.json).

## Workload and recovery

Each object is exactly 5,243,197 bytes: 5 MiB of `A`, then 317 bytes of `B`.
Every final object/file is byte-verified and has SHA-256
`6af7f69a62e921b301ace3584ea2727e0c34d2799f9276eccf172fcd5bb99a85`.
The consumer transfer ceiling is 32 MiB. Ordinary AWS SDK Go v2 requests use the
repository lockfile and a one-attempt retry setting; this is the local SigV4
endpoint profile, with synthetic credentials and no presigned links.

Uploads use two parts, 5 MiB and 317 bytes. The harness loses the first part's
reply, reopens retained servers, reconciles server-listed parts, then loses the
completion reply and resolves the completed object after another reopen. Each
part is sent once and completion is attempted once: payload lengths sum to
5,243,197 bytes, with zero part payload resend. These counters describe decoded
UploadPart request lengths, excluding HTTP framing and TCP retransmission.
Memory replaces the client while keeping its volatile server running.

Downloads use six ranges, five of 1 MiB and a final 317 bytes. After the first
verified range, filesystem/runtimefs/workspace stop and reopen the server/store
in the same Go test process, retaining their directories. Memory replaces only
the client. Every request retains the Start validator and checks exact Range,
If-Match, 206, Content-Range and length. Each sample consumes 5,243,197 range-body
bytes with six GETs and no prefix resend or range retry.

Separate W06.B [failure conformance](../../../conformance/resume_download_failure_test.go)
passes on all three retained profiles. Corrupt/truncated prefixes and
checksum-corrupt/truncated-JSON progress refuse before another GET, preserving
their inputs. A body-reader error after 65,536 bytes of the second range leaves
the saved 1 MiB prefix and progress byte-identical. A fresh client after another
reopen retries exactly that range with the original If-Match and completes.
Seven requests consume 5,308,733 payload bytes: the object plus the failed 64 KiB.
That is client-consumed body accounting; transport prefetch and server/TCP egress
are unmeasured. Existing overwrite/412 and memory-loss refusal cases remain.

## Observed costs

Cells show median [minimum–maximum] across three samples. MiB means 1,048,576
bytes. Go allocation totals are cumulative allocations during transfer and final
verification, after fixture/server setup (and download seed); they combine
client and server and are **not peak heap or Stow-only memory**. Process high-water
RSS includes setup. Elapsed time covers the same transfer/verification workflow,
including reopen/reconciliation, excluding build/deploy and setup.

| Direction | Profile | Go allocated MiB | Process RSS MiB | Backend allocated file bytes, median | Elapsed ms |
|---|---|---|---|---|---|
| Download | memory | 31.04 [31.03–31.05] | 39.52 [39.47–39.61] | 0 | 48 [47–53] |
| Download | filesystem | 31.11 [31.11–31.12] | 37.89 [37.87–38.55] | 5,259,264 | 50 [50–50] |
| Download | runtimefs | 31.11 [31.10–31.12] | 38.23 [37.80–38.27] | 5,259,264 | 51 [50–63] |
| Download | workspace | 31.20 [31.18–31.20] | 37.77 [37.46–38.25] | 5,255,168 | 94 [93–97] |
| Upload | memory | 60.48 [60.46–60.49] | 49.82 [49.62–51.99] | 0 | 66 [64–68] |
| Upload | filesystem | 70.76 [70.75–70.77] | 49.28 [45.83–49.45] | 5,259,264 | 118 [116–316] |
| Upload | runtimefs | 106.30 [106.17–106.32] | 62.45 [62.09–66.45] | 5,259,264 | 145 [120–162] |
| Upload | workspace | 65.78 [65.78–65.82] | 50.27 [49.92–52.07] | 5,255,168 | 123 [123–124] |

Disk columns sum final regular-file blocks (`st_blocks × 512`) under the backend
root. They exclude directories, the source fixture and client files, and do not
measure peak staging or physical-device writes. Download output plus progress
uses 5,243,629 apparent / 5,251,072 allocated file bytes; upload progress is 411
apparent bytes. Raw records retain apparent backend sizes and cumulative Linux
process read/write-byte counters. Those counters include setup and client/source
I/O; they cannot isolate server write amplification. Reads are cache-warm.

Even this small fixture allocates substantially more bytes over its lifetime
than the object size. These measurements provide a baseline for a named consumer;
they do not establish large-media suitability, sustainable throughput, cold-disk
performance, TLS/separate-host capacity, power-loss safety, portable unfinished
transfers, signed-link expiry, live AWS/R2 behavior or physical second-host use.

## Reproduce and verify

Use the [Railway-only probe recipe](../../../benchmark/README.md#resumable-transfer-cost-probe).
It refuses an existing output bundle/file. Export raw evidence before stopping
the task-owned deployment; keep existing result volumes. No measurement was run
locally. [SHA256SUMS](raw/SHA256SUMS) identifies every raw export file.

Local macOS ARM64 correctness checks passed: focused resume/example tests, full
conformance, focused download races and the full example race suite. Linux amd64
cross-compilation includes the opt-in cost tests. The unchanged Go quality and
file-size gates, 21 benchmark-tool tests and whitespace checks pass. The
[check logs](checks/) and [independent exported-data verification](checks/evidence-verification.json)
retain these results; CI qualification is recorded with the associated PR.
Opt-in cost tests skip in ordinary CI; only this Railway receipt qualifies costs.
The [canonical plan](../../development/storage-foundation-plan.md#next-milestone-supported-continuation)
owns the remaining consumer/host work.
