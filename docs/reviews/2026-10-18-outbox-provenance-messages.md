# Allocate temporary protobuf provenance messages together — 2026-10-18

## Result and scope

The encoder now allocates one fresh slice of provenance message values and points
its existing repeated-message list at those values. It previously allocated each
provenance message separately. Empty collections skip the new slice. The values
remain local to encoding; no pool, cache, persisted representation, dependency or
public API is added. Entry construction and the decoder are byte-identical to the
baseline. Sorting, canonicalization, validation, framing, locks and durability stay
on their existing paths.

At 4,096 provenance records, encoding drops from **4,101 allocations to 6** with
unchanged allocated bytes in this fixture. In longer confirmation, encode medians
improve **4.73% at one CPU and 15.14% at four CPUs**. A changed durable provenance
save with 16,384 retained records plus its target drops from **65,727 allocations
to 49,343** (24.93%). This is an allocation-count improvement; it is not a claim
that every request gets faster or allocates fewer bytes.

Baseline is `5d28d18`. `origin/main` was fetched and remains
`219d1dcfff9807b04660a40313b32127e5be2933`.
[Exact refs](2026-10-18-outbox-provenance-messages/raw/baseline.json) and the earlier
[phase profile](2026-10-18-outbox-write-phases.md),
[rejected write cache](2026-10-18-outbox-write-cache.md) and
[rejected map clone](2026-10-18-outbox-clone.md) establish completed work.
This change does not repeat those rejected implementations.

## Why only provenance

The first experiment grouped both entry and provenance messages. A full-slice entry
candidate reserves message storage before validating the first timestamp: the
4,096-entry early-error fixture allocates about **1.57 MB instead of 98.5 KB**.
It fails the new 512 KiB fixture budget twice normally and twice with race
detection. An entry-block candidate limits that case to about 147.7 KB, but its
large enqueue medians are 9.31–12.17% slower despite about 9% fewer whole-cycle
allocations. Neither entry implementation is adopted.

Provenance has a different error boundary: the existing encoder builds its entire
provenance list before entering timestamp-bearing records or calling Marshal.
Its loop has no error return. Grouping those already-required values avoids
per-record heap objects without introducing entry reservation on early timestamp
failure. The final early-error allocation remains about **98.5 KB**, matching the
baseline. All framing and error returns use the same implementation.

The initial encoder round-trip/error/resource cases pass the original code before
experimentation. Only the deliberately rejected full-entry slice fails the early
error budget; this is not evidence of a pre-existing production correctness bug.
[Exact case counts, original frame hashes and error allocations](2026-10-18-outbox-provenance-messages/raw/measure/contract-receipt.json)
retain the original, full-slice and bounded-entry results. Expected test exit 1
means a reproduced candidate regression, not a passing suite.

## Method and ownership guarantees

All formatting, builds, tests and measurements run on the existing authorized
Railway scratch runner, Go 1.25.6 / protobuf v1.36.12 / Node 25.9.0, 4 CPU / 4 GiB,
as UID 10001. Benchmark fixtures use TMPDIR on the results volume, verified ext4
(type 61267). Local work is editing, Git, packaging, hashing and exported-data
analysis. No local Go tool, test, benchmark or commit hook runs.

The matrices use separate original/candidate binaries from identical fixtures and
Go overlays. ABBA BAAB ordering gives four observations per variant/cell with
GOMAXPROCS 1 and 4 and a 400-ms minimum. The first cohort has **384 observations**;
the provenance-only cohort has **352**. Longer final-source confirmation adds
**80 paired and 48 identical-binary control observations**, at one-second minima.
All **864 observations** are retained separately; discarded candidates are not
pooled into accepted-source results. Medians/ranges do not establish statistical
significance or a service latency SLA.

Component fixtures contain either N provenance records or N records in each of
provenance, active and prepared collections. Mixed entries exercise all serialized
fields and four timestamp strings. Encode and unchanged-decode controls are
measured separately. Frame length, counts and decoding are checked outside timing.
Initial durable cycles retain N intents per active/prepared queue; enqueue and
prepare include admission plus completion/discard, two calls/replacements per
cycle. Provenance and renewal each make one call/replacement.

