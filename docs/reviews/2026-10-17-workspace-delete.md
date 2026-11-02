# Preserve committed workspace DELETE outcomes

Baseline `7eeced4` contains workspace multipart owner recovery. Fresh `origin/main`
is still `219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor.
[Baseline](2026-10-17-workspace-delete/raw/baseline.json).
This corrects the workspace DELETE site of A3 from the
[bounded audit](2026-10-16-storage-contract-audit.md).

## Reproduction

All execution ran on the existing Railway scratch runner as UID 10001. No local
validation or benchmark ran. Permanent regressions ran twice normally and under
race before implementation: **eight adverse leaf failures and ten passing controls
in each run**, without skips. [Normal](2026-10-17-workspace-delete/raw/before/before-normal.log),
[race](2026-10-17-workspace-delete/raw/before/before-race.log),
[counts](2026-10-17-workspace-delete/raw/counts.json).
The final before run uses the native rename error type, `os.LinkError`; an initial
test draft incorrectly expected `os.PathError`, which was corrected before this run.

The fixture replaces `.stow/index.json` with a directory after opening the real
workspace/runtime. Single deletion removes the 4-byte victim, but the error lacks
`ErrMutationCommitted`. Batch deletion stops after that removal and returns no
confirmed keys. Runtime immediately retains seven bytes/two objects instead of
three bytes/one object. The later survivor retains its exact bytes. Healthy single
and batch operations and canceled refusals pass. Both native and runtime-bound
entry points exercise the same real index failure.

## Change and results

Only [workspace objects](../../internal/storage/workspace/objects.go) changes in
production. After a successful unlink and in-memory index update, index save
failure uses the existing `storage.CommittedError` helper. Batch deletion includes
the committed key before returning the first error. The underlying rename error
and prior progress survive; later keys remain untouched. Healthy and missing-key
behavior is unchanged. Existing runtime accounting consumes this outcome without
new logic. The storage and workspace contracts were clarified before implementation.

All **18 focused leaf executions pass normally and under race**, repeated twice
with no skips: [normal](2026-10-17-workspace-delete/raw/after/focused-normal.log),
[race](2026-10-17-workspace-delete/raw/after/focused-race.log).
Immediate usage now reports three bytes/one object on the failure, and healthy
batch usage is zero. Canceled deletion still leaves the victim unchanged and
has no committed marker.

All [14 Railway checks](2026-10-17-workspace-delete/raw/after/checks.json) pass:
focused normal/race, full Go, broad storage/runtime/embedded/run-through/S3/public
API race, four conformance profiles, vet, quality/file-size/format, site build,
site tests and documentation links. The two existing mirror/live-upstream
conformance skips remain. [Result](2026-10-17-workspace-delete/raw/after/run.json).
Deployment `7de49edd-449b-44f1-a030-3d78a45cef2c` is the same qualified isolated
runner as the owner change; source updates occur only after previous runs finish.
[Update receipt](2026-10-17-workspace-delete/raw/delete-after-sync-receipt.json)
and [tested hashes](2026-10-17-workspace-delete/raw/after/environment.json)
identify the actual formatted sources. The new report gets an additional Railway
document-link check, recorded in the after evidence, before its commit.

## Costs and limits

No dependency, format, public API, index or abstraction is added. The success path
uses the same I/O; `CommittedError(nil)` returns nil. Index failure adds the existing
joined-error allocation and returns a confirmed batch key. No timing/allocation
claim is made, and no benchmark is warranted for this error-path correction.
Callers must preserve confirmed keys alongside an error, as the existing contract
requires. The index obstruction still needs repair/persistence; this does not
qualify power-loss durability or change the existing first-error batch policy.

## Next authorized slice

Filesystem multipart completion still loses metadata and the committed marker
when object publication succeeds but staging cleanup fails. It is the final known
A3 site and follows separately. Performance follow-up remains the part-listing
cost and Railway remeasurement of changed-version durable fallback; neither is
implemented here.
