# Railway storage comparison — October 13, 2026

The cloud batch completed successfully: **378 native samples and 45 S3 runs**, plus a separate 15-case smoke suite. Every measured/final read passed SHA-256 and length verification. There were zero failed workflows or dropped arrivals, including during warm-up. Quarantine teardown left no worker objects.

Stow memory provides a small server footprint and competitive verified-read throughput in this configuration. Filesystem writes and quarantine are substantially slower than the S3 baselines and are the clearest performance work to pursue. These are exploratory comparisons; sustained capacity qualification is incomplete.

## Setup

Builds and workloads ran on Railway. Both roles used the same immutable image and source snapshot. The target ran one implementation at a time with a 2 vCPU / 2 GiB ceiling and a persistent ext4 volume; the separate runner had a 4 vCPU / 4 GiB ceiling. Both reported AMD EPYC 9655P CPUs in US East. Native filesystem tests used the runner’s ephemeral overlay filesystem. Compare implementations within a lane.

S3: 1 MiB random files, four workers, 16 retained fixtures, private HTTP with SigV4 and keep-alive, three 20-second windows after five-second warm-ups. Targets restarted for each repetition; ordering rotated. Read hashing, SDK work and network costs are included. Quarantine adds a simulated 2 ms scanner delay, verdict PUT and two deletes.

The complete batch ran from 2026-10-13T14:56:21.002Z to 2026-10-13T15:19:37.153Z.

## S3 medians

| Implementation | PUT MiB/s | GET MiB/s | Quarantine files/s | Peak sampled server RSS MiB |
|---|---:|---:|---:|---:|
| stow-memory | 182.75 | 466.95 | 105.75 | 73.81 |
| s3proxy-memory | 210.45 | 398.65 | 114.50 | 573.89 |
| stow-filesystem | 7.05 | 119.40 | 2.00 | 36.15 |
| s3proxy-filesystem | 66.65 | 401.20 | 113.15 | 411.00 |
| minio-filesystem | 65.55 | 502.80 | 65.40 | 381.11 |

Stow memory’s verified GET median was 466.95 MiB/s versus 398.65 for S3Proxy memory; PUT was 182.75 versus 210.45 MiB/s, and quarantine 105.75 versus 114.50 files/s. Its maximum sampled server RSS was 73.81 MiB versus 573.90 MiB for S3Proxy memory. S3Proxy used a 64 MiB initial / 1 GiB maximum JVM heap; different tuning can change this tradeoff.

Stow filesystem measured 7.05 MiB/s PUT, 119.40 MiB/s GET and 2.00 quarantine files/s. MinIO measured 65.55, 502.80 and 65.40 respectively. The filesystem baselines have different storage formats, accounting and acknowledgement guarantees; this is an application-level comparison of these deployments. It does not isolate the cost of any one feature.

For full latency, RPS, per-workload RSS and repetition ranges, see the [S3 report](comparison/s3/report.md) and [summary](comparison/s3/summary.json). Quarantine throughput counts incoming payload once per completed file workflow; it does not sum upload and download wire bytes. RPS counts individual successful requests and therefore differs from files/s.

## Embedded results

Three 300 ms benchmark repetitions per case; 4 KiB, 1 MiB and 8 MiB files at CPU settings 1 and 4. The table shows the **1 MiB / CPU 4** medians. Go’s decimal MB/s values are converted to binary MiB/s.

| Implementation | PUT MiB/s | Verified GET MiB/s | Quarantine files/s | PUT allocated MiB/op |
|---|---:|---:|---:|---:|
| buffer-memory | 23,819.59 | 7,518.99 | 3,092.72 | 1.00 |
| afero-memory | 109,633.78 | 7,145.65 | 2,988.30 | 0.00 |
| stow-memory | 481.08 | 6,617.37 | 421.23 | 5.04 |
| tempfile-filesystem | 1,842.78 | 6,694.20 | 2,761.82 | 0.01 |
| atomic-filesystem | 1,022.46 | 6,712.67 | 673.15 | 0.02 |
| afero-filesystem | 2,057.78 | 6,956.40 | 3,026.64 | 0.01 |
| stow-filesystem | 25.03 | 177.49 | 9.77 | 23.58 |

The owned-buffer baseline clones each PUT and preserves previously opened bytes. Afero’s repeated writes reuse file storage and do not offer the same immutable object publication semantics. Afero and the temporary-file baseline omit file/directory sync; the atomic-file baseline syncs both. Stow additionally computes object metadata and enforces admission. These rows are useful overhead baselines with different guarantees.

Stow memory PUT allocates approximately 5.04 MiB per 1 MiB operation, versus roughly 1.00 MiB for the owned-buffer baseline. Stow filesystem PUT allocates about 23.58 MiB/op. Allocation is cumulative Go heap activity per operation, not retained memory or RSS. Native GET includes hashing, so its rate is partly an integrity-check benchmark. Native quarantine has no scan delay.

