# Storage product plan alignment

> **Consolidated on 2026-09-29; reference only.** The [new working-storage plan](storage-foundation-plan.md) is the sole work order. This earlier review/order is superseded. Preserve its dated findings as history; the new plan owns current status and acceptance. Former checklists, phase numbers and next steps below do not schedule work.

## 1. Plan validation summary

**Verdict: Adjusted; storage scope aligned.** The user selected portable working
storage with caller-owned execution. The canonical plan, milestone, entry-point
documentation and ADR lifecycle now reflect that boundary. Existing behavior is
unchanged; observed implementation gaps remain scheduled, not marked fixed.

The follow-up consolidation inventories all repository planning documents in
[the disposition register](planning-index.md). Old DX, remediation, FOSS, workspace,
portability and architecture work orders are superseded in full. Their surviving
work is assigned canonical S0–S4 IDs; execution proposals and conditional expansion
have explicit deferred dispositions. The MCP/OpenCode detail and milestone remain
active subordinate documents.

Consolidation validation, before the subsequent discovery pass: ADR index/digest
checks passed for 14 decisions; documentation command
surface checks passed for 12 documents; 272 changed-document local link targets and
`git diff --check` passed. Full implementation suites were not rerun for this
documentation change. The checkout was fast-forwarded to the already fetched
`5485a36`, preserving the existing local documentation edits.

The later [pre-code discovery](pre-code-discovery-2026-09-29.md) adds source-backed
capture/publication prerequisites, complete object payload preservation, pinned
OpenCode v2 lifecycle findings and public-registry delivery requirements. Its
proposed designs expand canonical IDs; this earlier alignment report is not a
claim that those newly identified gaps were already tested or resolved.

## 2. What the plan already covers well

- Real workspace files, durable identity, verified capture and explicit transfer.
- Shared Go storage semantics behind CLI and thin TypeScript/Python wrappers.
- Local-only defaults, distinct live-write consent and explicit ownership/cleanup.
- Separate storage and execution enforcement boundaries.
- Recorded independent tests, including failures, rather than coverage as proof of
  every public contract.

## 3. Gaps, conflicts, or redundancies to fix

1. **Execution was accepted as the automatic next product layer.** `work-session-plan.md`
   and ADR 0013 conflicted with the latest product decision. ADR 0014 narrows 0013;
   the execution proposal is explicitly deferred. Its original rationale is retained.
2. **Several plans claimed scheduling authority.** Workspace, deploy-anywhere, DX,
   FOSS, remediation and both architecture proposals are now explicitly historical.
   The planning index maps their work into `plan.md`; the remediation release gate
   is carried into S2 rather than left as a second active checklist. ADR 0014 also
   resolves older ADR references to a different plan of record.
3. **The milestone required a runner and attempt lifecycle.** It now requires a thin
   caller-triggered checkpoint integration and storage reconstruction. No second
   registry, execution schema or checkpoint implementation is a prerequisite.
4. **The product introduction overclaimed scope and discounted local S3.** README and
   CONTEXT now describe one storage product with multiple consumers. Workspace facade
   availability, upstream scope and process isolation are qualified.
5. **Test/release status was stale.** Canonical status now points to the assessed
   revision and records the red standards gate and public distribution gaps. It does
   not copy old success claims forward or present local package installs as publication.
6. **Context metadata could be lost by old readers.** A new optional JSON field is
  not automatically compatible: checkpoint typed round trips can discard unknown
  fields. Any extension needs explicit version/refusal and transport semantics.
7. **Important residual work was hidden in obsolete documents.** Standing registry
   policy, effective request limits, multipart/concurrency bounds, endpoint policy,
   corpus gaps, legacy-layout diagnostics and real resource measurements now have
   explicit S0/S1/S2 destinations. Old claims that delta CLI, local base-host support,
   lifecycle soak or contribution infrastructure are absent are retained only as
   history; those features are not scheduled for duplicate implementation.
8. **Inspection promised more than the public lookup provides.** MCP inspection now
   starts with identity/location/retention fields. A broader descriptor must extend
   the shared read-only API; it must not resume a live workspace to inspect it.

## 4. Existing code/product references to reuse or extend

- [Workspace lifecycle](/Users/ladmin/WebProjects/stow/pkg/stow/workspace.go):
  identity, embedded runtime, close/destroy and per-workspace limits.
- [Input preparation](/Users/ladmin/WebProjects/stow/pkg/stow/prepare.go): existing
  file/Git preparation and returned working directory for an external consumer.
- [External capture](/Users/ladmin/WebProjects/stow/pkg/stow/checkpoint_external.go:45):
  `CheckpointOf` works without acquiring another process's workspace session.
- [Capture implementation](/Users/ladmin/WebProjects/stow/pkg/stow/checkpoint_capture_core.go):
  shared scan/copy/verify/publication path; retain refusal of detected changes.
