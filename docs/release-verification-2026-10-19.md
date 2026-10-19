# v0.3.3 qualification — 2026-10-19 UTC

PR #78 was merged without PR #77's cleanup and snapshot fixes. The v0.3.2
publication workflow was cancelled; the public tag remains fixed at
`c97aa67c0697bec91f46cdcef82b814e5311fee8`. No GitHub release was published for
that tag. This candidate combines that main revision with PR #77 head
`e03efc95fe038b4f60958b85a5bf6138ea95e99f` and the
[workspace uncertainty correction](reviews/2026-10-19-workspace-uncertainty.md),
and prepares [v0.3.3](releases/v0.3.3.md). Historical October 15/18 receipts
qualify their named revisions only.

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

## Workspace uncertainty correction

C04 from the [bounded closure register](reviews/2026-10-18-correctness-closure.md)
is closed on this candidate. A workspace path that could not be read was
reported as an absent object, and a create-only write over that answer replaced
bytes that were still there. Only a confirmed absence is reported as absence
now; a path that could not be examined returns `storage.ErrObjectUncertain`
with its cause joined, and the read path, the path resolution and the listing
surface all carry the distinction.

The red reproduction runs the pre-fix revisions of the changed files through a
Go overlay, so the failure it reports is the product's answer and not a build
error. The green steps run the corrected code. Both the workspace store and the
HTTP boundary are covered, and the healthy controls — a readable object
refusing a create-only write, a genuinely absent key staying absent, and a
symlinked key staying absent — are retained.

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

The workspace uncertainty correction is qualified separately, because it changes
the source the receipts above describe. Railway deployment
`7200db6c-86a3-47a0-ba7b-6fa3925c383c` runs the same scratch service with the
corrected tree: the pre-fix revisions behind a Go overlay for the red
reproduction, the corrected code for the green run, the whole Go suite normally
and under race, the four conformance profiles, vet, the quality, size and format
gates, the site build and tests, and the documentation links. Its receipts and
exact qualified source hashes are preserved under
[workspace uncertainty evidence](assessment-evidence/2026-10-19-workspace-uncertainty/).
The receipts above qualify `98d35cb` and are not reissued for this revision.

That container does not deliver the parent-death signal, so the
`internal/parentwatch` package is run as a separate step that expects the
failure, beside a standard-library probe that shows the same fact directly. The
package depends on nothing outside the standard library, so it cannot be
affected by this change, and its test passes in the GitHub Actions lanes. The
limitation is recorded rather than assumed.

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
preserved. Three known correctness findings remain disclosed in the
[release notes](releases/v0.3.3.md); the workspace I/O mistaken for absence is
no longer among them. No physical-crash or executed-live-provider claim is made.

Native GitHub downloads remain the selected publication channel. npm/PyPI
publication remains deferred by the existing release policy. The required full
GitHub CI and four-platform packed-consumer rehearsal must pass on the combined
revision before publication; a rehearsal publishes nothing.
