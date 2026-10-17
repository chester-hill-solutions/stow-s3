# Preserve unknown workspace multipart owners during recovery

Baseline `13bc1534759fa7346ecdd71c14a9354493e801ce` contains the atomic part
publication fix. Fresh `origin/main` remains
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor of this branch.
[Baseline](2026-10-17-workspace-recovery/raw/baseline.json).
This corrects the workspace occurrence of A1 from the
[bounded audit](2026-10-16-storage-contract-audit.md), independently of A3.

## Reproduction before implementation

All execution was on the existing Railway scratch runner, as UID 10001 with the
previously qualified loopback names and orphan reaper. No local tests, builds,
benchmarks or quality gates ran. No benchmark ran anywhere in this change.

Permanent regressions ran twice normally and twice under race before production
changes: **38 adverse leaf failures and eight passing controls in each run**.
[Normal](2026-10-17-workspace-recovery/raw/before/before-normal.log),
[race](2026-10-17-workspace-recovery/raw/before/before-race.log),
[counts](2026-10-17-workspace-recovery/raw/counts.json).
These are native filesystem faults, not synthetic provider errors.

Truncated/null/empty JSON, invalid bucket/key, mismatched upload ID, missing
metadata and metadata replaced by a directory expose lookup/enumeration failures.
The real retained part is checked before repair. Close/reopen recovery admits a
new 12-byte object while ignoring an acknowledged 11-byte part under a 12-byte
cap; the PUT targets the same key, avoiding a separate object-slot explanation.
Healthy other-bucket filtering and genuinely absent upload controls pass.
Live validation and refresh also expose reservation loss/incomplete recovery.
Over S3, damaged upload listing incorrectly succeeds; missing metadata completion
incorrectly reports 404 instead of an uncertainty error.

A separate native clock callback obstructs `upload.json` after initiation creates
its directory. The old failure leaves an unknown directory. Stricter enumeration
must not strand a newly failed initiation and block later healthy reconciliation.

## Small correction

Only [workspace multipart implementation](../../internal/storage/workspace/multipart.go)
changes in production. Missing owner metadata becomes UploadNotFound only when
the upload directory itself is absent. Existing read/decode failures remain
errors; decoded owner identity must match the directory ID and contain a valid
bucket/key. Enumeration propagates failures before bucket filtering. Runtime's
existing reconciliation therefore refuses incomplete state without new logic.

Failed initiation makes a best-effort removal of its newly allocated upload
directory, matching the filesystem backend's existing behavior. It preserves the
original publication error. Previously acknowledged uploads are not removed.
If cleanup also fails, unknown retained state still blocks reconciliation.
The [storage](../compat-contract.md) and [workspace](../workspace-contract.md)
contracts were clarified before implementation. No dependency, public API,
storage format, index, cache or migration is added.

## Results and costs

All **46 focused leaf executions pass normally and under race**, with each case
repeated twice and no skips: [normal](2026-10-17-workspace-recovery/raw/after/focused-normal.log),
[race](2026-10-17-workspace-recovery/raw/after/focused-race.log).
Lookup, validation, enumeration, parts and completion preserve damaged state.
Repair permits completion with initiation metadata, restores the 11-byte quota
reservation, refuses a 2-byte PUT under cap 12 and permits a 12-byte PUT after
abort releases staging. Healthy/absent S3 outcomes stay 200/404; damaged listing
and completion return 500. Failed initiation no longer leaves unknown staging.

All [14 Railway checks](2026-10-17-workspace-recovery/raw/after/checks.json) pass:
focused normal/race; full Go; broad storage/runtime/embedded/run-through/S3/public
API race; four conformance profiles; vet; quality/file-size/format; site build,
site tests and documentation links. The two existing mirror-bucket/live-upstream
conformance skips remain. [Result](2026-10-17-workspace-recovery/raw/after/run.json).
Deployment `7de49edd-449b-44f1-a030-3d78a45cef2c` reached SUCCESS. The code was
updated in the same isolated container after the before run completed, preserving
warm tool caches; [source update receipts](2026-10-17-workspace-recovery/raw/owner-after-sync-receipt.json)
and [actual tested source hashes](2026-10-17-workspace-recovery/raw/after/environment.json)
identify the change. The Git fixture contains the exact original parent quality
baselines, not the repository history. Source/test formatting was done on Railway.
The runner remains available for the next explicitly authorized correctness slice.
The documentation check above precedes this new report. Uploading review reports
and evidence for an additional Railway link check awaits separate approval; the
source/test checks and their results are complete.

Healthy reads use the same filesystem reads plus ID/bucket/key validation. A
missing owner adds one directory stat. Failed initiation adds cleanup work only
on its error path. These costs are not benchmarked; there is no speed or allocation
claim. Corrupt state can now block listing for another bucket or runtime reopen
until repaired. That availability cost prevents unknown bytes from costing zero.
This is not a checksum for every possible syntactically valid corruption or a
physical-crash qualification. Existing whole-part buffering is unchanged.

## Next authorized slice

A3 workspace DELETE must preserve a known committed outcome after index failure,
including confirmed keys in partial batch results. Filesystem completion's cleanup
outcome follows as its own change. Both are already reproduced findings; no new
bug hunt or performance implementation is included here. Performance follow-up
remains the measured part-listing cost and Railway qualification of the earlier
changed-version durable-fallback regression.
