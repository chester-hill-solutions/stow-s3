# Competitor performance — October 13, 2026

Stow has a credible compact-server and fast native-startup story. It does not
lead filesystem download throughput or disk quarantine throughput. Product
comparisons cover S3-shaped stores: Stow, MinIO, S3Proxy and RustFS. SQLite,
Afero and buffers are internal engineering baselines, not equivalent agent-storage
products. Keep broad performance claims on hold. Nothing was published
to the website; the charts are prepared for private review.

All builds, tests, workloads and analysis for this batch run on Railway. The
complete comparison ran from 19:55:06 to 20:22:58 UTC. The measured source payload
is `8289f7bec9339e6e1ea26d70ad6eea40f99ef1be657d75c0762edb7981463447`.
Both roles have identical source, executable, harness and WASM hashes.

## S3 filesystem results

One target at a time, 2 vCPU / 2 GiB, the same persistent volume, private HTTP
without TLS. Each row uses 1 MiB payloads, four workers, 16 retained fixtures,
three 20-second runs after five-second warm-ups. Throughput is the median;
memory is the maximum sampled server RSS during that workload.
PUT repeatedly overwrites one key per worker; GET cycles the retained fixtures.
This is a warm bounded-set workload, not an expanding stream of unique objects.

| Product | PUT MiB/s | GET MiB/s | Quarantine files/s | PUT RSS MiB | GET RSS MiB |
|---|---:|---:|---:|---:|---:|
| Stow | 66.45 | 356.95 | 50.35 | 33.367 | 30.203 |
| S3Proxy | 66.70 | 417.45 | 123.00 | 398.246 | 379.828 |
| MinIO | 65.50 | 536.70 | 65.40 | 275.219 | 365.547 |
| RustFS | 65.35 | 559.90 | 53.35 | 199.105 | 184.012 |

Uploads cluster around 65–67 MiB/s; this setup does not establish a meaningful
upload-speed advantage. Stow's sampled RSS is much lower. Its GET rate is about
one-third below MinIO and RustFS, and disk quarantine trails MinIO. S3Proxy's
filesystem adapter has different durability behavior; these products do not
provide identical guarantees.

## S3 memory results

Same payload, concurrency and repetitions; no persistent object storage.

| Product | PUT MiB/s | GET MiB/s | Quarantine files/s | PUT RSS MiB | GET RSS MiB |
|---|---:|---:|---:|---:|---:|
| Stow | 197.35 | 505.60 | 113.95 | 68.598 | 67.195 |
| S3Proxy | 223.55 | 446.70 | 120.05 | 559.574 | 312.406 |

Stow reads faster in this batch and uses less sampled server memory, while
memory uploads and quarantine are slower than S3Proxy.

## Server startup and first write

Fresh server processes, empty prepared data directories, binaries already
present. Nine launches per product. Startup ends on authenticated readiness
(10 ms polls); first-write time extends the same clock through bucket creation
and a 4 KiB PUT over target loopback. Verification and cleanup follow the timer.

| Product/backend | Ready median ms | First write median ms |
|---|---:|---:|
| Stow memory | 18.88 | 22.10 |
| Stow filesystem | 26.77 | 41.22 |
| S3Proxy memory | 994.02 | 1,148.97 |
| S3Proxy filesystem | 992.10 | 1,085.88 |
| MinIO filesystem | 208.97 | 221.93 |
| RustFS filesystem | 104.78 | 151.49 |

This is a useful native-server startup result. It does not include image pulls,
deployments, agent orchestration or cold OS caches.

## Internal diagnostic baselines

These measurements quantify implementation costs. They are excluded from the
agent-storage product charts because buffers, files and SQLite do not provide
an equivalent S3-shaped blob-store API. Their results remain available for
regression tracking and optimization.

### Fresh Node processes

Ten fresh processes per product and payload. Includes Node launch, payload
preparation, selected package imports, WASM loading/compilation where used,
namespace/schema setup and first PUT. Parent-observed ready/write messages end
the timers. Peak process RSS includes the subsequent content verification.

| Implementation, 4 KiB | Ready median ms | First write median ms | Maximum process RSS MiB |
|---|---:|---:|---:|
| Stow WASM | 127.10 | 129.57 | 125.85 |
| Owned Node buffers | 23.25 | 23.25 | 45.86 |
| Node SQLite | 24.50 | 24.52 | 47.30 |

The current Stow WASM path has measurable loading and process-memory costs to
improve. These simpler primitives do not establish how Stow performs against
an equivalent S3-shaped agent-storage product.
The measured WASM is 9,800,798 bytes (9.35 MiB), SHA-256
`f4da0cecaa4d19307b77314be57a5b3cf50d9b784c175baead6b0e0a6a4fc2b0`.
No before/after Node startup measurement attributes this entire gap to protobuf.

### Embedded Go instances

