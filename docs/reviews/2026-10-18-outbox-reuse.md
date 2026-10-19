# Reuse byte-identical validated outbox state — 2026-10-18

## Evidence and change

The [preceding Railway investigation](2026-10-18-outbox-read-cost.md) showed a
16,385-row provenance lookup allocating 5.30 MB and spending most of its CPU in
full decoding. Disk reads were a small fraction of its latency. This change is
based on `f2a65d9`; `origin/main` was refreshed before work and is recorded in
[baseline.json](2026-10-18-outbox-reuse/raw/baseline.json).

Seven production lines add one private byte slice, compare a fresh file read
with its contents, remember successfully validated bytes, and clear them before
any persistence attempt. Every operation still acquires the existing mutex and
filesystem lock and performs `os.ReadFile`. Only exact byte equality can reuse
the corresponding normalized state. Different bytes take the original full
format/recovery validation path. State mutations still clone before publication.
No file-identity/size/timestamp shortcut, hash, dependency, public API or format is
introduced. Existing protobuf schema 4 and legacy JSON migration are unchanged.

The unchanged-provenance path still executes its file and parent-directory
fences. Permissions repair and symlink fallback still perform a full replacement.
Missing/unreadable/corrupt journals still fail before an operation can use cached
state. Clearing the retained bytes before persistence prevents a successful or
uncertain write from leaving bytes that describe a different in-memory state.

## Safety evidence

New black-box tests rewrite a valid journal in place while preserving inode,
length and modification time; a live handle must observe the new validator.
Same-metadata invalid UTF-8 and unsupported-schema edits must fail without
returning stale provenance or replacing the journal, and restoring valid bytes
must recover retained entries. A historical snapshot restored immediately after
a successful write must be read faithfully, preserving sequence allocation.

The baseline and isolated prototype each passed the complete file-outbox suite
twice normally and twice under race detection in the [pilot](2026-10-18-outbox-reuse/raw/pilot/comparison-commands.json).
A deliberately broken overlay omitting write invalidation fails the historical
restore test twice normally and twice under race detection. The corrected code
passes it. See [control results](2026-10-18-outbox-reuse/raw/repair/negative-control.json),
[normal failures](2026-10-18-outbox-reuse/raw/repair/negative-control-normal.log) and
[race failures](2026-10-18-outbox-reuse/raw/repair/negative-control-race.log). This is a sensitivity check
for the new optimization, not a claim that the baseline had this correctness bug.
The original defect being addressed is repeated unnecessary full decoding.

Existing coverage also checks competing handles/processes, lock failures,
missing/truncated/null journals, prepared/active ownership and tokens, legacy
migration, mode/symlink repair and post-rename directory-fence failures.

## Measurement method

All formatting, compilation, tests, conformance, race checks and benchmarks ran
on the existing Railway scratch runner. Local work was source editing, Git,
packaging, hashing and analysis of exported results. Pinned Go 1.25.6 and Node
25.9.0 ran in a 4-CPU/4-GiB cgroup with ext4 `/results`. Exact deployment, runtime
settings, binary/source hashes and commands are retained in each cohort.

The same diagnostic benchmark is compiled against the original production file
and the changed file. Journal fixtures are seeded once in a separate process,
so seed encoding does not enter the benchmark process's memory observations.
Shapes match the preceding investigation: extra provenance depths 0, 2,048,
16,384, 65,536 and 262,144, plus mixed 1,024/16,384 provenance and pending rows.
Every shape includes one target row. Large shapes are stress cases, not typical
workload or supported-capacity claims. Values, lengths and timestamps are fixed.

Pilot: 36 observations, three shapes, CPU 1, 100-ms minimum, ABBA order. Main:
336 timing observations, seven shapes × lookup/unchanged/changed saves × CPU 1/4,
four observations per variant/cell, 500-ms minimum and ABBA then BAAB order.
Each observation has a separate Go process. CPU 4 reverses fixture/stage order.
CPU settings are `GOMAXPROCS`, not client concurrency or affinity. All operations
are serial and files are warm. Reads are warmed outside timing. Changed-write
setup ends with two real changes, returning the target to its initial validator;
this invalidates the prototype before timing, so every timed changed save must
perform a full reload. Changed saves alternate equal-length validators. Target
values and fixture lengths are checked outside timing. No observation is dropped.