The provenance-only durable cohort uses the existing native benchmark. Its
`provenance=N` fixture has N retained provenance records plus a target;
`mixed=1024` has 1,024 active intents and 1,024 provenance records plus its target.
Changed saves alternate target ETags and replace the journal. Unchanged saves use
the existing validated-byte read and durability-fence path. Written-byte counters,
frame lengths and final target values are checked outside timing. These synthetic
backlogs exercise supported paths; they are not an asserted traffic distribution.

Fourteen deterministic frame vectors cover empty, singleton, multiple-record and
127/128/129/257-record fixtures with all fields. Their full framed SHA-256 values
match the original across every candidate and final tests. They also verify
round-trip values, nonmutation of source state and deterministic ordering. Eight
concurrent encodes of a shared read-only state must produce independent output
buffers; race detection repeats the case.

The input-retention test keeps only encoded bytes after a helper constructs 64
small entry keys and provenance ETags as views into distinct 1 MiB strings.
GC/live-heap sampling must stay below a generous 16 MiB positive-growth ceiling,
and the frame must decode to all 64 entries and provenance records. The stronger
provenance-field version is run against both the original and final candidate
before adoption. It passes normally and with race detection. The guard excludes
the observed 64 MiB caller-buffer retention pattern; it is not an RSS bound or a
new global quota. Setup and forced GC add bounded test CPU/memory work, not runtime
storage allocations. [Ownership proof](2026-10-18-outbox-provenance-messages/raw/ownership/plan.json) and
[final normal/race results](2026-10-18-outbox-provenance-messages/raw/final/green-receipt.json) retain the exact sources
and observations.

## Initial entry/provenance grouping: not shipped

This table includes unchanged decoder controls and every durable-cycle cell.
Depth in the durable cycle is per intent queue, so 4,096 means 8,192 retained
intents. The candidate is the rejected bounded-entry version, not final production.

