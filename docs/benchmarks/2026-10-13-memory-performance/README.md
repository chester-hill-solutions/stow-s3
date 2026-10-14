# Memory performance rerun — October 13, 2026

Measured on an Apple M1 Pro, eight logical CPUs, 16 GiB RAM, macOS 14.5, Node v25.9.0. These are exploratory measurements on a shared desktop.
The capacity rerun is faster than the earlier baseline. The five-minute quarantine run has lower latency and fewer dropped arrivals, but still records a brief loss event.

## Profile

All runs use the memory backend, loopback HTTP, SigV4, 1 MiB random payloads, four workers, 16 retained fixtures, eight waiting slots, a 10-second file deadline and 250 ms resource sampling.
Quarantine performs payload PUT, SHA-256-verified GET, a simulated 2 ms scanner delay, verdict PUT and two DELETE calls. It does not run a scanner, inspect packets or transfer flagged content to a filesystem backend.

- Capacity: three 20-second closed-loop repetitions per workload, each after five seconds of warm-up.
- Offered-rate sweep: three 20-second quarantine repetitions at each of 50, 100, 150 and 200 files/sec, each after five seconds of warm-up.
- Longer run: one 300-second quarantine measurement at 100 offered files/sec after ten seconds of warm-up.
- Fresh server per repetition. The native server and Node generator share this machine. No unrelated tests ran during load.

## Capacity comparison

Rates are median completed files/sec; at 1 MiB/file, the numbers also equal completed payload MiB/sec. Ranges cover the three new repetitions. P99 is the median of each run’s response p99, not a pooled percentile.

| Workload | Earlier files/sec | New files/sec | New range | New p99 ms | Peak sampled server MiB |
|---|---:|---:|---:|---:|---:|
| put | 404.00 | 460.25 | 448.15–462.95 | 13.80 | 95.39 |
| get | 716.00 | 758.90 | 726.70–774.55 | 12.12 | 64.38 |
| quarantine | 192.40 | 211.20 | 208.80–214.55 | 27.97 | 60.78 |

Every capacity repetition had zero operation failures and verified data. Quarantine’s median successful API request rate was 1056.50 RPS. A successful quarantine file involves five calls, so RPS alone overstates file-processing capacity.

The earlier capacity windows were five seconds with one second of warm-up. Both runs use the same harness and SDK lockfile hashes. The rebuilt binary and source commit differ; a Git comparison found no changes in the server/storage/runtime paths or SDK sources listed in validation.json. Binary hashes and build metadata remain recorded separately. The changed window length and shared-machine activity prevent attributing the difference solely to quietness.

## Quarantine offered-rate sweep

| Offered files/sec | Median window completions/sec | Median run p99 ms | Dropped / offered across three runs | Operation failures |
|---|---:|---:|---:|---:|---:|
| 50 | 50.00 | 22.03 | 0 / 3000 (0.000%) | 0 |
| 100 | 99.95 | 22.03 | 0 / 6000 (0.000%) | 0 |
| 150 | 149.85 | 30.29 | 12 / 9000 (0.133%) | 0 |
| 200 | 199.90 | 48.35 | 168 / 12000 (1.400%) | 0 |

At 150 files/sec, all 12 drops occurred in one repetition. At 200 files/sec, all 168 drops occurred in one repetition, including 115 within second 7. The other two repetitions at each rate had no drops. Median throughput hides this variation; neither rate demonstrated loss-free delivery across repetitions.
Near-perfect window throughput can be slightly below the offered rate even without drops because a few accepted workflows finish after the measurement window. Latency includes those drain completions. Dropped arrivals have no latency sample.

## Five-minute run at 100 files/sec

