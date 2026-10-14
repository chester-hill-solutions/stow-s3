# Measure Stow performance

Run reproducible storage workloads before publishing capacity claims.
The suite measures embedded Go storage, the S3 endpoint and a simulated quarantine workflow.
It does not run a DPI engine or block network traffic.
The [October 12 local evidence](benchmarks/2026-10-12-performance/README.md) records the first complete exploratory run.
The [October 13 memory rerun](benchmarks/2026-10-13-memory-performance/README.md) adds longer capacity windows, a quarantine arrival-rate sweep and a five-minute run.
The [Railway benchmark setup](railway-performance.md) records the isolated cloud environment and the work needed for matched implementation comparisons.

## Quick checks

Use the pinned Go toolchain and installed TypeScript package dependencies.
If dependencies are missing, run `npm ci --ignore-scripts --prefix packages/stow-s3`.
The performance harness starts the exact repository binary.
Installed platform binaries cannot override it.

```sh
make benchmark-performance-smoke
```

This command checks measurement accounting and runs short memory and filesystem workloads.
It also verifies eight acknowledged files after killing and restarting a filesystem server.
Temporary servers bind only to loopback.
The runner requires at least 1 GiB of free disk space, plus filesystem fixture headroom.
It checks free space before each scenario and samples it during load.
Ambient upstream credentials are removed from their environments.
The suite uses private temporary directories and removes them on normal completion or errors.
Interrupted benchmark processes can leave temporary directories behind.

## Local baseline

```sh
make benchmark-performance
```

The default configuration uses 1 MiB files and four workers.
It tests PUT, GET and quarantine against both storage backends.
Each scenario has three repetitions, one second of warm-up and five seconds of measurement.
It tests closed-loop capacity and 100 scheduled file arrivals per second.
The exploratory queue holds at most eight waiting files.
Each backend also gets one 30-second quarantine soak.
Queued work drains after each measurement window, so overload increases runtime.
These short results are exploratory.

The command saves results under a new `.cache/performance/` directory.
Set an explicit output directory when retaining evidence:

```sh
make benchmark-performance PERF_OUT=.cache/performance/release-candidate
```

An existing `config.json` causes refusal rather than replacement of earlier evidence.
Output includes:

| File | Contents |
|---|---|
| `config.json` | Effective workload and resource limits |
| `suite.json` | Machine, binary, source identity, run summaries and recovery result |
| Each scenario JSON | Latency, counters, per-second completions, queue peaks and resource samples |
| `summary.json` | Median results, variation, failures and completed sample counts |
| `report.md` | Results with measurement boundaries |
| `throughput-latency.svg` | Throughput versus response latency; point details appear on hover |

Failed file operations, failed integrity checks and leftover quarantine files produce a nonzero exit status.
Dropped arrivals are expected during overload exploration and remain explicit in the report.
They disqualify a rate from a zero-loss capacity claim.

## Sustained qualification

Use a quiet, dedicated machine for publishable measurements:

```sh
make benchmark-performance PERF_CONFIG=docs/benchmarks/performance-qualification.json
```

This profile uses 30 seconds of warm-up, five-minute windows and five repetitions.
It adds a 30-minute quarantine soak for each backend.
Budget approximately 6.5 hours, plus fixture preparation and overload drain time.
This is a loopback, warmed-cache profile.
It does not qualify TLS, cold disk reads or separate-host network capacity.

Copy the configuration to select workloads and extend the matrix:

| Setting | Suggested coverage |
|---|---|
| `sizes` | 4096, 65536, 1048576, 8388608 bytes |
| `concurrency` | 1, 4, 16, 64 workers |
| `rates` | 0 for closed loop; increasing scheduled file arrival rates |
| `retainedObjects` | Small and large retained sets |
| `scannerDelayMs` | Named simulated scan delay |
| `maxQueue` | Maximum waiting files; excludes active workers |
| `maxBytes`, `maxObjects` | Explicit server quotas |
| `serverConcurrency` | Maximum active HTTP requests accepted by Stow |
| `minFreeDiskBytes` | Reserved free disk headroom; default 1 GiB |

The runner takes the Cartesian product of sizes, workers, rates, workloads and backends.
Large matrices can take days.
Start with focused comparisons, then qualify the configurations intended for publication.
Fixture and concurrent-transfer budgets must fit the configured quotas.

For direct control, invoke the runner:

```sh
node packages/stow-s3/scripts/benchmark-performance.mjs --config docs/benchmarks/performance-local.json --out .cache/performance/local --binary bin/stow-s3 --data-root /tmp
```

