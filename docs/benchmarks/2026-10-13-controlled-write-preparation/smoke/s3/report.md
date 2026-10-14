# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

1 repetitions, 2 s windows, 0 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory-before | put | 178 | 178–178 | 178 | 59.581 | 63.645 | 0 / 0 | true |
| stow-memory | put | 172 | 172–172 | 172 | 57.257 | 64.094 | 0 / 0 | true |
| stow-filesystem-before | put | 70.5 | 70.5–70.5 | 70.5 | 111.522 | 28.309 | 0 / 0 | true |
| stow-filesystem | put | 70.5 | 70.5–70.5 | 70.5 | 91.397 | 26.543 | 0 / 0 | true |
| stow-memory-before | get | 387.5 | 387.5–387.5 | 387.5 | 88.709 | 44.992 | 0 / 0 | true |
| stow-memory | get | 438.5 | 438.5–438.5 | 438.5 | 23.151 | 43.016 | 0 / 0 | true |
| stow-filesystem-before | get | 359.5 | 359.5–359.5 | 359.5 | 26.878 | 19.371 | 0 / 0 | true |
| stow-filesystem | get | 440 | 440–440 | 440 | 21.168 | 20.336 | 0 / 0 | true |
| stow-memory-before | quarantine | 97.5 | 97.5–97.5 | 490.5 | 70.563 | 59.414 | 0 / 0 | true |
| stow-memory | quarantine | 99.5 | 99.5–99.5 | 502.5 | 77.173 | 63.055 | 0 / 0 | true |
| stow-filesystem-before | quarantine | 50.5 | 50.5–50.5 | 255.5 | 119.567 | 24.945 | 0 / 0 | true |
| stow-filesystem | quarantine | 52 | 52–52 | 263.5 | 114.901 | 25.09 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