| Benchmark | Before → candidate ms | Time delta | Allocation-count delta | Allocated-byte delta |
| --- | ---: | ---: | ---: | ---: |
| BenchmarkFileOutboxProtoBlocksCycle/depth=0/enqueue | 7.973458 → 7.456457 | -6.48% | +0.00% | +0.50% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=0/enqueue-4 | 8.743694 → 7.436875 | -14.95% | +0.00% | -0.01% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=0/prepare | 7.103455 → 6.781130 | -4.54% | +0.00% | +0.00% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=0/prepare-4 | 7.302520 → 7.083799 | -3.00% | +0.00% | +0.02% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=0/provenance | 3.520892 → 3.439442 | -2.31% | +0.00% | +0.00% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=0/provenance-4 | 4.183010 → 4.129311 | -1.28% | +1.22% | +0.04% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=0/renew | 3.544778 → 3.707691 | +4.60% | +0.00% | +0.01% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=0/renew-4 | 3.908216 → 3.687753 | -5.64% | +0.00% | +0.03% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=128/enqueue | 9.150531 → 8.549033 | -6.57% | -8.49% | -0.01% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=128/enqueue-4 | 8.738691 → 9.075036 | +3.85% | -8.49% | -0.01% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=128/prepare | 7.987750 → 8.835981 | +10.62% | -8.42% | +0.04% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=128/prepare-4 | 8.695804 → 8.839862 | +1.66% | -8.53% | -0.04% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=128/provenance | 4.068879 → 4.545482 | +11.71% | -8.44% | +0.02% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=128/provenance-4 | 4.382305 → 4.208609 | -3.96% | -8.54% | -0.04% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=128/renew | 4.643616 → 4.115550 | -11.37% | -8.28% | +0.10% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=128/renew-4 | 4.139282 → 4.224145 | +2.05% | -8.49% | -0.01% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=4096/enqueue | 44.023612 → 48.122645 | +9.31% | -8.92% | -0.00% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=4096/enqueue-4 | 43.036114 → 48.274563 | +12.17% | -8.89% | -0.00% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=4096/prepare | 48.590550 → 46.070006 | -5.19% | -9.24% | -0.20% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=4096/prepare-4 | 44.591906 → 47.233745 | +5.92% | -9.21% | -0.20% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=4096/provenance | 22.932136 → 25.459729 | +11.02% | -8.67% | +0.13% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=4096/provenance-4 | 21.662157 → 22.866358 | +5.56% | -9.24% | -0.26% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=4096/renew | 23.648736 → 23.967547 | +1.35% | -8.93% | -0.05% |
| BenchmarkFileOutboxProtoBlocksCycle/depth=4096/renew-4 | 22.022162 → 22.812919 | +3.59% | -9.24% | -0.25% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=0/decode | 0.000233 → 0.000243 | +4.21% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=0/decode-4 | 0.000255 → 0.000253 | -0.72% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=0/encode | 0.000172 → 0.000202 | +16.93% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=0/encode-4 | 0.000195 → 0.000189 | -3.12% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=128/decode | 0.019815 → 0.019859 | +0.22% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=128/decode-4 | 0.023432 → 0.022569 | -3.68% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=128/encode | 0.023627 → 0.022777 | -3.60% | -95.49% | +3.42% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=128/encode-4 | 0.022943 → 0.021594 | -5.88% | -95.49% | +3.42% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=4096/decode | 0.749618 → 0.679227 | -9.39% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=4096/decode-4 | 1.163696 → 1.006608 | -13.50% | +0.00% | -0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=4096/encode | 1.080691 → 1.021955 | -5.44% | -99.10% | +3.57% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=4096/encode-4 | 1.170551 → 0.929860 | -20.56% | -99.10% | +3.57% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=0/decode | 0.000234 → 0.000224 | -4.34% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=0/decode-4 | 0.000248 → 0.000242 | -2.70% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=0/encode | 0.000180 → 0.000193 | +7.26% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=0/encode-4 | 0.000192 → 0.000186 | -3.10% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=128/decode | 0.265483 → 0.277981 | +4.71% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=128/decode-4 | 0.349712 → 0.327095 | -6.47% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=128/encode | 0.249949 → 0.241067 | -3.55% | -26.89% | +0.26% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=128/encode-4 | 0.295971 → 0.255958 | -13.52% | -26.89% | +0.26% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=4096/decode | 11.598484 → 12.387943 | +6.81% | +0.00% | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=4096/decode-4 | 11.663970 → 11.130980 | -4.57% | +0.00% | -0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=4096/encode | 9.397782 → 10.592740 | +12.72% | -27.05% | +0.27% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=4096/encode-4 | 9.640275 → 9.741848 | +1.05% | -27.05% | +0.27% |

[All initial samples, ranges, bytes/counts, process heap/HWM and frame metrics](2026-10-18-outbox-provenance-messages/raw/measure/comparison-analysis.json)
and [commands/order/binary hashes](2026-10-18-outbox-provenance-messages/raw/measure/comparison-runs.json) are retained.
The allocation reduction does not erase the adverse latency cells. This candidate
is not used in the product.

## Provenance-only comparison before the empty-input guard

The entry path is restored to the original implementation. This cohort measures
one temporary provenance-value slice. The final empty-input guard is added only
after these observations; its exact-source confirmation follows below.

