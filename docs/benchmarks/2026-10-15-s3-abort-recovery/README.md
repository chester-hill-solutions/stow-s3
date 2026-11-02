# S3 validation reconciles confirmed missing uploads

Baseline `553103a` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. This follows the separate
[direct-abort correction](../2026-10-15-abort-retry/README.md). Earlier completed
corrections remain intact; this change addresses its HTTP prevalidation finding.

## Original defect and smallest correction

A truthful custom store removes an upload and its four-byte part, then reports an
unmarked error modeling loss of the abort acknowledgement. HTTP returns 500 and
the runtime conservatively retains reservations. A subsequent S3 request receives
404 / NoSuchUpload from route validation before direct abort recovery can run.
Byte/session capacity and, for an absent target, its object slot remain reserved.
The first conservative response is correct; later confirmed absence must be able
to reconcile accounting without manufacturing success.

The exact retained HTTP probe fails twice normally and twice under -race before
production changes: [source](raw/original-probe.go.txt),
[normal](raw/original-before.log), [race](raw/original-before-race.log).
It now passes twice in each mode: [normal](raw/original-after.log),
[race](raw/original-after-race.log). Both HTTP responses retain their existing
meaning, 500 then 404; actual upload/part data is gone and full capacity is reusable.

The production diff changes one runtime method and adds one private helper in the
same file: **24 added lines, one removed**. No S3 handler, adapter, storage backend,
interface, public API, dependency or format changes. Successful validation still
performs exactly one provider validation call and no recovery reads.

On failed validation, the helper first checks that the upload is cached and its
bucket/key match the authorized requested resource. Only an ordinary missing-upload
validation error permits a separate GetMultipartUpload observation. A canonical
ErrUploadNotFound from that observation, without a committed marker, confirms
absence. A target HEAD returning success or ordinary missing establishes whether
a shared object reservation is still needed. Existing removeUpload then reconciles
counters and reference counts.
The existing runtime lock covers the observations and cleanup. Context, resource
policy, authority and closed-state checks stay in their original order.

The validation error is returned unchanged: wrong routes remain 404, unknown or
committed validation errors keep their exact chains, and successful validation
remains successful. A wrong route, untracked ID, retained upload, unavailable or
ambiguous lookup, or unavailable/ambiguous target observation cannot release
reservations. Subsequent healthy
observations can recover without reopening. After ledger removal, later failures
perform no repeated recovery reads. Concurrent failures reconcile once. No backing
upload is mutated during this observational cleanup; read-only validation retains
its existing read authorization requirement.

Compatibility effect: all four multipart S3 routes can release stale reservations
when their prevalidation confirms upload absence. Existing object data, metadata,
Usage and HTTP status/body contracts are preserved. This does not make validation
and the following mutation atomic or add a durability guarantee. Providers must
report the missing sentinel only for confirmed absence, as existing recovery paths
already require.

## Regression and validation evidence

The final regression suite contains **51 cases**:

- Twenty-eight native cases combine absent/existing target with fourteen scenarios:
  canonical missing variants, wrapping, wrong bucket/key, retained upload, unavailable
  or committed/missing lookup, unavailable or committed/missing validation, failed or ambiguous
  HEAD, untracked ID and successful validation. They check exact returned errors,
  provider call counts, reservation state, unchanged existing bytes, healthy retry,
  no repeated recovery reads and full capacity reuse.
- Three cases check exact authority/policy/cancellation refusal types and no provider
  work. One read-only case permits observational cleanup with zero provider aborts.
  One policy case permits the requested wrong route but denies the cached target:
  no lookup/HEAD can cross into that target and no reservation is released.
- Shared-target recovery preserves the surviving two-byte part/object slot while
  admitting a replacement upload; final cleanup releases all reservations.
  Two concurrent validations perform one lookup/HEAD cleanup, with no double release.
- Sixteen public custom-store HTTP cases combine four routes (UploadPart,
  CompleteMultipartUpload, AbortMultipartUpload, ListParts), raw/wrapped absence,
  and absent/existing target. An initial truthful uncertain abort returns 500;
  subsequent validation returns 404 and restores reusable full capacity without
  another provider abort or changing the existing object.

The 49-case regression matrix was also run twice against original multipart.go,
then the candidate was restored in a finally block:
[original failures](raw/before-tests.log). Two additional cases then demonstrated
that using the existing size helper would accept a committed/missing HEAD chain:
[original HEAD ambiguity](raw/head-ambiguity-before.log). The helper now inspects
that raw HEAD result instead, retaining reservations on ambiguous replies. These
tests failed before that guard correction. The retained HTTP probe was reproduced
before any production edit. All 51 final cases pass twice normally and five times
under -race (**255 focused race cases**):
[normal](raw/after-tests.log), [race](raw/focused-race.log).

