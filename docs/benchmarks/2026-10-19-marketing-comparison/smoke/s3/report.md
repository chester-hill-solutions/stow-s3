# Railway S3 comparison

Complete: **true**. Exploratory measurements; qualification protocol incomplete.

1 repetitions, 0.5 s windows, 0 s warm-up. One target at a time, separate generator, private HTTP, no TLS.

| Implementation | Workflow | Files/s | Payload MiB/s range | Successful RPS | Median run p99 ms | Peak sampled server MiB | Failed / dropped | Verified |
|---|---|---:|---:|---:|---:|---:|---:|---|
| stow-memory | put | 1348 | 5.266–5.266 | 1348 | 4.057 | 18.836 | 0 / 0 | true |
| s3proxy-memory | put | 352 | 1.375–1.375 | 352 | 40.018 | 192.496 | 0 / 0 | true |
| stow-filesystem | put | 216 | 0.844–0.844 | 216 | 15.549 | 18.621 | 0 / 0 | true |
| s3proxy-filesystem | put | 316 | 1.234–1.234 | 316 | 36.956 | 182.105 | 0 / 0 | true |
| minio-filesystem | put | 190 | 0.742–0.742 | 190 | 20.288 | 155.254 | 0 / 0 | true |
| rustfs-filesystem | put | 132 | 0.516–0.516 | 132 | 19.802 | 151.035 | 0 / 0 | true |
| stow-memory | get | 2534 | 9.898–9.898 | 2534 | 1.794 | 18.883 | 0 / 0 | true |
| s3proxy-memory | get | 424 | 1.656–1.656 | 424 | 45.544 | 183.02 | 0 / 0 | true |
| stow-filesystem | get | 2730 | 10.664–10.664 | 2730 | 1.776 | 18.848 | 0 / 0 | true |
| s3proxy-filesystem | get | 420 | 1.641–1.641 | 420 | 36.59 | 181.477 | 0 / 0 | true |
| minio-filesystem | get | 2152 | 8.406–8.406 | 2152 | 2.418 | 241.125 | 0 / 0 | true |
| rustfs-filesystem | get | 2322 | 9.07–9.07 | 2322 | 2.233 | 153.84 | 0 / 0 | true |
| stow-memory | quarantine | 278 | 1.086–1.086 | 1402 | 17.006 | 18.91 | 0 / 0 | true |
| s3proxy-memory | quarantine | 66 | 0.258–0.258 | 336 | 62.757 | 186.91 | 0 / 0 | true |
| stow-filesystem | quarantine | 76 | 0.297–0.297 | 388 | 38.822 | 19.492 | 0 / 0 | true |
| s3proxy-filesystem | quarantine | 90 | 0.352–0.352 | 454 | 52.688 | 182.516 | 0 / 0 | true |
| minio-filesystem | quarantine | 152 | 0.594–0.594 | 760 | 19.595 | 170.031 | 0 / 0 | true |
| rustfs-filesystem | quarantine | 56 | 0.219–0.219 | 280 | 41.191 | 157.742 | 0 / 0 | true |

All targets use the same container and data volume. Memory quotas and backend durability features differ; these are bounded-set local-storage comparisons, not distributed durability comparisons.
Quarantine is upload, SHA-256-verified read, simulated scanner delay, verdict write and two deletes. It does not retain flagged content or run a scanner.
Generator and target resource metadata, cgroup throttling, version pins, startup times and per-run samples are saved in suite.json and the named raw files.
Response latency includes queue wait and accepted drain completions. Throughput counts completions inside the measurement window. Dropped arrivals have no latency sample.
S3Proxy includes JVM overhead. MinIO is a single-directory deployment, with distributed resilience unmeasured. Warm cache, copying, signing and client SHA-256 costs are included.
Inspect CPU limits, throttling, generator load and every repetition before publishing comparative claims.
