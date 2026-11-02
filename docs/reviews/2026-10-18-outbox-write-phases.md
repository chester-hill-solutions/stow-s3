# Outbox write phases on the Railway persistent volume — 2026-10-18

## Outcome

Keep the occupied-ID correctness guard. Its explicit-ID memory cost is measurable:
about 25–36 ns per prepare/discard cycle in this cohort, with unchanged allocation
counts. The longer ext4 comparison still has adverse medians, **+4.3% and +10.0%**.
The previous **+26.2% and +27.6%** observations remain in the
[admission report](2026-10-18-outbox-admission.md); they are not removed, replaced
or pooled with these new observations.

The phase evidence changes the next action. At 256 retained intents, file and
directory syncs consume about **89%** of aggregate cycle time. With 8,192 retained
intents, decoding and encoding consume about **58%**, and cloning another **9%**.
The admission/update itself is measured in microseconds in the instrumented path,
not milliseconds. No product implementation is changed in this investigation.
No fence is weakened, and no new index, checkpoint, storage abstraction, dependency,
format or public product API is added.

## Baseline and scope

The working baseline is `c32db46da7365256463c052cd57f95373679777b`.
`origin/main` was fetched and remains `219d1dcfff9807b04660a40313b32127e5be2933`.
The pre-admission source is `49eaa807bb8a4e97979d83568ba5f6937ddc1000`.
Exact refs are in [baseline.json](2026-10-18-outbox-write-phases/raw/baseline.json).
The previous goal turn committed the correctness change and its verified evidence;
this turn supplies measurements needed before choosing the next optimization.

The existing whole-journal path remains authoritative: acquire handle mutex and
filesystem lock, read/reload, clone all three maps, update, encode, write temporary
file, set permissions, sync file, close, rename, sync directory, unlock. Successful
writes invalidate the existing validated-byte cache, so the next mutation decodes
fresh bytes. Required error propagation and recovery semantics remain intact.

Three predictions guided the investigation: sync dominance should appear in file
and directory phases; journal CPU/allocations should grow with backlog; and a
material admission CPU regression should be visible outside filesystem waits.

All execution occurs on the already-authorized Railway scratch service
`83497842-c51d-420e-8ba1-181589730ad4`, benchmark environment
`3cd200cb-c94f-4e4d-9335-0f6c1b7e9411`, deployment
`e2aa1875-8a12-48ab-a612-c69b1f1c4e50`. Go 1.25.6, Node 25.9.0, GOMAXPROCS 4,
4 CPU/4 GiB cgroup, UID 10001. TMPDIR is explicitly under the ext4 `/results`
volume; [filesystem metadata](2026-10-18-outbox-write-phases/raw/main/fixture-filesystem.json) records the actual
path and statfs type. No benchmark runs on the local computer. Local actions are
reads, edits, packaging, Git, hashing and exported-data analysis.

## Uninstrumented before/after and control runs

The existing byte-identical `BenchmarkOutboxAdmission` source is used for both
revisions. Before differs only through a Go overlay of the original two changed
production files. Fixtures retain zero or 128 entries **in each queue**; 128 means
256 retained intents. Each timed operation prepares a fresh available explicit ID
and discards it: two public calls, two replacements, two file syncs and two
directory syncs. Seeding and retained-field verification are outside timing.
These are serial synthetic cycles, not a concurrent S3 workload or an SLA.

The ext4 before/after sequence is ABBA BAAB repeated four times: 32 processes,
16 observations per variant/cell, minimum one second per case, **64 observations**.
A separate eight-process ext4 A/A sequence executes the exact current binary under
both labels: **16 observations**, four per label/cell. Binary hashes are recorded,
not inferred from labels. Eight memory before/after processes add **16 observations**,
minimum 500 ms per case. The three cohorts stay separate: **96 observations total**.
Allocation bytes/counts are cumulative per two-call cycle, not retained heap/RSS.

| Cohort | Retained per queue | Before/label A median ms | After/label B median ms | Change | Allocs/cycle before → after |
| --- | ---: | ---: | ---: | ---: | ---: |
| Identical binary | 0 | 6.696 | 6.860 | +2.45% | 86 → 86 |
| Identical binary | 128 | 8.093 | 8.033 | -0.75% | 6,033 → 6,033 |
| Before/after | 0 | 6.824 | 7.117 | +4.29% | 86 → 86 |
| Before/after | 128 | 7.802 | 8.579 | +9.96% | 6,033 → 6,033 |

| Memory retained per queue | Before ns/cycle | After ns/cycle | Change | B/cycle before → after | Allocs/cycle before → after |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 203.60 | 239.30 | +17.53% | 352 → 352 | 1 → 1 |
| 128 | 215.30 | 240.55 | +11.73% | 352 → 352 | 1 → 1 |

The four chronological eight-process before/after blocks do not show a stable
positive effect. Their median changes are:

| Retained per queue | Block 1 | Block 2 | Block 3 | Block 4 |
| ---: | ---: | ---: | ---: | ---: |
| 0 | +1.06% | -4.74% | +15.28% | +9.07% |
| 128 | -8.22% | -1.02% | +6.89% | +4.12% |

Pooled medians need not equal block medians. Do not infer significance, eliminate
adverse samples or declare a zero implementation effect from this small set of
processes. The larger earlier slowdown is not reproduced at the same magnitude;
positive medians still remain and require caution.
[All raw observations, ranges, allocations and block calculations](2026-10-18-outbox-write-phases/raw/main/analysis.json)
and [commands, order and binary hashes](2026-10-18-outbox-write-phases/raw/main/comparison-runs.json) are retained.

## Phase measurements

