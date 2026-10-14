# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

3 repetitions, 20 s windows, 5 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory | put | 180.15 | 179.55–185 | 180.15 | 41.643 | 62.98 | 0 / 0 | true |
| s3proxy-memory | put | 192.65 | 188.85–192.7 | 192.65 | 39.229 | 435.063 | 0 / 0 | true |
| stow-filesystem | put | 66.5 | 66.4–66.5 | 66.5 | 125.666 | 26.898 | 0 / 0 | true |
| s3proxy-filesystem | put | 66.7 | 66.65–66.8 | 66.7 | 106.109 | 330.27 | 0 / 0 | true |
| minio-filesystem | put | 65.55 | 65.45–65.55 | 65.55 | 101.969 | 274.117 | 0 / 0 | true |
| rustfs-filesystem | put | 65.3 | 65.3–65.35 | 65.3 | 105.059 | 191.328 | 0 / 0 | true |
| stow-memory | get | 480.7 | 480.15–504.2 | 480.7 | 19.548 | 51.516 | 0 / 0 | true |
| s3proxy-memory | get | 464.85 | 441.25–498.05 | 464.85 | 26.087 | 327.715 | 0 / 0 | true |
| stow-filesystem | get | 484.15 | 466.85–485.05 | 484.15 | 19.743 | 21.117 | 0 / 0 | true |
| s3proxy-filesystem | get | 439.75 | 424.95–465.5 | 439.75 | 26.878 | 319.477 | 0 / 0 | true |
| minio-filesystem | get | 478.55 | 477.3–486.25 | 478.55 | 25.829 | 372.023 | 0 / 0 | true |
| rustfs-filesystem | get | 539.35 | 521.55–540.75 | 539.35 | 17.521 | 175.492 | 0 / 0 | true |
| stow-memory | quarantine | 112.45 | 111.55–114.25 | 562.6 | 55.022 | 61.148 | 0 / 0 | true |
| s3proxy-memory | quarantine | 111.85 | 110.6–114.15 | 559.4 | 54.478 | 397.855 | 0 / 0 | true |
| stow-filesystem | quarantine | 61.35 | 61.35–64.45 | 307.05 | 108.242 | 24.863 | 0 / 0 | true |
| s3proxy-filesystem | quarantine | 109.4 | 108.1–114.6 | 547.15 | 57.257 | 391.043 | 0 / 0 | true |
| minio-filesystem | quarantine | 65.45 | 65.4–65.45 | 327.35 | 96.059 | 380.805 | 0 / 0 | true |
| rustfs-filesystem | quarantine | 50.3 | 49.85–51.65 | 251.7 | 130.769 | 210.066 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
