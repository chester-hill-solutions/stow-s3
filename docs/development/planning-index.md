# Planning index and disposition register

**Index updated:** 2026-11-14. This register classifies current guides, retained
designs and historical records; it is not an implementation backlog. The
September 29 reconciliation outcomes below remain a dated snapshot. Retired work
orders now live under [history](../history/README.md).

## Start here

1. [Consolidated working-storage plan](storage-foundation-plan.md): the sole
   scope, ordering, status and dispatch authority, W01–W28 and the legacy S0–S4
   disposition map. Supporting documents below are design, contract, usage or
   dated evidence references; they do not create work or dispatch instructions.
2. [Portable workspace usage](../guides/portable-workspace-usage.md): actual APIs, commands
   and limits. See the [release notes](../releases/v0.4.0.md) and [npm publication
   record](npm-publishing.md#published-037) for artifact/version facts: native/Go
   v0.4.0, npm v0.3.7; Python remains source-only.
3. [ADR index](../adr/README.md): decisions in force; the custom-store conflict in
   ADR 0010 was resolved by W01; its remaining promotion-symbol documentation
   conflict is tracked in the canonical queue.
4. [Baseline implementation evidence](../history/implementation/implementation-2026-09-29.md),
   [later receiver/model evidence](../history/implementation/opencode-receiver-remediation-2026-09-29.md) and
   [conditional slice evidence](../design/guarded-storage-plan.md#verification-of-the-conditional-write-prerequisite):
   dated records with distinct scope. None certifies publication or Linux runtime.

The new plan replaces the previous canonical queue and all independent ordering
in the detail proposals. It carries the product definition of done, accepted
foundation contracts, implementation slices, qualification/release/adoption gates
and deferred boundary together. Existing contracts and ADRs remain normative.

## Document inventory

| Document | Current disposition |
| --- | --- |
| [storage-foundation-plan.md](storage-foundation-plan.md) | **Sole scope, ordering, status and dispatch authority.** W01–W28, dependencies, acceptance and legacy traceability. |
| [workspace-resource-plan.md](../design/workspace-resource-plan.md) | **Frozen design and dated execution reference.** Workspace detail maps to canonical W23–W28; the canonical plan owns all current status and next work. |
| [aboiteau-native-consumer.md](../design/aboiteau-native-consumer.md) | **Named consumer requirements and dated evidence reference.** Requirements map to canonical W items; no independent queue or status authority. |
| [browser-project-plan.md](../design/browser-project-plan.md) | **Completed ADR 0019 implementation record.** Current maintenance and release status route to the canonical plan; usage is [browser-project-storage.md](../guides/browser-project-storage.md). |
| [plan-storage-scale-and-transfer.md](../design/plan-storage-scale-and-transfer.md) | **Dated design/evaluation reference, not an active queue.** COPY REPLACE streaming and checkpoint payload reuse have since been implemented; general persistent ordered listing index is not claimed. Any further scale work requires measured workload evidence under W16 and canonical dispatch. |
| [receipt-retirement-proposal.md](../design/receipt-retirement-proposal.md) | **Unresolved lifecycle proposal, not authorized work.** Current bounded receipt/replay and recovery-hold behavior is governed by W02/W03 and their contracts; archive ownership/durability/budgets versus a declared replay horizon remains a deferred decision. |
| [gangcode-stow-boundary.md](../design/gangcode-stow-boundary.md) | **Ownership/integration reference.** Storage facts and durable delivery belong to Stow; agent meaning, context and coordination belong to GangCode. No independent queue. |
| [plan.md](../plan.md) | **Routing page.** Preserves old entry point/anchors; no separate queue. |
| [site/redesign-plan.md](../../site/redesign-plan.md) | **Completed design and publication record.** Current site/release status routes to the canonical plan. |
| [portable-workspace-usage.md](../guides/portable-workspace-usage.md) | **Current usage.** Concrete supported development APIs, commands and limits. |
| [implementation-2026-09-29.md](../history/implementation/implementation-2026-09-29.md) | **Dated baseline evidence.** Earlier aggregate validation and costs; later evidence is separately linked. |
| [opencode-receiver-remediation-2026-09-29.md](../history/implementation/opencode-receiver-remediation-2026-09-29.md) | **Dated later evidence.** Historical receiver/model results; current acceptance status routes to the canonical plan. |
| [compatibility-coverage-2026-09-29.md](../history/assessments/compatibility-coverage-2026-09-29.md) | **Evidence/coverage map.** Existing SDK/protocol coverage; uncovered work belongs to W06/W16/W20. |
| [portable-agent-workspace-goal.md](../history/plans/portable-agent-workspace-goal.md) | **Superseded milestone ordering; retained rationale.** Product/workflow/adoption acceptance absorbed into the new plan and W19/W21/W22. |
| [mcp-storage-integration-plan.md](../history/plans/mcp-storage-integration-plan.md) | **Superseded detail work order; retained design history.** Current status and remaining acceptance route to the canonical plan. |
| [resumable-transfer-plan.md](../design/resumable-transfer-plan.md) | **Retained transfer design reference.** Same-store SDK continuation W06; consumer recipe W21. No independent checklist. |
| [guarded-storage-plan.md](../design/guarded-storage-plan.md) | **Retained contract/slice evidence.** Current scope and status route to the canonical plan. |
| [workspace-notifications-plan.md](../design/workspace-notifications-plan.md) | **Retained notification design reference.** Core observation/readiness/codecs/diffs/webhooks W08–W11; portable state W12. Proposed API bodies are not shipment claims. |
| [protected-cache-plan.md](../design/protected-cache-plan.md) | **Retained broad storage design reference.** Access W04/W05, encryption W07/W14/W15, host qualification W17/W18. Cache is a profile. |
| [portable-recovery-plan.md](../design/portable-recovery-plan.md) | **Retained recovery design reference.** Holds/budgets W03, sections/takeover W12/W13 and envelopes W14/W15. Confirmed-takeover default is settled; proof is engineering work. |
| [foundation-review-2026-09-29.md](../history/plans/foundation-review-2026-09-29.md) | **Superseded review/order.** Findings/defaults absorbed into the new plan; no additional queue. |
| [storage-portability-design.md](../design/storage-portability-design.md) | **Implemented-baseline design rationale.** Illustrative old shapes remain dated; usage/evidence govern actual behavior. New scoped work W05. |
| [pre-code-discovery-2026-09-29.md](../history/plans/pre-code-discovery-2026-09-29.md) | **Historical discovery.** P01–P16 local implementation is recorded in baseline evidence; old deficiencies are not automatically reopened. |
| [storage-plan-alignment-2026-09-29.md](../history/plans/storage-plan-alignment-2026-09-29.md) | **Historical review.** Previous consolidation/handover, superseded by the current plan. |
| [task-resumability-plan.md](../history/plans/task-resumability-plan.md) | **Superseded interpretation.** Agent-task continuation is not the clarified transfer-resume scope. |
| [realtime-multiplayer-plan.md](../history/collaboration/realtime-multiplayer-plan.md), [engineering](../history/collaboration/realtime-multiplayer-engineering.md), [review](../history/collaboration/realtime-multiplayer-plan-review.md), [evaluation](../history/collaboration/realtime-collaboration-evaluation.md), [boundary](../history/collaboration/realtime-repository-boundary.md) | **Transferred history.** Active scope/status live in the [standalone collaboration plan](https://github.com/chester-hill-solutions/gangcode/blob/main/docs/plan.md). No RT work queue in Stow. |
| [remediation-plan.md](../history/plans/remediation-plan.md), [agent-dx-plan.md](../history/plans/agent-dx-plan.md), [agentic-dx-10-plan.md](../history/plans/agentic-dx-10-plan.md), [agent-workspace-plan.md](../history/plans/agent-workspace-plan.md) | **Previously superseded work orders.** Retain as dated design and implementation history; current scope and status route to canonical W01–W28. |
| [deploy-anywhere-plan.md](../history/plans/deploy-anywhere-plan.md), [foss-readiness-plan.md](../history/plans/foss-readiness-plan.md) | **Previously superseded plans.** Preserve as historical design records; current supported scope and status route to the canonical plan. |
| [environment architecture](../history/architecture/stow-environment.md), [environment implementation](../history/architecture/environment-implementation.md) | **Superseded proposals/order.** Existing composition preserved; any ADR/API contradiction is W01, not silent resurrection of old milestones. |
| [work-session-plan.md](../history/plans/work-session-plan.md), [agent-isolate-exploration.md](../history/plans/agent-isolate-exploration.md) | **Retired execution proposals.** Execution/sandbox/control-plane work remains outside the storage plan. |
| [driver-facade-plan-status.md](../history/plans/driver-facade-plan-status.md) | **Retired design.** AWS SDK/shared corpus retained; custom first-party S3-client replacement remains rejected. |
| [history/plan-through-2026-09-28.md](../history/plan-through-2026-09-28.md) | **Earlier frozen queue.** Original dates/status only. |
| [remediation-plan-2026-10-14.md](../history/plans/remediation-plan-2026-10-14.md) | **Historical code-evaluation/remediation record.** The bounded closure register and dated receipts preserve outcomes; no current queue. |
| [2026-10-15-cleanup-release-proposal.md](../reviews/2026-10-15-cleanup-release-proposal.md) | **Dated v0.3.2 release record.** Historical distribution/validation statements only; no current queue. |
| [opencode-pilot-instructions.md](opencode-pilot-instructions.md) | **Operating instructions for a real-agent pilot.** This runbook is not a plan or status authority; pilot scope and current acceptance route to the canonical plan. |
| [previous September 29 queue](../history/plan-before-foundation-consolidation-2026-09-29.md), [previous disposition register](../history/planning-index-before-foundation-consolidation-2026-09-29.md) | **Frozen before this consolidation.** Preserve earlier assertions and mappings; new plan owns present status. |
| [handoff-2026-09-27.md](../history/experiments/handoff-2026-09-27.md) | **Obsolete handoff.** Do not use it as resume instructions. |
| [distribution-spike.md](../history/experiments/distribution-spike.md), [assessment-2026-09-27.md](../history/experiments/assessment-2026-09-27.md), [agent-workspace-pilot.md](../history/experiments/agent-workspace-pilot.md) | **Dated experiments.** No independent work queue or current release claim. |
| [product assessment](../history/assessments/product-assessment-2026-09-29.md), [continuity pilot](../history/experiments/agent-continuity-pilot-2026-09-29.md), [assessment receipts](../assessment-evidence/2026-09-29/README.md) | **Dated evidence.** Preserve failures/workarounds as history and link subsequent fixes; do not mark repaired defects open. |
| [competitive-landscape.md](../design/competitive-landscape.md), [session baseline](../benchmarks/session-baseline.md), [tool baseline](../benchmarks/2026-09-27/tool-performance-baseline.md) | **Dated research/measurements.** Refresh relevant claims when executing W16/W22; not product or performance promises. |

## Reconciliation outcomes recorded on 2026-09-29

- Relocated handoff, default-client checksum, metadata, coordinated publication,
  standing registry policy and standards implementation are locally complete.
  Historical red gates and workarounds remain historical.
- Conditional forwarding/support/refusal is locally complete; richer guarded
  defaults and declared-input ready checks are not implemented.
- Local successful real-model continuation supersedes the older no-model evidence
  for current acceptance. Real interruption, second physical host and Linux remain.
- Namespace ACLs, dependency budgets/reserve, encryption, notification schema/core,
  portable operations/takeover and protected deployments remain new work.
- Account setup, publication, fresh separate provider receipts, anonymous published
  installs and independent repeat use remain separate gates.
- ADR 0010 §§4–5 were visibly amended on 2026-09-29 to retain the qualified
  custom-store seam. W01 defines the admission contract; the canonical queue owns
  guarded-save progress and the remaining legacy authority enforcement gaps.

## References that remain active

Accepted [ADRs](../adr/README.md), [shared admission](../storage-admission-contract.md),
[S3 compatibility](../contracts/compat-contract.md),
[workspace contract](../workspace-contract.md), [preparation manifest](../contracts/task-manifest.md),
[workspace vocabulary](../contracts/workspace-contract-steps.md), [browser persistence](../guides/browser-persistence.md),
[running/probing](../running-and-probing.md), [code standards](../CODE_STANDARDS.md) and
[conformance](../../conformance/CONFORMANCE.md) remain contracts/operating guidance.
README, CONTEXT, package usage, contribution/security/support docs and agent guidance
remain active surfaces. Retirement of a plan does not retire implemented behavior.

## Keeping one plan

- Add/update a W item in the canonical plan before scheduling new work.
- Record completion there with revision/profile/platform evidence; update usage
  and relevant contracts/ADRs when actual behavior changes.
- Preserve old bodies and stable anchors behind prominent historical/reference
  notices. A new document may explain a W item, but cannot create another queue.
- Deferred work needs a named consumer/evidence trigger and explicit scope/contract
  decision; it does not automatically follow W22.
- Documentation validation does not refresh runtime receipts, certify a release,
  deploy a service or complete a consumer pilot.
