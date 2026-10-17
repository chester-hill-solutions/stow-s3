# Pair refreshed bodies with their captured metadata and upstream state

Follow-up: [Railway performance requalification and directory-path reuse](../../reviews/2026-10-17-part-listing-performance.md)
records all new measurements, including adverse timings. Changed-version durable
fallback is now measured on Railway and remains the next separate change.

Corrected on 2026-10-16 from `67db556d2a817c75ad4154d52ce65e8c7a26fc17` (full identity in
[environment.json](raw/environment.json)), on `codex/core-copy-quality`.
Freshly fetched `origin/main` is
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor of this branch.
The preceding [reader ownership correction](../2026-10-16-runthrough-open-cleanup/README.md)
is preserved. No completed COPY, publication, accounting or stream work is repeated.

## Demonstrated defect

The saved repro sets the native memory cache limit to one byte and reads a
three-byte upstream object. Cache eviction removes the refreshed copy. Upstream
legitimately changes `old` to `new` before the fallback GET; Stow returns body
`new` with the old ETag, content type and user metadata. Both bodies have length
three, so existing length checks cannot detect the mismatch. No invalid provider
response or scheduling assumption is needed.

Before production edits, the original [source](../2026-10-16-runthrough-open-cleanup/raw/next-version-probe.go.txt)
fails twice [normally](raw/original-before.log) and twice [under race](raw/original-before-race.log).
An initial 23-case matrix also runs before production edits:
[normal](raw/before-tests.log), [race](raw/before-race.log).
Per repetition, 21 defect cases fail and two controls pass.

The final **29-case** regression matrix adds metadata-only replacement and durable
recovery/write coverage. Replayed against exact baseline production files, it has
**27 failed leaves and two passing controls per repetition**, twice both
[normally](raw/final-before.log) and [under race](raw/final-before-race.log).
No build failure is counted as a reproduced defect.

There is a second manifestation at the same refresh return boundary. A deterministic
channel barrier pauses the first refresh just before its cache GET. A second
refresh replaces the cached object, then the first GET captures the replacement.
Before the fix it returns that body's bytes with the metadata saved by the first
PUT. Adapter and runtime tests cover equal-length, longer and empty replacements;
no sleeps are used to produce the interleaving.

Fallback tests cover memory and file outboxes, same-length/longer/empty bodies,
metadata-only changes with an unchanged ETag, checksums, accounting, reader closure,
persistence failure, missing ETag, stable source and warming HEAD. They also show
the stale upstream observation surviving durable reopen and incorrectly refusing
a subsequent conditional mirror write against the version the caller actually read.

## Small correction

After a successful cache GET, return the metadata captured by that GET alongside
its reader. Do not substitute the earlier cache PUT's result. If the cache copy
has disappeared, return the fresh upstream GET's reader and metadata together.
No separate HEAD or re-read is introduced.

The fallback records its fresh upstream ETag through the existing saveUpstreamState
operation if it differs from the initially observed upstream ETag. That keeps
existing conditional propagation based on the version actually returned. An
unchanged ETag requires no additional provenance save, even if other metadata
changed. All returned properties still come from the fresh GET.

If the new observation cannot be recorded, return the original save error and
let the existing adapter boundary close the fresh reader. The original refresh
response still closes through its existing defer. Tests retain exactly-once
cleanup, nil results on failure and healthy recovery after persistence repair.
A missing fresh ETag now fails through the existing validation error, consistently
with the initial upstream GET, rather than returning an unrecordable observation.

A 14-line private retry helper keeps the existing refresh function within its
complexity limit. The production diff is **17 added and seven removed lines in
two existing files**, with no new public API, dependency, format, storage abstraction,
reader wrapper or buffer. Both provider GETs already allocated their descriptors;
the correction uses those results. Existing cache limits, eviction, TTL, authority,
HEAD warming, request count and authoritative storage accounting are retained.

The contract remains per-read captured version consistency. This does not add
per-caller upstream histories or serialize independent concurrent observations.
Provenance continues to use the existing per-key ETag model. Proper reader+metadata
results remain the provider's responsibility; malformed successful results with
nil metadata are not newly qualified.

## Validation

- [Final regressions](raw/after-tests.log): **29 leaves twice = 58 passes**.
- The original repro passes twice [normally](raw/original-after.log) and twice
  [under race](raw/original-after-race.log).