See [all embedded cases and allocations](comparison/embedded-report.md), [original Go output](comparison/embedded.txt) and [analysis](comparison/analysis.json).

## Resource checks and limits

| Implementation / workflow | Median server CPU % | Median generator CPU % | Max target throttled periods % |
|---|---:|---:|---:|
| stow-memory / put | 96.40 | 100.92 | 0.00 |
| s3proxy-memory / put | 116.38 | 100.64 | 3.50 |
| stow-filesystem / put | 95.55 | 5.12 | 0.00 |
| s3proxy-filesystem / put | 49.46 | 35.78 | 0.00 |
| minio-filesystem / put | 39.89 | 36.94 | 0.00 |
| stow-memory / get | 163.59 | 147.01 | 20.00 |
| s3proxy-memory / get | 122.17 | 146.41 | 12.00 |
| stow-filesystem / get | 153.04 | 99.00 | 0.00 |
| s3proxy-filesystem / get | 125.31 | 148.74 | 10.00 |
| minio-filesystem / get | 187.60 | 148.38 | 44.28 |
| stow-memory / quarantine | 101.78 | 125.18 | 0.00 |
| s3proxy-memory / quarantine | 117.77 | 133.26 | 5.00 |
| stow-filesystem / quarantine | 102.14 | 6.17 | 0.00 |
| s3proxy-filesystem / quarantine | 120.72 | 131.69 | 4.00 |
| minio-filesystem / quarantine | 78.41 | 92.95 | 0.00 |

CPU percentages use 100% per core. Throttling is the fraction of cgroup periods that were throttled, not a percentage of lost wall time. Some fast GET runs hit the target quota. The generator had zero quota-throttled periods, but used up to about 151% CPU; one Node generator and fixed concurrency can still limit client-observed rates. A generator/concurrency sweep is needed before claiming maximum server capacity.

At least 3,012.59 MiB of data-volume space remained in all samples. RSS was sampled every 250 ms from the server child process. It excludes the controller, generator and filesystem page cache, and can miss short peaks. This is a bounded retained set; it does not establish memory-versus-capacity scaling or leak-free operation. Kernel RSS high-water counters are also preserved in the raw files.

The slowest filesystem quarantine repetitions had only 44 completed latency samples. Their p99 values are close to the slowest observations and should not be advertised as an SLA. Throughput counts completions within the window; latency includes accepted drain work. Closed-loop tests do not test overload admission or sustainable scheduled arrival rates.

No TLS, real scanner, DPI processing, flagged-file retention/promotion, read-through cache misses, cold disk cache, power-loss durability or distributed object-store resilience was measured. Native and S3 lane ratios do not isolate HTTP overhead because the resource ceilings, retained sets and storage classes differ. MinIO is built from pinned release source; it is a single-directory deployment.

## Next measurements

Profile the filesystem write and delete paths first. The current implementation [reads and decodes full object records](../../../internal/storage/fs/fs_records.go), stores payloads in JSON records and [scans capacity during mutations](../../../internal/storage/fs/capacity.go). Together with the observed allocations, these are plausible contributors; a CPU/allocation profile and retained-object sweep should establish their contribution before changing the implementation. Memory PUT buffer allocation is a separate optimization candidate.

Then qualify the actual inspection-and-retention use case: memory inspection, promotion of flagged content to disk, a real scanner, scheduled arrival sweeps and a longer soak. Use the [qualification protocol](../../performance-testing.md) before advertising sustained capacity.

## Reproduction and evidence

- [Benchmark runner and commands](../../../benchmark/README.md), [Railway IaC](../../../benchmark/.railway/railway.ts) and [environment receipt](../railway-environment.json).
- [Comparison configuration](comparison/profile.json), [completion receipt](comparison/run.json) and [S3 raw file index](comparison/s3/suite.json).
- [Smoke completion receipt](smoke/run.json); smoke timings are correctness checks and are excluded from the comparison tables.
- [Validation and evidence hashes](validation.json).

An earlier comparison ended after eight S3 cases because MinIO accepted HTTP before storage initialization. It is excluded. The final harness requires a successful authenticated list-buckets request before preparation. The corrected smoke passed before restarting the entire comparison.

The deployed source payload SHA-256 is `84e03ebcea1d6926419744428628d16d09343c3a4f3dcf3ee19043dc8581faf6`. The IaC volume-region fix, post-processing analyzer and documentation updates were added after this image snapshot; they do not alter its measured workload code. Version pins, binary hashes, harness hash and deployment metadata are embedded in the raw receipts.

Both compute deployments were stopped after export. Services, volume contents and IaC definitions remain for later runs. No benchmark images or workload data are retained locally; the saved evidence consists of small text/JSON files.
