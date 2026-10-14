# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

3 repetitions, 20 s windows, 5 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory | put | 197.35 | 196.65–197.4 | 197.35 | 38.075 | 68.598 | 0 / 0 | true |
| s3proxy-memory | put | 223.55 | 220.8–229.3 | 223.55 | 34.128 | 559.574 | 0 / 0 | true |
| stow-filesystem | put | 66.45 | 66.45–66.55 | 66.45 | 93.234 | 33.367 | 0 / 0 | true |
| s3proxy-filesystem | put | 66.7 | 66.6–66.75 | 66.7 | 106.109 | 398.246 | 0 / 0 | true |
| minio-filesystem | put | 65.5 | 65.45–65.55 | 65.5 | 107.17 | 275.219 | 0 / 0 | true |
| rustfs-filesystem | put | 65.35 | 65.25–65.35 | 65.35 | 107.17 | 199.105 | 0 / 0 | true |
| stow-memory | get | 505.6 | 499.45–507.35 | 505.6 | 18.415 | 67.195 | 0 / 0 | true |
| s3proxy-memory | get | 446.7 | 381.4–451.6 | 446.7 | 21.168 | 312.406 | 0 / 0 | true |
| stow-filesystem | get | 356.95 | 353.05–393.3 | 356.95 | 29.105 | 30.203 | 0 / 0 | true |
| s3proxy-filesystem | get | 417.45 | 410.55–435.2 | 417.45 | 23.852 | 379.828 | 0 / 0 | true |
| minio-filesystem | get | 536.7 | 531.85–537.3 | 536.7 | 19.354 | 365.547 | 0 / 0 | true |
| rustfs-filesystem | get | 559.9 | 554.3–560.85 | 559.9 | 17.176 | 184.012 | 0 / 0 | true |
| stow-memory | quarantine | 113.95 | 113.6–114.6 | 570 | 51.833 | 69.012 | 0 / 0 | true |
| s3proxy-memory | quarantine | 120.05 | 119.95–126.1 | 600.5 | 52.352 | 430.383 | 0 / 0 | true |
| stow-filesystem | quarantine | 50.35 | 46.9–51.25 | 252.1 | 124.422 | 30.051 | 0 / 0 | true |
| s3proxy-filesystem | quarantine | 123 | 120.8–127.35 | 615.35 | 53.938 | 388.465 | 0 / 0 | true |
| minio-filesystem | quarantine | 65.4 | 65.4–65.45 | 327.15 | 98.97 | 372.84 | 0 / 0 | true |
| rustfs-filesystem | quarantine | 53.35 | 51.85–53.4 | 267.15 | 123.19 | 217.563 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
