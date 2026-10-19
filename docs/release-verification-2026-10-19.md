# v0.3.3 reconciliation qualification — 2026-10-19 UTC

PR #78 was merged without PR #77's cleanup and snapshot fixes. The v0.3.2
publication workflow was cancelled; the public tag remains fixed at
`c97aa67c0697bec91f46cdcef82b814e5311fee8`. No GitHub release was published for
that tag. This candidate combines that main revision with PR #77 head
`e03efc95fe038b4f60958b85a5bf6138ea95e99f` and prepares
[v0.3.3](releases/v0.3.3.md). Historical October 15/18 receipts qualify their
named revisions only.

## Reconciliation

The newer main implementations of filesystem multipart admission, record
publication, capacity uncertainty, API reference printing and native capacity
fixtures are retained. PR #77 adds retryable stream cleanup through Go/WASM,
bounded concurrent worker cleanup, multipart content fingerprints, consistent
live/snapshot reconciliation and early preparation/checkpoint admission. Three
identical multipart tests already moved into main's dedicated test file are
removed from the incoming file. No production storage implementation is replaced
by PR #77's older namespace fix.

The new `TestReadFailureRetainsRetryableCleanup` integration regression checks
short and excess reads, exact returned bytes, stream closure and retryable failed
cleanup. Against main's original stream implementation, both cases fail twice
normally and twice under race because explicit Close does not retry the failed
underlying close. The combined implementation passes. The initial validation also
caught duplicate test declarations and excessive branching in the new test;
both failures are preserved. The test assertions were separated into small
helpers; the quality baseline is unchanged.

## Qualification

All project execution occurs on the authorized Railway scratch runner or required
GitHub Actions runners. No local build, formatting, test or benchmark is run.
Railway deployment `8213372f-9a88-4f2b-b82c-abb6802651f4` uses Linux amd64,
Go 1.25.6, Node 25.9.0, four CPUs/4 GiB and an unprivileged test user. The runner
transport replaces only its three explicit harness files; those replacements are
excluded from release source equivalence.

Validation receipts and exact qualified source hashes are preserved under
[reconciliation evidence](assessment-evidence/2026-10-19-reconciliation/).
The initial run passes conformance, native/WASM builds, all 163 Node tests,
35 real Chromium tests (two configured skips), reference regeneration and
reproducible generated-output checks. The corrected run qualifies normal/race
runtime, workspace, filesystem and public workspace facade packages, reference and
generated checks, Go quality/size/format/vet and the v0.3.3 release plan.

Both validation runs' source bytes, regenerated WASM and generated API pages are
hash-verified on retrieval. All 1,188 Go/module/package inputs match the final
Railway receipt. Of 14,555 uploaded inputs, 14,550 match exactly; exclusions are
three scratch harness replacements and two subsequent documentation edits. New
qualification reports/evidence are checked by hosted CI. The Go API reference is
unchanged from #78's corrected output; only the Python version reference changes.
The packaged WASM grows by 1,797 bytes (0.039%), to 4,621,242 bytes. This size
comparison is not a timing or memory benchmark. The
[scratch runner is verified removed](assessment-evidence/2026-10-19-reconciliation/runner-stop-status.json);
the target service remains unchanged and stopped.

## Costs, contracts and limitations

Snapshot reconciliation now computes MD5 alongside its existing SHA-256 in a
single streaming pass, without whole-object buffering. Multipart manifests retain
one content fingerprint. Worker admission scans pending calls and creates a
temporary array proportional to the configured in-flight limit; cleanup is bounded
to 32 pending controls and one dispatched control. Failed cleanup errors now reach
callers and retained ownership allows retry. Legacy multipart entries lacking a
fingerprint are re-derived once and may change ETag/version or reset unverifiable
metadata. Edited snapshots match live metadata and remain read-only.

No dependency or public API is added by reconciliation. No new performance
measurement is claimed for the combined release. Earlier individual Railway
measurements retain their original scope; historical benchmark attempts and
failures imported from #77 remain unchanged. `git diff --check` reports carriage
returns in those original session logs; the evidence bytes are intentionally
preserved. All four known correctness findings remain disclosed in the
[release notes](releases/v0.3.3.md), with workspace I/O mistaken for absence the
highest-priority follow-up. No physical-crash or executed-live-provider claim is made.

Native GitHub downloads remain the selected publication channel. npm/PyPI
publication remains deferred by the existing release policy. The required full
GitHub CI and four-platform packed-consumer rehearsal must pass on the combined
revision before publication; a rehearsal publishes nothing.
