# Reject native outbox state reuse after writes — 2026-10-18

## Decision

**Reject the write-cache optimization. Production source is restored byte-for-byte
to baseline `feb0f27`. No runtime speedup, new cache or format/API change ships.**
The accepted change adds 17 canonicalization/fencing contracts and one resource
regression, plus the complete experimental evidence.

The candidate reduced normal backlog allocations by about 73% and allocated bytes
by about 46%. Its largest fixture improved 12–26% in the first timing cohort and
about 19% in final-source checks. Those gains are real measurements of a rejected
candidate. A final ownership probe found a much larger cost: 64 small entries kept
**67.7 MB** of additional live heap versus **82.6 KB** in
the baseline, even though both native journal frames were **5,246 bytes**.
Value equivalence after protobuf serialization did not imply bounded ownership.

`origin/main` was fetched and remains `219d1dcfff9807b04660a40313b32127e5be2933`.
[Exact refs](2026-10-18-outbox-write-cache/raw/baseline.json) and the
[preceding phase investigation](2026-10-18-outbox-write-phases.md) record the baseline.
Existing storage, S3, durability, migration, ownership and error contracts remain
unchanged. The active quality objective remains open.

## What failed

The existing native reload creates state from the stored bytes. Besides validation
and canonicalization, this releases references to caller string backing buffers.
A logical key can be a short view into a much larger string. Retaining the original
in-memory state after writing preserves that larger allocation indefinitely even
when every key, timestamp, token and other value matches a fresh decode.

The attempted optimization reused the already-produced native journal buffer and
checked serialized entries with the existing decoder/full value comparison. It
retained fresh full-file reads, byte comparisons, file/directory syncs and error
rollback. It passed canonicalization contracts, full normal/race tests and all four
conformance profiles. None of those value checks established string ownership.
The production implementation was not committed or deployed to the target service.

## Resource reproduction and regression

On the same Railway ext4 runner, admit 64 ordinary entries. Each 12-byte key is a
view into a distinct 1 MiB string created in a helper, which returns after Enqueue.
Other fields use normal UTC timestamps and small valid values. Read provenance to
exercise a post-write reload, run GC and keep the outbox alive while collecting
`runtime.MemStats`. No caller retains the large source strings. No upstream writes
occur; these are temporary journals.

| Implementation | Entries | Source bytes per entry | Journal frame B | Live heap growth B |
| --- | ---: | ---: | ---: | ---: |
| Baseline | 64 | 1,048,576 | 5,246 | 82,648 |
| Rejected candidate | 64 | 1,048,576 | 5,246 | 67,706,904 |

[Commands, exact samples and exits](2026-10-18-outbox-write-cache/raw/candidate-final/retention-receipt.json),
[baseline output](2026-10-18-outbox-write-cache/raw/candidate-final/retention-before.log),
[candidate output](2026-10-18-outbox-write-cache/raw/candidate-final/retention-candidate.log) and
[executed source](2026-10-18-outbox-write-cache/raw/candidate-final/source-retention_probe_test.go.txt) are retained.
These are live Go heap differences for this fixture, not RSS bounds or a service SLA.

The regression adds a generous 16 MiB live-growth ceiling for these 64 small
persisted entries after the read/GC boundary. It fails twice against the candidate
and passes twice against the baseline before restoration. The large separation
makes the failure deterministic in these recorded runs. The final normal/race
suites repeat it on the exact accepted source.
[Red/green commands and expected exits](2026-10-18-outbox-write-cache/raw/candidate-final/retention-regression-receipt.json)
and [candidate failures](2026-10-18-outbox-write-cache/raw/candidate-final/retention-regression-candidate-red.log)
remain separate from the 400 timing observations.

The guard is a test fixture, not a new general memory limit. It generates 64 MiB of
source backing strings, with additional transient concatenation allocations, and
performs explicit GC. That adds test CPU/IO and temporary allocations; it adds no
production allocation or public API.

## Other correctness contracts

