# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

6 repetitions, 20 s windows, 5 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory-before | put | 177.65 | 172.3–187 | 177.65 | 41.851 | 66.168 | 0 / 0 | true |
| stow-memory | put | 171.05 | 159.95–188.4 | 171.05 | 42.913 | 66.984 | 0 / 0 | true |
| stow-filesystem-before | put | 66.5 | 62.3–66.55 | 66.5 | 105.584 | 28.777 | 0 / 0 | true |
| stow-filesystem | put | 66.475 | 66.45–66.6 | 66.475 | 102.479 | 28.723 | 0 / 0 | true |
| stow-memory-before | get | 528.05 | 460.45–558.1 | 528.05 | 17.877 | 51.57 | 0 / 0 | true |
| stow-memory | get | 535.9 | 488.45–550 | 535.9 | 17.522 | 51.418 | 0 / 0 | true |
| stow-filesystem-before | get | 541.275 | 523.25–559.45 | 541.275 | 17.262 | 21.137 | 0 / 0 | true |
| stow-filesystem | get | 551.7 | 460.25–565.85 | 551.7 | 17.348 | 21.664 | 0 / 0 | true |
| stow-memory-before | quarantine | 104.825 | 101.55–109.85 | 524.375 | 56.974 | 61.746 | 0 / 0 | true |
| stow-memory | quarantine | 104.425 | 102.2–107.6 | 522.5 | 57.268 | 62.184 | 0 / 0 | true |
| stow-filesystem-before | quarantine | 55.875 | 42.65–56.85 | 279.7 | 116.631 | 28.57 | 0 / 0 | true |
| stow-filesystem | quarantine | 54.4 | 44.45–55.1 | 272.275 | 124.422 | 25.48 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
