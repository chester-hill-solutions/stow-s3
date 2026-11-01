# Admit durable saves before creating their intent

**Request-key filesystem saves bypassed namespace admission.** This change places the existing journal/object/receipt transaction inside the existing capacity admission interface. It reserves the projected final object and receipt, object staging, and the journal's existing bounded recovery allowance before any save intent is written. Unknown outcomes retain their durable reservation until verified reconciliation. No dependency, public API, storage format or S3 feature is added.

## Baseline and deterministic defect

The clean branch started at `24e63aec36709a927d9d3c895a7fc514510caf8d`. Fetching `origin/main` returned unchanged `219d1dcfff9807b04660a40313b32127e5be2933`. Prior COPY, object-record, multipart recovery and outbox remediations are preserved. The previous goal turn was status/audit only; this turn advances the native admission finding with a reproduction, implementation and independent validation.

The native constructor `NewFilesystemStoreWithNamespace` binds the store to a namespace. Ordinary object writes use `writeCapacityFile`, but request-key saves set `s.saves.pending` before publishing. `writeRecord` deliberately bypasses per-file admission while pending; the save journal also writes directly. No outer admission surrounded that transaction, and the existing `capacityActive` guard was never activated.

The tests use actual filesystem stores and public store calls, not a custom backend returning invented error combinations. Namespace maximum is 4 MiB with 1 MiB reserved for recovery. Both a 1,600,000-byte object that exceeds the two-copy staging allowance and a 4 MiB object exceeding the final payload allowance are accepted by the original request-key path, with and without a content guard. They remain readable and retain receipts. Ordinary and guard-only controls refuse both sizes. A successful 198-byte record leaves the old ledger at **zero payload bytes**.

Two stores also bypass the same namespace and host budgets, including simultaneous writers. Repeated large-metadata saves exceed the recovery reserve for receipts. At prepared, publishing, published and sync interruption boundaries, the original ledger retains no object/staging reservation. Terminal committed saves are likewise uncharged. Tests verify byte counts, absence on refusal, retained receipts, replay, competing stores, exact resource versions, short and bounded long keys, overwrite, deletion, in-process resolution and reopen recovery.

The [final original-source proof](2026-10-18-save-admission/raw/verified/verified-red-normal.log) and [race proof](2026-10-18-save-admission/raw/verified/verified-red-race.log) use byte-exact original production files through a Go overlay. Exit 1 is expected, and failure messages and passing controls are independently inspected. The [same native tests against the fix](2026-10-18-save-admission/raw/verified/focused-normal.log) and [race results](2026-10-18-save-admission/raw/verified/focused-race.log) pass twice each.

Initial reproduction artifacts are retained, including a fixture assertion that compared unreconciled setup metadata and a formatting failure caused by a root-owned uploaded test. The fixture explicitly reconciles setup before taking its refusal baseline; uploaded files are owned by UID 10001. Neither issue is used as defect evidence. Two intermediate quality failures came from new test complexity and were fixed by splitting helpers; the quality limit and baseline were not raised.

## Change and recovery contract

`commitSaveLocked` still validates the existing journal bounds, then delegates to private admission/publication helpers. The admission projects the exact binary object record and committed receipt using the existing encoders. The existing `saveEntryReservation` supplies conservative room for pending and replacement receipt files. One host admission lock surrounds publication; `capacityActive` suppresses recursive per-file admission during that already-reserved transaction.

A save error with an uncertain outcome is wrapped in the existing `capacity.ErrUnknown`. `Namespace.WithAdmission` retains its already-durable pending state for that sentinel. Ordinary callback failures still withdraw their reservation, as the existing contract test requires. Verified save finalization clears the journal pending state and reconciles observed files when outside an active admission. Constructor recovery continues to settle the journal before binding/reconciling the namespace.

No save outcome is inferred from an error alone. Prepared intents resolve as not committed; a publishing intent without matching object evidence stays unknown and keeps its reservation. Published saves verify the exact object marker/fingerprint before releasing staging. A terminal committed error preserves its committed classification. If the save finishes but the capacity ledger cannot settle, the returned error identifies the already-committed object. Retained request meaning, metadata ownership, checksums and same-key replay remain unchanged. Returning the existing owned metadata avoids an extra allocation from splitting the publication function.

Refused admission creates no new intent, receipt or object. Successful projection is checked against actual files through native dependency verification, including bounded-path key headers. Payload/staging accounting is settled immediately instead of depending on a later unrelated store scan.

