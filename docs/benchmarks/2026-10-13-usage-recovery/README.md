# Usage and Recovery State consolidation — October 13, 2026

The Embedded Runtime now owns one private accounting module for committed usage,
multipart reservations, staged reservations, revision tracking and recovery readiness.
Reconciliation builds an accounting snapshot instead of constructing another runtime.
Existing public interface declarations remain unchanged.

The runtime-layer reopening regression is fixed. An unresolved publication can
retain an admissible read and resolution handle while refusing mutations.
Known retained receipts remain resolvable when another pending publication is unknown.
Pending evidence is preserved. Opening quotas still apply.

## State transitions

PUT, copy, delete, partial batch deletion, staged publication and multipart completion
use the accounting module's transition implementation. Unknown mutations invalidate
cached usage; confirmed committed results update usage once.
Successful managed filesystem operations keep incremental accounting.
Editable workspaces and generic persistent adapters retain conservative reconciliation.

Active staged reservations survive reconciliation. Releasing a stage still observes
the runtime's store epoch, so reset cannot subtract an old reservation from new state.
Aborting one of several multipart uploads does not invent an object reservation
when the target object already exists.

Filesystem revisions remain stable when publication cannot be resolved.
Successful pending settlement advances the revision and triggers reconciliation.
Mutation admission and receipt observation share the snapshot implementation but
treat recovery uncertainty differently. Public mutation refusal exposes
`ErrSaveRequestUnknown` through the existing error-mapping interface.

## Validation

All builds and tests ran on Railway. No throughput benchmark ran in this pass.
The [two recovery regressions](red-regressions.json) failed against the previous
implementation, then passed in the final race-tested suite.

| Test | Observable behavior |
|---|---|
| `TestRuntimeReopensAfterInterruptedPublishing` | A real interrupted journal permits runtime reads and resolution while refusing mutation. |
| `TestFilesystemUnresolvedPendingKeepsObservation` | Public filesystem reopening preserves reads, retained receipts and malformed pending evidence. |
| `TestStagedReservationsSurvivePersistentReconciliation` | Another write cannot spend a stage's reserved bytes during reconciliation. |
| `TestAbortKeepsExistingMultipartTargetUnreserved` | Aborting one upload leaves the correct object-count allowance. |

The final validation passed `make standards`, all conformance lanes,
Node/WASM checks, protobuf drift checks and browser validation.
There were 33 passing browser tests and two optional skips.
The private chart check exercised 13 combinations and six desktop/mobile previews.
Coverage was 71.16% (10,859 of 15,261 statements).
The comment baseline decreased from 4,138 to 4,113; other quality limits were not raised.

The rebuilt SDK WASM is 6,488,824 bytes, SHA-256
`8c0d3785c92302b348665d674af84c1e25cf2048fb5df0002d53671e6d87c248`.
It grew by 8,053 bytes from the preceding artifact. Generated interface reference
pages were unchanged. The cloud-built WASM and lowered comment baseline were
copied back after hash verification.

## Evidence and scope

- [Final verification receipt](verification/receipt.json) and [complete log](verification/standards.log).
- [Generated SDK hashes](verification/generated-change.json) and [quality measurement](verification/go-quality-report.json).
- [Accounting implementation](../../../internal/runtime/usage_state.go), [multipart transitions](../../../internal/runtime/usage_multipart.go) and [recovery observations](../../../internal/runtime/usage_recovery.go).
- [Caller guidance](../../portable-workspace-usage.md) and [ADR 0020](../../adr/0020-binary-object-records.md).

Reproduction deployment: `41538d80-5f73-48a3-a9d1-108e4bd52738`, status `SUCCESS`
with both targeted tests failing as expected.
Final validation deployment: `a7a8f54d-472c-4cd5-bab3-42d7c588859f`, status `SUCCESS`.
Final validation input hash:
`a059a1c453d2a6276731760c37148ca8fff5bbed56597056288f12966305fb70`.

Temporary Railway compute was stopped after export. The
[environment manifest](../railway-environment.json) records the readback.
No website publication, commit or push occurred.
The object-write preparation and record-publication candidates remain separate work.
