# Correct filesystem multipart recovery accounting

Corrected from `f4b96adaf4f250f47b1d0c98de6144795130184d` on
`codex/core-copy-quality`. Freshly fetched `origin/main` is
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor of the baseline.
This implements **A1 only** from the [bounded audit](2026-10-16-storage-contract-audit.md).
The user's subsequent instruction removes backward compatibility as a requirement;
this change adds strict validation without migrations or legacy-format handling.

## Defect and original evidence

Native filesystem lookup/completion converted every multipart manifest read error
into `ErrUploadNotFound`; validation converted every such error to `ErrNoSuchUpload`;
enumeration skipped those uploads. Runtime reconstruction depended on that
incomplete enumeration. The audit reproduced an acknowledged 3-byte part being
ignored after manifest corruption and a new 4-byte object being accepted under a
4-byte cap. Staging remained on disk: 7 payload bytes under a 4-byte limit.

Permanent regressions were added and run before modifying implementation. They
reproduce the same failure after close/reopen for truncated JSON, a missing
manifest in a retained upload directory, and an unreadable manifest path. They
also demonstrate live validation releasing the existing part reservation and
failed reconciliation admitting another write. These are native filesystem/state
fault injections, not custom stores manufacturing return values. They do not
claim that the atomic writer normally creates malformed JSON or simulate a
physical crash.

The wire-level regression shows the consequence at the existing S3 interface:
corrupt upload listing wrongly succeeds with an empty list, and completion
reports 404 NoSuchUpload. Healthy listing/completion and genuine absence are
passing controls.

[Before normal](2026-10-16-multipart-recovery/raw/before-wire.log) and
[before race](2026-10-16-multipart-recovery/raw/before-race.log) each have **26
failing adverse leaf executions and 6 passing controls**, with every case repeated
twice. Existing tests did not cover a native damaged manifest through runtime
reconstruction and retained-byte admission together.

## Small correction

Only [fs_multipart.go](../../internal/storage/fs/fs_multipart.go) changes in
production. The existing manifest reader distinguishes a genuinely absent upload
directory from an existing upload with missing metadata. It preserves read/decode
errors and rejects decoded documents with an invalid bucket or key, including
`null` and `{}`. Syntactically valid but empty metadata must not silently disappear
through bucket filtering.

Lookup and completion return that reader's error. Validation translates only
confirmed upload absence to its existing NoSuchUpload result and reads under the
store's existing read lock. Enumeration returns manifest failures instead of
omitting their retained parts. No change to runtime accounting is needed: its
existing reconciliation already refuses on an error and keeps its prior state.
The [storage contract](../compat-contract.md) was clarified before implementation.

The regression tests cover:

- Native lookup, validation, enumeration and completion for truncated JSON, null,
  empty objects, invalid bucket/key, missing metadata and metadata I/O errors.
  Failed operations preserve the part, and completion works after repair.
- Runtime refusal to reopen unknown uploads, including an upload belonging to a
  later bucket in enumeration. After repair, the 3-byte reservation is restored,
  a 4-byte write is refused, and abort releases enough room for it to succeed.
- Live validation/refresh preserving reservations on uncertainty and recovery
  after repairing the metadata.
- S3 listing/completion errors for damaged state, healthy operation and genuine
  absence. Healthy uploads for other buckets retain ordinary filtering.

## Before and after

| Observation | Original | Corrected |
|---|---|---|
| Reopen with damaged upload state | Succeeds, reserves zero part bytes | Refused; retained part unchanged |
| New 4-byte object with retained 3-byte part under cap 4 | Admitted | Cannot be admitted through failed reopen; refused after repair restores accounting |
| Live validation of corrupt existing upload | NoSuchUpload; reservation released | Original failure; 3-byte reservation retained |
| Refresh of corrupt existing upload | Succeeds with omitted reservation | Fails and preserves prior accounting |
| Damaged upload listing / completion over S3 | 200 empty / 404 | 500 / 500 |
| Genuine absent upload completion over S3 | 404 | 404 |
| Healthy listing / completion over S3 | 200 / 200 | 200 / 200 |

All **32 permanent regression leaf executions pass normally and under race**
(two repetitions in each mode): [normal](2026-10-16-multipart-recovery/raw/after-tests.log),
[race](2026-10-16-multipart-recovery/raw/after-race.log).
The exact original saved audit quota probe also passes twice normally and twice
under race: [normal](2026-10-16-multipart-recovery/raw/original-after.log),
[race](2026-10-16-multipart-recovery/raw/original-after-race.log).
Its temporary test file was removed. No race reports or skips occur in these
focused runs.

Final checks pass: [full Go suite](2026-10-16-multipart-recovery/raw/full-go.log),
[broad storage/runtime/adapter/S3 race suite](2026-10-16-multipart-recovery/raw/broad-race.log),
[Go vet](2026-10-16-multipart-recovery/raw/vet.log),
[quality/file-size ratchets](2026-10-16-multipart-recovery/raw/quality.log), and
[documentation/site checks](2026-10-16-multipart-recovery/raw/docs.log).
All [four conformance profiles](2026-10-16-multipart-recovery/raw/conformance-summary.log)
pass: memory, filesystem, runtime-memory and runtime-filesystem. The existing
mirror-bucket decision and live-upstream tests remain skipped in each profile.
[Full conformance log](2026-10-16-multipart-recovery/raw/conformance-full.log).
Conformance ran before test-helper cleanup; later changes affected test arrangement
and one production comment, with no production behavior change. The final full Go
and broad race runs include the simplified tests. The
[source freeze](2026-10-16-multipart-recovery/raw/source-freeze.json) and
[regression counts](2026-10-16-multipart-recovery/raw/regression-counts.json)
identify the final tested sources and outcomes.

The initial quality gate flagged the test functions' complexity. Their setup
and assertions were split into smaller helpers, and the existing
[quality/file-size ratchets](2026-10-16-multipart-recovery/raw/quality-final.log)
now pass without raising a baseline.

## Costs and limits

There is no optimization or performance claim. **No benchmarks were run**;
future benchmarks remain Railway-only. Healthy manifest reads perform the same
filesystem reads as before, with additional bucket/key validation. A missing
manifest adds one directory stat to establish whether the upload itself is gone.
Validation now takes the existing store read lock and can wait behind a writer.
Timing and allocation deltas have not been measured.

Strict failure can block multipart listings for any bucket, or runtime reopen,
when an upload's metadata cannot establish its owner. That is intentional: the
store cannot safely decide that unknown retained data belongs elsewhere or
costs nothing. Parts and metadata are preserved for repair; no automatic cleanup,
new index, migration, dependency, public API or storage format is added.

This detects missing/unreadable data, JSON decoding failures and invalid identity
fields. It does not detect every possible corruption that still forms a valid
bucket/key record. No new checksum/format or physical-crash guarantee is claimed.
Filesystem multipart completion's separate post-publication cleanup error remains
A3 and is unchanged by this manifest-read correction.

## Next priority — left unchanged

**A2: workspace part replacement can publish bytes while retaining an old part
descriptor.** Its deterministic evidence remains in the bounded audit. Resolving
that persistence split is the next independent change; any proposed extra scan
or hashing work first needs comparative Railway measurements. A3's committed
outcome sites remain queued. The demoted synthetic HEAD cases are not promoted
back into the native priority list.
