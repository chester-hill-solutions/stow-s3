# Preserve published filesystem completion outcomes after cleanup failure

Baseline `83c1ebe` includes the independent workspace DELETE correction and owner
recovery. Fresh `origin/main` remains
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor.
[Baseline](2026-10-17-filesystem-completion/raw/baseline.json).
This corrects the final confirmed A3 site from the
[bounded audit](2026-10-16-storage-contract-audit.md).

## Reproduction

All tests and checks ran on Railway as UID 10001, with enforced Linux directory
permissions. No local validation or benchmarks ran. The existing native cleanup
recovery test is strengthened to require its already-published outcome. A new
runtime regression uses the real filesystem store and a healthy control.

With staging directory mode 0555, reading parts and publishing the object succeed,
then cleanup fails with a native permission error. The object contains `body`,
but completion returns nil metadata, lacks `ErrMutationCommitted`, and immediate
runtime usage is zero bytes/zero objects instead of four bytes/one object. Four
adverse leaf executions fail and two healthy controls pass in each before run,
repeated twice normally and under race, with no skips:
[normal](2026-10-17-filesystem-completion/raw/before/before-normal.log),
[race](2026-10-17-filesystem-completion/raw/before/before-race.log),
[counts](2026-10-17-filesystem-completion/raw/counts.json).

## Change and results

The production change is one return statement in
[filesystem multipart completion](../../internal/storage/fs/fs_multipart.go).
After successful object publication, cleanup failure returns the existing object
metadata together with `storage.CommittedError` wrapping the original cleanup
error. Earlier publication failures retain their existing behavior. Runtime's
existing result/accounting and cleanup-reconciliation paths consume this outcome;
no runtime production code changes. The storage contract was clarified first.

All **six focused leaf executions pass normally and under race**, repeated twice
without skips: [normal](2026-10-17-filesystem-completion/raw/after/focused-normal.log),
[race](2026-10-17-filesystem-completion/raw/after/focused-race.log).
Returned size, multipart ETag, version, content type, metadata and modification
time match the published record. `errors.Is`/`errors.As` preserve the permission
cause. Immediate usage includes the four-byte object; retained staging still
reserves four bytes and its upload-session slot. Repair and abort release staging
without deleting the object. The native test also verifies retry completion removes
staging after repair. Healthy completion removes staging immediately.

The [initial complete Railway suite](2026-10-17-filesystem-completion/raw/initial-after/checks.json)
passes focused normal/race, full Go, broad storage/runtime/embedded/run-through/S3/public
API race, all four conformance profiles, vet, site build, site tests and documentation
links. Its quality gate finds that adding the native test assertion exceeded the
existing complexity limit. The assertion moves unchanged into a small helper;
no production code or quality baseline changes. The final [five Railway checks](2026-10-17-filesystem-completion/raw/after/checks.json)
pass format, focused normal/race, affected-package vet and quality/file-size/format.
The two existing mirror/live-upstream skips remain in each conformance profile;
focused regressions have no skips. [Final result](2026-10-17-filesystem-completion/raw/after/run.json).
Deployment `7de49edd-449b-44f1-a030-3d78a45cef2c` is the same isolated qualified
runner as the preceding two corrections. Source updates follow completed before
runs, preserving warm tool caches. [Update receipt](2026-10-17-filesystem-completion/raw/completion-after-sync-receipt.json)
[final test-only update](2026-10-17-filesystem-completion/raw/completion-final-sync-receipt.json)
and [actual tested hashes](2026-10-17-filesystem-completion/raw/after/environment.json)
identify formatted source. Production hashes are identical between the full-suite
and final test-helper runs. An additional Railway link check covers this report
and the consolidated review status, with its receipt in the after evidence.

## Costs and compatibility

No dependency, storage format, public API, cache, index or abstraction is added.
The healthy path has no new I/O. The error path returns the already-built metadata
and adds the existing joined committed-error allocation. There is no performance
claim or benchmark for this error-path correction. Native callers can now receive
metadata together with the error and must retain the documented committed outcome;
HTTP still reports an internal error for cleanup failure. Staging remains reserved
until absence is confirmed. Permission injection is qualified on Linux; no Windows
fault-path or physical power-loss durability claim is made.

## Remaining work

The three confirmed correctness slices are complete: workspace owner recovery,
workspace DELETE, and filesystem completion cleanup. This closes the bounded A1–A3
queue, not every conceivable storage failure. No further correctness finding was
implemented or new audit started. The next work is comparative Railway performance
measurement: the known part-listing cost and the previously reported changed-version
durable-fallback regression. The latter's earlier local figures must be remeasured
on Railway before deciding an optimization. Existing whole-object buffering and
indexing/checkpoint designs are unchanged.
