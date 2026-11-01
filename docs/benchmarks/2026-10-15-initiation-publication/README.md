# Committed multipart initiation reserves its session and object capacity

Baseline `452f590` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. Earlier completed corrections
are retained. This addresses the committed-initiation finding from the
[part-upload review](../2026-10-15-part-publication/README.md). Abort failure
accounting remains a separate finding.

## Demonstrated defect

A supported public custom Store really creates an upload, then returns its known
descriptor with `ErrMutationCommitted` and a subsequent failure. The adapter drops
the descriptor on every error; runtime `finishUpload` also rejects every error
before registering the session and missing-object reservation.

The original probe creates **two real uploads under `MaxMultipartUploads: 1`**,
with two provider calls. The first result is nil even though its upload exists.
It reproduces twice normally and twice under the race detector before production
changes: [normal](raw/original-before.log), [race](raw/original-before-race.log).
The same probe now passes twice in each mode: [normal](raw/original-after.log),
[race](raw/original-after-race.log).

The finalized native/public regressions also ran twice before implementation:
[original failures](raw/before-tests.log). Four native committed cases, the native
shared-target case, eight public committed cases and the S3 case fail per run;
success and ordinary-refusal cases pass. They independently demonstrate lost
identity, missing session/object reservations, admission past the session cap,
consumption of a missing target's object slot by another object, and inability to
abort the unregistered upload through the runtime.

## Minimal correction and contracts

Four added lines and five removed lines across three existing functions correct
the full boundary. The custom adapter preserves the descriptor on committed
errors. Runtime uses the existing `recordMutation` decision when registering the
upload, and returns its cloned descriptor with the original error. Session
registration, target deduplication, object reservations and metadata cloning all
reuse existing code. No production helper, dependency, interface, API, format or
storage abstraction is added.

The existing runtime lock protects admission and registration. Ordinary failures
still return nil and reserve nothing, even if the provider supplies an uncommitted
descriptor. A missing-object cause joined with the committed marker remains a
committed outcome, with its full error chain. A returned descriptor is not a
success indication: callers still receive the failure. This adds no rollback or
durability guarantee and depends on providers reporting truthful committed state.

Intentional compatibility effect: writes or further initiations that previously
bypassed quota after a committed error can now be refused. An upload for an
existing object consumes a session but no additional object slot; uploads sharing
one missing target share its object reservation. Object byte/count Usage continues
to describe completed objects. Recovery through completion or abort uses the
existing interfaces. There is no new automatic rollback or backend scan.

## Regression and validation evidence

Eight native matrix cases cover absent/existing targets and success, committed
error, committed error containing a missing-object cause, and ordinary refusal.
A ninth native case proves that two committed uploads share one target reservation,
consume two session slots, refuse a third before another provider call, and release
the object reservation only after the final abort.

Sixteen public cases cover the same matrix with both completion and abort recovery.
The fixture uses existing real public-store test operations and distinct upload
IDs. Tests verify exact upload identity, initiation time, content type, metadata,
error preservation, independent metadata ownership, no object mutation at
initiation, session/object limits, recovery, and reclaimed capacity. Native tests
also preserve the internal checksum algorithm; unsupported public checksum fields
remain unset. The ordinary-refusal fixtures deliberately return an uncommitted
`not-created` descriptor to ensure its presence does not imply publication.

A twenty-sixth case enters through the S3 handler: post-initiation failure still
returns 500 / `InternalError`; the upload is listed by the actual provider, counts
toward quota and can be aborted through the runtime. The intentionally failing
next-finding probe is removed before the broad suites.

All twenty-six cases pass twice normally and five times under `-race`
(**130 focused race cases**). Full Go tests, relevant runtime/storage/S3/run-through/
embedded/public-API race suites, all four conformance profiles, relevant vet,
existing quality/file-size ratchets, formatting/diff checks, generated-output
verification, WASM smoke and seven package integration tests pass. Logs are in
`raw`; the [conformance summary](raw/conformance-summary.log) records each profile.

## Comparative costs and measurement limits

`BenchmarkCustomInitiationLifecycle` repeatedly initiates and removes one real
upload with one metadata entry through the public custom-store seam. The success
case performs runtime initiation and runtime abort in both versions. Setup and
close are outside timing. Each iteration starts and ends with no stored uploads.

The committed-recovery comparison has an explicit limitation: **the old runtime
cannot abort the upload it failed to register**. After that expected
`ErrUploadNotFound`, only the benchmark cleans it up directly through the provider.
The corrected runtime aborts normally. Both physically create and remove one real
upload per iteration, but the corrected version now performs registration,
metadata/result cloning and the existing abort reconciliation. Its recovery abort
also reaches the existing object HEAD that the old failed runtime abort skipped.
This is a comparison against broken accounting/recovery, not proof of equivalent
runtime work or an isolated initiation latency measurement. Permanent regressions
do not use that benchmark fallback.