The main cohort also has 24 separate fixed-count lookup observations: 32 timed
operations at 16,384/262,144 provenance and mixed=16,384, CPU 1/4, ABBA order.
These compare memory at equal timed operation counts; process setup and Go's
one-iteration calibration remain included in high-water measurements.
There are **396 retained observations** across pilot, main timing and fixed-count
cohorts. The cohorts are not pooled into a single median.

B/op measures cumulative timed allocation. `process-live-heap-B` is process-wide
HeapAlloc after verification and a forced GC while the handle remains live; the
save probes include a verification lookup. It includes runtime/test overhead and
is not an isolated handle-size measurement. Differences between matched binaries
estimate the additional retained memory. `process-HWM-B` is process-lifetime
VmHWM sampled after timed work, including constructor, preparation and calibration.
It is neither retained heap nor a per-operation increment. RSS belongs to the Go
process and excludes parent-runner overhead and buffered kernel file cache.
Fixed-count probes supplement the time-based probes because the faster variant
completes more work during the same time. GC and calibration can produce noisy,
non-monotonic high-water values; no process-memory bound is claimed.

## Main comparison

Cells show before → after median latency in milliseconds. Allocation columns are
actual lookup B/op; extra heap is the matched lookup median process-live-heap
difference in KiB. Full observations/ranges for every operation are retained.

CPU 1:

| Fixture | Lookup ms | Unchanged save ms | Changed save ms | Lookup B/op | Extra live heap KiB |
| --- | ---: | ---: | ---: | ---: | ---: |
| provenance=0 | 0.008 → 0.007 | 0.332 → 0.331 | 5.231 → 5.219 | 1,872 → 1,096 | 0.4 |
| provenance=2048 | 0.327 → 0.018 | 0.455 → 0.320 | 6.252 → 6.494 | 647,424 → 123,464 | 120.0 |
| provenance=16384 | 3.306 → 0.180 | 3.563 → 0.296 | 18.982 → 19.194 | 5,301,600 → 926,280 | 904.1 |
| provenance=65536 | 15.884 → 0.913 | 17.555 → 1.033 | 67.324 → 70.281 | 21,664,992 → 3,678,792 | 3,591.8 |
| provenance=262144 | 83.545 → 4.902 | 78.689 → 5.042 | 525.579 → 500.185 | 86,995,808 → 14,688,840 | 14,343.9 |
| mixed=1024 | 0.907 → 0.028 | 1.031 → 0.318 | 9.219 → 9.435 | 1,368,912 → 164,424 | 159.9 |
| mixed=16384 | 20.683 → 0.840 | 21.215 → 0.994 | 69.101 → 67.514 | 22,149,042 → 2,572,872 | 2,512.2 |

CPU 4:

| Fixture | Lookup ms | Unchanged save ms | Changed save ms | Lookup B/op | Extra live heap KiB |
| --- | ---: | ---: | ---: | ---: | ---: |
| provenance=0 | 0.008 → 0.007 | 0.326 → 0.328 | 4.312 → 4.139 | 1,872 → 1,096 | 2.8 |
| provenance=2048 | 0.435 → 0.027 | 0.481 → 0.320 | 6.512 → 6.114 | 647,426 → 123,464 | 131.5 |
| provenance=16384 | 4.589 → 0.225 | 4.481 → 0.292 | 19.049 → 19.476 | 5,301,602 → 926,280 | 903.8 |
| provenance=65536 | 16.838 → 0.974 | 16.182 → 1.110 | 67.754 → 65.682 | 21,664,996 → 3,678,792 | 3,592.1 |
| provenance=262144 | 65.739 → 3.667 | 73.176 → 4.239 | 498.143 → 475.674 | 86,995,808 → 14,688,840 | 14,344.0 |
| mixed=1024 | 1.446 → 0.048 | 1.419 → 0.319 | 8.605 → 8.937 | 1,368,924 → 164,424 | 165.6 |
| mixed=16384 | 17.820 → 0.694 | 19.292 → 0.733 | 56.984 → 58.355 | 22,149,058 → 2,572,872 | 2,512.2 |

[Main observations](2026-10-18-outbox-reuse/raw/main/samples.json), [all medians/ranges](2026-10-18-outbox-reuse/raw/main/analysis.json),
[pilot observations](2026-10-18-outbox-reuse/raw/pilot/samples.json) and [pilot ranges](2026-10-18-outbox-reuse/raw/pilot/analysis.json)
retain favorable and adverse results. These small balanced samples are diagnostic,
not statistical-significance claims. Timing, memory and relative results can
change with CPU scheduling, storage, contention, key lengths and workload mix.

