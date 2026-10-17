# Bounded storage contract audit — 2026-10-16

Follow-up: **the confirmed native A1–A3 correctness queue is now corrected**.
The final [filesystem completion change](2026-10-17-filesystem-completion.md) retains
published metadata and accounting after cleanup failure, validated on Railway.
The findings below remain historical evidence; performance follow-up is next.

Follow-up: **workspace DELETE now preserves its committed outcome and partial
batch results**, with [Railway regression evidence](2026-10-17-workspace-delete.md).
Filesystem completion cleanup is the remaining confirmed A3 site.

## Result and scope

**Audit baseline: three defect groups, two high and one medium; no production change in the audit.**

Follow-up: **A1 is corrected** in the separate
[multipart recovery change](2026-10-16-multipart-recovery.md). **A2 is corrected** in
the separate [atomic workspace part change](2026-10-16-workspace-parts.md). A3 remains
queued. That follow-up also confirms the same A1 owner-enumeration failure in
the workspace backend; it is the next priority and is left unchanged.

Follow-up: **workspace owner recovery is now corrected** in the separate
[workspace recovery change](2026-10-17-workspace-recovery.md). The two A3 sites
remain the confirmed correctness queue.

The audit’s immediate recommendation was A1, native filesystem multipart recovery. The
previous marked-missing HEAD preflight finding is conditional defensive work,
not the highest-priority demonstrated native failure.

Reviewed branch `codex/core-copy-quality` at
`2d708cf80bea044af1325c342c6700da4a79ecc5`. Freshly fetched `origin/main` is
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor of this branch.
[Baseline and fetch](2026-10-16-storage-contract-audit/raw/baseline.json).
Prior fixes remain in place; this is an audit of their cumulative state.

The fixed boundary is native memory, filesystem-record and editable-workspace
stores; runtime quota/result handling; public embedded adapters; run-through
read/propagation/recovery; S3 COPY and error handoff. The reviewed contracts are:
absence versus uncertainty, committed mutation results, reader ownership,
version/body consistency, and accounting after failed publication or recovery.
Browser persistence, registry/checkpoint lifecycle, exhaustive permission/security
review, live upstream conformance and physical crash/disk fault qualification
are outside this audit. Existing crash-process tests are baseline coverage, not
new crash evidence for the failures below.

## Why the findings kept coming

The review had become a sequence of individual error-branch fixes instead of a
producer-to-consumer review of a few shared contracts. Some fixes address real
native paths; others exercise combinations manufactured by a custom test store.
Those are different levels of evidence and should not have received the same
"next confirmed finding" priority.

The common test gap is checking the error without checking the resulting data,
returned identity and accounting together. For example, the existing filesystem
multipart cleanup test proves an error is returned and the object exists, but
never requires that the returned error identifies commitment or includes that
object's metadata. Workspace completion now has that stronger coverage;
filesystem completion still has the gap. Backend differences matter more than
adding permutations of the same synthetic sentinel combination.

There is no evidence here that every successive discovery was introduced by the
preceding fix. These are existing failure paths exposed by a widening review.
This audit also does not establish that ordinary healthy CRUD is broadly broken.
All newly demonstrated cases require an injected filesystem failure or damaged
persistent state; none is evidence of an observed production incident.

## Contract inventory and reachability

| Boundary | Producer and consumer trace | Assessment |
|---|---|---|
| Native memory mutation | Immutable object publication under the store lock; ordinary refusal or success. No post-publication I/O. | No new failure reproduced. |
| Native filesystem object read | `GetObject` closes failed record opens and returns no reader on error; successful body and header share the open record. `HeadObject` reports record errors. | No native reader-plus-opening-error or committed-marker HEAD producer found. |
| Native workspace read | Successful opens return file plus derived metadata; some path/open failures are collapsed to missing. | Broader I/O-to-absence handling is a review limitation below; no native committed-marker HEAD producer found. |
| Native S3 upstream client | SDK failure returns no reader; invalid metadata closes the SDK body before returning. HEAD does not add a mutation marker. | Recent opening-reader fixes are defensive for custom implementations at this boundary. |
| Publication then bookkeeping | Workspace PUT/completion already return committed metadata after index failure. Workspace DELETE and filesystem completion do not preserve equivalent outcomes. | A3. |
| Multipart persistent descriptors | Workspace part bytes and descriptor are separate publications. Filesystem part listing derives sizes/ETags from actual files. | A2 is specific to the workspace split. |
| Missing versus corrupt multipart state | Filesystem lookup converts every manifest read/decode failure to missing; enumeration skips unreadable manifests. Runtime trusts enumeration to recover reservations. | A1, reachable through the native backend. |
| Runtime/public adapters | Existing fixes preserve committed descriptors and account for them; all failed mutations mark usage dirty. Workspace rescans before later mutations; managed filesystem uses revisions. | A3 has immediate result/accounting effects; a permanent quota leak is not established there. A1 defeats the scan itself. |
| S3 conditional COPY | Date-only conditions reach `ConditionalCopyStore`; native copy implementations check the captured source under their lock. REPLACE checks the source GET descriptor and carries its known size into PUT. | Earlier fixes already cover this. The explicitly documented weaker fallback for custom stores without conditional COPY is not a new defect. |
| Run-through | Native cache eviction/version mismatch has been fixed. Prepared recovery now retains uncertain intents. Preflight still accepts a missing-plus-committed HEAD result. | The last case has a saved custom-provider repro but no identified native producer; demoted below A1–A3. |