## Railway performance and costs

All formatting, builds, tests, race/conformance checks, vet, quality/site/documentation checks and benchmarks ran only on the authorized Railway scratch runner, deployment `2fd1f57f-d3ae-44ac-b681-20ec699841eb`. Go 1.25.6, Linux amd64, UID 10001, GOMAXPROCS 4, 4 CPU/4 GiB container. Fixture writes use the mounted ext4 `/results` volume (statfs 61267). The target service was not started or modified.

Temporary benchmark source is excluded from the production change. Original/fixed test binaries use identical fixtures. Each measured operation is one fresh request-key save with an absent-object guard; namespace/host/store creation and cleanup are outside timing and allocation accounting. Cases use 0, 64 KiB and 1 MiB bodies, bound and unbound. Eight ABBA/BAAB original/fixed runs and eight same-binary control runs provide four observations per label/cell: **96 observations**. All adverse observations are retained. These are synthetic allocation and latency measurements, not a production workload or peak RSS claim. The fixed bound case also verifies the resulting charge outside timing.

The original bound path is faster partly because it omits the required accounting. These results measure the cost of restoring the contract, not an optimization speedup. Positive changes below mean more/slower.

| Save | Median time, original → fixed | Time change | Bytes/op, original → fixed | Objects/op, original → fixed | Same-binary control time change |
| --- | ---: | ---: | ---: | ---: | ---: |
| bound=false/bytes=0 | 20.643 → 20.802 ms | +0.77% | 52,184 → 52,090 | 466 → 466 | +3.56% |
| bound=false/bytes=1048576 | 33.151 → 33.571 ms | +1.27% | 2,160,412 → 2,160,176 | 478 → 478 | +3.35% |
| bound=false/bytes=65536 | 25.695 → 22.199 ms | -13.61% | 191,628 → 191,710 | 468 → 468 | -16.07% |
| bound=true/bytes=0 | 19.080 → 30.704 ms | +60.93% | 52,121 → 124,614 | 467 → 1,028 | +12.52% |
| bound=true/bytes=1048576 | 35.531 → 45.527 ms | +28.13% | 2,159,499 → 3,294,868 | 476 → 1,046 | -6.55% |
| bound=true/bytes=65536 | 23.099 → 34.397 ms | +48.91% | 191,580 → 340,044 | 468 → 1,033 | +7.19% |

[All samples, binary hashes and commands](2026-10-18-save-admission/raw/verified/measurements.json), [medians and full ranges](2026-10-18-save-admission/raw/verified/comparison-analysis.json), [benchmark fixture](2026-10-18-save-admission/raw/save-bench.go.txt).

New costs are authoritative file scans/hashes, projected record/receipt encoding, and durable capacity-ledger writes. Encoding the projected record adds a body-sized buffer on the bound save path. The host admission lock now also serializes this save transaction with other admitted host writes; contention is not quantified by the fresh single-save benchmark. Unresolved saves conservatively retain both projection and staging allowance until resolution. Earlier refusal near recovery/staging limits is intentional enforcement and can affect callers that relied on the bypass. No stronger physical disk reservation, external-writer fencing or bounded-memory claim is introduced.

## Final validation and next finding

The final job passes the standard 14 gates: formatting; quality/file-size/format ratchets; focused normal and race tests twice; full Go suite; broad storage/capacity/runtime/embedded/runthrough/S3/public-package race tests; four conformance profiles; vet; site build/tests; documentation links. Two additional original-source normal/race proofs and six benchmark setup/build/measurement/cleanup steps also pass. The temporary benchmark Go file is removed after measurement. Commands, exit statuses and source hashes are retained in the [final checks](2026-10-18-save-admission/raw/verified/checks.json) and [validation receipt](2026-10-18-save-admission/raw/verified/validation-receipt.json). Report and evidence receive a further Railway documentation check before runner teardown.

The next highest-priority lead is **multipart part publication under a bound namespace**: `UploadPart` still calls the direct atomic-file writer, and `CreateMultipartUpload` writes its manifest directly. The source map shows these bypassing the admission helpers used by ordinary records. Reproduce payload/staging and shared-host budget behavior through those native methods before changing them. Bucket metadata writes and ordinary post-rename sync-error accounting also remain audit leads. No next production fix is implemented in this change.

The [artifact manifest](2026-10-18-save-admission/raw/artifact-manifest.json) records hashes and lengths for retained proofs, source snapshots, measurements, validation, report checks and teardown evidence.