- [Entire run-through race suite](raw/focused-race.log): **259 leaves five times =
  1,295 passes**, including deterministic overlapping refresh, outbox concurrency,
  restart, policy, offline and prior cleanup checks; no skips or races.
- [Full Go suite](raw/full-go.log), [broad runtime/storage/S3/run-through/public
  race suite](raw/broad-race.log) and [vet](raw/vet.log) pass.
- [Four conformance profiles](raw/conformance-summary.log) pass: memory,
  filesystem, runtime-memory and runtime-filesystem. Each retains its two existing
  live-upstream/mirror-bucket skips.
- [Quality and file-size gates](raw/quality.log), [generated comparison](raw/generated.log),
  [WASM smoke](raw/wasm-smoke.log), [seven package integration tests](raw/package-tests.log),
  [source/provenance checks](raw/final-source-check.log) and
  [documentation checks](raw/docs.log) pass. Exact commands are retained in the
  [runner](raw/validate.py.txt) and [final checks](raw/final-checks.py.txt).

The memory-only WASM artifact rebuilds **byte-for-byte unchanged at 4,619,197 bytes**:
[hashes](raw/wasm.json), [build](raw/build-wasm.log), [package build](raw/package-build.log).
A test matching the reader's body and descriptor after eviction/replacement would
have prevented the original metadata discard; the existing interfaces provide a
sufficient seam, without new storage machinery.

## Comparative measurements — changed durable fallback is substantially slower

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64. CPU labels specify GOMAXPROCS, not OS
processor affinity. No other agent tests or builds ran during timing. Unrelated
host load was uncontrolled. Start/end load averages (1/5/15 minutes) were:
**7.53 / 7.43 / 10.35 → 9.76 / 7.96 / 10.39**.
These are context, not proof of the cause of a sample.

Two optimized ordinary test binaries contain identical final tests and benchmarks;
only baseline production files are overlaid. Normal read benchmarks remain the
same as the preceding review. New benchmarks use real one-byte native cache eviction,
two existing upstream GETs, three-byte payloads and either a memory outbox or a real
FileOutbox in a temporary directory. The journal contains one provenance key and
no queued operations. Changed-version cases update upstream between each pair of
GETs; both implementations incur those fixture updates. Stable cases do not.

Two alternating ABBA/BAAB cycles at CPU 1 and 8, with two repetitions per block,
produce **288 retained observations**, eight per case/version/CPU:
[plan](raw/plan.json), [runner/parser](raw/measure.py.txt), [raw](raw/measurements.log),
[all samples](raw/samples.json), [summary with ranges and both cycle comparisons](raw/summary.json),
[compiler/source/binary identities](raw/environment.json).
No adverse result is removed. Earlier code often produces an incorrect descriptor
and stale state, so these are correctness costs, not equivalent-result speedups.

| CPU | Case | Median ns/op before → after | Change | Same-code absolute change, median |
| --- | --- | ---: | ---: | ---: |
| 1 | `file/changed` | 10,523,628.00 → 19,519,319.50 | +85.48% | 6.04% |
| 1 | `file/stable` | 10,420,227.00 → 10,377,639.00 | -0.41% | 2.57% |
| 1 | `memory/changed` | 5,299.00 → 5,360.50 | +1.16% | 0.74% |
| 1 | `memory/stable` | 3,186.00 → 3,281.00 | +2.98% | 3.75% |
| 1 | `cached` | 601.10 → 604.95 | +0.64% | 2.52% |
| 1 | `head-refresh` | 1,792.50 → 1,817.00 | +1.37% | 1.34% |
| 1 | `local` | 358.80 → 344.15 | -4.08% | 6.38% |
| 1 | `local-64KiB` | 346.35 → 350.75 | +1.27% | 2.65% |
| 1 | `refresh` | 2,035.50 → 1,990.50 | -2.21% | 3.37% |
| 8 | `file/changed` | 9,660,047.00 → 19,056,936.50 | +97.28% | 3.63% |
| 8 | `file/stable` | 10,899,714.00 → 10,218,409.00 | -6.25% | 1.31% |
| 8 | `memory/changed` | 4,556.50 → 4,597.00 | +0.89% | 3.10% |
| 8 | `memory/stable` | 2,747.50 → 2,744.00 | -0.13% | 2.01% |
| 8 | `cached` | 493.40 → 505.85 | +2.52% | 4.24% |
| 8 | `head-refresh` | 1,526.00 → 1,562.50 | +2.39% | 1.52% |
| 8 | `local` | 278.25 → 282.20 | +1.42% | 9.19% |
| 8 | `local-64KiB` | 285.60 → 276.15 | -3.31% | 3.26% |
| 8 | `refresh` | 1,699.00 → 1,722.50 | +1.38% | 2.09% |