- Offered: 30000 files. Completed including drain: 29995. Window completions: 29994 (99.98 files/sec).
- Dropped: 5 (0.0167%). Operation failures: 0. All drops occurred in second 53.
- Response p99: 25.83 ms; maximum 169.91 ms. Service p99: 24.09 ms.
- Queue wait p99: 2.06 ms; maximum 129.91 ms. Peak waiting queue: 8 files.
- Scheduler lag p99: 1.98 ms; maximum 32.78 ms. Generator event-loop delay maximum: 51.02 ms.
- Server sampled RSS peak: 51.00 MiB. First sample: 50.94 MiB; final sample: 41.63 MiB.
- Average server CPU: 52.59%; generator CPU: 76.53%. CPU 100% means one core.
- Generator sampled RSS peak: 207.75 MiB. Server RSS does not include generator memory.

| Minute | Minimum sampled server MiB | Maximum sampled server MiB |
|---|---:|---:|
| 1 | 41.47 | 51.00 |
| 2 | 41.03 | 45.20 |
| 3 | 42.38 | 49.11 |
| 4 | 39.17 | 46.02 |
| 5 | 39.95 | 48.94 |

Sampled RSS fluctuated without sustained growth over this five-minute window. This is evidence for the fixed 16 MiB fixture set and bounded worker objects; it does not prove absence of a memory leak or measure large-cache scaling. RSS includes runtime overhead, and sampling can miss shorter peaks. GC pause attribution was not measured.

The earlier 30-second memory run at 100 files/sec dropped 81/3000 arrivals (2.70%), with 232.89 ms response p99 and 75.45 MiB peak sampled server RSS. The longer rerun is substantially better, while its five drops still rule out a zero-loss claim.

## Interpretation and next measurement

This run supports reporting observed local measurements with their workload and loss counts. It does not establish DPI line-rate performance, scanner throughput, TLS performance or sustained zero-loss capacity.
At GET capacity, median generator CPU was about 1.5 cores versus about 0.64 server cores. SDK, hashing and generator work are candidate limits, but CPU averages alone do not identify the bottleneck. A separate load-generator host and profiles around stall events would make the next capacity result more informative.
For a sustained public claim, use the qualification duration/repetition protocol, confirm no dropped or failed arrivals at the advertised rate, and measure a realistic retained working set and scanner. The present five-minute run has only one repetition; none of these suites completes that protocol.

The desktop was quieter by the user’s observation, but background processes remained active. The pre-run snapshot included mediaanalysisd around 41% CPU, a Codex renderer around 15%, and Docker and opencode processes. No background processes were stopped. Historical load averages do not prove saturation. See background-before.json and background-after.json.

## Evidence and reproduction

- [Capacity report](capacity/report.md), [effective config](capacity/config.json), [summary](capacity/summary.json) and [raw index](capacity/suite.json).
- [Arrival-rate report](rates/report.md), [effective config](rates/config.json), [summary](rates/summary.json) and [raw index](rates/suite.json).
- [Five-minute report](steady/report.md), [effective config](steady/config.json), [summary](steady/summary.json) and [raw index](steady/suite.json).
- [Validation record](validation.json): all 22 runs completed, zero operation failures, verified data, no quarantine leftovers, disk reserve maintained, and 12 measurement tests passed after the load runs. All three benchmark commands exited zero; dropped arrivals are reported separately from operation failures.
- Each suite preserves its harness source snapshot, binary SHA-256, build metadata, SDK lockfile hash and dirty-checkout identity. Scenario JSON files contain per-second arrivals and sampled resources.
- [Earlier evidence](../2026-10-12-performance/README.md) and [testing guide](../../performance-testing.md).

Build with `make build`, then run each profile sequentially into a new output directory:

```sh
node packages/stow-s3/scripts/benchmark-performance.mjs --config docs/benchmarks/2026-10-13-memory-performance/capacity-profile.json --out .cache/performance/memory-rerun-capacity
node packages/stow-s3/scripts/benchmark-performance.mjs --config docs/benchmarks/2026-10-13-memory-performance/rates-profile.json --out .cache/performance/memory-rerun-rates
node packages/stow-s3/scripts/benchmark-performance.mjs --config docs/benchmarks/2026-10-13-memory-performance/steady-profile.json --out .cache/performance/memory-rerun-steady
```
