# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

3 repetitions, 20 s windows, 5 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory | put | 167.5 | 163.2–170.1 | 167.5 | 45.544 | 65.047 | 0 / 0 | true |
| s3proxy-memory | put | 185.9 | 181.35–187.25 | 185.9 | 41.643 | 533.613 | 0 / 0 | true |
| stow-filesystem | put | 66.5 | 66.35–66.5 | 66.5 | 128.192 | 26.738 | 0 / 0 | true |
| s3proxy-filesystem | put | 66.7 | 66.7–66.8 | 66.7 | 106.109 | 346.336 | 0 / 0 | true |
| minio-filesystem | put | 65.6 | 65.45–65.65 | 65.6 | 104.019 | 267.906 | 0 / 0 | true |
| rustfs-filesystem | put | 65.35 | 65.25–65.4 | 65.35 | 111.522 | 193.457 | 0 / 0 | true |
| stow-memory | get | 461.85 | 455.85–479.95 | 461.85 | 21.168 | 49.66 | 0 / 0 | true |
| s3proxy-memory | get | 441.6 | 415.9–453.9 | 441.6 | 26.878 | 340.938 | 0 / 0 | true |
| stow-filesystem | get | 467.65 | 450.8–483.6 | 467.65 | 20.545 | 21.594 | 0 / 0 | true |
| s3proxy-filesystem | get | 428.65 | 426.7–433.95 | 428.65 | 29.987 | 340.996 | 0 / 0 | true |
| minio-filesystem | get | 467.75 | 466.25–474.55 | 467.75 | 25.573 | 363.387 | 0 / 0 | true |
| rustfs-filesystem | get | 497.75 | 491.6–523.7 | 497.75 | 18.599 | 179.84 | 0 / 0 | true |
| stow-memory | quarantine | 102.9 | 101.85–103.55 | 514.8 | 61.387 | 60.199 | 0 / 0 | true |
| s3proxy-memory | quarantine | 104.55 | 100.9–105.45 | 523 | 61.387 | 411.387 | 0 / 0 | true |
| stow-filesystem | quarantine | 56.15 | 55.05–56.8 | 281 | 129.474 | 24.996 | 0 / 0 | true |
| s3proxy-filesystem | quarantine | 104.45 | 100.85–106.5 | 522.4 | 58.407 | 414.363 | 0 / 0 | true |
| minio-filesystem | quarantine | 65.35 | 65.35–65.45 | 327.05 | 100.959 | 386.902 | 0 / 0 | true |
| rustfs-filesystem | quarantine | 46.25 | 45.15–48.25 | 231.5 | 143.02 | 206.98 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
