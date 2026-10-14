# Local storage performance evidence

Measured on **2026-10-12, Toronto time**, using an Apple M1 Pro with 16 GiB RAM.
These are exploratory measurements from a shared desktop.
They do not establish sustained DPI throughput or a latency guarantee.

The S3 profile uses loopback HTTP, SigV4, 1 MiB files and four keep-alive workers.
It retains 16 fixtures and allows eight queued files.
Each scenario has three five-second runs after one second of warm-up.
Each backend also has one 30-second quarantine overload soak at 100 offered files/s.
The simulated scanner waits two milliseconds after a verified read.

## Observed results

These are median completed payload rates across the three closed-loop runs.
Quarantine rates include upload, verified read, verdict write and two deletes.

| Backend | PUT MiB/s | GET MiB/s | Quarantine files/s | Quarantine successful RPS |
|---|---:|---:|---:|---:|
| Memory | 404.0 | 716.0 | 192.4 | 963.4 |
| Filesystem | 6.0 | 128.4 | 1.6 | 8.0 |

The full report includes variation, response latency, memory observations and overload failures.
These figures describe different persistence profiles and cannot substitute for each other.
The embedded benchmarks use a smaller retained set and exclude HTTP and SDK work.

## Validation and limits

- The suite completed all 38 S3 scenarios.
- Every final verification passed SHA-256 and length checks.
- No final quarantine leftovers remained.
- Disk headroom stayed above the configured 1 GiB reserve in every run.
- Eight acknowledged files verified after process SIGKILL and restart.
- The overload scenarios dropped 5,868 offered files across the suite.
- The filesystem overload soak had eight file deadline failures.
- The memory soak also dropped files; 100 files/s is not a zero-loss sustained claim from this evidence.

The command returned nonzero because the workload recorded deadline failures.
All results were saved before that exit.
No real scanner, packet interception, TLS, cold-cache reads or power-loss simulation ran.
The earlier interrupted experiment is excluded from this evidence.

## Evidence files

- [Generated report](report.md) and [throughput versus latency chart](throughput-latency.svg).
- [Effective configuration](config.json), [run summaries](summary.json) and [raw run index](suite.json).
- Scenario JSON files named in the raw run index contain queue, resource and per-second observations.
- [Embedded raw output](embedded.txt) and [embedded metadata](embedded-metadata.json).
- [Embedded benchmark source snapshot](embedded-benchmark-source.txt).
- [Validation record](validation.json) confirms the standards gate, measurement tests and completed runs.
- The `harness/` directory preserves the S3 measurement sources whose hash appears in `suite.json`.

Embedded measurements use three repetitions and 300 ms target windows.
They cover 4 KiB, 1 MiB and 8 MiB workflows with one and four Go execution threads.
The source checkout was dirty; binary, harness and benchmark source hashes identify the measured inputs.
Harness copies are audit snapshots; restore them into the repository script paths to reproduce them.

Use the [performance testing guide](../../performance-testing.md) for commands and interpretation.
Run the longer qualification profile on a quiet machine before publishing sustained capacity claims.
