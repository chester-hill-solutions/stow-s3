# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

1 repetitions, 0.5 s windows, 0 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory | put | 1358 | 5.305–5.305 | 1358 | 4.758 | 19.035 | 0 / 0 | true |
| s3proxy-memory | put | 310 | 1.211–1.211 | 310 | 42.476 | 185.535 | 0 / 0 | true |
| stow-filesystem | put | 184 | 0.719–0.719 | 184 | 26.673 | 18.598 | 0 / 0 | true |
| s3proxy-filesystem | put | 354 | 1.383–1.383 | 354 | 49.318 | 186.559 | 0 / 0 | true |
| minio-filesystem | put | 234 | 0.914–0.914 | 234 | 12.87 | 161.273 | 0 / 0 | true |
| rustfs-filesystem | put | 122 | 0.477–0.477 | 122 | 28.522 | 153.434 | 0 / 0 | true |
| stow-memory | get | 2394 | 9.352–9.352 | 2394 | 2.491 | 18.879 | 0 / 0 | true |
| s3proxy-memory | get | 394 | 1.539–1.539 | 394 | 38.133 | 192.516 | 0 / 0 | true |
| stow-filesystem | get | 2536 | 9.906–9.906 | 2536 | 2.864 | 19.293 | 0 / 0 | true |
| s3proxy-filesystem | get | 408 | 1.594–1.594 | 408 | 40.822 | 185.602 | 0 / 0 | true |
| minio-filesystem | get | 2554 | 9.977–9.977 | 2554 | 3.426 | 258.445 | 0 / 0 | true |
| rustfs-filesystem | get | 2542 | 9.93–9.93 | 2542 | 3.259 | 150.199 | 0 / 0 | true |
| stow-memory | quarantine | 342 | 1.336–1.336 | 1720 | 10.136 | 18.996 | 0 / 0 | true |
| s3proxy-memory | quarantine | 78 | 0.305–0.305 | 394 | 53.512 | 187.996 | 0 / 0 | true |
| stow-filesystem | quarantine | 82 | 0.32–0.32 | 422 | 40.818 | 18.973 | 0 / 0 | true |
| s3proxy-filesystem | quarantine | 68 | 0.266–0.266 | 352 | 52.984 | 189.488 | 0 / 0 | true |
| minio-filesystem | quarantine | 112 | 0.438–0.438 | 560 | 33.555 | 160.133 | 0 / 0 | true |
| rustfs-filesystem | quarantine | 56 | 0.219–0.219 | 288 | 43.184 | 167.863 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
