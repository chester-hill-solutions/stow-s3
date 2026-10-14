# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

1 repetitions, 0.5 s windows, 0 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory | put | 1500 | 5.859–5.859 | 1500 | 4.017 | 18.875 | 0 / 0 | true |
| s3proxy-memory | put | 286 | 1.117–1.117 | 286 | 46.459 | 190.891 | 0 / 0 | true |
| stow-filesystem | put | 214 | 0.836–0.836 | 214 | 17.006 | 18.859 | 0 / 0 | true |
| s3proxy-filesystem | put | 320 | 1.25–1.25 | 320 | 45.093 | 185.441 | 0 / 0 | true |
| minio-filesystem | put | 216 | 0.844–0.844 | 216 | 17.176 | 165.051 | 0 / 0 | true |
| rustfs-filesystem | put | 124 | 0.484–0.484 | 124 | 20.988 | 157.984 | 0 / 0 | true |
| stow-memory | get | 2442 | 9.539–9.539 | 2442 | 2.324 | 18.77 | 0 / 0 | true |
| s3proxy-memory | get | 402 | 1.57–1.57 | 402 | 42.059 | 191.156 | 0 / 0 | true |
| stow-filesystem | get | 2088 | 8.156–8.156 | 2088 | 2.951 | 19.25 | 0 / 0 | true |
| s3proxy-filesystem | get | 428 | 1.672–1.672 | 428 | 38.075 | 181.324 | 0 / 0 | true |
| minio-filesystem | get | 2300 | 8.984–8.984 | 2300 | 3.601 | 253.418 | 0 / 0 | true |
| rustfs-filesystem | get | 2948 | 11.516–11.516 | 2948 | 2.37 | 157.258 | 0 / 0 | true |
| stow-memory | quarantine | 340 | 1.328–1.328 | 1702 | 10.237 | 18.68 | 0 / 0 | true |
| s3proxy-memory | quarantine | 72 | 0.281–0.281 | 362 | 55.613 | 199.844 | 0 / 0 | true |
| stow-filesystem | quarantine | 86 | 0.336–0.336 | 434 | 30.623 | 19.016 | 0 / 0 | true |
| s3proxy-filesystem | quarantine | 88 | 0.344–0.344 | 452 | 45.381 | 181.961 | 0 / 0 | true |
| minio-filesystem | quarantine | 142 | 0.555–0.555 | 714 | 20.75 | 164.43 | 0 / 0 | true |
| rustfs-filesystem | quarantine | 60 | 0.234–0.234 | 308 | 41.124 | 155.914 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
