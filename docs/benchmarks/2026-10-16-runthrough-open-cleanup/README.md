# Close failed run-through reads without losing the opening error

Corrected on 2026-10-16 from `fe6238d571ab59c5358bc6ee887753b51e713cb4`,
on `codex/core-copy-quality`. Freshly fetched `origin/main` is
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor of this branch.
The [previous runtime cleanup](../2026-10-16-open-error-cleanup/README.md) remains
intact. That change could only close readers it received; run-through discarded
some readers before reaching it. This change addresses that separate boundary.
[Source, toolchain and binary identities](raw/environment.json) pin the comparison.

## Before: opened files survive refused reads

The retained two-path probe was replayed before production edits:
[normal](raw/original-before.log), [race](raw/original-before-race.log).
Both local reads and upstream refreshes return the original error while leaving a
real file open. Each case fails twice in each log: **four expected failures per log**.
The original [probe source](../2026-10-16-open-error-cleanup/raw/next-runthrough-probe.go.txt)
uses the real adapter-to-runtime composition and verifies accounting and repaired retries.

A new **40-case** matrix ran twice before production edits:
[normal](raw/before-tests.log), [race](raw/before-race.log).
Per repetition, **25 defect cases fail and 15 existing-behavior controls pass**.
The final test sources were also replayed twice against the exact baseline
production files, with the same results:
[normal overlay](raw/final-before.log), [race overlay](raw/final-before-race.log).
No build failure is counted as a reproduced defect.

The matrix covers direct adapter and runtime reads of authoritative local data,
cached data, initial upstream fetches, post-refresh cache reopen failures and
failed fresh fetches after cache disappearance. It includes nil readers, Close
errors carrying missing/committed markers, warming HEAD errors, stable-version
successful fallback, metadata-only HEAD hits and original committed opening errors.
A callback at the second upstream request verifies that the failed cache reader
has already been closed before that request starts.

File captures assert Close exactly once, descriptor closure and zero payload
reads for a reader returned with an opening error. The matrix also checks unchanged
runtime accounting, repaired retries, body length, ETag, content type and user
metadata. Native metadata-only hits still open no bodies. Existing runtime cleanup
already handles cached error readers that reach it; those are controls here.

As with the preceding finding, reader+error combinations are injected provider
responses, not a claim that native filesystem GET naturally produces them.
Retained os.File references make the missing Close deterministic. Eventual file
finalization is not bounded request cleanup, and arbitrary readers need not have
finalizers. The next finding below uses ordinary native cache eviction instead.

## Change and contracts

The adapter's existing GetObject boundary now closes a reader returned alongside
an error and returns a nil reader with that same error and metadata result.
Local and retried-upstream error paths preserve the reader until that boundary
rather than discarding it. Existing nil/error metadata behavior is retained;
this does not manufacture a successful object from an opening refusal.

Upstream refresh registers its existing Close whenever it obtains a nonnil
response, before testing the opening error. This also covers warming HEAD, whose
public result intentionally contains no reader. A failed cache reopen closes its
reader before either returning its error or requesting the upstream fallback.
The original captured upstream response still closes on every refresh exit.
Successful returned bodies remain owned by their caller until Close or runtime EOF.

Close errors do not replace or join the original opening error. They therefore
cannot turn an ordinary failure into missing/committed status, or change whether
the existing missing-cache fallback runs. Failed cache reads no longer hand callers
a live reader with an error. Providers that already released a resource should
return nil; the adapter cannot close resources they keep private.

This is **14 added and four removed production lines in two existing files**.
No new production interface, helper abstraction, wrapper, buffer, dependency,
public API or storage format is introduced. Existing cache limits, TTL,
authority, provenance, eviction planning and source-version fallback behavior
remain as before. In particular, this does not correct the distinct metadata/body
version mismatch documented below.