| Benchmark | Before → candidate ms | Time delta | Allocations before → candidate | Allocated-byte delta |
| --- | ---: | ---: | ---: | ---: |
| BenchmarkFileOutboxProvenance/mixed=1024/changed | 9.803854 → 7.265516 | -25.89% | 16,471.0 → 15,447.0 | +0.31% |
| BenchmarkFileOutboxProvenance/mixed=1024/changed-4 | 8.853485 → 7.956179 | -10.14% | 16,471.0 → 15,447.0 | +0.31% |
| BenchmarkFileOutboxProvenance/mixed=1024/unchanged | 0.299196 → 0.304124 | +1.65% | 21.0 → 21.0 | +0.01% |
| BenchmarkFileOutboxProvenance/mixed=1024/unchanged-4 | 0.327816 → 0.326172 | -0.50% | 22.0 → 22.0 | -0.02% |
| BenchmarkFileOutboxProvenance/provenance=0/changed | 3.269271 → 3.304316 | +1.07% | 43.0 → 43.0 | -0.01% |
| BenchmarkFileOutboxProvenance/provenance=0/changed-4 | 3.224016 → 3.689589 | +14.44% | 43.0 → 43.0 | -0.01% |
| BenchmarkFileOutboxProvenance/provenance=0/unchanged | 0.327861 → 0.327506 | -0.11% | 19.0 → 19.0 | +0.00% |
| BenchmarkFileOutboxProvenance/provenance=0/unchanged-4 | 0.326387 → 0.329953 | +1.09% | 19.0 → 19.0 | +0.00% |
| BenchmarkFileOutboxProvenance/provenance=128/changed | 3.935856 → 3.817408 | -3.01% | 567.0 → 439.0 | +0.75% |
| BenchmarkFileOutboxProvenance/provenance=128/changed-4 | 3.570924 → 3.891803 | +8.99% | 567.0 → 439.0 | +0.75% |
| BenchmarkFileOutboxProvenance/provenance=128/unchanged | 0.318042 → 0.313988 | -1.27% | 19.0 → 19.0 | +0.00% |
| BenchmarkFileOutboxProvenance/provenance=128/unchanged-4 | 0.334333 → 0.333095 | -0.37% | 19.0 → 19.0 | -0.01% |
| BenchmarkFileOutboxProvenance/provenance=16384/changed | 18.759989 → 18.531322 | -1.22% | 65,727.0 → 49,343.0 | +0.09% |
| BenchmarkFileOutboxProvenance/provenance=16384/changed-4 | 19.088417 → 18.026425 | -5.56% | 65,727.0 → 49,343.0 | +0.09% |
| BenchmarkFileOutboxProvenance/provenance=16384/unchanged | 0.277842 → 0.294390 | +5.96% | 46.5 → 45.0 | -0.01% |
| BenchmarkFileOutboxProvenance/provenance=16384/unchanged-4 | 0.320832 → 0.335680 | +4.63% | 51.0 → 52.0 | +0.01% |
| BenchmarkFileOutboxProvenance/provenance=2048/changed | 5.026485 → 5.389906 | +7.23% | 8,264.0 → 6,216.0 | +0.68% |
| BenchmarkFileOutboxProvenance/provenance=2048/changed-4 | 5.351591 → 5.230736 | -2.26% | 8,264.0 → 6,216.0 | +0.68% |
| BenchmarkFileOutboxProvenance/provenance=2048/unchanged | 0.306937 → 0.302885 | -1.32% | 20.0 → 20.0 | +0.01% |
| BenchmarkFileOutboxProvenance/provenance=2048/unchanged-4 | 0.330287 → 0.320915 | -2.84% | 20.0 → 22.0 | +0.17% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=0/decode | 0.000243 → 0.000225 | -7.51% | 4.0 → 4.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=0/decode-4 | 0.000239 → 0.000233 | -2.51% | 4.0 → 4.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=0/encode | 0.000172 → 0.000203 | +17.86% | 3.0 → 3.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=0/encode-4 | 0.000204 → 0.000194 | -5.07% | 3.0 → 3.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=128/decode | 0.021261 → 0.019294 | -9.25% | 399.0 → 399.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=128/decode-4 | 0.020621 → 0.022061 | +6.98% | 399.0 → 399.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=128/encode | 0.024099 → 0.020720 | -14.02% | 133.0 → 6.0 | +3.42% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=128/encode-4 | 0.023536 → 0.021071 | -10.47% | 133.0 → 6.0 | +3.42% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=4096/decode | 0.741784 → 0.683993 | -7.79% | 12,325.0 → 12,325.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=4096/decode-4 | 1.013834 → 1.072746 | +5.81% | 12,325.0 → 12,325.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=4096/encode | 0.954538 → 0.927776 | -2.80% | 4,101.0 → 6.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=false/depth=4096/encode-4 | 1.051224 → 0.866982 | -17.53% | 4,101.0 → 6.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=0/decode | 0.000235 → 0.000236 | +0.55% | 4.0 → 4.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=0/decode-4 | 0.000226 → 0.000243 | +7.65% | 4.0 → 4.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=0/encode | 0.000168 → 0.000175 | +3.65% | 3.0 → 3.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=0/encode-4 | 0.000202 → 0.000192 | -4.87% | 3.0 → 3.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=128/decode | 0.251366 → 0.255804 | +1.77% | 5,285.0 → 5,285.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=128/decode-4 | 0.328954 → 0.322703 | -1.90% | 5,285.0 → 5,285.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=128/encode | 0.228459 → 0.246528 | +7.91% | 1,417.0 → 1,290.0 | +0.26% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=128/encode-4 | 0.281963 → 0.262016 | -7.07% | 1,417.0 → 1,290.0 | +0.26% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=4096/decode | 12.210585 → 11.662553 | -4.49% | 168,039.0 → 168,039.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=4096/decode-4 | 10.979593 → 11.271731 | +2.66% | 168,039.0 → 168,039.0 | -0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=4096/encode | 10.128143 → 10.031878 | -0.95% | 45,065.0 → 40,970.0 | +0.00% |
| BenchmarkOutboxProtoBlocks/mixed=true/depth=4096/encode-4 | 9.923307 → 10.233216 | +3.12% | 45,065.0 → 40,970.0 | +0.00% |