Before experimenting, 17 cases pass the original implementation twice normally
and twice under race detection. Blindly retaining written bytes breaks 13 cases
twice: text/time canonicalization, historical-attempt reconciliation, token-counter
clamping and provenance normalization. Four controls continue to pass. Expected
exit 1 in that probe means successful reproduction, not a passing regression suite.
[Exact cases/counts](2026-10-18-outbox-write-cache/raw/proof/contract-receipt.json) and
[proof steps](2026-10-18-outbox-write-cache/raw/proof/checks.json) retain the rejected blind prototype.

The guarded candidate passes those 17 contracts, including same-handle recovery
after a real directory-sync permission fault, changed/missing peer bytes and
comparison with a fresh handle. All tests run as UID 10001, and the fault case is
not skipped. The accepted final source adds the separate resource guard: **18
cases, each twice normally and twice under race, 36 passes per focused mode**.
The original baseline had no demonstrated correctness defect in these contracts;
the tests prevent attempted optimizations from violating existing behavior.

## Candidate timings: retained, not shipped

Every table below concerns the rejected candidate. Both favorable and adverse
results remain. They do not describe a committed runtime improvement. Retained
frame length/live-heap observations from ordinary small-string fixtures did not
cover the caller-buffer ownership case above.

## Measurement method

All formatting, compilation, tests and measurements run on the authorized Railway
scratch deployment `8fb6e6de-de0b-4adf-9041-d29c24308347`, service
`83497842-c51d-420e-8ba1-181589730ad4`, benchmark environment
`3cd200cb-c94f-4e4d-9335-0f6c1b7e9411`. Go 1.25.6, Node 25.9.0, 4 CPU/4 GiB
cgroup, validation UID 10001. GOMAXPROCS is 1 or 4. TMPDIR is explicitly under
ext4 `/results`, verified before timing. Local actions are editing, Git, packaging,
hashing and exported-data analysis. No local benchmark or Go test runs.

Both binaries use the same benchmark/contracts and seeded fixtures, changing only
the two production files through overlays. Queue depths are **per queue**: 128
means 256 retained intents, 4,096 means 8,192. Seeding, warm snapshots, GC setup and
retained-field/reopen verification are outside timing. The fixtures are synthetic
backlogs, not an asserted production traffic distribution or S3 latency SLA.

Enqueue and prepare measure admission plus completion/discard: **two API calls and
two durable replacements per operation**. Provenance alternates a changed value;
renew updates a held claim: **one API call and one replacement**. Times across
these methods are not interchangeable request latencies. The fallback puts an
unusual non-hour timestamp offset in a retained record; full decode and eligibility
work remain necessary. All retained fields and the final provenance/lease are
checked after timing. Allocation bytes/counts are cumulative per operation, not
live heap or RSS.

The first ABBA BAAB sequence runs eight processes, 400-ms minimum per case,
GOMAXPROCS 1/4, four observations per variant/cell: **224 observations**. Longer
targeted before/after and identical-current-binary cohorts each add 64 observations,
with 1-second minima; an eight-process four-CPU fallback cohort adds 16. **368
observations before the comment-only quality repair** remain in separate cohorts. Rebuilt binary hashes must match
the first cohort before confirmation. A/A labels execute the same guarded binary.
No tolerance/baseline is raised, and adverse observations are not dropped.

## First cohort: all operations

Times are per-cell medians. CPU denotes GOMAXPROCS. The final column is cumulative
allocation count per measured operation.

