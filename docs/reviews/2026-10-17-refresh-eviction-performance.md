# Avoid cache work for objects that cannot fit

Follow-up: [outbox durability and journal-depth measurements](2026-10-17-outbox-durability.md).

This change streams an oversized upstream GET after recording its provenance,
instead of writing the object into a cache that immediately evicts it and fetches
it again. Oversized HEAD/prewarm reads return the same observed metadata and close
the upstream body without consuming it. Ordinary cache fills and genuine cache-
disappearance fallback retain their existing behavior.

Base `488378bb29dff15865cba4bddbad3238e9f3bbb3` includes the previous correctness
and listing work. A fresh fetch reports `origin/main`
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor.
[Exact baseline](2026-10-17-refresh-eviction-performance/raw/baseline.json). No completed correctness work is repeated.
One independently reviewable change is implemented; the next finding is left alone.

## Reproduction and rejected candidate

The [original regression](2026-10-17-refresh-eviction-performance/raw/bypass-regression/reproduce.log) fails twice
against the original adapter source: all eight native memory/filesystem cache,
adapter/runtime and memory/file-outbox combinations issue two upstream GETs.
The first stream is consumed and closed before the caller receives a second one.
HEAD also consumes a body that cannot remain cached. Source changes between those
GETs require the existing second durable provenance save. Existing fallback and
concurrent-observation race controls pass on the original source.

The first candidate opened the cached snapshot before eviction. Its
[192 timing observations](2026-10-17-refresh-eviction-performance/raw/snapshot-timings/summary.json) reduce the extra
GET and changed-version save, but its [96 resource observations](2026-10-17-refresh-eviction-performance/raw/snapshot-resources/summary.json)
show a real new cost: a 16-MiB object retains **20,653,976 heap bytes** while the
reader is held, compared with 1,928 bytes on the baseline. A filesystem reader
holds 16,777,363 unlinked file bytes. That candidate is rejected and is absent from
the final production code. Every timing, including adverse rows, remains saved.

The baseline memory cache also allocates 102,649,104 bytes during opening of that
16-MiB source, despite a one-byte cache budget. That comparative measurement
justifies bypassing a demonstrably impossible admission rather than retaining the
extra payload or changing storage abstractions.

## Selected change and contracts

[adapter.go](../../internal/runthrough/adapter.go) keeps the first provenance save
before any cached publication or successful direct return. When the separate
cache has a positive byte limit and the source exceeds the entire limit, it
removes an older cached copy of that key, then transfers the existing upstream
reader and its own metadata to GET. HEAD returns the metadata and closes the
reader. Unrelated cache entries are retained. Empty, exactly fitting, unlimited
and shared-store cases use the existing cache path.

[cache_read.go](../../internal/runthrough/cache_read.go) contains two small private
admission/deletion helpers and the unchanged cached-open/error-cleanup/fallback
block moved out of the refresh routine to keep its complexity within the existing
limit. The changed-ETag fallback still persists the new observation and closes a
reader on preservation failure. There is no dependency, format, public API, index,
checkpoint, new cache or storage abstraction.

The [regressions](../../internal/runthrough/refresh_eviction_test.go) cover native
cache variants, runtime accounting, checksums/properties paired with the reader,
file-outbox reopen, first-save and stale-copy deletion failures, reader ownership,
read-time upstream errors, HEAD not consuming an uncacheable body, zero/exact-limit
admission, and preserving unrelated cached keys. If upstream changes during the
cache-removal boundary, a write based on the observed older version conflicts.
The original version-consistency tests now force actual cache disappearance at
open, without a byte limit that would bypass that fixture. Their failure/reopen/
conditional-write checks remain enabled. The existing durable-provenance eviction
test now fills a one-object cache with a second key, so it still tests a real
eviction and restart rather than relying on impossible admission.

## Railway measurements

All builds, formatting, benchmarks, tests, race/conformance, vet, quality, site and
document checks execute on the same authorized Railway scratch runner. Local work
is source editing, packaging, orchestration, exported-data analysis and Git.
Deployment `b42dbfbd-9386-4a95-96e0-f74bd4fbc454` reaches SUCCESS with pinned Go 1.25.6,
linux/amd64, 4 CPU / 4 GiB and the persistent ext4 results volume. Commands execute
as UID 10001 using the existing child reaper. The snapshot preserves executable
modes. Task-only Go overlays live under an underscore directory and do not enter
product package discovery.