The initial empty changed-save cell at four CPUs is +14.44%, medium changed save
is +8.99%, and the 2,048-record one-CPU changed save is +7.23%. These adverse cells
remain visible. Some unmodified decode/unchanged controls also move in time.
No universal latency benefit is inferred. [Every provenance-only observation](2026-10-18-outbox-provenance-messages/raw/provenance/comparison-analysis.json)
and [commands/source hashes](2026-10-18-outbox-provenance-messages/raw/provenance/comparison-runs.json) are retained.

## Exact final-source confirmation and same-binary controls

The final source skips the new allocation when there are no provenance records.
The selected file regexp `changed$` also matches `unchanged`; every actual control
row is included. Paired rows compare original/final binaries. A/A rows execute the
identical final binary under both labels, verified by binary SHA-256. CPU is four
for file cases; component names retain their Go CPU suffix (no suffix means one).

| Cohort | Benchmark | Before/A → final/B ms | Time delta | Allocation-count delta |
| --- | --- | ---: | ---: | ---: |
| aa | BenchmarkFileOutboxProvenance/provenance=0/changed-4 | 3.761721 → 3.394824 | -9.75% | +0.00% |
| aa | BenchmarkFileOutboxProvenance/provenance=0/unchanged-4 | 0.316698 → 0.318503 | +0.57% | +0.00% |
| aa | BenchmarkFileOutboxProvenance/provenance=128/changed-4 | 3.421829 → 3.600863 | +5.23% | +0.00% |
| aa | BenchmarkFileOutboxProvenance/provenance=128/unchanged-4 | 0.318860 → 0.316622 | -0.70% | +0.00% |
| aa | BenchmarkFileOutboxProvenance/provenance=16384/changed-4 | 18.508631 → 18.432875 | -0.41% | +0.00% |
| aa | BenchmarkFileOutboxProvenance/provenance=16384/unchanged-4 | 0.303643 → 0.331583 | +9.20% | -4.62% |
| paired | BenchmarkFileOutboxProvenance/provenance=0/changed-4 | 3.965865 → 3.411128 | -13.99% | +0.00% |
| paired | BenchmarkFileOutboxProvenance/provenance=0/unchanged-4 | 0.327574 → 0.327340 | -0.07% | +0.00% |
| paired | BenchmarkFileOutboxProvenance/provenance=128/changed-4 | 4.183862 → 3.631314 | -13.21% | -22.57% |
| paired | BenchmarkFileOutboxProvenance/provenance=128/unchanged-4 | 0.316977 → 0.316787 | -0.06% | +0.00% |
| paired | BenchmarkFileOutboxProvenance/provenance=16384/changed-4 | 19.110311 → 18.582005 | -2.76% | -24.93% |
| paired | BenchmarkFileOutboxProvenance/provenance=16384/unchanged-4 | 0.316961 → 0.303694 | -4.19% | -1.56% |
| paired | BenchmarkOutboxProtoBlocks/mixed=false/depth=0/encode | 0.000173 → 0.000170 | -1.76% | +0.00% |
| paired | BenchmarkOutboxProtoBlocks/mixed=false/depth=0/encode-4 | 0.000169 → 0.000174 | +3.04% | +0.00% |
| paired | BenchmarkOutboxProtoBlocks/mixed=false/depth=4096/encode | 0.994324 → 0.947330 | -4.73% | -99.85% |
| paired | BenchmarkOutboxProtoBlocks/mixed=false/depth=4096/encode-4 | 1.077014 → 0.913922 | -15.14% | -99.85% |