Full Go tests, relevant runtime/storage/S3/run-through/embedded/public race suites,
all four conformance profiles ([summary](raw/conformance-summary.log)), relevant
vet, unchanged quality/file-size ratchets, formatting/diff checks, documentation
links, generated-output verification, WASM smoke and seven package integration
tests pass. The separately failing next-finding probe is removed before broad
validation. Existing baselines are not raised.

## Comparative measurements and new costs

BenchmarkRuntimeMultipartValidation exercises the actual runtime over a real memory
store with one active upload. Successful cases have one or 64 metadata entries;
wrong-key and untracked-ID cases exercise invalid requests. Runtime/store setup,
initiation and metadata construction are outside timing. This includes runtime
checks/locking and provider validation, not HTTP/XML/network overhead.

BenchmarkMultipartValidationRecoveryAccounting uses normalized explicit eight-byte,
one-object/one-upload limits and a real memory store. Each iteration rebuilds the
ledger, records an upload/four-byte part and an uncertain abort, then validates the
already-absent upload. The real target is absent or holds the three-byte object
created outside timing. Ledger construction is included, admission is not executed
inside timing, and no payload is staged by this accounting model. Its conservative
ledger corresponds to the real post-fault test state. The baseline's stale ledger
is discarded after each iteration; it cannot complete a healthy repeated recovery
loop at the configured cap. This is not end-to-end S3 latency or peak memory.

Identical benchmark sources are compiled against baseline and candidate. Go
1.25.6, Apple M1 Pro, macOS 14.5 ARM64. Serial baseline/candidate/candidate/baseline
blocks contain three samples each, first at one processor then eight: six samples
per version/case/configuration. Validation samples have 5,000,000 fixed iterations;
recovery samples 250,000. No agent builds/tests run concurrently with measurements;
other host activity is uncontrolled. These loops are serial even with GOMAXPROCS=8.
All commands, samples/outliers, binary digests and host details are retained in
raw benchmark logs, [measurements](raw/measurements.json) and
[environment](raw/benchmark-environment.json).

| Final implementation | Processors | Median ns/op before → after | Change | Before range | After range |
|---|---:|---:|---:|---:|---:|
| Success, one metadata entry | 1 | 70.500 → 71.000 | +0.7% | 69.27–71.98 | 69.38–80.26 |
| Success, 64 entries | 1 | 69.645 → 69.740 | +0.1% | 69.27–71.14 | 69.50–90.38 |
| Wrong key | 1 | 69.050 → 101.995 | +47.7% | 68.34–69.87 | 92.97–120.90 |
| Untracked ID | 1 | 74.990 → 87.405 | +16.6% | 74.58–75.17 | 86.11–95.65 |
| Recovery accounting, absent target | 1 | 671.850 → 1,085.500 | +61.6% | 641.80–747.90 | 1,055.00–1,146.00 |
| Recovery accounting, existing target | 1 | 585.900 → 1,039.000 | +77.3% | 573.60–608.70 | 998.00–1,199.00 |
| Success, one metadata entry | 8 | 69.605 → 70.630 | +1.5% | 69.16–70.30 | 69.17–75.69 |
| Success, 64 entries | 8 | 69.740 → 69.885 | +0.2% | 69.12–70.99 | 68.72–74.18 |
| Wrong key | 8 | 69.720 → 92.080 | +32.1% | 68.68–71.17 | 90.74–97.29 |
| Untracked ID | 8 | 74.390 → 85.225 | +14.6% | 73.61–75.27 | 84.51–86.01 |
| Recovery accounting, absent target | 8 | 552.750 → 906.500 | +64.0% | 530.60–597.50 | 872.60–927.80 |
| Recovery accounting, existing target | 8 | 492.650 → 846.550 | +71.8% | 478.00–524.20 | 832.80–1,018.00 |

Every successful/wrong-key/untracked-ID sample remains **0 B/op, 0 allocations**.
Final successful medians increase 0.1–1.5% (approximately 0.1–1.0 ns), with
overlapping distributions: no speedup or exact latency parity is claimed. Earlier
phases' larger positive/negative timing differences are retained below. The ordinary
path adds a nil-error branch;
no additional storage read or metadata clone is introduced there. Invalid routes
pay for cached identity checks: about 22–33 ns for wrong key and 11–12 ns for an
untracked ID in this comparison, without recovery I/O or allocation.

