# Memory HTTP PUT generator diagnosis

All workloads executed on Railway. Original server binary SHA matches the retained controlled comparison.

| Generator processes | Total clients | Median MiB/s | Client CPU % | Server CPU % |
|---|---|---|---|---|
| 1 | 4 | 173.2 | 101.511 | 83.263 |
| 4 | 4 | 516.225 | 279.16 | 169.6 |

Paired geometric effect: 198.4%; conditional 95% interval 187.017 to 210.235%.

- Shared Railway hosts; six paired windows, conditional interval excludes broader host-to-host variation.
- No TLS; SDK default checksum/signing and verification hashing preserved.
- Profiler runs excluded from throughput comparison; mutex/block/heap profiles include startup and warmup.
- Worker starts may differ by up to 50 ms over 20-second windows; actual skew recorded.
- Server CPU sampling includes barrier and final verification; client CPU counts measured traffic plus drain.
- Sum of per-process RSS peaks is an upper bound; shared pages may be counted repeatedly.
