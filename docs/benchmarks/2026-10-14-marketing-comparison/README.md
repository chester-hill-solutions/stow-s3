# Refreshed marketing comparisons — October 14, 2026

The optimized Stow code was rebuilt and compared against pinned MinIO, S3Proxy
and RustFS versions on Railway. All builds, tests, workloads, analysis and chart
verification ran on Railway. The local website dataset now contains these results;
the website has not been published. The private preview is in
[preview/preview.html](preview/preview.html).

Startup and sampled server memory remain the clearest comparative strengths.
Filesystem download rates were roughly level with MinIO in this resource-limited
batch, while RustFS remained faster. Stow disk quarantine trailed MinIO and
S3Proxy, and exceeded RustFS. Upload rates clustered closely. These observations
do not establish broad performance superiority or sustained production capacity.

## Filesystem results

Same persistent volume, one server at a time, 2 CPU / 2 GiB ceilings. Three
20-second windows after five-second warmups, 1 MiB objects, four workers and
16 retained fixtures. Rates are medians. RSS is peak sampled server RSS during
the respective workload, not allocated bytes per operation.

| Product | PUT MiB/s | GET MiB/s | Quarantine files/s | PUT RSS MiB | GET RSS MiB |
| --- | ---: | ---: | ---: | ---: | ---: |
| Stow | 66.50 | 484.15 | 61.35 | 26.898 | 21.117 |
| S3Proxy | 66.70 | 439.75 | 109.40 | 330.270 | 319.477 |
| MinIO | 65.55 | 478.55 | 65.45 | 274.117 | 372.023 |
| RustFS | 65.30 | 539.35 | 50.30 | 191.328 | 175.492 |

Stow GET ranges were 466.85–485.05 MiB/s; MinIO ranges were 477.30–486.25.
Their observed ranges overlap, so the slightly higher Stow median is not evidence
of a general speed advantage. MinIO GET used about 195–197% process CPU and
was throttled in 68.5–80.6% of sampled scheduling periods at the configured
two-CPU ceiling. Results describe this resource profile, not unrestricted speed.
The generator recorded no throttled periods; its maximum average CPU across
cases was 151.839%, below its four-CPU ceiling.

## Memory backend results

Same workload settings, with objects retained in RAM rather than on disk.

| Product | PUT MiB/s | GET MiB/s | Quarantine files/s | PUT RSS MiB | GET RSS MiB |
| --- | ---: | ---: | ---: | ---: | ---: |
| Stow | 180.15 | 480.70 | 112.45 | 62.980 | 51.516 |
| S3Proxy | 192.65 | 464.85 | 111.85 | 435.063 | 327.715 |

The memory GET and quarantine observed ranges overlap. Stow's lower sampled
RSS is clear in this batch; a broad throughput advantage is not established.

## Server startup and first write

Nine fresh processes per configuration, empty prepared data directories,
binaries already present. Startup ends on authenticated readiness, with 10 ms
polls. First write extends that clock through bucket creation and a 4 KiB PUT
over target loopback. This measures storage server startup, not complete agent
startup, deployment, image downloads or browser/WASM initialization.

| Product/backend | Ready median ms | First write median ms |
| --- | ---: | ---: |
| Stow memory | 18.33 | 22.68 |
| Stow filesystem | 26.12 | 41.18 |
| S3Proxy memory | 1,013.79 | 1,163.96 |
| S3Proxy filesystem | 953.93 | 1,074.56 |
| MinIO filesystem | 244.90 | 257.88 |
| RustFS filesystem | 110.50 | 154.16 |

## Scope and validation

The S3 products expose equivalent-shaped blob operations, with different
durability and feature guarantees. Filesystem Stow uses durable record
publication; S3Proxy's filesystem adapter differs. RustFS uses a single drive
with strict durability. Replication, failover and complete S3 feature parity
are unmeasured.

Quarantine is PUT, SHA-256/length-verified GET, a two-millisecond simulated
scanner delay, verdict PUT and two deletes. It does not measure actual scanning,
retention of flagged content, promotion, TLS or cold caches. PUT overwrites
bounded worker keys; GET cycles warm fixtures. These are exploratory observations
on one host, without confidence intervals or a sustained-capacity qualification.

All six S3 configurations passed the smoke profile. The full comparison completed
54 S3 cases, with zero failed or dropped workflows, verified data, matching build
identities and maintained disk headroom. Diagnostic baselines also completed
486 native samples, 360 Go startup samples and 60 Node startup samples; they
are excluded from the marketing dataset. Site build and unit tests passed.
Railway Chromium verified 13 chart combinations and produced six desktop/mobile
previews with no browser errors. Compute was stopped after checksum-verified export.

## Evidence

- [Marketing dataset](agent-storage.json), [previous dataset](previous-agent-storage.json)
  and [separate-batch observations](previous-comparison.json). The latter are not
  paired causal estimates; competitor rates also changed between batches.
- [Complete run](comparison/run.json), [analysis](comparison/analysis.json),
  [S3 suite](comparison/s3/suite.json), [S3 report](comparison/s3/report.md)
  and individual raw cases in `comparison/s3/`.
- [Source manifest](source.json), [build identity](build-identity.json),
  [chart source checksum](chart-source.json) and [export checksum](export.json).
- [Cloud refresh controller](refresh.mjs), [controller status](status.json),
  [site/build/browser log](refresh.log) and [chart verification](preview/receipt.json).

Source payload SHA-256:
`bb32c46b5247d6f72192d2d381f70495a9de5a4d80eefbc3ebb3bbbffb398228`.
Comparison run: `639202b1-4c59-44ab-8567-142e764b4a18`.
Smoke run: `07f0b443-570f-4cca-9cbd-79ecb3af8a65`.
Target deployment: `e09d0550-2eaf-454b-883d-16a3d38e2ccb`.
Generator deployment: `23fab8fe-35c5-4e0a-857e-e76df896dd6b`.
Retained cloud root: `/results/marketing-refresh-20261014`.
Comparison finished at 2026-10-14T18:39:30.187Z; chart verification finished
at 2026-10-14T18:39:55.268Z. Both services use the existing Railway IaC resource
profiles in `benchmark/.railway/railway.ts`; no new services or volumes were created.
