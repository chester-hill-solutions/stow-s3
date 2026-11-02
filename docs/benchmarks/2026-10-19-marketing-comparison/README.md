# Refreshed marketing comparisons — October 19, 2026

The published v0.3.3 build was compared against pinned MinIO, S3Proxy and
RustFS versions on Railway. All builds, tests, workloads, analysis and chart
verification ran on Railway. The website dataset now contains these results.

The measured revision is `f307914f141a`. Its product
source is byte-identical to the published `v0.3.3` tag; the difference is
documentation, the CI workflow and the benchmark module's dependency
metadata. The previous dataset was measured on `e7140fde9fc6`.

These are exploratory development measurements on one host. They do not
establish broad performance superiority or sustained production capacity.

## Filesystem results

Same persistent volume, one server at a time, 2 CPU / 2 GiB ceilings. Three
20-second windows after five-second warmups, 1 MiB objects, four workers and
16 retained fixtures. Rates are medians. RSS is peak sampled server RSS during
the respective workload, not allocated bytes per operation.

| Product | PUT MiB/s | GET MiB/s | Quarantine files/s | PUT RSS MiB | GET RSS MiB |
|---|---:|---:|---:|---:|---:|
| MinIO | 65.60 | 467.75 | 65.35 | 267.906 | 363.387 |
| RustFS | 65.35 | 497.75 | 46.25 | 193.457 | 179.840 |
| S3Proxy | 66.70 | 428.65 | 104.45 | 346.336 | 340.996 |
| Stow | 66.50 | 467.65 | 56.15 | 26.738 | 21.594 |

## Memory backend results

Same workload settings, with objects retained in RAM rather than on disk.

| Product | PUT MiB/s | GET MiB/s | Quarantine files/s | PUT RSS MiB | GET RSS MiB |
|---|---:|---:|---:|---:|---:|
| S3Proxy | 185.90 | 441.60 | 104.55 | 533.613 | 340.938 |
| Stow | 167.50 | 461.85 | 102.90 | 65.047 | 49.660 |

## Server startup and first write

Nine fresh processes per configuration, empty prepared data directories,
binaries already present. Startup ends on authenticated readiness, with 10 ms
polls. First write extends that clock through bucket creation and a 4 KiB PUT
over target loopback. This measures storage server startup, not complete agent
startup, deployment, image downloads or browser/WASM initialization.

| Product/backend | Ready median ms | First write median ms |
|---|---:|---:|
| Stow memory | 18.41 | 22.32 |
| S3Proxy memory | 1091.32 | 1226.19 |
| Stow filesystem | 28.11 | 44.41 |
| S3Proxy filesystem | 1020.42 | 1129.23 |
| MinIO filesystem | 248.48 | 263.40 |
| RustFS filesystem | 124.22 | 180.32 |

## Scope and validation

The S3 products expose equivalent-shaped blob operations, with different
durability and feature guarantees. Filesystem Stow uses durable record
publication; S3Proxy's filesystem adapter differs. RustFS uses a single drive
with strict durability. Replication, failover and complete S3 feature parity
are unmeasured.

Quarantine is PUT, SHA-256/length-verified GET, a two-millisecond simulated
scanner delay, verdict PUT and two deletes. It does not measure actual scanning,
retention of flagged content, promotion, TLS or cold caches. PUT overwrites
bounded worker keys; GET cycles warm fixtures. These are exploratory
observations on one host, without confidence intervals or a sustained-capacity
qualification.

All 54 S3 cases completed with 0 failed
and 0 dropped workflows, verified data, matching build
identities and maintained disk headroom. The generator recorded
0 throttled periods and a maximum
average generator CPU of 153.0%.

## Change against the previous batch

The two batches are separate runs, so the differences below are observations
and not a controlled paired causal estimate. The previous dataset is retained
beside the new one.

| Product | Workload | Before MiB/s | After MiB/s | Observed change | Before RSS MiB | After RSS MiB |
|---|---|---:|---:|---:|---:|---:|
| Stow memory | put | 180.15 | 167.50 | -7.0% | 62.980 | 65.047 |
| S3Proxy memory | put | 192.65 | 185.90 | -3.5% | 435.063 | 533.613 |
| Stow filesystem | put | 66.50 | 66.50 | +0.0% | 26.898 | 26.738 |
| S3Proxy filesystem | put | 66.70 | 66.70 | +0.0% | 330.270 | 346.336 |
| MinIO filesystem | put | 65.55 | 65.60 | +0.1% | 274.117 | 267.906 |
| RustFS filesystem | put | 65.30 | 65.35 | +0.1% | 191.328 | 193.457 |
| Stow memory | get | 480.70 | 461.85 | -3.9% | 51.516 | 49.660 |
| S3Proxy memory | get | 464.85 | 441.60 | -5.0% | 327.715 | 340.938 |
| Stow filesystem | get | 484.15 | 467.65 | -3.4% | 21.117 | 21.594 |
| S3Proxy filesystem | get | 439.75 | 428.65 | -2.5% | 319.477 | 340.996 |
| MinIO filesystem | get | 478.55 | 467.75 | -2.3% | 372.023 | 363.387 |
| RustFS filesystem | get | 539.35 | 497.75 | -7.7% | 175.492 | 179.840 |
| Stow memory | quarantine | 112.45 | 102.90 | -8.5% | 61.148 | 60.199 |
| S3Proxy memory | quarantine | 111.85 | 104.55 | -6.5% | 397.855 | 411.387 |
| Stow filesystem | quarantine | 61.35 | 56.15 | -8.5% | 24.863 | 24.996 |
| S3Proxy filesystem | quarantine | 109.40 | 104.45 | -4.5% | 391.043 | 414.363 |
| MinIO filesystem | quarantine | 65.45 | 65.35 | -0.2% | 380.805 | 386.902 |
| RustFS filesystem | quarantine | 50.30 | 46.25 | -8.1% | 210.066 | 206.980 |

## Evidence

- [Marketing dataset](agent-storage.json), [previous dataset](previous-agent-storage.json)
  and [separate-batch observations](previous-comparison.json).
- [Complete run](comparison/), [analysis](comparison/analysis.json),
  [S3 suite](comparison/s3/suite.json) and [S3 report](comparison/s3/report.md).
- [Build identity](build-identity.json) and the source payload it records.
- [Refresh controller](refresh.mjs), [controller status](status.json),
  [run log](refresh.log) and [chart verification](preview/receipt.json).

Comparison run: `403b5fff-5648-49f7-b478-899eecf31182`.
Smoke run: `f2911721-b6ab-4637-829c-8855c4cfb58d`.
Comparison finished at 2026-10-19T19:49:22.375Z; chart verification finished at
2026-10-19T19:57:20.506Z.
Source payload SHA-256: `b34705cc5c076a13f47753a79c3a8a31d9e8de50fdd5f4cd4a00fdc092b6b277`.
Chart source archive: 7359873 bytes, SHA-256 `7086d6146460dbec020ea7e672798761315debc3ac5bcb91ee86bca37fcaa7c3`.

The runner and the target are two services in the Railway `benchmark`
environment of the `stow-conformance-scratch` project, and they use the
existing resource profiles in `benchmark/.railway/railway.ts`. No new services
or volumes were created.