- [Checkpoint types](/Users/ladmin/WebProjects/stow/pkg/stow/checkpoint.go:18),
  [archive export](/Users/ladmin/WebProjects/stow/pkg/stow/checkpoint_archive.go),
  [import](/Users/ladmin/WebProjects/stow/pkg/stow/checkpoint_import.go) and
  [delta application](/Users/ladmin/WebProjects/stow/pkg/stow/delta_apply.go):
  reuse existing versioned state and define any new context preservation end to end.
- [Native storage composition](/Users/ladmin/WebProjects/stow/cmd/stow-s3/store.go),
  [runtime adapter](/Users/ladmin/WebProjects/stow/cmd/stow-s3/runtime_store.go),
  [workspace storage](/Users/ladmin/WebProjects/stow/internal/storage/workspace):
  extend for the shared directory/S3 surface; avoid a second synchronized store.
- [TypeScript workspace wrapper](/Users/ladmin/WebProjects/stow/packages/stow-s3/src/workspace.ts)
  and [Python wrapper](/Users/ladmin/WebProjects/stow/packages/stow-s3-py/src/stow_s3/workspace.py):
  translate the same CLI contract rather than own capture semantics.

## 5. API impact and validation notes

**This alignment changes no implementation API or persisted format.** Existing S3
session APIs, workspace IDs, checkpoint IDs, and close/destroy distinctions remain.
The current `CheckpointOptions` has parent/limits/sensitive-file options and no turn
or caller-context field. The initial recipe can store context in declared workspace
files, requiring no API extension.

If a later typed attachment is justified, bound its size and sensitivity, define
publication with the checkpoint, and specify preservation through export/import,
restore, preview and delta application. Handoff parsing rejects unknown fields;
checkpoint readers use typed unmarshalling that may silently discard additions.
Required context may need a new format version so older tools refuse it clearly.
Do not call the change backward-compatible until that decision is tested.

File capture excludes private `.stow` state and Git internals. Required object
metadata needs an explicit transport contract, not a wholesale copy of ownership,
lock or credential state. `LookupWorkspace` exposes checkpoint retention caps,
not all runtime authority/quotas. Reconciliation after a lost capture reply also
needs a supported way to discover the committed result; a lookup requiring the
lost checkpoint ID cannot alone satisfy that requirement.

The CLI workspace S3 facade is unfinished, so exposing it is real implementation
work. Per-turn checkpoint requests must not introduce a Stow-owned agent execution
state machine or imply arbitrary writers have been stopped by Stow.

## 6. Final adjusted plan, summarized

1. Repair observed storage correctness/compatibility defects and restore readiness.
2. Complete one shared directory/S3 and portable-state contract using current modules.
3. Deliver coherent public artifacts and prove supported clean installs.
4. Deliver the thin local MCP storage adapter and workflow guide; demonstrate OpenCode
   in a separate prepared workspace with selected saved context, bounded checkpoint
   retries and explicit failure. Verify the turn barrier before promising automatic
   coverage and continue from transferred state on the second supported host.
5. Validate an S3 fixture workflow and voluntary repeated storage use; let evidence
   determine expansion. Execution and sandbox ownership stay deferred.

## 7. Handover items for engineering, enumerated

1. **`workspace_handoff.go` / `workspace_archive.go`:** emit relocatable references;
   verify copied bundles adopt with original paths unavailable and corrupt input refused.
2. **S3 checksum/header paths and compatibility corpus:** close the default-client
   checksum and metadata-casing failures; verify with actual supported SDK/CLI clients.
3. **`internal/runthrough` / CLI configuration:** make upstream scope explicit and
   verify restrictions still hold after local bucket creation; preserve offline and
   separate live-write consent behavior.
4. **Native runtime composition / workspace backend:** expose the facade over the
   same store; verify object/file visibility and metadata, without duplicate storage.
5. **Prepare/checkpoint/archive/delta paths:** decide Git-base and object-fixture
   preservation; verify modes, deletions, required metadata and unsupported cases.
6. **Checkpoint API / integration example:** save context as files initially; invoke
   external capture at a caller-safe boundary. Verify capture refusal/retry, retained
   prior saves, and repeat-checkpoint caps without adding an executor.
7. **Distribution manifests, release workflow and wrappers:** fix gate blockers and
   validate version-matched published artifacts in fresh supported consumers.
8. **Milestone validation:** exercise an actual caller-owned runner plus an S3 fixture
   consumer; measure manual recovery effort and retained use. Do not turn synthetic
   worker results into claims of real-agent success or security containment.

9. **MCP storage adapter and agent guidance:** follow [the integration plan](mcp-storage-integration-plan.md), reuse Go/CLI operations, and verify tool/schema parity plus safe-boundary behavior in one real host. MCP is an access layer, not an execution or turn-detection mechanism.

10. **Planning maintenance:** use [the canonical plan](plan.md)'s S0–S4 IDs as the
    current queue and [the disposition register](planning-index.md) for source
    traceability. Keep retired bodies frozen. Verify documentation links, command
    examples and ADR digests when plans change; attach implementation evidence only
    when the corresponding work actually completes.