The error path now pays the returned reader's Close cost. Close can block or fail
according to the provider; it is called once and cleanup errors are not retried or
newly exposed. The GetObject boundary adds an error check to healthy reads, and
refresh changes when its deferred Close is registered. Modest healthy-path CPU
costs cannot be ruled out by these measurements.

## Validation

- [Final new regressions](raw/after-tests.log): **40 leaves twice = 80 passes**.
- The original two-path probe passes twice [normally](raw/original-after.log) and
  [under race](raw/original-after-race.log).
- [Entire run-through race suite](raw/focused-race.log): **230 leaves five times =
  1,150 passes**, including policy, offline, cache eviction/TTL, restart,
  concurrent outbox and runtime-composition coverage. No skips or races.
- [Full Go suite](raw/full-go.log), [broad runtime/storage/S3/run-through/public
  race suite](raw/broad-race.log) and [vet](raw/vet.log) pass.
- [Four conformance profiles](raw/conformance-summary.log) pass: memory,
  filesystem, runtime-memory and runtime-filesystem. Each retains the two existing
  live-upstream/mirror-bucket skips.
- [Quality and file-size gates](raw/quality.log), [generated comparison](raw/generated.log),
  [WASM smoke](raw/wasm-smoke.log), [seven package integration tests](raw/package-tests.log),
  [final source/cleanup checks](raw/final-source-check.log) and
  [documentation checks](raw/docs.log) pass.
- [Validation runner](raw/validate.py.txt) and [final checks](raw/final-checks.py.txt)
  retain the exact commands.

The rebuilt WASM is **byte-for-byte unchanged at 4,619,197 bytes**:
[hashes](raw/wasm.json), [build](raw/build-wasm.log), [package build](raw/package-build.log).
Run-through is outside that memory-only WASM path. No generated asset needs updating.
Reader ownership tests at the existing adapter boundary would have prevented this
leak; no architecture expansion is needed.

## Measurements and new costs

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64; CPU labels mean GOMAXPROCS, not processor
affinity. No other agent tests or builds overlapped timing. Unrelated host activity
was uncontrolled. Load averages (1/5/15 minutes) at the start and end were:
**13.19 / 18.95 / 17.69 → 11.87 / 18.00 / 17.41**.
This is context, not a demonstrated explanation of any sample.

Two optimized ordinary Go test binaries use identical final benchmarks. Only the
baseline adapter/cache-read files are overlaid. The benchmark never recreates
stream implementations or removes WriterTo. Synthetic error readers can be reused
without leaking file descriptors in the old benchmark. Repeated refresh is forced
by an in-memory cache HEAD miss at one fixed key, so fixture growth does not drive
allocation or eviction cost. Healthy reads copy to io.Discard; this is not an HTTP,
disk, network, payload-throughput, RSS or peak-heap measurement.

Two alternating ABBA/BAAB cycles at CPU 1 and 8, with two repetitions per block,
produce **288 retained samples**, eight per case/version/CPU. See [plan](raw/plan.json),
[runner/parser](raw/measure.py.txt), [raw](raw/measurements.log),
[all samples](raw/samples.json) and [summary with ranges, allocations and both cycle
comparisons](raw/summary.json). Nothing is filtered out.

