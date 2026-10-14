# Paired write-preparation comparison — October 13, 2026

Managed filesystem saves improved in this Railway comparison.
Median throughput increased from 98.290 to 114.884 MiB/s.
Median allocated bytes decreased from 7,417,247.5 to 2,166,604.5 per acknowledged 1 MiB save, about 71% less.
This measures cumulative allocation per operation, not resident memory or retained object size.

The median paired throughput change was +15.638%.
The geometric mean change was +14.053%, with a conditional 95% interval of +2.829% to +26.501%.
Five of the six managed-save pairs improved; one decreased.
The benchmark performs 64 new keyed replacements per sample and verifies the final payload after timing.
It retains receipts and preserves file and directory synchronization.

The S3 results do not establish a throughput improvement from this refactor.
Every S3 interval includes no change.
Memory PUT measured lower and remains unresolved; the data also permits an increase.
This is not evidence of regression-free performance.

## S3 results

Rates are medians across six windows per variant.
PUT and GET use MiB/s; quarantine uses completed files/s.
Paired changes compare adjacent windows for the same backend and round.
They can differ from the ratio of the two overall medians.

| Backend | Workflow | Before | After | Median paired change | Conditional 95% interval |
|---|---|---:|---:|---:|---:|
| Memory | PUT | 177.650 | 171.050 | −4.314% | −10.616% to +6.794% |
| Filesystem | PUT | 66.500 | 66.475 | +0.038% | −1.695% to +3.909% |
| Memory | GET | 528.050 | 535.900 | −0.467% | −8.550% to +14.416% |
| Filesystem | GET | 541.275 | 551.700 | +2.249% | −10.043% to +9.065% |
| Memory | Quarantine | 104.825 | 104.425 | −1.388% | −4.265% to +3.609% |
| Filesystem | Quarantine | 55.875 | 54.400 | −3.616% | −19.935% to +14.843% |

Peak sampled filesystem GET RSS was 21.137 MiB before and 21.664 MiB after.
Peak sampled memory PUT RSS was 66.168 MiB before and 66.984 MiB after.
The large allocation reduction belongs to keyed native saves; ordinary S3 PUT does not exercise retained save receipts.
Filesystem quarantine windows included fewer than 1,000 completed files, limiting interpretation of their p99 latency.

The memory PUT generator consumed about one CPU in both variants, while the target remained below two CPUs.
That suggests investigating client execution as well as the server; it does not establish the limiting path.
Per-window process CPU, cgroup throttling and event-loop observations are retained in the analysis and raw files.

## Startup and first write

Each entry contains 18 fresh server-process observations from the S3 cases.
First-write timing includes startup and a verified 4 KiB bootstrap write over target-local HTTP.
These timings do not include container startup or a WASM agent startup.

| Backend | Ready before / after | First write before / after |
|---|---:|---:|
| Memory | 17.256 / 16.903 ms | 20.574 / 20.878 ms |
| Filesystem | 25.921 / 25.400 ms | 37.736 / 37.908 ms |

## Controls and scope

All builds, race tests, workload smoke checks, measurements and performance analysis ran on Railway.
Both roles used the same image identity and both variants ran on the same target worker and persistent volume.
The target had two CPUs and 2 GiB memory; the generator had four CPUs and 4 GiB memory.
Both reported AMD EPYC 9655P processors.

The S3 profile used 1 MiB objects, four clients, 16 retained fixtures, five-second warm-ups and 20-second windows.
Matching backend variants ran adjacent windows; their order reversed on alternate rounds.
The completed run contains 72 S3 windows, 72 native comparison samples and 12 managed-save samples.
There were zero failed or dropped files; every completed S3 window passed payload and disk-headroom verification.
The authenticated S3 smoke suite also passed all 12 cases.

Native measurements use the runner's ephemeral storage; S3 filesystem measurements use the target's persistent volume.
The protocol uses warm caches and private HTTP without TLS.
Quarantine includes a SHA-256-verified GET, a two-millisecond simulated scanner, verdict PUT and two deletes.
It does not run a real scanner, retain flagged content or qualify sustained capacity.

Intervals use Student-t limits on the mean paired log throughput ratio with five degrees of freedom.
They describe these six rounds on one shared worker and do not account for every source of host or storage variability.
Native ordinary PUT, GET and quarantine intervals also include no change.
The complete native results and allocations appear in the analysis.

## Provenance and retention

The preceding implementation was reconstructed using the recorded seven-file patch and checked file hashes.
It retains the usage/recovery consolidation and earlier streaming changes.
It is not the historical pre-streaming build and cannot attribute the older HTTP PUT regression.
Both variants passed race tests; the preceding variant excludes the new empty-reader and computed-checksum retry tests.
Its expected retry failures were separately reproduced during the cloud build.

Run: `7f7cb5a5-f003-468c-9a58-a946673457a5`.
UTC measurement interval: October 14, 03:22:15–03:53:52 (October 13 in Toronto).
Source payload SHA-256: `1dd00722a4f0fec71b45a2f8de222fd1dcfded1b917bddbfa83b2eb54cb0e7da`.
Runner deployment: `c544a874-4682-4e38-93ca-b27117c8af7e`.
Target deployment: `68cce335-7eb7-4811-b3f2-a6fd122d3b71`.
Both were observed `SUCCESS`, then `REMOVED` after verified export.

- [Completed run receipt](comparison/run.json), [paired analysis](comparison/analysis.json) and [paired report](comparison/paired-report.md).
- [Effective profile](comparison/profile.json), [S3 raw index](comparison/s3/suite.json) and [smoke receipt](smoke/receipt.json).
- [Binary and source identities](comparison/build-variants.json), [retention receipt](comparison/binary-retention.json) and [export verification](export.json).
- [Executed analyzer](comparison/analyze-controlled.mjs), [native source archive](comparison/native-source.tar.gz) and [baseline regression log](comparison/baseline-regression.txt).
- [Baseline reconstruction patch](../../../benchmark/variants/before-write-preparation.patch) and [patch specification](../../../benchmark/variants/write-preparation.json).

Both server and native benchmark binaries remain on the Railway results volume under the retained build path in the receipt.
The uploaded source archive was verified against the compiled production-source hash.
No binaries were downloaded locally.
The website dataset and SDK artifact were unchanged; no chart was published.
