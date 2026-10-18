# Outbox full-journal read and decode cost — 2026-10-18

## Scope and product relevance

This is a measurement-only change on top of `dfe17b0`. Production code, journal
format and public APIs are unchanged. `origin/main` was refreshed before starting;
the recorded baseline is in [baseline.json](2026-10-18-outbox-read-cost/raw/baseline.json).

Every file-backed provenance lookup/save reloads the journal under the existing
process and filesystem locks. The decoder first constructs generated protobuf
messages, then validates and converts them into native maps. Successful cache
reads can save provenance; cache eviction removes the cached object and its cache
index entry, retaining the upstream baseline used by later propagation. Therefore
retained distinct-key history, as well as queued work, can grow this journal.
The cache byte/object limits govern the separate upstream-derived object cache;
these measurements do not establish a violation of a promised journal memory bound.
See [cache limits](../run-through.md#cache-limits-and-expiration),
[the outbox contract](../outbox.md) and the implementation paths
`internal/runthrough/cache_read.go`, `cache_policy.go` and `provenance.go`.

## Method and limits

All compilation, formatting, validation, profiling and benchmarking ran on the
existing Railway scratch runner. Local work was source editing, Git, packaging
and analysis of exported results. The runner used pinned Go 1.25.6 and Node
25.9.0, a 4-CPU/4-GiB cgroup and its ext4 `/results` volume. The deployment,
cgroup and host details are in [environment.json](2026-10-18-outbox-read-cost/raw/diagnostic-environment.json).
CPU 1 and 4 are `GOMAXPROCS` settings; operations are serial, with no competing
client processes. These are warm-file measurements, not cold-start, throughput,
concurrency, end-to-end S3 latency or supported-capacity guarantees.

Seven fixtures contain 0, 2,048, 16,384, 65,536 or 262,144 extra provenance rows,
or 1,024/16,384 extra provenance rows plus the same number of pending entries.
Each also contains one target provenance row. Keys, ETags and timestamps have
fixed shapes matching the earlier provenance benchmarks. Larger journals are
stress cases, not claims about typical deployments. Fixture creation happens in
a separate process, keeping encoder construction out of benchmark RSS. Fixture
sizes, row counts and initial hashes are retained in [fixtures.json](2026-10-18-outbox-read-cost/raw/fixtures.json).

Six stages run in a separate process per fixture/stage/CPU combination, with two
repetitions, a one-second Go benchmark minimum and allocation reporting. CPU-4
fixture and stage order reverses CPU-1 order. All **168 final timing observations** are
retained; there is no before/after production implementation in this experiment.
Decode/unmarshal probes use pre-read bytes; read/API probes reread the warm file.
Public lookup/save
stages open the real file-backed outbox and call its existing methods. Changed
saves alternate equal-length ETags. Counts and target values are checked outside
timing, and all 42 stage/fixture cases also run once under race detection.

- `read`: `os.ReadFile`, without decoding or locking.
- `unmarshal`: generated protobuf unmarshalling from existing bytes. It omits
  native-state construction and the decoder's format/unknown-field/recovery
  validation, so it is a component probe, not a replacement implementation.
- `decode`: the complete existing decoder from existing bytes.
- `lookup`, `unchanged`, `changed`: the actual public outbox operations, including
  fresh reload and their applicable locking and durability work.

These stage timings are not additive: live data, garbage collection, system calls
and setup differ. B/op reports cumulative allocation, not retained heap or RSS.
`process-HWM-B` is Linux process-lifetime VmHWM sampled after timed work. It includes
constructor/setup, calibration and earlier repetitions of the same cell, and is
not an isolated per-operation memory increment. Each cell has its own process;
the seed process is excluded. Read-only cells retain the previous read buffer
through the next read; decode probes similarly retain the previous decoded result
through the next decode. Measurements use normal runtime GC defaults; the selected
[Go runtime settings](2026-10-18-outbox-read-cost/raw/runtime-settings.json) were recorded without credentials.
High-water values can vary substantially with calibration and GC history and
need not grow monotonically across fixtures. They are observations, not a memory
requirement or a bound for a real deployment.

## Results

CPU-1 median latency in milliseconds:

| Fixture | Journal bytes | Read | Unmarshal | Decode | Lookup | Unchanged save | Changed save |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| provenance=0 | 65 | 0.004 | <0.001 | <0.001 | 0.007 | 0.328 | 3.818 |
| provenance=2048 | 114753 | 0.012 | 0.248 | 0.299 | 0.328 | 0.393 | 5.817 |
| provenance=16384 | 917569 | 0.069 | 2.104 | 2.881 | 3.293 | 3.496 | 19.286 |
| provenance=65536 | 3670081 | 0.169 | 10.458 | 14.049 | 17.109 | 16.791 | 65.528 |
| provenance=262144 | 14680129 | 0.948 | 38.852 | 71.994 | 78.754 | 83.919 | 504.707 |
| mixed=1024 | 157598 | 0.013 | 0.464 | 0.659 | 0.831 | 0.916 | 7.762 |
| mixed=16384 | 2566529 | 0.115 | 10.839 | 17.032 | 20.773 | 20.017 | 63.362 |

Actual lookup medians; RSS is the maximum observed whole-process high water through the measured work:

| CPU | Fixture | ms/op | Allocated B/op | Allocations/op | Max process HWM MiB |
| ---: | --- | ---: | ---: | ---: | ---: |
| 1 | provenance=0 | 0.007 | 1,872 | 18 | 17.9 |
| 1 | provenance=2048 | 0.328 | 647,424 | 6,182 | 24.1 |
| 1 | provenance=16384 | 3.293 | 5,301,600 | 49,253 | 84.6 |
| 1 | provenance=65536 | 17.109 | 21,664,992 | 196,907 | 107.4 |
| 1 | provenance=262144 | 78.754 | 86,995,808 | 787,505 | 395.9 |
| 1 | mixed=1024 | 0.831 | 1,368,912 | 12,338 | 21.9 |
| 1 | mixed=16384 | 20.773 | 22,149,042 | 196,794 | 114.3 |
| 4 | provenance=0 | 0.007 | 1,872 | 18 | 19.6 |
| 4 | provenance=2048 | 0.424 | 647,428 | 6,182 | 31.1 |
| 4 | provenance=16384 | 4.425 | 5,301,604 | 49,253 | 73.8 |
| 4 | provenance=65536 | 18.145 | 21,664,997 | 196,907 | 84.4 |
| 4 | provenance=262144 | 71.170 | 86,995,816 | 787,505 | 237.6 |
| 4 | mixed=1024 | 1.326 | 1,368,925 | 12,338 | 21.7 |
| 4 | mixed=16384 | 17.501 | 22,149,053 | 196,794 | 87.7 |

[Every observation](2026-10-18-outbox-read-cost/raw/samples.json), [medians and full ranges](2026-10-18-outbox-read-cost/raw/analysis.json)
and [exact commands](2026-10-18-outbox-read-cost/raw/diagnostic-commands.json) include all stages and both CPU
settings. Two repetitions per cell are a diagnostic sample, not a statistical
significance study. Results can change with storage, GC, contention, key/value
sizes, pending-entry composition and deployment resources.

The initial 168-observation cohort completed all measurements and tests but failed
its quality gate because the benchmark helper exceeded the complexity ceiling.
The helper was split into setup, measurement and verification functions, and the
entire validation/matrix was repeated. No baseline was raised. The two cohorts
have different benchmark instrumentation and are not pooled. Both remain evidence:
[initial checks](2026-10-18-outbox-read-cost/raw/initial/checks.json), [initial observations](2026-10-18-outbox-read-cost/raw/initial/samples.json)
and [initial ranges](2026-10-18-outbox-read-cost/raw/initial/analysis.json). There are **336 retained timing
observations** across both cohorts. Tables above use the final source only.

## Profiles and decision

CPU profiling uses an actual lookup at 65,536 extra provenance rows, CPU 1, with a
five-second minimum. Allocation profiling uses the 16,384-row actual lookup,
20 timed iterations and `memprofilerate=1`, separately from timing observations.
Allocation profiles include process setup/calibration and are not direct per-op
estimates. [CPU profile top](2026-10-18-outbox-read-cost/raw/lookup-cpu-top.log),
[allocated bytes](2026-10-18-outbox-read-cost/raw/lookup-alloc-space.log),
[allocated objects](2026-10-18-outbox-read-cost/raw/lookup-alloc-objects.log) and the [CPU](2026-10-18-outbox-read-cost/raw/lookup-cpu.pprof) / [allocation](2026-10-18-outbox-read-cost/raw/lookup-alloc.pprof) profiles are
retained.

Full decoding is the dominant remaining cost for large-journal lookups and
unchanged saves. At 16,384 extra provenance rows, CPU-1 medians are 0.069 ms
for a warm file read, 2.881 ms for full decoding from bytes and 3.293 ms
for the actual lookup. The actual lookup profile attributes **85.44% of sampled
CPU** cumulatively to `decodeOutboxProto`. The separate allocation profile
attributes **82.36% of allocated bytes** cumulatively to that decoder. Generated
message objects, string copies, repeated-field slices and the native map all
contribute; the profile tables retain their individual costs.

At 262,144 extra provenance rows, CPU-1 lookup is 78.754 ms and allocates
87.00 MB/op. A changed save is 504.707 ms and allocates 154.15 MB/op.
The latter includes cloning, full serialization and durable replacement and
remains a distinct cost. These measurements establish a scaling constraint in an
existing supported path; they do not demonstrate a new correctness defect or
certify those stress sizes as deployment limits.

The next comparison should test reuse of the existing decoded state only after
an ordinary locked file read proves the bytes are identical to the last fully
validated snapshot. Compare repeated lookup/unchanged saves, real changed saves
and cross-process updates against this baseline. Measure the additional retained
journal bytes per handle as well as allocations, RSS and latency. Content changes,
in-place corruption, missing files, legacy migration and uncertain post-rename
persistence must still follow existing validation/recovery behavior. File identity,
size and timestamps alone are insufficient evidence of identical content.

This experiment has not begun. It would target repeat decoding of unchanged
content; it would not remove changed-journal decoding, serialization, full-file
reads or linear journal storage. No indexing, incremental checkpointing, decoder
replacement or new storage abstraction has started.

## Validation and cost

All 13 Railway stages passed: formatting, the full Go suite, broad storage/runtime/
embedded/run-through/S3/public-package race tests, four conformance profiles, vet,
quality/file-size/format gates, site build/tests, documentation links, and the
measurement step. The measurement step also passed its 42-case race smoke.
[Checks](2026-10-18-outbox-read-cost/raw/checks.json) record commands and results; [source hashes](2026-10-18-outbox-read-cost/raw/environment.json)
bind them to the exact benchmark and production source. The source manifest records
the uploaded tree before Railway formatting; the final source hashes record the
formatted benchmark returned to the local worktree.

The only code addition is an opt-in Linux diagnostic benchmark/fixture generator.
Ordinary tests skip fixture generation unless explicitly configured, and the
benchmark requires a separately seeded fixture directory. It adds no production
state, dependencies, formats or public APIs. No production speedup is claimed.
Benchmark binaries and fixtures were deleted after their metadata/profiles were
saved. All raw observations and the harness are retained. The report is checked
on Railway against the already validated source hashes before the runner stops.

[Artifact manifest](2026-10-18-outbox-read-cost/raw/artifact-manifest.json) records the retained files and hashes.