Adverse main latency medians: CPU 1 mixed=1024/changed: +2.35%; CPU 1 provenance=16384/changed: +1.12%; CPU 1 provenance=2048/changed: +3.87%; CPU 1 provenance=65536/changed: +4.39%; CPU 4 mixed=1024/changed: +3.85%; CPU 4 mixed=16384/changed: +2.41%; CPU 4 provenance=0/unchanged: +0.76%; CPU 4 provenance=16384/changed: +2.24%. All original pilot adverse medians also remain in its retained analysis. Timing ranges are available for attribution limits; no claim separates code cost from filesystem/scheduling variation without further evidence.

Pilot adverse latency medians were mixed=1024/changed: +12.72%; provenance=0/changed: +7.37%; provenance=0/unchanged: +9.78%; provenance=16384/changed: +2.51%. The mixed=1024 changed-save median was +12.72% in the pilot and +2.35% in the longer main CPU-1 cohort. Both remain evidence, with overlapping timing ranges; neither result is substituted for the other.

## Memory and costs

Fixed-count lookup probes below show maximum observed whole-process high-water
RSS and median post-GC process live heap, in MiB, before → after. These are different
quantities and neither is a memory requirement for a real Stow deployment.

| CPU | Fixture | Max HWM RSS MiB | Live heap MiB |
| ---: | --- | ---: | ---: |
| 1 | mixed=16384 | 104.1 → 51.4 | 10.33 → 12.78 |
| 1 | provenance=16384 | 79.0 → 24.3 | 2.73 → 3.61 |
| 1 | provenance=262144 | 386.1 → 147.7 | 38.38 → 52.38 |
| 4 | mixed=16384 | 95.5 → 57.8 | 10.34 → 12.79 |
| 4 | provenance=16384 | 58.3 → 24.6 | 2.74 → 3.62 |
| 4 | provenance=262144 | 238.0 → 145.0 | 38.39 → 52.40 |

The handle can retain one journal read buffer, whose capacity may exceed file
length, plus a 24-byte slice field on this 64-bit target. On a changed journal,
the previous retained buffer can coexist transiently with the new read/decoder.
Writes still clone, encode and replace the whole journal. Ordinary unchanged
reads still allocate/read the whole encoded file and compare all bytes; the
optimization removes repeat decoding, not linear I/O, storage or memory scaling.
Cache byte/object limits still govern cached objects, not journal history.

[Fixture sizes](2026-10-18-outbox-reuse/raw/main/fixtures.json), [runtime settings](2026-10-18-outbox-reuse/raw/main/runtime-settings.json),
[build/source hashes](2026-10-18-outbox-reuse/raw/main/comparison-build.json), [commands](2026-10-18-outbox-reuse/raw/main/comparison-commands.json)
and [fixed-count observations](2026-10-18-outbox-reuse/raw/main/cells.json) make those costs reviewable.

## Validation and evidence handling

The changed production source passed focused file-outbox tests twice normally
and twice under race detection, the full Go suite, broad storage/runtime/embedded/
run-through/S3/public-package race tests, four conformance profiles, vet, quality,
file-size/format gates, site build/tests and documentation links on Railway.
The [main checks](2026-10-18-outbox-reuse/raw/main/checks.json) include one control-harness failure: its
unprivileged process attempted to overwrite the root-owned outer log with the
same name. The corrected log name was verified in a separate [repair job](2026-10-18-outbox-reuse/raw/repair/checks.json)
after all timing measurements, with byte-identical Go sources. Only that affected
control was repeated; all main performance evidence is retained.

An initial deployment failed before compilation because the upload used the
packaging directory instead of its source root. The corrected deployment ran all
validation and benchmarks. The [failed build log](2026-10-18-outbox-reuse/raw/pilot-build-failure.log)
is retained. Neither harness correction changes production behavior or supplies
missing performance observations by substitution.

The final report and operational documentation are checked on Railway against
the tested Go source hashes. Binaries and fixture directories are removed after
metadata is saved; the scratch deployment is stopped after verified export.
No dependency, public API or persistence format changes are introduced.

## Next finding

Snapshot-only reads still call `readState`, cloning the complete provenance map
before returning pending/prepared entries. These paths serve existing propagation
ordering, recovery and inspection. Measure their cost and ownership requirements
before changing them. Changed writes also retain their full-journal clone/encode/
publication cost; no indexing, incremental checkpointing or storage abstraction
has begun.
