# Stow performance measurements

Profile: **Local exploratory baseline; loopback HTTP; warmed caches**. Recorded 2026-10-13T03:11:06.674Z.

Measurement windows: 5 s; warm-up: 1 s; repetitions: 3; soak: 30 s per backend.
Scanner delay: 2 ms; retained fixtures: 16; queue capacity: 8; file deadline: 10000 ms.
Suite complete: **true**. See [effective configuration](config.json) and [raw run index](suite.json).

**Exploratory measurements. The public capacity protocol has not been completed.**

Source commit: `a9fc459210edcb3f716d1ced30e8da7f388390b0`. Dirty checkout: **true**.
Machine: Apple M1 Pro; 16 GiB RAM; darwin arm64; 23.5.0.

| Backend / workload | Bytes/file | Concurrency | Offered files/s | Runs | Completed files/s | Payload MiB/s (min–max) | Successful RPS | Median run p99 response ms | Peak sampled server MiB | Failed / dropped |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| memory / put | 1048576 | 4 | closed loop | 3 | 404 | 404 (388.2–441.4) | 404 | 23.852 | 87.188 | 0 / 0 |
| memory / put | 1048576 | 4 | 100 | 3 | 100 | 100 (100–100) | 100 | 27.146 | 61.953 | 0 / 0 |
| memory / get | 1048576 | 4 | closed loop | 3 | 716 | 716 (577–758) | 716 | 12.617 | 63.828 | 0 / 0 |
| memory / get | 1048576 | 4 | 100 | 3 | 100 | 100 (100–100) | 100 | 5.922 | 55.844 | 0 / 0 |
| memory / quarantine | 1048576 | 4 | closed loop | 3 | 192.4 | 192.4 (170.8–202.8) | 963.4 | 33.124 | 75.984 | 0 / 0 |
| memory / quarantine | 1048576 | 4 | 100 | 3 | 99.8 | 99.8 (99.8–99.8) | 499.4 | 42.904 | 66.609 | 0 / 0 |
| filesystem / put | 1048576 | 4 | closed loop | 3 | 6 | 6 (5–6.2) | 6 | 734.33 | 37.063 | 0 / 0 |
| filesystem / put | 1048576 | 4 | 100 | 3 | 4.4 | 4.4 (4–4.8) | 4.4 | 2894.317 | 36.313 | 0 / 1399 |
| filesystem / get | 1048576 | 4 | closed loop | 3 | 128.4 | 128.4 (109.4–130.2) | 128.4 | 51.833 | 30.969 | 0 / 0 |
| filesystem / get | 1048576 | 4 | 100 | 3 | 100 | 100 (99.8–100) | 100 | 34.814 | 27.891 | 0 / 0 |
| filesystem / quarantine | 1048576 | 4 | closed loop | 3 | 1.6 | 1.6 (1.6–1.6) | 8 | 2535.842 | 33.844 | 0 / 0 |
| filesystem / quarantine | 1048576 | 4 | 100 | 3 | 1.6 | 1.6 (1.6–1.6) | 8.4 | 7099.026 | 35.297 | 0 / 1440 |
| memory / quarantine (soak) | 1048576 | 4 | 100 | 1 | 96.967 | 96.967 (96.967–96.967) | 485.066 | 232.887 | 75.453 | 0 / 81 |
| filesystem / quarantine (soak) | 1048576 | 4 | 100 | 1 | 1.067 | 1.067 (1.067–1.067) | 6.801 | 10027.597 | 32.875 | 8 / 2948 |

![Throughput and latency](throughput-latency.svg)

## Measurement scope

- S3 over loopback HTTP, SigV4 signing, AWS SDK, keep-alive connections and one attempt per request. TLS and separate-host networking are unmeasured.
- Quarantine performs PUT, verified GET, simulated scanner delay, verdict PUT and two DELETE requests per successful file. No real scanner or traffic blocking runs.
- Payload throughput counts completed file workflows during the measurement window. RPS counts successful individual API requests during that window. Verdict bytes are separate in raw operation metrics.
- Response latency includes scheduled-arrival queue wait. Service latency starts when a worker takes the file. Dropped arrivals have no latency sample and are reported separately.
- File deadlines include queue wait. Queue-expired files count as failures without issuing API requests or contributing service-time samples. A timed-out mutation can have an unknown outcome; it is not reported as confirmed corruption.
- Accepted work drains after the window. Drain completions enter latency histograms but do not increase window throughput. Resource observations include drain.
- Histograms use bounded bins with approximately 1% resolution. p99 needs enough observations; inspect minimumCompletedSamples in summary.json.
- Fixtures contain random bytes. PUTs reuse one key per worker and change a sequence prefix. Quarantine reuses worker keys after deletion. These test bounded retained sets, not unlimited ingestion.
- Reads stream through SHA-256 verification. Generation, hashing, SDK and verification costs are included in client-observed results.
- Warm-up and measured activity share a warmed server and filesystem cache. Cold-cache disk performance is unmeasured.
- RSS is sampled, not an OS high-water guarantee. CPU sampling uses process CPU time. Allocation rate is unavailable here; embedded Go benchmarks report allocations separately.
- Linux process I/O counters are recorded when available. Other platforms report null. Retained footprint is not cumulative disk write volume.
- Disk headroom is checked before each run and sampled during load. Inspect diskHeadroomMaintained in summary.json before accepting a result.

## Recovery

Acknowledged files verified after process SIGKILL: **true** (8 files). Restart to verified: 49.121 ms.
This checks process restart after completed writes. Power failure and interrupted publication are unmeasured.

## Publication

Publish a workload-specific claim with its hardware, backend, object size, concurrency, duration, configuration and raw results. Publish variation and failed/dropped counts.
Review scheduler lag and generator CPU before accepting a capacity result. Growing queue wait or large scheduling lag can indicate an overloaded client or server.
The chart includes overloaded points. Zero errors, verified data and stable queueing are required before describing a rate as sustainable.
This report does not establish production readiness, a latency SLA, crash durability under power loss, or DPI line-rate capacity.
