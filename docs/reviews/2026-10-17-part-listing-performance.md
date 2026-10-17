# Reduce multipart listing allocations without weakening recovery

Follow-up: [oversized cache admission](2026-10-17-refresh-eviction-performance.md) addresses the measured oversized-object fallback cost. The measurements below describe this earlier listing change.

Base `5f8f4844f5cfd987eb8e6d7cdb5a1d9af7f98bc2` contains the three preceding
correctness corrections. A fresh fetch still reports `origin/main`
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor.
[Baseline and historical variant identities](2026-10-17-part-listing-performance/raw/baseline.json).
This completes one independently reviewable performance change. Durable fallback
is measured and remains the next item; it is not modified here.

## Railway-only baseline

All builds, formatting, benchmarks, profiles, tests, race/conformance checks,
quality gates and document checks run on the existing Railway scratch runner.
Local work is source editing, packaging, orchestration, exported-data analysis and
Git. No local benchmark or validation runs. No dependency, format, public API,
index, checkpoint, storage abstraction or new cache is added.

Deployment `dc05f3be-9d15-4d23-bbf3-a1cdccdd7979` reached SUCCESS with Go 1.25.6,
linux/amd64, a 4-CPU/4-GiB limit and the persistent ext4 results volume.
Commands run as UID 10001 with the previously qualified loopback names and orphan
reaper. CPU labels mean GOMAXPROCS 1 and 4, not affinity or concurrent clients.
The old removed deployment could not be redeployed; a fresh sanitized tracked
snapshot was uploaded to the same service. The target service remains untouched.

The [first cohort](2026-10-17-part-listing-performance/raw/requalification/run.json)
retains **320 observations**, eight per workload/version/CPU, from two ABBA/BAAB
cycles with 200-ms minimum time and two repetitions per block. Historical source
overlays reproduce the old code while keeping the current tests and benchmark
fixtures identical. Those old implementations have known correctness defects;
they are timing references, not rollback candidates or equivalent-result claims.
[All baseline metrics/ranges/cycle changes](2026-10-17-part-listing-performance/raw/requalification/summary.json).

16-part listing reproduces 53.5/54.9 μs historically versus 87.3/94.3 μs currently
at CPU 1/4, with **115 → 235 allocations**. The earlier 234-allocation report
predates owner-identity validation. Small replacement retains its atomic-publication
benefit; large replacement has mixed timing. All six original workloads remain
in the raw results.

Changed file-outbox fallback reproduces **3.557 → 7.358 ms at CPU 1 (+106.8%)** and
**3.423 → 6.340 ms at CPU 4 (+85.2%)**. Provenance saves change **one → two**;
allocations change 109 → 164 and 110 → 165. Stable fallback still saves once,
with CPU-1 median +2.1% and CPU-4 -1.6%. Memory and stable controls are retained.
The older local near-2× finding is now independently reproduced on Railway.
The fresh observation remains durable and consistent; no save is removed here.

## Evidence before selecting the change

The [allocation profile](2026-10-17-part-listing-performance/raw/requalification/alloc-top.log)
and [escape analysis](2026-10-17-part-listing-performance/raw/requalification/escape.log)
identify repeated InternalPath/path joining, per-record header buffers and
reflective sorting. Profiles include fixture setup; they are diagnostic attribution,
not peak memory or benchmark allocation totals. Benchmark B/op and allocations/op
come from the timed Go benchmarks themselves.

The [64-observation incremental comparison](2026-10-17-part-listing-performance/raw/stepwise/summary.json)
measures current code, directory-path reuse, additional shared header scratch,
and additional generic sorting. It uses forward/reverse variant order and four
samples per workload/version/CPU. Directory reuse has the lowest 16-part listing
median at both CPUs and reduces 235 → 203 allocations. Shared scratch reaches
188, and sorting 185, but does not establish a further latency improvement.
Those variants add code/helper interfaces and are not retained.

A [128-observation confirmation](2026-10-17-part-listing-performance/raw/short-confirmation/comparison.md)
shows mixed latency: small listings are about 6% slower, CPU-1 large listing is
+1.8%, and CPU-4 small runtime replacement is +10.5%. These adverse measurements
are preserved, not dismissed. They trigger a second 128-observation confirmation
with a **one-second minimum**, identical workloads and ABBA/BAAB ordering.

## Final comparative results