**The changed-version durable fallback regresses materially:** 10.52 → 19.52 ms
at CPU 1 (**+85.48%**) and 9.66 → 19.06 ms at CPU 8 (**+97.28%**).
The measured provenance-save count changes **one → two per operation**. The fresh
observation must be persisted to avoid the demonstrated stale conditional-write
basis. This additional local persistence costs about nine milliseconds in this
fixture. It is not hidden as timing noise; the corresponding same-code variations
are much smaller. FileOutbox persistence includes its existing serialization,
locking and durable file publication work.

Stable fallbacks remain at **one save per operation**; normal cache-hit/local/
refresh/HEAD paths gain no save or upstream request. The changed memory-outbox
fallback also performs two saves, with sampled timing increases of 1.16% and
0.89%. Stable/normal timings include both increases and decreases, including
stable memory fallback +2.98% and CPU-8 cache hit +2.52%. No latency parity or
speedup claim is made. CPU/cycle ranges and every allocation sample remain in the
linked data.

### Allocations: extra persistence has a real cost

| CPU | Fallback case | B/op before → after | Allocs/op before → after | Provenance saves/op before → after |
| --- | --- | ---: | ---: | ---: |
| 1 | `file/changed` | 10,350.0 → 15,274.0 | 109 → 164 | 1 → 2 |
| 1 | `file/stable` | 7,867.5 → 7,872.5 | 84 → 84 | 1 → 1 |
| 1 | `memory/changed` | 5,448.0 → 5,464.0 | 56 → 57 | 1 → 2 |
| 1 | `memory/stable` | 2,968.0 → 2,968.0 | 31 → 31 | 1 → 1 |
| 8 | `file/changed` | 10,647.0 → 16,508.5 | 109 → 164.5 | 1 → 2 |
| 8 | `file/stable` | 8,814.0 → 8,776.0 | 85 → 85 | 1 → 1 |
| 8 | `memory/changed` | 5,465.5 → 5,482.0 | 56 → 57 | 1 → 2 |
| 8 | `memory/stable` | 2,977.5 → 2,977.0 | 31 → 31 | 1 → 1 |

Changed memory fallback adds **one allocation and about 16 B/op**. Changed durable
fallback adds roughly **55 allocations and 4.9–5.9 KiB/op** in this single-key
journal fixture. Stable fallback allocation counts are unchanged; its B/op medians
vary slightly as shown. Normal local/local-64KiB/cache-hit/refresh/warming-HEAD
allocated bytes and allocation counts match exactly at each CPU setting:
472/6, 472/6, 536/8, 1,392/22 and 1,128/18 respectively.

A real provider's network, storage and journal sizes can change latency. These
measurements do not establish HTTP throughput, peak heap or RSS, nor do they qualify
large-journal or production network behavior. Existing whole-object cache storage
behavior is unchanged. A changed fallback can now fail when its additional
provenance save fails; the caller receives no successful mismatched/stale observation,
and readers are closed. That new I/O/error opportunity is the main compatibility
cost, restricted to a fallback whose ETag changed.

## Next confirmed remaining finding — left unchanged

Outbox propagation opens its authoritative source directly through a.local.GetObject.
If that opening returns a reader plus an error, propagateWrite returns the error
before its existing Close defer is installed. The upper adapter GET cleanup is
not involved in this path. A real file remains open after a locally committed
mirror PUT whose propagation was refused.

The separate [file-backed probe](raw/next-propagation-probe.go.txt) fails twice
[on the baseline](raw/next-before.log), [under baseline race](raw/next-before-race.log),
[on this candidate](raw/next-after.log) and [under candidate race](raw/next-after-race.log).
It verifies the committed failure marker, zero source reads, no initial upstream
PUT, Close count zero and an open descriptor. A repaired subsequent write succeeds.
This is injected provider opening failure, not evidence that native filesystem
GET normally returns that combination. No outbox propagation implementation is
modified in the current version-consistency correction.