Fresh instances inside a running host, CPU setting 1, twenty samples per case.
Prepared directories and payload generation are outside the clock; opening,
namespace/schema setup and the first PUT are included. These are not fresh
process measurements. Selected 4 KiB first-write medians:

| Implementation/backend | First write µs |
|---|---:|
| Owned Go buffers | 0.44 |
| Afero memory | 1.29 |
| Stow memory | 6.935 |
| SQLite memory | 49.479 |
| Temporary files + rename | 20.73 |
| Afero filesystem | 11.12 |
| Files + rename + fsync | 523.830 |
| SQLite filesystem, WAL + FULL | 1,755.942 |
| Stow filesystem | 2,054.860 |

Stow memory opens and writes quickly relative to SQLite, but raw adapters cost
less. Native throughput and allocation results include all nine adapters,
4 KiB / 1 MiB / 8 MiB payloads, CPU settings 1 and 4, and three 300 ms repetitions.
Stow's ETag hashing, object metadata and quota work remain included; raw adapters
do not reproduce those features. Go heap allocation excludes SQLite's C
allocations and is separate from total or retained memory.

## Evidence and limits

All 54 S3 runs, 486 native samples, 360 Go startup samples and 60 Node processes
completed. Reads and final-write checks use SHA-256 and length verification;
there were zero measured/warm-up failures or drops, no leftover worker objects,
and sufficient disk headroom. The independent smoke covered 18 S3 runs, 162
native samples, 54 Go startup samples and 12 Node processes.

The generator reported no quota-throttled periods; its highest average CPU
usage was 154.838%. Some target runs reached the two-core ceiling, with up to
92% of cgroup periods throttled. That fraction is not lost wall time. A fixed
concurrency, one Node generator and shared cloud hardware can constrain rates.

Quarantine is PUT → verified GET → simulated verdict PUT → two deletes. S3
includes a 2 ms simulated scanner delay; Go does not. Native GET repeatedly
reads one seed object, and native PUT overwrites one key per worker. Real scanning, DPI parsing,
flagged-file retention/promotion, TLS, replication, failover, cold caches and
sustained-arrival capacity are unmeasured. Native filesystem cases use ephemeral
runner storage; S3 cases use the persistent target volume. Compare within lanes.

Versions: Go 1.25.6, Node 25.9.0, Afero 1.15.0, go-sqlite3 1.14.52
(SQLite 3.53.4), Node SQLite 3.51.3, S3Proxy 4.1.1, MinIO
`RELEASE.2025-10-15T17-29-55Z`, RustFS 1.0.1. RustFS uses one drive with strict
durability; SQLite disk uses WAL and FULL sync. Distributed behavior is unmeasured.

See the [completion receipt](comparison/run.json), [analysis](comparison/analysis.json),
[S3 ranges/latency/RPS/memory](comparison/s3/report.md),
[full native table](comparison/embedded-report.md),
[Go startup samples](comparison/agent.txt), [Node samples](comparison/node-agent.json),
[historical combined diagnostic dataset](comparison/website.json), and
[smoke analysis](smoke/analysis.json). The [runner methods](../../../benchmark/README.md)
describe measurement boundaries and reproduction.

## Original combined preview validation

This receipt describes the earlier combined preview. The S3-only product
preview is verified separately; the original evidence remains intact.

The [Railway validation receipt](verification/receipt.json) records repository
standards, all conformance lanes, Node/WASM tests, SDK browser tests and protobuf
generation checks. SDK browser tests passed 33 cases; the optional 1 GiB workload
and direct-provider live qualification were skipped. The
[chart preview receipt](verification/preview-receipt.json) records 39 chart
combinations, eight desktop/mobile screenshots and no browser errors. The
[generated-output manifest](verification/generated-change.json) confirms no SDK
output changed and the measured WASM hash still matches.

Verification deployment `fce13738-fcb4-44e8-b115-c1ffbe53cfa5` reached `SUCCESS`.
The target and runner were both stopped after export and read back as `REMOVED`;
their result/data volumes remain available. Private HTML and screenshots are
kept outside the source tree. The preview was visually inspected at desktop and
mobile sizes. No website publication was performed.


## S3-only agent-storage preview

The product charts now compare only Stow, MinIO, S3Proxy and RustFS. Filesystem
and memory cases remain separate; all measured peers appear even where Stow
trails. Startup and first write use the fresh S3 server measurements above.
The Go/Node/SQLite/buffer results remain internal diagnostics in this report.

The [S3-only chart dataset](comparison/agent-storage.json) was derived on Railway
from the existing completed batch. No workloads were rerun. The
[product-preview receipt](verification-product/receipt.json) and
[Chromium receipt](verification-product/preview-receipt.json) record the site
build, three passing site tests, documentation links, 13 chart checks, six
screenshots and zero browser errors. Deployment
`b37d3d8d-2ae7-4a8f-839d-529330a1e38a` reached `SUCCESS`; both services were read
back as `REMOVED` after export. The revised private preview was visually
inspected at desktop and mobile sizes. No website publication was performed.
