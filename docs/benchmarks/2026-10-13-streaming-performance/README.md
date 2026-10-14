# Streaming and quarantine optimization — October 13, 2026

The code changes improve disk reads and quarantine, reduce disk write allocations,
and lower sampled server memory. They do not establish an across-the-board speedup:
memory HTTP PUT and quarantine measured lower in this batch. All builds, tests,
workloads and performance analysis ran on Railway. The website remains unpublished.

## S3 results

Same workload and CPU limits as the [earlier competitor batch](../2026-10-13-competitor-performance/README.md):
1 MiB objects, four workers, 16 retained fixtures, three 20-second windows after
five-second warm-ups. Target: 2 CPU / 2 GiB and the same persistent data volume.
Generator: 4 CPU / 4 GiB. Both report AMD EPYC 9655P. Private HTTP, no TLS.
PUT overwrites a bounded key set; GET uses warm fixtures. Quarantine includes a
verified read, a 2 ms simulated scanner delay, a verdict write and two deletes.

| Backend / workflow | Before | After | Rate change | p99 ms before → after | Sampled RSS MiB before → after |
|---|---:|---:|---:|---:|---:|
| Memory PUT | 197.35 MiB/s | 174.70 MiB/s | −11.5% | 38.075 → 42.480 | 68.598 → 66.910 |
| Disk PUT | 66.45 MiB/s | 66.50 MiB/s | +0.1% | 93.234 → 101.969 | 33.367 → 28.273 |
| Memory GET | 505.60 MiB/s | 510.95 MiB/s | +1.1% | 18.415 → 18.973 | 67.195 → 51.676 |
| Disk GET | 356.95 MiB/s | 492.70 MiB/s | +38.0% | 29.105 → 19.354 | 30.203 → 20.988 |
| Memory quarantine | 113.95 files/s | 108.55 files/s | −4.7% | 51.833 → 53.938 | 69.012 → 61.211 |
| Disk quarantine | 50.35 files/s | 56.65 files/s | +12.5% | 124.422 → 119.567 | 30.051 → 25.629 |

Rates and p99 values are medians across repetitions; RSS is the maximum sampled
server RSS for that workload. This is a cross-run exploratory comparison on shared
cloud hardware. Competitors were not remeasured. It cannot establish sustained
capacity, real DPI/scanner performance, cold-cache behavior, or distributed durability.

All 18 S3 cases and 18 selected native samples passed. Measured and warm-up
workflows had zero failures or drops; payload length and SHA-256 checks passed;
quarantine left no worker objects. Minimum sampled disk headroom was 4,685.91 MiB.
The generator had no quota-throttled periods. The largest target throttled-period
fraction was 4.5%, compared with 92% in the earlier Stow disk GET cases. A throttled
period fraction does not measure lost wall time.

Native disk PUT allocated bytes/op fell from 2,217,360 to 1,112,601; disk quarantine
fell from 2,305,756 to 1,156,305, both about 50%. Native disk GET rose from 3,667.84
to 4,806.93 MiB/s. Native memory PUT was slightly faster (809.98 → 823.96 MiB/s),
while native memory GET was lower (7,425.23 → 6,817.83 MiB/s). These short native
windows changed from 300 ms to 500 ms and use ephemeral runner storage.
Allocated bytes/op are separate from retained memory and RSS.

The lower memory HTTP PUT result is unresolved. Its repetitions do not overlap
the earlier range, so it should not be dismissed as noise. Native memory PUT did
not show the same drop. The generator averaged approximately one CPU during both
PUT batches; that suggests examining client/transport costs, but does not isolate
a cause. A controlled alternating before/after run is needed to attribute it.

## Implementation and guarantees

- Native S3 GET returns the captured object stream, with independent readers able
  to read concurrently. Chunked copies still check context, runtime lifecycle and
  policy. Seekable readers retain seek support for ranges.
- Managed filesystem usage updates incrementally. Opening, explicit refresh,
  changed store revisions and uncertain mutations reconcile usage; generic stores
  and editable workspaces retain conservative refresh behavior. Raw filesystem
  edits require explicit `RefreshUsage` under the managed-store contract.
- Ordinary records write the protobuf header and owned payload separately into
  one atomic file. Capacity-bound publication retains its admission path. Every
  publication still syncs the file and immediate parent; successfully fenced
  ancestor directories are cached for the managed handle and invalidated on removal.
- HTTP body reads use bounded size hints. Built-in stores compute/validate PUT
  checksums once; unsupported adapters retain the existing handler fallback.

The new tests cover lazy adapter reads, snapshot seeking, concurrent reads,
close waiting for active reads, policy revocation between copied chunks,
incremental accounting, direct store mutations, uncertain committed writes,
and requested PUT checksums. Existing recovery, guard, permission and conformance
checks also passed. See [ADR 0020](../../adr/0020-binary-object-records.md).

The regenerated SDK WASM is 6,480,771 bytes (6.18 MiB), up 45,417 bytes from the
previous size-reduced artifact. SHA-256:
`2e9fae1653da71245e2342adcaeafa39cc44391ddc72ddf7ff614e71688fd2a8`.
No new startup or first-write timing was measured by this regression profile.

## Evidence

- [Cloud comparison and ranges](regression/comparison.json), [latency and RSS table](regression/comparison.md).
- [Raw S3 index](regression/s3/suite.json), [S3 report](regression/s3/report.md), [resource analysis](regression/analysis.json).
- [Native throughput and allocations](regression/embedded-report.md), [completion receipt](regression/run.json).
- [Cloud comparison source](regression/compare-regression.mjs).
- [Full validation receipt](verification/receipt.json), [validation log](verification/standards.log), [generated artifact hashes](verification/generated-change.json).

Validation deployment `aa2bded5-b990-45c3-a193-be4995b3eacb` reached `SUCCESS`:
`make standards`, all conformance lanes, Node/WASM tests, 33 browser tests
(two optional skips), protobuf drift validation, and 13 chart combinations.
The complete validation input hash was
`ac6e2b0067ebd27e3fec335e642e46d23e64b97140d5d72b630e5826e261cbf8`.
The generated WASM and formatting-only Go reference update were saved back to
the checkout after validation.

Benchmark build source hash:
`ea612bb6ac196c85af211b7d34942369be08a88fe170d7e9bd6d47896d27e1db`.
Runner deployment: `1759d12f-2b45-4cd7-a5ce-8ab2b53f1bae`.
Target deployment: `3fc51368-d9f8-4351-9e36-9327b3a47f2c`.
Both roles had matching source, native executable, harness and SDK hashes.
The measured run was `128a9a91-501f-45d9-9211-af0eddf7c083`.
Temporary compute was stopped after export; service status is recorded in the
[environment manifest](../railway-environment.json).