## Ranked actionable findings

### A1 — High: corrupt filesystem multipart state disappears from quota recovery

Locations: [GetMultipartUpload](../../internal/storage/fs/fs_multipart.go#L94),
[ValidateMultipartUpload and enumeration](../../internal/storage/fs/fs_multipart.go#L229),
[runtime recovery](../../internal/runtime/state.go#L144).

A native upload has an acknowledged 3-byte part. Close the store, replace its
`manifest.json` with truncated JSON, and reopen. `GetMultipartUpload` returns
`ErrUploadNotFound`; `ListMultipartUploads` succeeds with no upload. Runtime
reconciliation therefore reserves zero bytes and accepts a new 4-byte object
under `MaxBytes: 4`. The 3-byte part remains on disk: **7 payload bytes under a
4-byte limit**. With the original manifest, the exact same write is refused.
The failure survives store close/reopen and is not a fabricated provider error.

The test deliberately injects disk-state corruption; it does not show that
Stow's atomic writer produces truncated manifests during a normal write.
Corruption must nevertheless not be interpreted as proof of absence when
reconciliation depends on a complete enumeration. Unreadable state needs a
reported recovery error, with all staging evidence retained.

Smallest correction to investigate: distinguish missing files from read/decode
failure consistently in lookup, validation and enumeration; make reconciliation
refuse unknown uploads rather than omit their bytes. Do not add an index or
storage format. Compatibility cost: a store with damaged upload state may stop
opening or admitting mutations until repaired, instead of silently ignoring it.
Tests must cover healthy/missing/corrupt/unreadable state and recovery after
repair, including existing uploads belonging to other buckets.

### A2 — High: failed workspace part replacement leaves a false descriptor

Locations: [UploadPart](../../internal/storage/workspace/multipart.go#L155),
[ListParts](../../internal/storage/workspace/multipart.go#L321).

Upload part 1 as `old` (3 bytes), then replace it with `replacement` (11 bytes).
A deterministic body callback obstructs `upload.json` after the native method
has loaded it. Part publication succeeds; saving the descriptor fails. Restore
the previous descriptor document and call the normal APIs. `ListParts` still
reports the old 3-byte size and old ETag, while the actual file contains the
11-byte replacement. Completing from that returned list fails with
`ErrInvalidPart`. The healthy replacement control lists and completes correctly.

This is real filesystem failure injection against the native method, not an
invented return value. Restoring the descriptor isolates the persistence split;
it is not a claimed automatic recovery feature. This audit did not run a
physical-crash simulation or an end-to-end runtime quota exploit for A2. Runtime
recovery's reliance on listed sizes makes under-accounting a supported code-path
concern, rather than a separately measured outcome here.

The invariant needed is that listing/recovery describe the bytes actually
retained, even after one publication succeeds and the next fails. Prefer deriving
part facts from files using existing interfaces, as the filesystem backend does,
or another minimal consistent repair. Simply adding `ErrMutationCommitted`
would not repair the persisted stale descriptor. Any solution that adds scans
or hashing needs comparative measurements **on Railway** before selection.
Compatibility risk: recovery must handle both fresh parts and replaced parts,
including an upload left by an older build, without silently dropping data.

Follow-up: A2 now uses one atomic descriptor/payload record, selected after
comparative Railway measurements. The user removed the historical compatibility
requirement below this audit; old raw staged parts are refused without migration.
[Regression, measured costs and remaining recovery scope](2026-10-16-workspace-parts.md).

### A3 — Medium: completed native mutations lose their committed outcome

Two related sites form one contract problem, but should be fixed in separately
reviewable changes:

- [Workspace DELETE](../../internal/storage/workspace/objects.go#L155): obstruct
  `.stow/index.json`, delete an object, then repair the index path. The object is
  absent, but the error has no `ErrMutationCommitted` marker. Batch deletion also
  omits the key from its confirmed-deleted results.
- [Filesystem multipart completion](../../internal/storage/fs/fs_multipart.go#L219):
  make the upload directory read-only so reads/publication succeed and cleanup
  fails. Completion returns nil metadata and a plain cleanup error although the
  object exists. Runtime immediately reports zero object bytes/objects with a
  4-byte part reservation; after repair and `RefreshUsage`, it correctly reports
  the 4-byte object plus the retained 4-byte part.

Both reproduce with native stores. The cleanup probe requires enforced POSIX
directory permissions; it ran without skips on this macOS host. Do not claim
Windows coverage for this injection. The workspace index probe uses an explicit
path obstruction. These are known post-publication bookkeeping failures, not
proof of a permanent lost object or lasting quota bypass. A retry may recover,
but callers still lose the authoritative outcome and identity of the first call.

Use existing committed-error/result contracts; retain cleanup errors and retained
upload reservations. Preserve partial batch progress. No storage abstraction is
needed. Compatibility cost: callers may now receive metadata/confirmed keys
alongside an error and must retain the already documented committed semantics.

## Demoted and unresolved, not more automatic fixes

- **Marked-missing HEAD preflight:** the previous
  [saved repro](../benchmarks/2026-10-16-prepared-recovery/README.md#next-confirmed-finding--left-unchanged)
  really shows an unsafe reaction to that input. It wraps a native store to
  manufacture `ErrMutationCommitted` joined with `ErrObjectNotFound` on HEAD.
  Native memory/filesystem/workspace HEAD and the supplied S3 client do not
  generate that combination in the traced paths. The marker describes a local
  mutation; its use on HEAD is not a documented native outcome. Keep it as
  conditional custom-provider hardening, not the leading native data-loss claim.
  Related missing checks in read/propagation/provenance belong to the same
  contract review; they are not independently ranked urgent bugs here.
- **Reader plus opening error:** several recent cleanup fixes are appropriate
  defensive ownership handling. The supplied native readers and SDK wrapper
  already close/discard their reader before returning an opening error. The
  saved file-descriptor probes exercise injected custom-provider combinations;
  a real file in a probe does not establish a native producer for that result.
- **Post-rename sync failures and workspace I/O-to-missing conversion:** source
  review identifies uncertainty after atomic rename and broad absence mapping
  in workspace path resolution. No deterministic consequence probe is retained
  for those additional branches in this audit, so they are not confirmed A1–A3
  findings. Durability uncertainty must not be blindly relabeled committed.
- **Performance debt:** the prior changed-version durable fallback report retains
  [local near-2× timing cost](../benchmarks/2026-10-16-runthrough-version/README.md#comparative-measurements--changed-durable-fallback-is-substantially-slower).
  It has not been requalified on Railway, and the later isolated Railway
  classifier test does not resolve it. No new performance claim or optimization
  is made here. All future benchmarks remain Railway-only.

## Evidence and stopping point

The saved [workspace probe](2026-10-16-storage-contract-audit/raw/workspace-probes.go.txt)
and [filesystem/runtime probe](2026-10-16-storage-contract-audit/raw/filesystem-probes.go.txt)
assert the required contracts before any implementation change. Each matrix ran
twice normally and twice under the race detector with Go 1.25.6:

| Probe matrix | Normal healthy / failing leaves | Race healthy / failing leaves |
|---|---:|---:|
| Workspace single DELETE, batch DELETE, part replacement | 6 / 6 | 6 / 6 |
| Filesystem completion and corrupt-upload recovery | 4 / 4 | 4 / 4 |

All 20 adverse leaf executions fail as intended; all 20 healthy controls pass.
No cases skip and no data race is reported. These are contract failures under
the race detector, not claims of newly discovered Go data races.
[Workspace normal](2026-10-16-storage-contract-audit/raw/native-probes.log),
[workspace race](2026-10-16-storage-contract-audit/raw/native-probes-race.log),
[filesystem normal](2026-10-16-storage-contract-audit/raw/filesystem-probes.log),
[filesystem race](2026-10-16-storage-contract-audit/raw/filesystem-probes-race.log).
The [replay runner](2026-10-16-storage-contract-audit/raw/replay.py.txt) accepts
a checkout and output directory. The temporary `_test.go` files were removed after capture.
They become permanent regression tests only with their respective future fixes.

Fresh static checks pass: [Go vet](2026-10-16-storage-contract-audit/raw/vet.log)
across the reviewed Go packages and [Go quality/file-size ratchets](2026-10-16-storage-contract-audit/raw/quality.log).
The preceding baseline already passed [full Go tests](../benchmarks/2026-10-16-prepared-recovery/raw/full-go.log),
[broad race tests](../benchmarks/2026-10-16-prepared-recovery/raw/broad-race.log)
and [four conformance profiles](../benchmarks/2026-10-16-prepared-recovery/raw/conformance-summary.log).
Those unchanged-source results are reused, not presented as fresh executions;
conformance retains its documented mirror-bucket and live-upstream skips.
The [verification record](2026-10-16-storage-contract-audit/raw/verification.json)
checks probe counts, prior source hashes and that every tracked Go source/test
still matches the audit baseline. [Documentation/site checks](2026-10-16-storage-contract-audit/raw/docs.log) pass for this report.
No benchmarks, implementation, dependency, public API or storage format changes.

This audit stopped at this consolidated inventory. A1 has since been implemented
and reported independently in the linked follow-up. A2 and each A3 site remain
queued; A2 is now the next priority. Completion of this audit means the bounded
producer/consumer review is recorded and ranked; it does not mean every storage fault is proven
safe or that another bug cannot exist.