The final comparison retains **192 timing samples**, eight per workload/version/
CPU, in ABBA and BAAB cycles with one-second minimum time and two repetitions per
block. CPU labels are GOMAXPROCS 1 and 4, not affinity or concurrent-client counts.
Both binaries use identical current test fixtures. The baseline overlays the
original adapter; added private cache helpers are unused on that baseline path.
The changed-source fixture mutates upstream once per cache-deletion attempt in
both versions, including an absent oversized entry, so fewer GETs do not also
remove fixture mutations. This differs from the earlier per-GET mutation fixture;
older report timings are retained as history and are not directly pooled here.
[Plans, sources and binary hashes](2026-10-17-refresh-eviction-performance/raw/final-timings/build.json),
[all samples](2026-10-17-refresh-eviction-performance/raw/final-timings/samples.json),
[all ranges](2026-10-17-refresh-eviction-performance/raw/final-timings/summary.json),
[block resource/load snapshots](2026-10-17-refresh-eviction-performance/raw/final-timings/block-resources.json).

| CPU | Workload | Median before → after | Change | B/op before → after | Allocations before → after | Saves/op | GETs/op |
|---|---|---:|---:|---:|---:|---:|---:|
| 1 | oversized memory/stable | 2.530 → 0.541 μs | -78.60% | 2,968.0 → 456.0 | 31 → 9 | 1 → 1 | 2 → 1 |
| 1 | oversized memory/changed | 3.527 → 1.282 μs | -63.64% | 4,376.0 → 1,544.0 | 47 → 19 | 2 → 1 | 2 → 1 |
| 1 | oversized file/stable | 3.703 → 3.148 ms | -14.97% | 7,657.0 → 5,141.0 | 85 → 62 | 1 → 1 | 2 → 1 |
| 1 | oversized file/changed | 6.757 → 3.395 ms | -49.75% | 13,751.0 → 6,229.0 | 154 → 72 | 2 → 1 | 2 → 1 |
| 1 | control cache-hit | 0.748 → 0.743 μs | -0.78% | 1,208.0 → 1,208.0 | 12 → 12 | 0 → 0 | 0 → 0 |
| 1 | control head-evicted | 3.887 → 3.722 ms | -4.23% | 7,360.0 → 5,141.0 | 79 → 62 | 1 → 1 | 1 → 1 |
| 4 | oversized memory/stable | 2.607 → 0.548 μs | -78.99% | 2,975.0 → 457.0 | 31 → 9 | 1 → 1 | 2 → 1 |
| 4 | oversized memory/changed | 3.950 → 1.447 μs | -63.37% | 4,387.0 → 1,547.0 | 47 → 19 | 2 → 1 | 2 → 1 |
| 4 | oversized file/stable | 3.244 → 3.131 ms | -3.46% | 7,720.5 → 5,210.5 | 85 → 63 | 1 → 1 | 2 → 1 |
| 4 | oversized file/changed | 6.336 → 3.263 ms | -48.51% | 13,903.5 → 6,268.0 | 155 → 73 | 2 → 1 | 2 → 1 |
| 4 | control cache-hit | 0.811 → 0.814 μs | +0.35% | 1,208.0 → 1,208.0 | 12 → 12 | 0 → 0 | 0 → 0 |
| 4 | control head-evicted | 3.248 → 3.056 ms | -5.92% | 7,367.5 → 5,144.5 | 80 → 63 | 1 → 1 | 1 → 1 |

Adverse final medians: CPU 4 BenchmarkRefreshVersionControl/cache-hit: +0.35%.
No observations or slower controls are discarded. These sequential mock-upstream
measurements qualify this code path; they do not establish live-provider network
latency, HTTP throughput or journal-depth scaling. The returned source snapshot
can differ from the old second GET when a concurrent upstream write occurs;
each successful result remains internally coherent and its provenance matches
the observation used by this operation. Per-key provenance is not per-reader
history or concurrent-write serialization.

The final resource comparison retains **96 observations**: memory/filesystem
caches, 1/16-MiB generated streaming sources, one-byte cache budget, ABBA/BAAB,
three repetitions and one operation per invocation. It samples after explicit GC
while a reader is held, then after consuming and releasing it. The source generator
does not preallocate the payload. Open-phase allocation includes the common
instrumentation; it is not a universal per-request bound. Negative small heap/RSS
deltas reflect GC/scavenging and measurement noise.

