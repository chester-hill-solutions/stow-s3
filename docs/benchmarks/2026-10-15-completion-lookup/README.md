# Retain multipart accounting after ambiguous completion lookup

Baseline `b070dea` on `codex/core-copy-quality`; a fresh fetch of `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. This addresses the separate
[completion lookup finding](../2026-10-15-s3-abort-recovery/README.md#next-confirmed-finding--ambiguous-completion-lookup-discards-retained-state).
Earlier corrections are retained; no COPY, buffering, indexing or checkpoint
optimization is included.

## Original defect and correction

A real workspace completion publishes the four-byte object but cannot remove its
multipart files because the upload directory is read-only. A wrapper at the native
multipart seam then returns an unavailable lookup joined with ErrUploadNotFound
and ErrMutationCommitted. Both joined-error orders reproduce the problem: runtime
completion treats the missing cause as authoritative, discards the retained
four-byte reservation and upload session, and drops the lookup uncertainty from
its returned error. The actual upload/part remains on disk.

After restoring permissions and reliable lookups, a policy permitting the actual
target cannot abort the retained upload: authorization reports
ErrResourceUnresolved because completion discarded its cached identity. Explicit
RefreshUsage was a workaround. HTTP similarly returns 500 on the repaired abort,
where a recoverable retained upload should return 204. This is evidence of lost
recovery/accounting state, not permanent object data loss or a proven workspace
quota bypass.

The production change adds one condition to reconcileCompletionError. Only
ErrUploadNotFound **without** ErrMutationCommitted confirms absence and calls the
existing removeUpload. An ambiguous missing result instead takes the existing
errors.Join path, retaining the upload and its reservations and preserving the
lookup error. Successful completion does not enter this method. Ordinary confirmed
absence still frees capacity. No new read, lock, interface, dependency, public API
or format is introduced; there is no new recovery mechanism.

The exact earlier probe is retained [unchanged](raw/retained-probe.go.txt).
Its replay [source](raw/original-probe.go.txt) makes RefreshUsage and the second
abort conditional on failure of the first abort; otherwise correcting recovery
would make that unconditional second abort correctly report missing. The replay
fails twice normally and twice under -race before production edits:
[normal](raw/original-before.log), [race](raw/original-before-race.log).
It passes twice in both modes afterward:
[normal](raw/original-after.log), [race](raw/original-after-race.log).
The permission fault was enforced on this host; these cases did not skip.

## Regression and validation evidence

Twenty permanent cases cover four workspace joined/wrapped error orders, fourteen
memory-backed reconciliation controls (absent/existing targets), and two S3
policy-scoped recovery cases. They check published bytes, ETag/version and metadata,
actual retained parts, usage/reservations, one post-completion lookup, direct abort
without RefreshUsage, and reuse of byte/object/session capacity. Confirmed raw,
wrapped and real missing results still clean up; unknown and marker-only results
retain conservative reservations. Real memory completion removes its upload before
faulted lookup, so a later confirmed-missing abort must also release that
conservative reservation.

The regressions fail twice against the original production code:
[before](raw/before-tests.log). They pass twice after the one-condition fix,
alongside relevant existing publication tests: [after](raw/after-tests.log).
Five focused race repetitions cover **100 new cases**:
[race](raw/focused-race.log).

Full Go tests, relevant runtime/storage/S3/run-through/embedded/public race suites,
all four conformance profiles ([summary](raw/conformance-summary.log)), relevant
vet, unchanged quality/file-size ratchets, source formatting, documentation links,
generated-output verification, WASM smoke and seven package integration tests pass.
[Full Go](raw/full-go.log), [broad race](raw/broad-race.log),
[quality](raw/quality.log), [format/docs](raw/format-docs.log) and
[package](raw/package-tests.log) outputs are retained. No ratchet baseline is raised.
The temporary next-finding probe and dependency symlink are removed.
Each conformance profile retains two existing skips: opt-in live upstream coverage
and mirrored bucket creation awaiting an authority decision. None of the new
regression cases skipped on this host.

## Comparative measurements and costs

BenchmarkCompletionLookupAccounting models the actual post-publication accounting
sequence under explicit eight-byte, one-object/one-upload limits. Each iteration
creates a fresh ledger, registers an upload and four-byte part, accounts the
published four-byte object and runs reconciliation over a lookup-only fixture.
Ledger construction is timed; no backend or payload is staged. Fresh ledgers are
necessary because the original ambiguous path incorrectly removes state while
the correction retains it. This is an accounting microbenchmark, not end-to-end
S3 latency or peak memory.

BenchmarkMemoryCompletionSuccessLifecycle uses the real memory runtime to initiate,
upload four bytes and successfully complete, overwriting the same target after
the first iteration. Setup/close are outside timing; actual runtime checks,
accounting and storage work are inside. It guards the successful path, which
bypasses the changed reconciler. It does not measure disk or HTTP/XML overhead.

Identical benchmark sources are compiled against baseline and candidate using
Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64. Each pass uses serial
baseline/candidate/candidate/baseline blocks with three samples each, first at one
processor, then eight: six samples per version/case/configuration. These loops
are serial even with GOMAXPROCS=8. No agent builds/tests run concurrently with
measurements; other host activity is uncontrolled. Both passes, every sample and
outlier, exact commands, binary digests and environment are retained:
[environment](raw/benchmark-environment.json), [first pass](raw/measurements.json),
[longer pass](raw/long-measurements.json).

The first pass uses 250,000 accounting iterations and 50,000 successful lifecycle
iterations. The longer pass uses 1,000,000 and 500,000 respectively. The repeat
investigates the first pass's material timing differences; it does not replace
or discard those results.

First pass (all results retained):

| Case | Processors | Median ns/op before → after | Change | Before range | After range |
|---|---:|---:|---:|---:|---:|
| Confirmed missing | 1 | 1,055.000 → 1,086.300 | +3.0% | 929.60–1,784.00 | 852.30–1,652.00 |
| Wrapped missing | 1 | 1,280.500 → 1,107.000 | -13.5% | 992.50–1,587.00 | 1,002.00–1,723.00 |
| Unknown lookup | 1 | 1,070.000 → 1,049.000 | -2.0% | 952.80–1,180.00 | 921.10–1,119.00 |
| Ambiguous, marker first | 1 | 1,485.500 → 1,079.000 | -27.4% | 912.60–2,182.00 | 876.50–1,359.00 |
| Ambiguous, missing first | 1 | 1,054.050 → 947.650 | -10.1% | 879.60–1,765.00 | 866.20–1,654.00 |
| Retained upload | 1 | 836.450 → 962.500 | +15.1% | 804.20–1,919.00 | 874.70–1,319.00 |
| Successful lifecycle | 1 | 4,121.000 → 6,086.500 | +47.7% | 4,035.00–4,216.00 | 4,554.00–9,611.00 |
| Confirmed missing | 8 | 776.750 → 1,134.500 | +46.1% | 732.70–927.30 | 968.00–1,418.00 |
| Wrapped missing | 8 | 785.100 → 1,218.500 | +55.2% | 727.00–793.70 | 1,003.00–1,569.00 |
| Unknown lookup | 8 | 722.100 → 1,124.500 | +55.7% | 700.30–997.90 | 839.10–1,427.00 |
| Ambiguous, marker first | 8 | 1,056.600 → 983.500 | -6.9% | 729.80–1,373.00 | 805.50–1,277.00 |
| Ambiguous, missing first | 8 | 859.350 → 1,007.500 | +17.2% | 750.60–1,244.00 | 674.80–1,235.00 |
| Retained upload | 8 | 740.750 → 820.200 | +10.7% | 679.90–838.10 | 711.80–909.30 |
| Successful lifecycle | 8 | 3,695.000 → 4,365.000 | +18.1% | 3,614.00–4,808.00 | 3,362.00–7,840.00 |

Longer pass (all results retained):

| Case | Processors | Median ns/op before → after | Change | Before range | After range |
|---|---:|---:|---:|---:|---:|
| Confirmed missing | 1 | 1,944.000 → 977.000 | -49.7% | 823.20–2,915.00 | 881.60–1,454.00 |
| Wrapped missing | 1 | 1,651.150 → 1,517.500 | -8.1% | 861.80–4,015.00 | 1,015.00–2,083.00 |
| Unknown lookup | 1 | 1,358.000 → 2,004.500 | +47.6% | 752.40–2,114.00 | 923.50–2,279.00 |
| Ambiguous, marker first | 1 | 1,531.150 → 1,543.000 | +0.8% | 835.10–2,401.00 | 869.20–2,192.00 |
| Ambiguous, missing first | 1 | 1,495.500 → 1,613.000 | +7.9% | 1,004.00–2,595.00 | 826.80–2,515.00 |
| Retained upload | 1 | 1,438.000 → 1,726.750 | +20.1% | 792.50–2,009.00 | 775.10–3,976.00 |
| Successful lifecycle | 1 | 5,998.000 → 6,005.500 | +0.1% | 3,808.00–8,544.00 | 3,683.00–10,359.00 |
| Confirmed missing | 8 | 833.650 → 1,018.350 | +22.2% | 778.20–937.20 | 847.30–1,497.00 |
| Wrapped missing | 8 | 872.400 → 1,140.550 | +30.7% | 791.50–962.40 | 936.50–2,095.00 |
| Unknown lookup | 8 | 791.250 → 1,214.500 | +53.5% | 679.00–1,732.00 | 935.60–1,775.00 |
| Ambiguous, marker first | 8 | 1,024.050 → 1,015.500 | -0.8% | 768.20–1,122.00 | 766.80–2,488.00 |
| Ambiguous, missing first | 8 | 832.300 → 857.050 | +3.0% | 751.70–976.30 | 791.80–1,416.00 |
| Retained upload | 8 | 807.200 → 990.500 | +22.7% | 689.60–1,741.00 | 875.60–1,153.00 |
| Successful lifecycle | 8 | 3,896.500 → 5,203.500 | +33.5% | 3,533.00–4,130.00 | 4,111.00–7,552.00 |

The first successful-lifecycle medians worsen **47.7% / 18.1%**, an increase of
1,965.5 / 670 ns at one/eight processors. The longer pass measures **+0.1% /
+33.5%**, or +7.5 / +1,307 ns. These material adverse measurements are not dropped.
The longer one-processor baseline itself changes from 3,808–4,123 ns in its first
block to 7,873–8,544 ns in its last; the candidate blocks similarly span
3,683–4,361 and 7,650–10,359 ns. Both implementations show substantial time drift.
The eight-processor successful distributions also vary, with a materially higher
candidate median. The runs do not establish latency parity, causation or a speedup;
the timing uncertainty remains a qualification limit for this change.

The compiled CompleteMultipartUpload instruction sequence has the same control
flow/call targets and stack frame; its differences are address relocations,
including a shifted string address. The added classification occurs only in the
error reconciler. [Disassembly comparison](raw/completion-codegen-diff.log) supports
that successful completion adds no executed guard; it does not explain away the
observed whole-lifecycle timing difference or establish performance parity.
Other unchanged-behavior cases also show large median changes (including +47.6%
and +53.5% for unknown lookups in the longer pass). No optimization is inferred
from these noisy results and no machinery is added to chase them.

The two ambiguous orders change **1,152 B / 12 allocations → 1,192 B / 13
allocations (+40 B, one allocation)** per modeled lifecycle. The existing joined
error is now retained rather than omitted; skipping erroneous cleanup also avoids
its target-key allocation. Confirmed missing stays 1,152 B / 12 allocations;
unknown stays 1,192 B / 13; retained stays 1,176 B / 13. Successful lifecycle stays
34 allocations, with cumulative byte totals reported in the measurements. These
are allocation totals, not peak heap or RSS; no payload buffer is added.

Compatibility/capacity cost: uncertain missing observations keep reservations,
including when the provider actually removed the upload. A subsequent healthy
confirmed-missing abort, or an authoritative usage refresh, can reconcile them.
This conservative behavior prevents premature release; the original completion
error and the joined lookup uncertainty remain observable. Ordinary missing
results gain an additional error-chain check, with no extra provider call. The
runtime lock and context propagation remain unchanged.

The refreshed packaged WASM remains **4,617,093 bytes (zero size change)**, with a
new digest. [Artifact metadata](raw/wasm.json) records both digests; generated-output
verification, WASM smoke and package tests check the refreshed artifact.

## Next confirmed finding — ambiguous object HEAD strands quota

A separate probe uses a real memory store with a three-byte object at a
one-object cap, then injects an unavailable HEAD joined with ErrObjectNotFound
and ErrMutationCommitted at the native store seam. In either order,
storedObjectSize accepts the missing cause as absence. DeleteObject then removes
the real object and returns success while runtime Usage remains three bytes/one
object. A healthy delete retry returns missing and cannot free the phantom quota;
a one-byte PUT to another key fails the object cap.

Both orders reproduce twice normally and twice under -race, without a reported
data race: [source](raw/next-head-ambiguity-probe.go.txt),
[normal](raw/next-head-ambiguity.log), [race](raw/next-head-ambiguity-race.log).
This is a deliberately injected ambiguous read result, not a claim that the
built-in memory HEAD naturally emits committed mutation errors. The demonstrated
failure is incorrect accounting and failed capacity reuse after actual deletion.
No production change to object-size observation/deletion is included here.

Reproduce by temporarily copying the source text to
`internal/runtime/head_ambiguity_review_probe_test.go`, running only
TestReviewDeleteAmbiguousObjectHead with -count=2, optionally -race, then removing
it. This failing next-finding probe is removed before broad validation.