In longer paired confirmation, the initially adverse empty/medium four-CPU changed
cells instead improve 13.99%/13.21%; the large changed save improves 2.76%. The
same-binary file medians range from -9.75% to +9.20%. These observations demonstrate
variability; they do not prove zero implementation effect or turn all first-cohort
regressions into noise. The one-CPU 2,048-record +7.23% is not remeasured here.
The final-source encode allocation benefit repeats exactly, and the truly empty
codec is -1.76%/+3.04% at one/four CPUs. No fixed request-latency gain is claimed.
[All final paired/A/A samples and ranges](2026-10-18-outbox-provenance-messages/raw/confirmation/confirmation-analysis.json)
and [exact commands, order and binary identities](2026-10-18-outbox-provenance-messages/raw/confirmation/confirmation-runs.json)
are retained separately from earlier cohorts.

## Costs, contracts and remaining work

Successful encoding still retains all provenance messages until Marshal returns;
they now occupy one contiguous temporary allocation. Storage remains proportional
to the live collection size, and whole-journal read/encode memory remains O(N).
No stronger total-memory limit is introduced. Runtime allocation classes can add
padding: isolated 128-record provenance encoding allocates 3.42% more bytes;
sampled changed file saves add up to 0.75%. At the 4,096-record component, total
allocated bytes are unchanged. This is fewer heap objects, not a general reduction
in allocated-byte volume. No provenance message pointers escape the encoder.

The output remains framed protobuf schema 4, with identical field values and
ordering in the vectors. The generated schema, native/legacy readers, timestamp
conversion, ID/token fences, malformed-identity checks, locks, state rollback,
file/directory syncs and S3 behavior are unchanged. No dependency, public API,
format, migration, indexing or incremental-checkpointing change is needed.

All 14 final Railway stages pass: formatting; unchanged quality/file-size/format
gates; focused normal/race; full Go; broad race across storage/runtime/embedded/
run-through/S3/public packages; all four conformance profiles; vet; site build/tests;
and documentation links. The focused subset has **50 cases passing twice per mode:
100 normal and 100 race passes, with no failures or skips**. The tested production
hash is identical to the confirmed candidate. [Final checks](2026-10-18-outbox-provenance-messages/raw/final/checks.json),
[source/hash proof](2026-10-18-outbox-provenance-messages/raw/final/green-receipt.json) and [environment](2026-10-18-outbox-provenance-messages/raw/final/environment.json)
retain the evidence. All final Go bytes come from Railway formatting. Report/site/
link checks run again after this document and evidence are saved. The scratch
runner is then stopped; the separate target remains removed and unchanged.
The [artifact manifest](2026-10-18-outbox-provenance-messages/raw/artifact-manifest.json) includes final validation and
teardown receipts.

The next priority remains decode/entry-path CPU and allocation cost, which has the
larger measured share at backlog. Entry grouping stays rejected until a candidate
improves its measured tradeoff. No next optimization is automatically implemented
in this change.