`--data-root` selects the volume used for private test directories.
Its parent directory must exist.
The runner raises the HTTP body limit to fit the configured object size.
This profile uses single PUT requests; multipart uploads are not measured.
Payloads are random fixtures with a changing sequence prefix on writes.
PUTs overwrite worker keys; quarantine deletes and reuses worker keys.
This bounds retention and does not simulate unlimited new-key ingestion.

## Embedded Go benchmarks

```sh
make benchmark-embedded
```

This measures memory and filesystem storage without HTTP or SDK overhead.
It runs 4 KiB, 1 MiB and 8 MiB PUT, verified GET and quarantine workflows.
Go reports time per workflow, decimal MB/s, allocated bytes and allocations per workflow.
The harness uses one or four Go execution threads and parallel workers.
Quarantine includes a verified read, verdict write and two deletes, with no scanner delay.
Results include worker-local buffer setup but exclude initial fixtures and runtime opening.
They use warmed caches and bounded worker keys.
Each runtime has a 256 MiB quota, 4,096 allowed objects and one seed object.
This smaller retained set differs from the S3 profile's 16 fixtures.

The target runs ten repetitions per point.
It saves `embedded.txt` and `embedded-metadata.json` with the compiler, source hash, machine and GC settings.
Use Go's [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) for statistical comparisons.
It reports medians, confidence intervals and significant differences.
Keep raw outputs for both builds and run them on the same machine.
Record the filesystem alongside embedded results.
Go MB/s uses decimal megabytes; the S3 report uses binary MiB/s.

## Understand the metrics

**Files/s** counts completed file transfers or quarantine workflows during the measurement window.
**Payload MiB/s** multiplies that count by the configured file size.
**Successful RPS** counts completed individual API requests during the window.
A successful quarantine file needs five requests: PUT, GET, verdict PUT and two DELETEs.
Window boundaries and incomplete workflows can change the observed requests-per-file ratio.
Raw operation metrics also report completed failed attempts and their status codes.

**Service latency** starts when a worker begins a file.
**Response latency** starts at its scheduled arrival and includes queue wait.
Latency distributions include all admitted work, including work completed after the window.
Drain completions do not increase measured throughput.
`timeoutMs` limits each file from its scheduled arrival, including queue wait.
Queued files that expire fail without issuing API requests.
Service latency excludes these unstarted files.
Timed-out writes can have unknown outcomes and do not establish corruption.
The report preserves unknown outcomes separately from verified objects.
Dropped arrivals have no latency sample.
Always publish their count beside latency results.

Rate-controlled tests continue scheduling arrivals when the server slows.
The harness bounds the queue and records scheduling delay and rejected arrivals.
This avoids silently reducing offered load during slow service.
The [wrk2 documentation](https://github.com/giltene/wrk2) explains this measurement problem, called coordinated omission.
Inspect generator CPU, scheduling delay and event-loop delay before accepting a capacity result.

Histograms retain bounded bins with about 1% resolution.
Repeated-run reports show the median of each run's p99, not a pooled request percentile.
Collect at least 10,000 completed samples per run before emphasizing p99.
Longer tests are needed for low file arrival rates.

Server RSS is sampled separately from generator RSS.
The reported peak can miss brief memory spikes.
CPU time can exceed one core's wall time; 100% means one CPU core.
The sampler itself adds overhead.
Linux process I/O counters report disk bytes when available.
macOS reports these fields as unavailable.
Retained apparent and allocated bytes describe final footprint, not cumulative writes.
Embedded benchmarks report allocation costs; the S3 sampler does not measure them.

## Publish supported claims

Keep workload-specific claims alongside the effective configuration and raw results.
Include hardware, disk, OS, compiler, source commit, binary hash and GC settings.
The suite records binary, harness and dependency-lock hashes.
It saves a measurement harness snapshot under `harness/`.
A dirty source checkout must be archived or committed for reproducibility.

Use the median across repetitions and publish the observed range.
Require zero failed operations, zero dropped arrivals, verified content and stable queueing for a sustainable-rate claim.
Require maintained disk headroom and a quiet host.
Publish operation mix, object size, retained count and test duration.
Report warmed-cache results as warmed-cache results.
Do not convert loopback object throughput into DPI line-rate capacity.

The SIGKILL probe checks completed writes after process termination.
It does not simulate power loss, lost disk caches or interrupted writes.
Encryption, real malware scanners, disk-full handling and separate-host TLS qualification remain separate tests.
Timing checks stay outside `make standards` because shared CI does not provide stable performance measurements.
Measurement-accounting tests run in `make check-scripts`.

The manual **Performance evidence** GitHub Actions workflow runs on Linux and macOS.
Choose `smoke` or `local` and download its raw measurements and reports.
It also runs short embedded benchmarks.
Shared-runner results remain exploratory; dedicated-machine qualification stays a separate command.
