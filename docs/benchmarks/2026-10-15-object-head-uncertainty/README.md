# Refuse ambiguous object HEAD before mutation

Baseline `058255d` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. This addresses the separate
[object HEAD finding](../2026-10-15-completion-lookup/README.md#next-confirmed-finding--ambiguous-object-head-strands-quota).
Earlier corrections remain intact. No indexing, checkpointing, buffering
optimization, dependency, public API or storage format is introduced.

## Evidence and smallest correction

The retained deterministic probe creates a real three-byte object under an
explicit eight-byte/one-object cap, then injects an unavailable HEAD joined with
ErrObjectNotFound and ErrMutationCommitted. Both joined-error orders reproduce:
storedObjectSize treats the missing cause as confirmed absence, DeleteObject
removes the actual object and returns success, but Usage remains three bytes/one
object. A healthy delete retry returns missing and a one-byte PUT elsewhere is
blocked by the phantom object count.

The [original probe](raw/original-probe.go.txt) fails twice normally and twice
under -race before production edits: [normal](raw/original-before.log),
[race](raw/original-before-race.log). After the correction it passes twice in
both modes: [normal](raw/original-after.log), [race](raw/original-after-race.log).
The error is deliberately injected at the native store seam over real memory
storage; this does not claim built-in memory HEAD naturally emits committed
mutation errors. The demonstrated defect is actual deletion with incorrect quota.

Batch deletion independently repeats the same classification in its HEAD
preflight. Native regressions demonstrate it deleting a healthy earlier key and
the faulted target while leaving quota charged for the latter. The shared helper
also serves buffered/staged PUT, COPY, multipart initiation/completion and abort.
Unknown target size must not be replaced with a zero-size/missing observation in
any of those paths.

The production diff changes **two existing conditions in objects.go**. A missing
cause only confirms absence if no committed marker is present. The shared helper
and batch preflight otherwise return the original error unchanged. No helper,
provider call, lock, body processing or allocation is added. Batch preflight still
finishes its observations before issuing any deletion; normal missing keys retain
their existing contracts. All existing authorization, context and quota checks
remain in their original order.

## Regression coverage and validation

The final suite has **59 cases**:

- Nine helper controls check success, raw/wrapped missing, unknown, marker-only,
  both joined orders and wrapping. Error identity is exact.
- Forty-four mutation cases combine four ambiguity orders with eleven paths:
  single/batch deletion, their separate quota-store views, buffered PUT, staged
  PUT admission and publication recheck, COPY, multipart initiation, completion
  and abort. They verify actual object bytes/versions/ETags/metadata, no provider
  deletion, unchanged usage/revision/reservations and retained parts, target HEAD
  counts, a healthy retry and full byte-capacity reuse. Staged recheck permits
  preparation but cannot publish and must release staging reservations.
- Two ordinary-missing batch controls preserve request order, missing-key success,
  duplicate-key accounting and full capacity reuse.
- Four HTTP controls exercise single and batch DELETE in both joined orders.
  Ambiguity returns 500/InternalError without deleting data; healthy retry keeps
  the existing 204/200 responses and releases the full one-object/eight-byte cap.

The native defect was reproduced before the guards were added. After correcting
the HTTP fixture's required Content-Length header, the final matrix was also run
twice against original objects.go, restored in a finally block:
[baseline failures](raw/before-tests.log). The final tests pass twice with the two
guards: [after](raw/after-tests.log). The
[old fixture's 400 response](raw/http-fixture-before-length-fix.log) is not treated
as evidence of the storage defect.

Five focused race repetitions pass **295 cases** without skips:
[focused race](raw/focused-race.log). Full Go tests, relevant runtime/storage/S3/
run-through/embedded/public race suites, all four conformance profiles, relevant
vet, unchanged quality/file-size ratchets, source formatting, generated-output
verification, WASM smoke and seven package integration tests pass.
[Full Go](raw/full-go.log), [broad race](raw/broad-race.log),
[conformance summary](raw/conformance-summary.log), [quality](raw/quality.log) and
[package tests](raw/package-tests.log) retain the results. Each conformance profile
keeps two existing skips: opt-in live upstream coverage and mirrored bucket
creation awaiting an authority decision. No new regression skipped and no ratchet
baseline is raised.

One post-fix replay attempt failed during Go setup because unicode/utf8 was
reported missing from the pinned standard library. The directory was present on
immediate inspection, go list succeeded and the replay then passed normally and
under -race without a toolchain/source change. The failed attempt is retained:
[setup failure](raw/original-after-toolchain-failure.log).

## Comparative measurements and costs

BenchmarkStoredObjectHeadClassification calls the real shared helper over a
lookup-only implementation of the existing storage interface. Fixed metadata and
preconstructed error chains are outside timing; classification and result checks
are inside. It isolates the classifier, without provider I/O, metadata cloning,
error construction, object bodies or ledger setup. It measures cumulative
allocations, not RSS or peak memory.

BenchmarkMemoryDeleteHeadLifecycle performs a real four-byte runtime PUT followed
by single deletion each iteration, under eight-byte/one-object caps. The next PUT
therefore exercises ordinary missing-key observation. The additional
BenchmarkMemoryBatchDeleteHeadLifecycle performs the same PUT then deletes
[target, missing, target], exercising both existing and missing HEADs, duplicate
accounting and the batch condition. Store/runtime/bucket setup, close, input bytes
and key-list construction are outside timing; runtime and real memory storage
work are inside. Neither is an HTTP, disk or throughput benchmark.

Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64. Serial
baseline/candidate/candidate/baseline blocks contain three samples each at one
processor, then eight: six samples per version/case/configuration. Classifier
samples have 5,000,000 iterations; real lifecycles 250,000. Loops remain serial at
GOMAXPROCS=8. No agent builds/tests run concurrently with measurements; other host
activity is uncontrolled. Every sample/outlier, exact command, binary digest and
allocation count is retained in [measurements](raw/measurements.json) and
[environment](raw/benchmark-environment.json).

The batch benchmark was added afterward to measure the second corrected site
explicitly. It uses the same two-guard implementation pair and matched final
benchmark sources within that comparison. Original classifier/single-loop
binaries and results were retained rather than relabeled or overwritten. Their
[original source](raw/classification-and-single-bench.go.txt) and the separate
[batch measurements](raw/batch-measurements.json) and
[batch environment](raw/batch-benchmark-environment.json) record the distinction.

| Case | Processors | Median ns/op before → after | Change | Before range | After range |
|---|---:|---:|---:|---:|---:|
| Present | 1 | 3.069 → 3.000 | -2.2% | 2.924–3.636 | 2.941–3.134 |
| Raw missing | 1 | 7.339 → 15.255 | +107.9% | 7.230–7.491 | 15.070–15.740 |
| Wrapped missing | 1 | 10.660 → 26.625 | +149.8% | 10.590–10.770 | 26.330–26.940 |
| Unknown lookup | 1 | 13.395 → 11.605 | -13.4% | 13.260–13.720 | 11.400–12.050 |
| Ambiguous, marker first | 1 | 23.350 → 31.875 | +36.5% | 23.010–24.380 | 31.590–33.280 |
| Ambiguous, missing first | 1 | 12.550 → 40.730 | +224.5% | 12.440–12.790 | 39.760–41.840 |
| Real PUT/single delete | 1 | 2,044.500 → 2,096.000 | +2.5% | 2,032.000–2,082.000 | 2,067.000–2,178.000 |
| Real PUT/batch delete | 1 | 2,949.000 → 3,067.500 | +4.0% | 2,816.000–6,064.000 | 2,770.000–3,594.000 |
| Present | 8 | 2.959 → 2.975 | +0.5% | 2.942–3.023 | 2.928–3.054 |
| Raw missing | 8 | 7.248 → 15.130 | +108.7% | 7.189–7.340 | 15.040–24.250 |
| Wrapped missing | 8 | 10.480 → 26.285 | +150.8% | 10.380–10.610 | 26.020–31.890 |
| Unknown lookup | 8 | 13.065 → 11.495 | -12.0% | 13.040–13.150 | 11.280–12.450 |
| Ambiguous, marker first | 8 | 23.000 → 31.770 | +38.1% | 22.760–23.160 | 31.440–33.450 |
| Ambiguous, missing first | 8 | 12.380 → 39.870 | +222.1% | 12.240–12.590 | 39.420–109.100 |
| Real PUT/single delete | 8 | 1,802.000 → 1,868.000 | +3.7% | 1,766.000–1,967.000 | 1,791.000–3,297.000 |
| Real PUT/batch delete | 8 | 3,312.500 → 2,606.000 | -21.3% | 2,378.000–4,550.000 | 2,364.000–3,959.000 |

The raw-missing classifier costs approximately **7.9 ns more (+108–109%)**;
wrapped missing adds **15.8–16.0 ns (+150–151%)**. The new committed-marker scan is
real work, not free. Marker-first ambiguity adds **8.5–8.8 ns (+36.5–38.1%)**;
missing-first ambiguity adds **27.5–28.2 ns (+222–225%)**, including traversal to
the later marker and the benchmark's returned-error check. No speedup is inferred
from the present/unknown timing reductions.

The real single cycle measures **2,044.5 → 2,096 ns (+2.5%, +51.5 ns)** at one
processor and **1,802 → 1,868 ns (+3.7%, +66 ns)** at eight. The candidate's
3,297 ns outlier is retained. The real batch cycle measures **2,949 → 3,067.5 ns
(+4.0%, +118.5 ns)** and **3,312.5 → 2,606 ns (−21.3%, −706.5 ns)** respectively.
Batch samples vary widely: the baseline reaches 6,064 ns at one processor and
both eight-processor ranges overlap substantially. These results do not establish
a batch speedup or exact lifecycle parity. All adverse and favorable samples are
retained; earlier reviews' separate timing concerns are not resolved by this run.

All classifier samples remain **0 B/op, zero allocations**. The single lifecycle
stays **1,200 B/op, 20 allocations**. The batch lifecycle stays **1,616 B/op,
27 allocations**. These cumulative totals are not a claim about peak memory. No payload buffer or allocation is added by
either condition. Larger error chains can increase classification time.

Compatibility/capacity behavior is deliberately conservative: ambiguous HEADs now
refuse mutation and preserve their original error rather than declaring absence.
Clients must restore a reliable observation before retrying; retained multipart
and staging state keep their existing cleanup rules. This prevents new phantom
accounting; it does not reconstruct accounting corrupted before the fix. Existing
RefreshUsage remains the authoritative repair route for prior stale state.
No additional provider call or lock hold for I/O is introduced. No speedup,
exact latency parity or resolution of earlier reports' performance concerns is
claimed, and no substantial optimization is proposed.

The refreshed packaged WASM grows **4,617,093 → 4,617,224
bytes (+131 bytes)**. [Artifact metadata](raw/wasm.json) records both digests.
Generated-output verification, WASM smoke and package integration tests validate
the refreshed artifact.

## Next confirmed finding — ReadForSave fabricates absence on ambiguous GET

A separate native probe injects the same ambiguous missing/committed chain during
GET over a real memory store holding an existing object. ReadForSave converts it
into success with ObservedAbsence=true, dropping the uncertainty. Both joined
orders and both wrapped variants reproduce twice normally and twice under -race:
[source](raw/next-save-read-probe.go.txt), [normal](raw/next-save-read.log),
[race](raw/next-save-read-race.log). There is no reported data race.

The probe also verifies the boundary: a subsequent SaveObject with the fabricated
absence condition is refused by the backend's guard, leaving the original bytes
and version intact. A healthy ReadForSave then yields the actual object and a
normal guarded save succeeds. This finding is incorrect read/condition semantics,
not a demonstrated guard bypass or overwrite. Its committed/missing read result
is deliberately injected; built-in memory GET does not naturally emit it.
ReadForSave remains unchanged in this correction.

Reproduce by temporarily copying the text to
internal/runtime/save_read_review_probe_test.go and running only
TestReviewReadForSaveAmbiguousAbsence with -count=2, optionally -race, then removing
it. The probe reuses the committed regression error-chain helper. Temporary next-
finding probes are removed before broad validation.
