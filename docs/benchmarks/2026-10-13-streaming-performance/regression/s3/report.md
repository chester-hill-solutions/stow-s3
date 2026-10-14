# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

3 repetitions, 20 s windows, 5 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory | put | 174.7 | 173.6–187.3 | 174.7 | 42.48 | 66.91 | 0 / 0 | true |
| stow-filesystem | put | 66.5 | 66.5–66.55 | 66.5 | 101.969 | 28.273 | 0 / 0 | true |
| stow-memory | get | 510.95 | 482.45–516.55 | 510.95 | 18.973 | 51.676 | 0 / 0 | true |
| stow-filesystem | get | 492.7 | 475.7–519.25 | 492.7 | 19.354 | 20.988 | 0 / 0 | true |
| stow-memory | quarantine | 108.55 | 107.8–109.9 | 543.15 | 53.938 | 61.211 | 0 / 0 | true |
| stow-filesystem | quarantine | 56.65 | 56.15–56.65 | 283.5 | 119.567 | 25.629 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