Identical benchmark/fixture source is compiled before and after. Go 1.25.6,
Apple M1 Pro, macOS 14.5 ARM64. Serial baseline/candidate/candidate/baseline blocks
contain three fixed 50,000-iteration samples, first at one processor and then eight:
six samples per version/case/processor count. No agent tests or builds run
concurrently with measurements; other host activity is uncontrolled. Every sample,
command and binary hash is retained in [measurements](raw/measurements.json) and
[environment metadata](raw/benchmark-environment.json).

| Lifecycle | Median before → after | Timing change | Allocations before → after |
|---|---:|---:|---:|
| One processor, success | 1.920 → 1.979 µs | +3.1% | 15 → 15 |
| One processor, committed recovery | 0.813 → 1.966 µs | **+141.8% (2.42×)** | 3 → 15 |
| Eight processors, success | 1.667 → 2.010 µs | **+20.6%** | 15 → 15 |
| Eight processors, committed recovery | 0.648 → 1.976 µs | **+205.2% (3.05×)** | 3 → 15 |

One-processor success ranges are 1.823–2.135 µs before and 1.863–2.077 µs after;
committed-recovery ranges are 0.775–0.876 µs before and 1.874–3.977 µs after.
Eight-processor success ranges are 1.536–2.038 µs before and 1.558–2.678 µs after;
committed-recovery ranges are 0.639–0.701 µs before and 1.626–2.465 µs after.
The successful lifecycle's allocation count and storage calls are unchanged;
its timing medians are not presented as parity or a speedup. The earlier part-upload
review's distinct unresolved timing concern is not considered resolved by this
initiation benchmark.

After validation finishes, a reversed-order candidate/baseline/baseline/candidate
repeat at eight processors yields success **1.539 → 1.566 µs (+1.8%)** and
committed recovery **0.624 → 1.540 µs (+146.7%, 2.47×)**. Success ranges are
1.485–2.785 µs before and 1.476–1.756 µs after; recovery ranges are
0.602–0.644 µs before and 1.524–1.573 µs after. Success stays at 1,848 B/op and
15 allocations; recovery stays at 359–360 → 1,848 B/op and 3 → 15 allocations.
The initial +20.6% success median and the +1.8% repeat are both retained. These
observations establish no speedup or exact latency-parity claim. The consistently
larger committed-recovery cost is explicit, rather than hidden by pooling it
with successful operations.

At one processor, success remains **1,847 B/op and 15 allocations**. Committed
recovery changes **359 → 1,847 B/op and 3 → 15 allocations**: **1,488 more allocated
bytes and twelve allocations per measured lifecycle**. At eight processors,
success is 1,848 B/op in both versions; committed recovery is 359–360 before and
1,848 after, with the same allocation counts. These are cumulative allocation
totals, not peak heap or RSS. Metadata size and provider behavior affect the cost.
The corrected error path now pays for the registration, independent metadata
copies and ordinary runtime recovery that the old path skipped. That work is
necessary to honor the existing quota and recovery contracts.

Initiation itself adds no backend operation, HEAD, scan, lock or payload buffer.
The benchmark's now-reachable recovery HEAD belongs to the existing abort path.
Part buffering, storage formats and configured limits remain unchanged. No
indexing, checkpointing or substantial optimization is proposed.

The rebuilt packaged WASM remains **4,617,032 bytes**, with a changed digest:
[artifact comparison](raw/wasm.json).

## Next confirmed finding — committed abort strands reservations

A truthful custom store removes its upload and its four-byte part, then returns
`ErrMutationCommitted` with a later abort failure. Runtime `finishAbort` records
the mutation but rejects all errors before releasing the byte, missing-object and
session reservations. A new four-byte PUT and a new initiation are then refused
at four-byte / one-object / one-session limits, even though the provider has no
uploads or retained part bytes. Reported object Usage remains zero bytes/objects.

The probe uses ordinary successful initiation and part upload, so it isolates
abort behavior and does not depend on the initiation defect corrected here.
It reproduces twice normally and twice under `-race`, without a reported data
race: [source](raw/next-abort-probe.go.txt), [normal](raw/next-abort-probe.log),
[race](raw/next-abort-probe-race.log). Copy the text temporarily to
`pkg/stow/abort_review_probe_test.go`, run only
`TestReviewCommittedAbortReleasesQuota` with `-count=2`, optionally `-race`, then
remove it. It reuses this change's public fixture. Abort accounting is deliberately
unchanged in this independently reviewable initiation correction.