| Depth per queue | Operation | CPU | Before → after ms | Time change | Allocs before → after | Allocated-byte change |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 0 | enqueue | 1 | 6.879 → 7.168 | +4.20% | 85.0 → 67.0 | -16.62% |
| 0 | enqueue | 4 | 7.299 → 7.076 | -3.06% | 85.0 → 67.0 | -16.62% |
| 0 | prepare | 1 | 6.641 → 6.879 | +3.59% | 85.0 → 67.0 | -16.62% |
| 0 | prepare | 4 | 6.407 → 6.906 | +7.78% | 85.0 → 67.0 | -16.62% |
| 0 | provenance | 1 | 3.270 → 3.304 | +1.03% | 41.0 → 34.0 | -16.54% |
| 0 | provenance | 4 | 3.914 → 3.933 | +0.48% | 41.0 → 34.0 | -16.55% |
| 0 | renew | 1 | 3.236 → 3.809 | +17.73% | 52.0 → 36.0 | -23.38% |
| 0 | renew | 4 | 3.461 → 4.025 | +16.31% | 52.0 → 36.0 | -23.50% |
| 128 | enqueue | 1 | 7.882 → 9.631 | +22.18% | 5,991.5 → 1,620.0 | -45.82% |
| 128 | enqueue | 4 | 8.038 → 8.312 | +3.41% | 5,998.5 → 1,620.0 | -45.86% |
| 128 | prepare | 1 | 7.777 → 8.651 | +11.23% | 5,997.0 → 1,620.0 | -45.85% |
| 128 | prepare | 4 | 7.368 → 8.227 | +11.65% | 6,000.5 → 1,620.0 | -45.87% |
| 128 | provenance | 1 | 4.318 → 3.779 | -12.48% | 2,998.0 → 812.0 | -45.84% |
| 128 | provenance | 4 | 4.759 → 4.845 | +1.82% | 2,999.0 → 812.0 | -45.85% |
| 128 | renew | 1 | 3.990 → 3.844 | -3.67% | 3,006.5 → 812.0 | -45.87% |
| 128 | renew | 4 | 4.488 → 3.870 | -13.76% | 3,004.0 → 812.0 | -45.85% |
| 4096 | enqueue | 1 | 47.687 → 36.637 | -23.17% | 183,343.0 → 49,292.0 | -46.22% |
| 4096 | enqueue | 4 | 44.620 → 33.512 | -24.90% | 183,119.5 → 49,292.0 | -46.18% |
| 4096 | prepare | 1 | 47.159 → 37.436 | -20.62% | 181,734.0 → 49,292.0 | -45.92% |
| 4096 | prepare | 4 | 44.719 → 33.853 | -24.30% | 183,119.5 → 49,292.0 | -46.18% |
| 4096 | provenance | 1 | 22.596 → 19.828 | -12.25% | 90,870.5 → 24,649.0 | -45.95% |
| 4096 | provenance | 4 | 22.646 → 16.670 | -26.39% | 91,942.5 → 24,650.0 | -46.34% |
| 4096 | renew | 1 | 24.617 → 18.685 | -24.10% | 91,721.5 → 24,648.0 | -46.22% |
| 4096 | renew | 4 | 22.075 → 18.103 | -17.99% | 91,842.0 → 24,648.0 | -46.26% |
| 128 | fallback prepare/discard | 1 | 7.794 → 8.499 | +9.04% | 6,004.0 → 6,009.0 | +0.02% |
| 128 | fallback prepare/discard | 4 | 7.929 → 8.068 | +1.76% | 6,003.0 → 6,010.0 | +0.04% |
| 4096 | fallback prepare/discard | 1 | 47.276 → 48.383 | +2.34% | 182,321.0 → 181,746.0 | -0.20% |
| 4096 | fallback prepare/discard | 4 | 43.691 → 48.921 | +11.97% | 182,321.0 → 181,359.0 | -0.33% |

The initial adverse +22.18% medium enqueue, +17.73% empty renew and +11.65% medium
prepare cells remain visible. Allocation improvements do not erase those timings.
[All initial observations, ranges and memory metrics](2026-10-18-outbox-write-cache/raw/proof/comparison-analysis.json)
and [commands/order/binary hashes](2026-10-18-outbox-write-cache/raw/proof/comparison-runs.json) are retained.

## Longer confirmation and same-binary controls

