# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

1 repetitions, 0.5 s windows, 0 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory | put | 1552 | 6.063–6.063 | 1552 | 3.899 | 18.992 | 0 / 0 | true |
| s3proxy-memory | put | 318 | 1.242–1.242 | 318 | 42.059 | 191.34 | 0 / 0 | true |
| stow-filesystem | put | 232 | 0.906–0.906 | 232 | 12.617 | 18.961 | 0 / 0 | true |
| s3proxy-filesystem | put | 354 | 1.383–1.383 | 354 | 35.869 | 185.906 | 0 / 0 | true |
| minio-filesystem | put | 214 | 0.836–0.836 | 214 | 16.02 | 151.039 | 0 / 0 | true |
| stow-memory | get | 2234 | 8.727–8.727 | 2234 | 2.951 | 18.73 | 0 / 0 | true |
| s3proxy-memory | get | 400 | 1.563–1.563 | 400 | 35.162 | 183.965 | 0 / 0 | true |
| stow-filesystem | get | 2892 | 11.297–11.297 | 2892 | 2.541 | 19.246 | 0 / 0 | true |
| s3proxy-filesystem | get | 434 | 1.695–1.695 | 434 | 38.075 | 175.582 | 0 / 0 | true |
| minio-filesystem | get | 2614 | 10.211–10.211 | 2614 | 2.645 | 255.984 | 0 / 0 | true |
| stow-memory | quarantine | 384 | 1.5–1.5 | 1928 | 7.983 | 18.668 | 0 / 0 | true |
| s3proxy-memory | quarantine | 68 | 0.266–0.266 | 344 | 55.299 | 195.504 | 0 / 0 | true |
| stow-filesystem | quarantine | 84 | 0.328–0.328 | 420 | 32.927 | 18.945 | 0 / 0 | true |
| s3proxy-filesystem | quarantine | 74 | 0.289–0.289 | 378 | 81.132 | 189.695 | 0 / 0 | true |
| minio-filesystem | quarantine | 152 | 0.594–0.594 | 764 | 17.367 | 165.941 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
