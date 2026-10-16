# Preserve prepared intents until local absence is reliable

Corrected on 2026-10-16 from `8e537ac0c0d0e92dd4bd435c0b376e94075ca7bf`, on
`codex/core-copy-quality`. Freshly fetched `origin/main` remains
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor of this branch.
The preceding [propagation reader cleanup](../2026-10-16-propagation-open/README.md)
and all prior corrections are preserved. Existing remediation and ADR 0002/0003
require recovery against the immutable local record before upstream propagation.
This change preserves that contract, without adding a storage feature or optimization.

## Demonstrated defect

The [saved probe](../2026-10-16-propagation-open/raw/next-prepared-probe.go.txt)
prepares a PUT in a real FileOutbox, commits a new local record, then injects a
HEAD error joining ErrObjectNotFound with ErrMutationCommitted. Prepared recovery
mistakes that marked error for reliable absence, silently discards the intent,
and returns success. The discard survives journal reopen. The actual local
replacement still exists, but a healthy retry cannot propagate it: the only
recovery intent is gone.

Before changing production code, the saved probe fails twice
[normally](raw/original-before.log) and twice [under race](raw/original-before-race.log).
An ordinary uncertain HEAD error is a passing control. The original probe passes
twice [normally](raw/original-after.log) and twice [under race](raw/original-after-race.log)
after the correction.

The permanent matrix also demonstrates the DELETE consequence. An uncommitted
delete still has its original local object. The same marked-missing lookup
incorrectly promotes its prepared deletion to pending. RetryPending can then
delete the upstream object despite the intact local version. This is the inverse
side of the same faulty absence classification, in the same recovery function.
Both manifestations are reproduced before changing the implementation.

These are injected provider HEAD results around native memory/filesystem records
and a real FileOutbox. They do not claim a naturally occurring filesystem error
or a new physical crash simulation. Journal reopening is real; the underlying
local store remains in the same process. Existing process-crash tests also run
in the full run-through suite.

## Small correction and preserved contracts

In reconcilePreparedEntryLocked, a missing-object error is reliable absence only
if it does **not** also match ErrMutationCommitted. The write branch discards an
intent only on that reliable absence; the delete branch commits it only on that
reliable absence. Otherwise both return the original HEAD error unchanged and
leave the prepared intent alone. The diff changes **two existing conditions**;
there is no new production helper, interface, format, dependency or public API.

Successful HEAD results follow the same immutable-version comparisons. Reliable
raw or wrapped missing errors retain the existing behavior: discard a prepared
write, or commit a prepared deletion with its original version. Ownership,
fencing, per-key ordering, retry scheduling and live-write consent use their
existing code. The formerly swallowed HEAD error now reaches recovery callers;
later layers retain their existing error mapping. No S3 wire format changes. The failed lookup does not authorize any local mutation or
upstream call at this recovery boundary.

An uncertain prepared intent deliberately continues blocking that key until
recovery can establish its local state. Providers that return a committed marker
with missing must supply a reliable subsequent lookup for recovery to proceed;
this is stricter than silently treating that combination as absence. The
correction does not add another lookup, a journal write, a timer or retry machinery.

## Railway-only measurement policy

Performance benchmarks for this change run only on the existing dedicated Railway
runner. Both binaries are built there before measuring. Local runs are correctness,
race and conformance tests; no local benchmark samples are taken for this change.
Earlier local reports remain historical evidence, not Railway qualification.

## Regression coverage

The **64-case** matrix includes:

- Memory and filesystem local stores, with a real FileOutbox.
- Prepared PUT, COPY, multipart and DELETE through RecoverPrepared and RetryPending.
- Marked-missing, wrapped/reordered marked-missing and ordinary uncertain HEAD errors.
- Reliable raw/wrapped missing controls across all four operation labels.

COPY and multipart cases model their already-published destination record,
which is what prepared recovery observes. They do not exercise separate source
copying or multipart initiation/completion in these tests.

On the unchanged production implementation, **32 defect cases fail and 32
controls pass per repetition**, twice [normally](raw/before-tests.log) and twice
[under race](raw/before-race.log). No build failure is counted as evidence. After the correction, all 64 pass twice:
[128 normal passing leaf executions](raw/after-tests.log).

Each uncertain call returns the exact HEAD error, performs one HEAD, retains
its ID, previous local version and upstream condition, and makes no upstream
request. The journal bytes remain unchanged before reopen, and the intent
survives reopen. On repaired lookup, committed writes deliver the new body
exactly once; uncommitted deletions leave both objects intact and retire the
unneeded intent. A second retry performs no mutation. Reliable absence controls
retain deletion versions and complete the proper upstream effect. No sleeps
establish the interleaving or recovery result.

## Verification

The final Go implementation and regression sources pass:

- [All Go tests](raw/full-go.log), [broad storage/runtime/S3/run-through race checks](raw/broad-race.log) and [vet](raw/vet.log).
- [Five repetitions of the entire run-through race suite](raw/focused-race.log): **1,780 passing leaf executions**.
- [Four conformance profiles](raw/conformance-summary.log): memory, filesystem, runtime-memory and runtime-filesystem; [full output](raw/conformance-full.log). The pre-existing mirror-bucket authority decision and opt-in live upstream tests remain skipped in each profile. No live AWS qualification is claimed.
- [Go/file-size gates](raw/quality.log), [WASM build](raw/build-wasm.log), [package build](raw/package-build.log), [generated-artifact checks](raw/generated.log), [real WASM smoke test](raw/wasm-smoke.log) and [seven package integration tests](raw/package-tests.log).

These correctness checks ran locally with Go 1.25.6. Performance benchmarks ran
only on Railway. The [verification runner](raw/validate.py.txt) and
[final provenance audit](raw/final-provenance.json) distinguish the two.
The [rebuilt WASM](raw/wasm.json) remains byte-for-byte identical:
**4,619,197 bytes**, SHA-256
`a828baf915d451665700e039bf7a1895bdfcd2306c2298cca9ea9482668f1193`.
Native prepared recovery is outside that artifact.

## Railway before/after measurements

The only benchmark execution for this change is Railway job
`2e94fe7d-63a4-48cb-af0d-5aa56fe0da13`, deployed as
`ddeed917-0e0e-4e22-b463-fd0a3e61b6ef` on the existing private benchmark runner.
The target service remains stopped. No new project, service or volume is created.
Both optimized ordinary Go test binaries are compiled during Railway image build,
before runtime measurements, with identical final tests and benchmarks. Only
outbox_adapter.go is replaced by its exact baseline source in the reference binary.
The complete uploaded native source payload is hash-checked during the build;
the four changed Go files match the local correctness-tested source freeze.

[Successful deployment](raw/railway-deployment.json), [job receipt](raw/run.json),
[export hashes](raw/railway-export-receipt.json), [input payload manifest](raw/railway-context-manifest.json),
[compiler/binary/source/runtime identities](raw/environment.json),
[source freeze](raw/source-freeze.json), [final provenance](raw/final-provenance.json).
The [Dockerfile](raw/Dockerfile.txt), [build script](raw/build.mjs.txt) and
[Railway-only runner script](raw/diagnosis.mjs.txt) retain the execution method.
The existing benchmark controller and authenticated controls are reused.
The remote script refuses to execute outside this Railway environment/service.

### Environment and method

- Go 1.25.6, Linux amd64, AMD EPYC 9655P host CPU.
- Verified cgroup ceilings: **4 vCPU / 4 GiB**, one replica. The host's 48 logical CPUs and host RAM are not the container allocation.
- Runtime measurement on the deployed runner, with compilation and dependency downloads finished. No conformance or target load runs alongside it; health/status controls remain available.
- GOMAXPROCS settings **1 and 4**, with serial benchmark workers.
- Two ABBA/BAAB cycles, two repetitions per block and 200 ms per observation: **256 retained samples**, eight per case/version/CPU.

[Plan](raw/plan.json), [raw output](raw/measurements.log), [all samples](raw/samples.json),
[summary with ranges and both cycle comparisons](raw/summary.json),
[per-block cgroup/load observations](raw/block-resources.json).
The readable log trims trailing padding on CPU header lines only;
[normalization hashes](raw/log-normalization.json) distinguish it from the exact
export receipt. All timing, allocation, iteration and action lines are unchanged.
The measurement span records **781 cgroup periods, zero throttled periods and
zero throttled microseconds**. This does not establish physical-host isolation,
a quiet clock or sustained production capacity. Before/after measurements use
the same placement; earlier desktop reports are not cross-host references.

The benchmark isolates reconcilePreparedEntryLocked. Successful HEAD uses native
MemoryStore; missing, marked and unknown results are injected. A stateless
CoordinatedOutbox counter records Commit/DiscardPrepared dispatch without I/O.
It does not time FileOutbox durability, locking, prepared-entry scans, queue
claims, upstream networking, S3 requests, process startup, peak heap or RSS.
Actual journal retention and reopen behavior are covered by the integration tests.