| Cache / source bytes | Open-phase allocated bytes before → after | Held heap delta before → after | Held unlinked file bytes before → after |
|---|---:|---:|---:|
| memory/bytes=1048576 | 5,254,416 → 10,536 | 1,928 → 416 | 0 → 0 |
| memory/bytes=16777216 | 102,649,104 → 10,536 | 1,928 → 416 | 0 → 0 |
| filesystem/bytes=1048576 | 241,072 → 19,400 | -7,040 → -8,288 | 0 → 0 |
| filesystem/bytes=16777216 | 241,120 → 19,432 | -7,040 → -8,288 | 0 → 0 |

[Resource samples and RSS ranges](2026-10-17-refresh-eviction-performance/raw/final-resources/summary.json),
[diagnostic source](2026-10-17-refresh-eviction-performance/raw/refresh_resource_probe_test.go.txt).
The probe's forced-GC timings are not used as throughput evidence. Total retained
observations across rejected and selected candidates are **576**. The selected
path avoids full-payload cache allocations and leaves no unlinked cached payload.
Whole-object runtime APIs still allocate caller-owned result bytes; this is not
process-memory isolation or a new limit on upstream/network buffers.

## Validation and costs

[Initial selected-code checks](2026-10-17-refresh-eviction-performance/raw/bypass-initial-checks/checks.json) pass all four
conformance profiles (memory, filesystem, runtime-memory, runtime-filesystem),
focused normal/race, vet, site and doc checks. Full Go and broad race initially
find the old test's expectation of an eviction for a never-retained object; the
quality gate finds refresh complexity. Both are corrected without weakening
restart/conflict assertions, increasing a baseline or adding an exemption.
The unchanged fallback block is moved into the existing cache-read file.
[Final ten-check run](2026-10-17-refresh-eviction-performance/raw/final-checks/checks.json) passes format, focused normal/
race twice, full Go, broad storage/runtime/embedded/run-through/S3/public API race,
vet, quality/file-size/format, site build/tests and document links.
The production comment ratchet decreases from 4,082 to 4,081. After measurement,
the four conformance profiles are rerun against the exact final source hashes.
[Final conformance receipt](2026-10-17-refresh-eviction-performance/raw/final-checks/conformance-final-receipt.json).
Existing live-provider skips remain; Linux qualification does not substitute for
running Windows/macOS tests.

The rejected candidate's earlier test-helper complexity failure and the resource
probe's root-owned overlay-directory setup failure are also retained. The latter
is repaired only in the scratch overlay tree and the measurement is rerun before
using any result. [Probe setup failure](2026-10-17-refresh-eviction-performance/raw/probe-setup-failure/format-prototypes.log).

Compatibility and resource costs are explicit:

- Oversized entries are never inserted, so they no longer increment the eviction
  counter. Removing an older cached copy still counts as an eviction. Other keys
  are no longer displaced by an object that cannot fit.
- Returned metadata, including ETag and Last-Modified, comes from the first
  upstream GET whose reader is returned. An oversized read is unavailable offline.
- GET may encounter body errors during Read rather than cache population. The
  caller owns Close after successful opening; failed provenance/deletion closes
  the reader internally. Existing direct-fallback reads already use this stream
  error contract. No successful durable observation is returned before its save.
- A stale cached-copy deletion can fail and refuse the read. This preserves cache/
  provenance consistency; it does not hide an I/O failure as absence.
- One durable provenance save remains. Its I/O and failure opportunity are retained.
  No claim is made for inaccurate custom-provider size metadata or network-wide
  memory bounds.

[Final tested hashes](2026-10-17-refresh-eviction-performance/raw/final-checks/environment.json) match the measured source.
Export receipts verify lengths and SHA-256 before runner shutdown. A final Railway
link check covers this report and follow-up links, with a source/report hash
receipt retained alongside final checks. The benchmark target is untouched; the
scratch runner is stopped after verified export.

## Next highest-priority performance investigation

`FileOutbox.SaveUpstreamState` still loads, clones and rewrites the whole saved
state even for an unchanged observation. The remaining single durable save is
visible in stable oversized and HEAD cases. Measure latency, allocations and
recovery behavior across realistic journal depths before proposing an idempotent
save or a persistence change. It is not modified here. No filesystem indexing,
incremental checkpointing or next optimization is started.