Recovery accounting changes **1,120 B/10 allocations → 1,184 B/12 allocations**
for an absent target (**+64 B, two allocations**), and **912 B/nine allocations →
1,136 B/12 allocations** for an existing target (**+224 B, three allocations**).
It now performs one upload lookup, target HEAD and the existing cleanup that the
baseline skipped, adding about 354–453 ns to these measured ledger lifecycles.
HEAD uses existing validation/metadata behavior; target metadata size, key lengths
and provider implementations can increase costs. These are cumulative allocation
totals, not RSS/peak heap. No object-body read or payload-sized buffer is added.

New cold-path cost/risk: a matching failed validation can perform up to two extra
provider reads while holding the existing runtime lock. Slow providers can delay
other operations during these observations. Calls receive the original context;
failed observations retain reservations and keep the original validation response.
No extra provider read is made for success, wrong resource or unknown upload ID.

The final packaged WASM grows **4,617,032 → 4,617,093 bytes (+61 bytes)**,
with a new digest. Its baseline and intermediate comparisons are retained in
[artifact metadata](raw/wasm.json). Generated-output, WASM smoke and package tests
verify the refreshed artifact.

## Measured guard-order revision and stricter HEAD guard

The first candidate classified missing errors before checking the cached route.
Measurements showed unnecessary error-chain work for invalid requests. The final
helper first checks cached identity and only classifies a matching request. This
is a small local guard reordering based on measured cost, with the same recovery
conditions, no new structure and the same passing regressions. All initial samples,
commands and binary hashes remain in `initial-*` raw files and
[initial measurements](raw/initial-measurements.json).

Initial before → candidate medians (ns/op) were: one-processor success/one entry
81.020 → 74.020 (−8.6%), success/64 76.620 → 74.320 (−3.0%), wrong-key
72.855 → 124.050 (+70.3%), untracked-ID 78.100 → 112.600 (+44.2%), absent-target
accounting 741.00 → 1,094.50 (+47.7%), existing-target accounting 872.75 → 1,259.50
(+44.3%). Eight-processor values were 71.075 → 73.250 (+3.1%), 77.380 → 83.485
(+7.9%), 73.440 → 118.050 (+60.7%), 78.755 → 158.850 (+101.7%),
589.35 → 1,039.50 (+76.4%), and 608.20 → 902.55 (+48.4%), respectively.
Allocation counts/bytes were the same as the final comparison. An initial absent-
target candidate sample reached **4,912 ns/op**; it remains in the raw data.

The cached-route-first version was measured before the stricter HEAD guard was
added. Its full samples, commands and binary digests are retained in `route-order-*`
files and [measurements](raw/route-order-measurements.json). Successful medians
ranged −4.2% to +8.2% against their paired baselines; wrong-key overhead was
35.0–37.6% and untracked-ID overhead 17.1–17.4%. Recovery accounting overhead
was 61.8–80.6%, with the same allocation deltas. Those candidate measurements
are preserved rather than relabeled as final results.

The separate runs are not a controlled direct comparison between candidates;
background/host variation remains. The code inspection establishes that invalid
cached routes now skip error-chain traversal. Final invalid-request overhead is
still reported above. Earlier reviews' separate performance concerns are not
considered resolved. No buffering, indexing or checkpoint optimization is proposed.

## Next confirmed finding — ambiguous completion lookup discards retained state

A separate native probe uses real workspace multipart publication and its existing
permission-based cleanup fault, leaving the real four-byte part/upload intact.
The post-completion lookup wrapper reports uncertainty joined with both
ErrMutationCommitted and canonical ErrUploadNotFound, in either order. Unlike the
new validation guard and existing public lookup adapter, reconcileCompletionError
treats any matching missing cause as confirmed absence. It drops the four-byte
reservation/session and omits the lookup uncertainty from the returned error.

After repairing permissions and restoring normal lookup, abort with a policy that
permits the actual target fails ErrResourceUnresolved: the cached upload/resource
was prematurely discarded before policy authorization can resolve it. An explicit
RefreshUsage restores the authoritative upload, then abort succeeds and preserves
the published object. The probe verifies that workaround; this is not evidence of
permanent data loss or a demonstrated quota bypass on the workspace profile.

Both joined-error orders fail twice normally and twice under -race, without
reported data races: [source](raw/next-completion-lookup-probe.go.txt),
[normal](raw/next-completion-lookup-probe.log),
[race](raw/next-completion-lookup-probe-race.log). The host enforced the cleanup
permission fault; these runs did not skip. The lookup error is deliberately injected
at the native store seam. No change to completion recovery is included here.

Reproduce by temporarily copying the text to
`internal/runtime/completion_lookup_review_probe_test.go`, running only
TestReviewCompletionAmbiguousMissingLookup with -count=2, optionally -race, then
removing it. The existing fixture skips hosts without enforced directory permissions.