| CPU | Case | Median ns/op before → after | Change | Both cycle changes | Same-code absolute change, median |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | `delete/marked` | 48.28 → 29.20 | -39.54% | -38.85%, -41.54% | 9.70% |
| 1 | `delete/missing` | 41.55 → 48.30 | +16.24% | +10.84%, +19.91% | 6.06% |
| 1 | `delete/present` | 111.85 → 117.15 | +4.74% | +6.30%, +4.22% | 9.37% |
| 1 | `delete/unknown` | 13.44 → 13.82 | +2.79% | +6.40%, +1.39% | 4.29% |
| 1 | `put/marked` | 22.73 → 28.17 | +23.93% | +23.70%, +19.27% | 2.88% |
| 1 | `put/missing` | 15.88 → 20.11 | +26.57% | +25.95%, +28.58% | 4.80% |
| 1 | `put/present` | 141.25 → 143.85 | +1.84% | -1.00%, +1.37% | 2.16% |
| 1 | `put/unknown` | 13.26 → 13.12 | -1.02% | -2.96%, +0.96% | 2.51% |
| 4 | `delete/marked` | 52.41 → 31.08 | -40.70% | -42.18%, -41.90% | 5.40% |
| 4 | `delete/missing` | 43.25 → 49.12 | +13.60% | +8.00%, +11.94% | 6.07% |
| 4 | `delete/present` | 119.45 → 118.40 | -0.88% | +2.70%, -0.67% | 4.91% |
| 4 | `delete/unknown` | 13.62 → 14.16 | +3.89% | -1.52%, +12.40% | 2.34% |
| 4 | `put/marked` | 25.01 → 28.18 | +12.69% | +10.58%, +19.71% | 7.90% |
| 4 | `put/missing` | 17.32 → 22.30 | +28.72% | +37.82%, +24.90% | 7.59% |
| 4 | `put/present` | 146.65 → 143.10 | -2.42% | -1.22%, -7.54% | 2.48% |
| 4 | `put/unknown` | 13.16 → 14.54 | +10.41% | +12.19%, +6.21% | 2.87% |

**The extra missing-error classification has a measurable cost.** Reliable-missing
PUT adds **4.22 ns / +26.57%** at CPU 1 and **4.98 ns / +28.72%** at CPU 4.
Reliable-missing DELETE adds **6.75 ns / +16.24%** and **5.88 ns / +13.60%**.
These are error-classification microbenchmarks, not whole durable-recovery
latencies. The additional errors.Is traversal runs only after a missing match.
No extra HEAD, persistence or upstream operation is added.

Marked PUT costs **+23.93% / +12.69%** (5.44 / 3.18 ns); it now returns the
original error and retains the intent. Marked DELETE timings fall by about 40%,
while the operation changes from incorrect Commit dispatch to returning the
original error. These changed-result paths are correctness costs, not equivalent
result speedups. Their counters change from one incorrect discard/commit per
call to **zero**, and errors/op changes **0 → 1**. Unknown errors retain one
error and no commit/discard; all paths retain one HEAD per call.

Healthy-record medians range from **-2.42% to +4.74%**. Unknown DELETE increases
2.79%/3.89%; unknown PUT includes **+10.41%** at CPU 4 (13.17 → 14.54 ns).
All adverse samples, ranges, cycle comparisons and same-code variation remain
in the evidence. No exact latency-parity or broad speedup claim is made.

### Allocations and compatibility costs

Every retained observation has identical allocated bytes and allocation counts:

| HEAD result | B/op before → after | Allocs/op before → after |
| --- | ---: | ---: |
| Present, native MemoryStore | 208 → 208 | 2 → 2 |
| Missing, marked or unknown | 0 → 0 | 0 → 0 |

The main behavior change is conservative recovery: a marked-missing error now
surfaces and its prepared intent remains durable and blocks that key until a
reliable lookup is available. Ordinary missing, successful versions, ownership
and propagation policy retain their existing contracts. The previous local
changed-version durable-fallback measurements are not requalified or repaired
by this isolated Railway classifier comparison.

Railway's SFTP download timed out. The same completed result directory was
exported through working SSH, with every file length and SHA-256 checked before
cleanup. The retained results volume is untouched. [Export receipt](raw/railway-export-receipt.json),
[runner cleanup](raw/railway-runner-after.json), [target still stopped](raw/railway-target-after.json).

## Next confirmed finding — left unchanged

Mutation preflight's localVersionStrict still classifies a marked-missing HEAD
as ordinary absence and returns an empty previous version. This occurs before
prepared recovery and is a separate entry point from the corrected reconciliation.

The [separate deterministic probe](raw/next-preflight-probe.go.txt) keeps a real
native MemoryStore object and a real FileOutbox. Its first HEAD returns a missing
error joined with ErrMutationCommitted; later HEAD calls are healthy. The local
PUT is explicitly refused without committing. Preflight nevertheless admits it,
records an empty previous version, then recovery mistakes the unchanged original
record for a committed replacement. The caller receives old metadata and a
false committed mutation result. A healthy retry sends the old local bytes upstream,
replacing `remote-base`, although the requested local write never committed.

The marked-missing case fails twice and the ordinary uncertain HEAD control
passes twice in each [baseline normal](raw/next-before.log),
[baseline race](raw/next-before-race.log), [candidate normal](raw/next-after.log)
and [candidate race](raw/next-after-race.log). [Probe runner](raw/next-probe.py.txt).
This is provider fault injection, not a claim that native filesystem HEAD
spontaneously produces that error. The probe checks local version preservation,
false committed status, an invented pending intent and the actual upstream body
change. No preflight implementation or permanent test is added in this prepared
recovery correction.

[Documentation/site checks](raw/docs.log) accompany this report.
