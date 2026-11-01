# Committed part uploads retain their identity and byte reservation

Baseline `b7eff79` on `codex/core-copy-quality`; freshly fetched `origin/main`
remains `219d1dcfff9807b04660a40313b32127e5be2933`. Earlier completed corrections
are retained. This addresses the committed-part finding in the
[custom multipart completion review](../2026-10-15-custom-multipart-publication/README.md).
No other multipart mutation is corrected in this change.

## Original defect and smallest correction

A supported public custom Store really retains the four-byte part `part`, then
returns its known Part and `ErrMutationCommitted` with a later failure. Its adapter
discarded that Part on every error. Runtime also rejected all errors when updating
part reservations. The runtime returned nil Part while retaining the committed
marker, with no reservation for the actual four stored bytes.

A subsequent four-byte PUT to the same target succeeded with `MaxBytes: 4`:
**eight actual bytes remained stored under a four-byte cap**. Object usage reports
four bytes / one object; part bytes belong in the separate reservation accounting.
The original probe fails twice normally and twice under the race detector before
production changes: [normal](raw/original-before.log),
[race](raw/original-before-race.log). It now passes twice in each mode:
[normal](raw/original-after.log), [race](raw/original-after-race.log).

The finalized regression harness also ran twice before changing production:
[original failures](raw/before-tests.log). Six native and six custom committed
cases plus the HTTP quota check fail per repetition; success and ordinary-refusal
cases pass. Native tests independently expose lost identity, stale reservations
of zero/two/six bytes after publishing a four-byte replacement, incorrect admission
of another part or object, and incorrect refusal of an exact-capacity write after
a shrinking replacement.

The production diff is four added lines and five removed lines across three
existing functions. The custom adapter retains known Part metadata on a committed
error. Runtime `finishPart` uses the existing `recordMutation` decision to charge
the real replacement size on success or committed error. Runtime returns the Part
clone with the original error, so accounting cannot accidentally turn a failed
operation into a reported success. Ordinary errors still return nil Part and keep
the old reservation. No helper, interface, dependency, API or format is added to
production.

The existing runtime lock, admission checks, replacement-size delta and cleanup
paths are unchanged. Error chains, including a missing-object cause joined with
the committed marker, survive. The S3 regression still returns 500 / `InternalError`
for the post-storage failure, while preventing the subsequent quota bypass.
The Part result identifies the committed outcome; it adds no rollback or durability
guarantee. Providers remain responsible for truthful committed results. Callers
that continued writing after committed errors can now receive quota refusals
where they previously exceeded the configured cap; that stricter admission is
the intended compatibility change. A returned Part still accompanies an error.

## Regression and conformance evidence

Twelve native cases and twelve public custom-store cases exercise first parts,
growth from two to four bytes and shrinkage from six to four bytes. Each runs with
success, committed failure, committed failure containing a missing-object cause,
and ordinary refusal. A twenty-fifth case enters through the real S3 handler.

The native fixture wraps the actual memory backend. Tests check exact Part fields,
including LastModified, independent ownership of the returned Part, actual listed
part sizes, reservation totals, unchanged object usage, rejection before another
provider upload call, refusal above the combined byte cap, acceptance at the cap,
and release of bytes and the session slot on abort. Public tests reuse the existing
single-part fixture, implement actual replacement, recover from ordinary refusal,
complete the stored bytes with the original content type/metadata/ETag convention,
and prove completion releases the part reservation. The public Part type has no
LastModified field; the adapter continues leaving that internal field unset.

All twenty-five cases pass twice normally and five times under the race detector
(**125 focused race cases**). Full Go tests, relevant runtime/storage/S3/run-through/
embedded/public-API race suites, and all four conformance profiles pass. Relevant
vet, existing quality/file-size ratchets, formatting/diff checks, generated-output
verification, WASM smoke and seven package integration tests pass. Actual logs are
retained in `raw`; the [conformance summary](raw/conformance-summary.log) records
each backend profile. Intentionally failing probes were removed before the broad
suites.

## Comparative measurements and new costs

`BenchmarkCustomPartPublication` replaces one real 4 KiB part repeatedly through
the public custom-store adapter and native runtime. Success and committed-error
cases use the same actual storage operation; the committed error is fixed and
returned after storing the part. Both binary versions accept the original error
and intentionally ignore the returned Part, allowing the defective version to be
measured without changing the stored workload. Setup, body creation and cleanup
are outside timing. This is a small-part comparison, not a qualification of all
part sizes or backends.

Identical benchmark/fixture source was compiled before and after the production
change. Go 1.25.6, Apple M1 Pro, macOS 14.5 ARM64. Serial baseline/candidate/
candidate/baseline blocks each contain three fixed 50,000-operation samples, first
with one Go processor and then eight: six samples per version/case/processor count.
No agent tests or builds ran concurrently. Other host activity is uncontrolled.
All samples, binary hashes and commands are retained in
[measurements](raw/measurements.json) and
[environment metadata](raw/benchmark-environment.json).