| Cohort | Depth per queue | Operation | CPU | Before/label A → after/label B ms | Time change |
| --- | ---: | --- | ---: | ---: | ---: |
| Same binary | 0 | enqueue | 1 | 7.276 → 7.038 | -3.27% |
| Same binary | 0 | enqueue | 4 | 7.333 → 8.044 | +9.70% |
| Same binary | 0 | renew | 1 | 3.317 → 3.149 | -5.06% |
| Same binary | 0 | renew | 4 | 3.447 → 3.455 | +0.22% |
| Same binary | 128 | enqueue | 1 | 8.928 → 9.336 | +4.58% |
| Same binary | 128 | enqueue | 4 | 8.189 → 7.865 | -3.95% |
| Same binary | 128 | renew | 1 | 4.391 → 4.244 | -3.35% |
| Same binary | 128 | renew | 4 | 4.238 → 4.044 | -4.58% |
| Before/after | 0 | enqueue | 1 | 6.406 → 7.244 | +13.07% |
| Before/after | 0 | enqueue | 4 | 7.319 → 6.257 | -14.50% |
| Before/after | 0 | renew | 1 | 3.382 → 3.452 | +2.06% |
| Before/after | 0 | renew | 4 | 3.189 → 3.211 | +0.70% |
| Before/after | 128 | enqueue | 1 | 9.084 → 7.397 | -18.56% |
| Before/after | 128 | enqueue | 4 | 8.339 → 7.748 | -7.09% |
| Before/after | 128 | renew | 1 | 4.431 → 3.944 | -10.98% |
| Before/after | 128 | renew | 4 | 4.926 → 4.395 | -10.79% |
| Fallback before/after | 128 | fallback prepare/discard | 4 | 8.654 → 8.528 | -1.46% |
| Fallback before/after | 4096 | fallback prepare/discard | 4 | 45.617 → 50.693 | +11.13% |

The targeted medium cases improve about 7–19% in confirmation and repeat the
allocation reduction. This does not remeasure every initial cell: the initial
medium prepare slowdown remains unresolved. Empty enqueue changes sign across
CPU settings (+13.07% / -14.50%); empty renew is +2.06% / +0.70%. Same-binary labels
range from -5.06% to +9.70%. These limited samples show variability but do not prove
zero implementation effect or statistical significance. No universal latency
improvement is claimed. The large fallback's +11.97% initial / +11.13% confirmation
is a reproducible cost of eligibility work when the state cannot be reused.
[Separate confirmation/control samples and ranges](2026-10-18-outbox-write-cache/raw/confirmation/confirmation-analysis.json)
and [exact commands and matching binary hashes](2026-10-18-outbox-write-cache/raw/confirmation/comparison-runs.json)
are retained rather than pooled with the initial cohort.

## Rejected candidate source confirmation

The first final validation passed all functional/conformance stages but failed the
comment-line ratchet (4080 versus 4077). The only source repair removes the three
new explanatory comment lines; the explanation remains in this report. No baseline
is raised. [The failed quality gate](2026-10-18-outbox-write-cache/raw/initial-final/quality.log) and
[the exact comment-only diff receipt](2026-10-18-outbox-write-cache/raw/comment-only-repair.json) are retained.

The repaired source reruns the complete final validation and adds 32 measurements
at GOMAXPROCS 4, ABBA BAAB, 400-ms minimum. The benchmark source and original native
baseline are unchanged. This separate cohort qualifies the exact rejected-candidate Go bytes;
it is not substituted for either earlier cohort. **400 observations total** are
retained across the three before/after/control cohorts.

| Depth per queue | Operation | Before → after ms | Time change | Allocation change | Allocated-byte change |
| ---: | --- | ---: | ---: | ---: | ---: |
| 128 | enqueue | 8.327 → 7.606 | -8.66% | -72.98% | -45.83% |
| 128 | renew | 4.823 → 4.592 | -4.80% | -72.95% | -45.83% |
| 4096 | enqueue | 45.908 → 36.993 | -19.42% | -72.96% | -46.03% |
| 4096 | renew | 21.650 → 17.611 | -18.66% | -73.07% | -46.14% |