| CPU | Workload | Time before → after | Change | B/op before → after | Allocations before → after |
|---|---|---:|---:|---:|---:|
| 1 | List 1 × 1 KiB | 16.227 → 15.560 μs | -4.11% | 3,976.0 → 3,752.0 | 45 → 43 |
| 1 | Runtime replace 1 × 1 KiB | 3.832 → 4.353 ms | +13.60% | 22,641.0 → 22,386.5 | 207 → 205 |
| 1 | List 16 × 1 MiB | 89.147 → 82.526 μs | -7.43% | 22,849.0 → 19,265.0 | 235 → 203 |
| 1 | Runtime replace 16 × 1 MiB | 15.338 → 15.458 ms | +0.78% | 6,344,456.0 → 6,341,027.5 | 432 → 400 |
| 4 | List 1 × 1 KiB | 16.515 → 16.225 μs | -1.75% | 3,981.0 → 3,756.0 | 45 → 43 |
| 4 | Runtime replace 1 × 1 KiB | 3.833 → 3.792 ms | -1.08% | 22,687.5 → 22,490.0 | 207 → 205 |
| 4 | List 16 × 1 MiB | 93.037 → 86.874 μs | -6.62% | 22,879.5 → 19,286.0 | 235 → 203 |
| 4 | Runtime replace 16 × 1 MiB | 15.370 → 15.358 ms | -0.08% | 6,346,293.5 → 6,342,795.0 | 432 → 400 |

Adverse longer-cohort medians: CPU 1 Runtime replace 1 × 1 KiB: +13.60%; CPU 1 Runtime replace 16 × 1 MiB: +0.78%.
[Every longer-cohort sample](2026-10-17-part-listing-performance/raw/long-confirmation/samples.json),
[all ranges and same-code cycle changes](2026-10-17-part-listing-performance/raw/long-confirmation/summary.json).
The [32-observation identical-binary control](2026-10-17-part-listing-performance/raw/identical-control/comparison.md)
measures the variable small runtime case with two labels using identical binary
hashes. It is context for storage variation, not grounds to discard a slower
comparison row. No observation or adverse row is removed. These are bounded sequential
measurements on shared storage, not an HTTP throughput or universal speedup claim.
The allocation reduction is consistent across all samples. The large listing
retains **203 allocations**, still above the historical 115: per-record open,
header read, physical stat and close checks remain. Allocation bytes are not peak
heap, RSS, an object memory bound or a durability qualification.

## Small production change and contract checks

Only [part_record.go](../../internal/storage/workspace/part_record.go) changes in
production: compute the upload-directory path once, then join each canonical part
filename to it. The existing `partPath` behavior is preserved. Descriptor/payload
atomicity, canonical filename parsing, same-file header/stat validation, ordering,
lock scope, reader cleanup and corruption refusal are unchanged. There is no
shared buffer, global retained state, additional I/O or new helper interface.
A single directory string lives for the call instead of repeatedly allocating it.
No latency parity is promised; measured timing increases are shown above.

The [new regression](../../internal/storage/workspace/listing_descriptor_test.go)
passes twice normally and under race on unchanged production code before the
optimization, then again afterward. This is a measured allocation regression,
not a newly failing functional contract. The test covers numeric part ordering,
independent returned metadata, replacement, pagination, reopen, a damaged final
header returning no partial result, and successful repair. Existing atomic part,
corruption, completion and owner/quota recovery regressions remain enabled.
[Focused counts](2026-10-17-part-listing-performance/raw/test-counts.json).

The [initial 14-check run](2026-10-17-part-listing-performance/raw/initial-validation/checks.json)
passes focused normal/race, broad storage/runtime/embedded/run-through/S3/public
API race, vet, quality/file-size/format, site build/tests and documentation links.
Full Go and four conformance profiles fail because the uploaded snapshot omitted
Git executable bits: provider-script tests cannot execute `live-provider.sh`.
This is a packaging error, not a product change. The
[mode-restoration receipt](2026-10-17-part-listing-performance/raw/mode-restoration-receipt.json)
verifies script hashes remain unchanged and restores the exact Git modes.
The [final Railway recheck](2026-10-17-part-listing-performance/raw/after/checks.json)
verifies those modes, reruns full Go and all four affected conformance profiles,
and passes. Successful race/quality checks are not needlessly repeated; production
and regression source hashes are identical between both validation phases.
The two existing mirror/live-upstream conformance skips remain; focused regressions
have no skips. [Final result](2026-10-17-part-listing-performance/raw/after/run.json).
The full-suite quality fixture uses the exact parent baseline JSON, not a copy
of repository history. Isolated prototype Go files are moved beneath a benchmark
underscore directory before full tests so they are not standalone product packages.

[Applied source receipts](2026-10-17-part-listing-performance/raw/after-sync-receipt.json)
and [actual tested source hashes](2026-10-17-part-listing-performance/raw/after/environment.json)
match the selected measured prototype byte for byte. All builds retain binary
hashes, compiler flags, source identities, plans, load/cgroup snapshots and mount
information. Export receipts verify file length and SHA-256 before runner shutdown.
An additional Railway document-link check covers this report and updated review
links; its log and source/report hash receipt are retained with the final evidence.

## Next highest-priority item

Changed-version durable fallback is now a confirmed Railway performance cost of
about **1.85–2.07×**, caused by two real provenance saves in this fixture. It needs
its own correctness-preserving change and failure/reopen/concurrent-observation
qualification. Its implementation is unchanged in this commit. No filesystem
indexing, incremental checkpointing, whole-object buffering rewrite or new storage
abstraction is started.
