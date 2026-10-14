# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

1 repetitions, 0.5 s windows, 0 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory | put | 1210 | 4.727–4.727 | 1210 | 5.152 | 17.598 | 0 / 0 | true |
| s3proxy-memory | put | 246 | 0.961–0.961 | 246 | 46.924 | 183.039 | 0 / 0 | true |
| stow-filesystem | put | 178 | 0.695–0.695 | 178 | 19.255 | 17.891 | 0 / 0 | true |
| s3proxy-filesystem | put | 348 | 1.359–1.359 | 348 | 34.469 | 179.59 | 0 / 0 | true |
| minio-filesystem | put | 206 | 0.805–0.805 | 206 | 16.506 | 151.082 | 0 / 0 | true |
| stow-memory | get | 1926 | 7.523–7.523 | 1926 | 2.921 | 17.551 | 0 / 0 | true |
| s3proxy-memory | get | 346 | 1.352–1.352 | 346 | 37.698 | 184.691 | 0 / 0 | true |
| stow-filesystem | get | 2106 | 8.227–8.227 | 2106 | 2.752 | 17.836 | 0 / 0 | true |
| s3proxy-filesystem | get | 424 | 1.656–1.656 | 424 | 36.59 | 177.176 | 0 / 0 | true |
| minio-filesystem | get | 2134 | 8.336–8.336 | 2134 | 2.592 | 233.406 | 0 / 0 | true |
| stow-memory | quarantine | 298 | 1.164–1.164 | 1494 | 14.648 | 17.938 | 0 / 0 | true |
| s3proxy-memory | quarantine | 56 | 0.219–0.219 | 284 | 79.878 | 186.328 | 0 / 0 | true |
| stow-filesystem | quarantine | 72 | 0.281–0.281 | 370 | 39.44 | 17.48 | 0 / 0 | true |
| s3proxy-filesystem | quarantine | 64 | 0.25–0.25 | 328 | 67.325 | 176.598 | 0 / 0 | true |
| minio-filesystem | quarantine | 120 | 0.469–0.469 | 606 | 23.014 | 173.262 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
