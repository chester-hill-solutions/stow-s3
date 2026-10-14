# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

3 repetitions, 20 s windows, 5 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory | put | 194.7 | 192.65–196.9 | 194.7 | 38.456 | 67.121 | 0 / 0 | true |
| stow-filesystem | put | 66.45 | 66.4–66.5 | 66.45 | 94.167 | 35.563 | 0 / 0 | true |
| stow-memory | get | 480.65 | 477.4–482.55 | 480.65 | 19.163 | 70.887 | 0 / 0 | true |
| stow-filesystem | get | 371.35 | 346.55–374.45 | 371.35 | 28.817 | 32.895 | 0 / 0 | true |
| stow-memory | quarantine | 109.95 | 109.15–110.5 | 550.1 | 55.022 | 72.422 | 0 / 0 | true |
| stow-filesystem | quarantine | 47.5 | 47.05–48.2 | 237.7 | 138.813 | 33.707 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