| Case | Median before → after | Timing change | Allocations before → after |
|---|---:|---:|---:|
| One processor, success | 15.456 → 15.297 µs | −1.0% | 26 → 26 |
| One processor, committed error | 15.570 → 17.417 µs | **+11.9%** | 24 → 26 |
| Eight processors, success | 24.644 → 33.534 µs | **+36.1%** | 26 → 26 |
| Eight processors, committed error | 35.540 → 35.119 µs | −1.2% | 24 → 26 |

One-processor success ranges are 13.385–16.990 µs before and 14.101–16.310 µs
after; committed-error ranges are 13.515–16.497 µs before and 14.788–39.812 µs
after. Eight-processor success ranges are 13.678–44.481 µs before and
29.573–37.772 µs after; committed-error ranges are 32.450–39.080 µs before and
29.966–37.071 µs after. The timing distributions overlap; the adverse medians are
reported rather than replaced with a parity or speedup claim.

The reverse-order eight-processor repeat, after all suites finished, also has
adverse medians: success **15.779 → 19.120 µs (+21.2%)**, committed error
**13.598 → 16.703 µs (+22.8%)**. Success ranges are 12.849–18.556 µs before and
13.706–27.896 µs after; error ranges are 12.878–14.619 µs before and
12.970–21.452 µs after. Allocations remain 26 for success and 24 → 26 for errors.
Success bytes range from 39,372–39,374 before and 39,371–39,373 after; error bytes
range from 39,244–39,246 before and 39,371–39,372 after. These repeated adverse
eight-processor medians are a **potential performance regression**, despite the
overlap. The one-processor success result does not establish eight-processor parity.

A separate diagnostic pair profiles 200,000 successful replacements per version,
at eight processors: **16.152 → 15.800 µs (−2.2%)**, 26 allocations in both. This
instrumented single pair is not pooled with the unprofiled samples and does not
prove the absence of a regression. Both top profiles are dominated by runtime/OS
primitives, with no identified new storage operation. Disassembly confirms
`recordMutation` is inlined into `finishPart` in both versions, the same 208-byte
stack frame and the same map-operation call targets. No cause for the success
latency difference was established. The correction is not presented as latency
neutral; no speculative optimization is introduced to explain away the data.
[Diagnostic metadata](raw/performance-diagnostics.json), raw benchmark/profile-top
logs and both `finishPart` disassemblies are retained. Profile timing differs from
the earlier runs, so the diagnostic pair does not supersede the adverse medians.

Success allocations are unchanged: exactly **39,353 B/op and 26 allocations** at
one processor. Committed errors now allocate **39,225 → 39,353 B/op and 24 → 26
allocations**: **128 additional bytes and two allocations** for the existing
adapter conversion and runtime Part clone. At eight processors the initial
success allocation ranges are 39,370–39,373 before and 39,369–39,371 after;
committed-error ranges are 39,241–39,243 before and 39,370–39,371 after. The small
byte variation does not alter the two-allocation cost. Totals are cumulative
allocation per operation, not peak heap or RSS.

Committed errors now also perform the existing map lookup/update and reservation
delta instead of exiting early. They remain errors and consume quota according
to the actual retained part. Those costs correct the violated contract. Success
adds no storage call, rescan, lock, lookup of backend state or payload copy. Existing
part buffering and limits remain unchanged. No optimization, filesystem indexing,
checkpointing or new storage abstraction is proposed.

The rebuilt packaged WASM remains **4,617,032 bytes**, with a changed digest:
[artifact comparison](raw/wasm.json).

## Next confirmed finding — committed initiation bypasses the session cap

The custom multipart initiation adapter discards an upload returned with a
committed error; runtime `finishUpload` also rejects every error before registering
its reservation. A truthful fixture really creates a distinct upload on each call,
then returns that upload and a committed marker. Runtime loses the first upload's
identity and admits another creation despite `MaxMultipartUploads: 1`. The store
then contains **two real uploads at a one-upload cap**, with two provider calls.
No external writer, concurrency or payload failure is needed.

It reproduces twice normally and twice under `-race`, without a reported data
race: [source](raw/next-initiation-probe.go.txt),
[normal](raw/next-initiation-probe.log), [race](raw/next-initiation-probe-race.log).
Copy the text temporarily to `pkg/stow/initiation_review_probe_test.go` and run
only `TestReviewCommittedInitiationQuota` with `-count=2`, optionally `-race`, then
remove it. It reuses this change's public fixture, replacing the old fixture's
fixed upload ID with distinct IDs. Initiation accounting remains unchanged in
this independently reviewable part-upload correction.