[Exact final observations and source hashes](2026-10-18-outbox-write-cache/raw/candidate-final/final-analysis.json) and
[final commands and binary hashes](2026-10-18-outbox-write-cache/raw/candidate-final/comparison-runs.json) are retained.


## Candidate buffer cost and qualification limits

The candidate retained one native frame in the existing byte cache after a
successful reusable write. An untimed Railway probe measured length/capacity:
14/24 bytes when empty, 25,486/25,486 at depth 128 per queue, and
815,118/815,118 at depth 4,096 per queue.
[Exact capacity results](2026-10-18-outbox-write-cache/raw/candidate-final/capacity-receipt.json) are retained.
The ordinary large enqueue benchmark's process live heap was about 7.59 → 7.47 MB,
and allocated bytes were 31.62 → 17.02 MB per two-call cycle. These process values
include fixtures and preceding subcases; they are not isolated per-handle peaks.
The retention probe demonstrates why those ordinary-input metrics were insufficient.

Fallback writes also showed a repeated roughly 11% slowdown for a large journal
with an unusual timestamp. Small/mid timings varied, including initial +22.18% and
+17.73% cells. A/A variation did not prove zero implementation effect. Those costs
are preserved in the tables, but neither that code nor its performance tradeoffs
ship. No workload threshold, new copy machinery, weaker fence or storage abstraction
is introduced to rescue the rejected design.

The candidate's first final validation passed all functional/conformance stages but
failed the comment-line ratchet (4080 > 4077). Removing only the three new explanatory
comment lines restored that gate; all 18 subsequent candidate checks passed before
the resource failure was found. [The quality failure](2026-10-18-outbox-write-cache/raw/initial-final/quality.log),
[comment-only change](2026-10-18-outbox-write-cache/raw/comment-only-repair.json) and
[candidate qualification](2026-10-18-outbox-write-cache/raw/candidate-final/checks.json) are retained. No baseline
was raised. Green functional gates were evidence, not proof of every resource property.

## Accepted result and validation

The two production files are restored to their recorded baseline hashes. The
existing read cache, authoritative reads, native/legacy decoders, full-state writes
and durability fences remain unchanged. Only regression tests and evidence are
committed. [Accepted source verification](2026-10-18-outbox-write-cache/raw/final/accepted-source-verification.json)
and [regression receipt](2026-10-18-outbox-write-cache/raw/final/accepted-receipt.json) check exact bytes and cases.

All 15 accepted Railway stages pass: formatting and source verification; focused
normal/race tests twice; full Go suite; broad storage/runtime/embedded/run-through/
S3/public-package race tests; four conformance profiles (memory, filesystem,
runtime-memory, runtime-filesystem); vet; site build/tests; documentation links; and
quality/file-size/format gates. [Accepted commands/results](2026-10-18-outbox-write-cache/raw/final/checks.json)
are separate from the rejected candidate's checks.

The final report is checked on the same Railway source. Every timing observation,
failed prototype, resource sample, executed overlay, source/transfer hash and teardown
receipt is retained. Benchmark binaries are removed after hashes are recorded. The
scratch runner is stopped after verified exports; the separate target service
remains removed and unchanged. All Go, formatting, code checks, tests, race,
conformance, documentation checks and measurements run on Railway. Local work is
reads, edits, packaging, Git, hashing and exported-data analysis.

Next correctness investigation: reproduce fencing-token exhaustion. The existing
allocator still increments uint64 without the ID allocator's exhaustion guard;
the serializer permits the maximum value. This is a code finding awaiting an
operational reproduction, not a claim of tested corruption. No next fix is included.

The write-performance problem remains open. Any future reuse candidate must bound
ownership of keys, entry text, IDs and provenance, pass the new retention guard,
and demonstrate net value after copying costs. Existing full-journal clone/encode
and durable sync costs remain. No indexing, incremental checkpoint, new storage
abstraction, dependency, format or public API is added.