| CPU | Case | Median ns/op before → after | Change | Same-code absolute change, median |
| --- | --- | ---: | ---: | ---: |
| 1 | `cached` | 629.00 → 634.75 | +0.91% | 10.71% |
| 1 | `head-refresh` | 1,768.00 → 1,932.00 | +9.28% | 4.08% |
| 1 | `local` | 361.30 → 358.60 | -0.75% | 10.78% |
| 1 | `local-64KiB` | 336.70 → 349.65 | +3.85% | 6.26% |
| 1 | `local-nil-error` | 158.75 → 156.80 | -1.23% | 12.20% |
| 1 | `local-reader-error` | 169.20 → 154.55 | -8.66% | 25.13% |
| 1 | `refresh` | 2,041.00 → 1,976.00 | -3.18% | 10.87% |
| 1 | `upstream-nil-error` | 114.85 → 118.65 | +3.31% | 21.92% |
| 1 | `upstream-reader-error` | 112.70 → 116.35 | +3.24% | 45.14% |
| 8 | `cached` | 499.25 → 494.60 | -0.93% | 3.56% |
| 8 | `head-refresh` | 1,522.00 → 1,540.50 | +1.22% | 0.62% |
| 8 | `local` | 275.80 → 300.55 | +8.97% | 3.90% |
| 8 | `local-64KiB` | 278.25 → 273.20 | -1.81% | 3.30% |
| 8 | `local-nil-error` | 128.35 → 126.60 | -1.36% | 10.61% |
| 8 | `local-reader-error` | 128.55 → 127.35 | -0.93% | 10.17% |
| 8 | `refresh` | 1,684.00 → 1,755.50 | +4.25% | 6.14% |
| 8 | `upstream-nil-error` | 102.95 → 104.25 | +1.26% | 1.92% |
| 8 | `upstream-reader-error` | 102.80 → 101.55 | -1.22% | 5.01% |

The table retains the adverse **+9.28% warming HEAD** result at CPU 1 and
**+8.97% tiny local GET** at CPU 8. CPU-8 local cycles both increase (+19.70%,
+4.93%); their same-code variation ranges from 2.94% to 22.27%. Warming HEAD
also increases in both CPU-8 cycles (+5.25%, +1.03%). Healthy 64 KiB local GET
increases in both CPU-1 cycles (+2.28%, +8.61%). Other samples decrease.
The small number of cycles and variable timings do not establish latency parity,
or a stable regression magnitude. Improvements are not claimed as speedups;
error paths now perform necessary additional cleanup.

**Allocated bytes and allocation counts match for every measured case at both
CPU settings.** These are total per-operation measurements, not extra costs:

| Case | B/op before = after | Allocs/op before = after |
| --- | ---: | ---: |
| Local, local 64 KiB | 472 | 6 |
| Cached | 536 | 8 |
| Refresh | 1,392 | 22 |
| Warming HEAD | 1,128 | 18 |
| Local reader/nil opening error | 208 | 2 |
| Upstream reader/nil opening error | 48 | 1 |

For nonnil failed-open results the synthetic `failed-close/op` metric changes
**0 → 1**; nil-reader errors remain at zero. That metric only counts the synthetic
failed reader, not healthy caller-owned or refresh-response closes. The real
provider's syscall/network release time is additional and is not estimated by the
synthetic reader. The verified gain is bounded ownership cleanup without additional
allocations, with possible small healthy-path timing costs explicitly retained.

## Next highest-priority confirmed finding — not implemented

A normal cache size limit can force refresh to return a new upstream body with
metadata from an older upstream version. The probe sets a **one-byte cache cap**
and fetches a three-byte object. Native cache eviction removes the captured copy;
upstream legitimately changes `old` to `new` before the second GET. Both bodies
have length three, so this is not an inaccurate-size provider or a short/long-body
fault. The runtime returns body `new` with the old ETag, content type and metadata.

The ordinary MemoryStore cache and real adapter/runtime path reproduce the defect
without reader+error injection or timing assumptions:
[source](raw/next-version-probe.go.txt), [baseline](raw/next-before.log),
[baseline race](raw/next-before-race.log), [candidate](raw/next-after.log),
[candidate race](raw/next-after-race.log). It fails twice in each log; the native
cache is verified empty and exactly two upstream GETs occur. This can mislabel
S3 responses and break validator/version consistency. The missing-cache retry
currently discards the fresh GET's metadata and returns the previous cached
metadata; upstream provenance also remains from that earlier fetch.

The cleanup change deliberately preserves that fallback's existing behavior.
Correcting source-version consistency requires a separate regression and review;
no version, ETag or provenance logic is changed here.