Instrumentation exists only in Go overlay copies archived as `.txt`, not product
source. Timers surround existing operations without replacing IO, skipping syncs,
ignoring errors or changing publication order. A private test installs an
in-process sink during serial measured cycles, buffers phase durations and emits
JSON after each cycle. A diagnostic-only exported sink connects the two overlay
packages; it is absent from every product Go file. Formatting/build/tests run on
Railway. Instrumentation overhead, scheduler/GC effects and cache warmth limit
absolute comparisons with uninstrumented binaries.

The current-path probe runs 60 cycles at each of three depths: **180 cycles**.
It checks every expected phase occurs twice per cycle, then reopens and compares
all retained state. Idle journal bytes below exclude the temporary probe intent.
Shares use summed phase time divided by summed wall time, not sums of medians.
Nested reload/read/decode and atomic/detail values are not additive.

| Retained per queue | Idle journal B | Cycle p50 ms | Cycle p95 ms | File + directory sync share | Decode share | Encode share | Clone share |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 14 | 5.641 | 8.947 | 96.80% | 0.16% | 0.12% | 0.02% |
| 128 | 25,486 | 8.466 | 12.326 | 88.84% | 3.82% | 2.93% | 0.96% |
| 4096 | 815,118 | 43.010 | 49.117 | 28.77% | 31.77% | 25.78% | 8.72% |

A separate paired probe applies the same instrumentation to both revisions,
ABBA BAAB, eight processes, 60 cycles per depth/process at depths 0 and 128:
**960 cycles**, 240 per variant/depth. These are eight process-level observations,
not 960 independent benchmark repetitions. The table uses medians of the four
process statistics in each label. Phase columns are per-process mean duration,
then median across processes; they should not be summed as an exact decomposition.

| Retained per queue | Cycle p50 ms before → after | Atomic publication ms before → after | Admission/update µs before → after |
| ---: | ---: | ---: | ---: |
| 0 | 6.615 → 7.094 | 6.716 → 7.325 | 1.995 → 2.264 |
| 128 | 8.443 → 8.266 | 8.112 → 7.758 | 1.406 → 1.739 |

The paired 128-depth result changes sign relative to the uninstrumented comparison;
the empty result remains adverse. In the empty paired case, the atomic-publication
increase is roughly 0.61 ms while admission/update increases roughly 0.27 µs.
The observations localize dominant variation to filesystem publication, but do not
prove the implementation has no effect on filesystem scheduling or establish
steady-state provider latency. The nanosecond-scale memory admission cost is real.
The original adverse result remains visible rather than being classified away as
noise. [Paired summaries and per-process statistics](2026-10-18-outbox-write-phases/raw/paired/paired-analysis.json)
and [paired commands and hashes](2026-10-18-outbox-write-phases/raw/paired/comparison-runs.json) are separate.

## Component allocations

A separate steady-loop benchmark directly measures clone, decode and encode of
the same seeded state, three observations per component/depth, 500-ms minimum:
**27 observations**. Sink values prevent dead-code elimination. These component
measurements exclude locks, file reads, syncs and cross-component cache/GC effects;
their medians are not a prediction of end-to-end savings.

| Retained per queue | Component | Median µs/op | B/op | Allocs/op |
| ---: | --- | ---: | ---: | ---: |
| 0 | clone | 0.115 | 144 | 3 |
| 0 | decode | 0.217 | 288 | 4 |
| 0 | encode | 0.192 | 184 | 3 |
| 128 | clone | 38.218 | 103,392 | 265 |
| 128 | decode | 199.607 | 232,114 | 2,202 |
| 128 | encode | 145.911 | 138,784 | 519 |
| 4096 | clone | 2,126.884 | 3,320,595 | 8,229 |
| 4096 | decode | 6,988.951 | 7,606,081 | 69,702 |
| 4096 | encode | 5,070.352 | 4,358,306 | 16,391 |

The 8,192-intent backlog is a synthetic scaling probe, not evidence of an observed
production traffic distribution. Decode alone allocates about 7.6 MB per invocation
there. It is a more promising CPU/allocation target than the tiny admission guard,
but its isolated cost is not comparative proof for a particular optimization.

## Validation, evidence and next action

Both Railway jobs completed successfully: seven main stages and four paired
stages. Current run-through/atomic-file suites pass normally and under race.
The instrumented current suites also pass normally and under race, including
retained-state reopen assertions. [Main checks](2026-10-18-outbox-write-phases/raw/main/checks.json),
[overlay checks](2026-10-18-outbox-write-phases/raw/main/post-checks.json) and [paired checks](2026-10-18-outbox-write-phases/raw/paired/checks.json)
retain exact commands. [Source verification](2026-10-18-outbox-write-phases/raw/main/product-source-verification.json)
checks all 695 Go/module files against the deployed input manifest; collection
also checks these hashes against the current worktree. The product Go files remain
byte-identical to the already conformance-qualified correctness commit; no product
change is claimed here, so four conformance profiles were not rerun for this
evidence-only investigation.

All formatting, compilation, code tests and timing run on Railway. Binary hashes,
executed overlay source copies, harnesses, environment limits, exports and local
analysis scripts are retained. Benchmark binaries are deleted after recording
hashes. The report is checked with the site build/tests and documentation link
checker on Railway before committing. The runner is stopped after verified exports;
the target service remains removed and unchanged. The final artifact manifest
includes those receipts.

Next, compare a minimal reduction in repeated native-journal decoding using the
existing byte-identity cache contract. A candidate must preserve canonicalization,
validation of peer replacements, stale-claim fencing, error rollback and failed-sync
recovery. Measure whole-cycle time and allocations on ext4 at small and backlog
depths before adoption. This report does not authorize bypassing those invariants
or skipping durability fences. No filesystem indexing, incremental checkpointing
or new storage abstraction is justified by these measurements.
