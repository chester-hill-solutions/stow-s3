# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

3 repetitions, 20 s windows, 5 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory | put | 182.75 | 181.9–187.5 | 182.75 | 41.643 | 73.77 | 0 / 0 | true |
| s3proxy-memory | put | 210.45 | 210–215.6 | 210.45 | 36.956 | 469.93 | 0 / 0 | true |
| stow-filesystem | put | 7.05 | 6.55–7.25 | 7.05 | 649.004 | 36.152 | 0 / 0 | true |
| s3proxy-filesystem | put | 66.65 | 66.65–66.9 | 66.65 | 105.059 | 382.496 | 0 / 0 | true |
| minio-filesystem | put | 65.55 | 65.45–65.6 | 65.55 | 102.989 | 278.66 | 0 / 0 | true |
| stow-memory | get | 466.95 | 423.5–467.2 | 466.95 | 20.545 | 70.508 | 0 / 0 | true |
| s3proxy-memory | get | 398.65 | 374.35–444.1 | 398.65 | 25.069 | 317.703 | 0 / 0 | true |
| stow-filesystem | get | 119.4 | 118.8–120.55 | 119.4 | 49.318 | 28.156 | 0 / 0 | true |
| s3proxy-filesystem | get | 401.2 | 392.2–445.15 | 401.2 | 23.151 | 307.168 | 0 / 0 | true |
| minio-filesystem | get | 502.8 | 492.65–510.75 | 502.8 | 20.958 | 359.398 | 0 / 0 | true |
| stow-memory | quarantine | 105.75 | 104.2–106.25 | 528.9 | 57.257 | 73.805 | 0 / 0 | true |
| s3proxy-memory | quarantine | 114.5 | 114.3–116 | 572.55 | 55.573 | 573.895 | 0 / 0 | true |
| stow-filesystem | quarantine | 2 | 2–2.05 | 10.65 | 2115.249 | 30.516 | 0 / 0 | true |
| s3proxy-filesystem | quarantine | 113.15 | 112.3–118.4 | 566 | 58.407 | 411 | 0 / 0 | true |
| minio-filesystem | quarantine | 65.4 | 65.35–65.5 | 327.35 | 98.97 | 381.109 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
