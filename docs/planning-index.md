# Planning index and disposition register

**Reconciled:** 2026-09-29 against HEAD f549276 plus the current development tree,
accepted ADR/interview decisions and later implementation/model evidence. This
register classifies documents; it is not an implementation backlog.

## Start here

1. [Consolidated working-storage plan](storage-foundation-plan.md): the sole
   scope/order/status queue, W01–W22 and the complete old S0–S4 disposition map.
2. [Portable workspace usage](portable-workspace-usage.md): actual APIs, commands
   and limits in the unpublished development tree.
3. [ADR index](adr/README.md): decisions in force; the custom-store conflict in
   ADR 0010 is explicitly tracked by W01.
4. [Baseline implementation evidence](implementation-2026-09-29.md),
   [later receiver/model evidence](opencode-receiver-remediation-2026-09-29.md) and
   [conditional slice evidence](guarded-storage-plan.md#verification-of-the-conditional-write-prerequisite):
   dated records with distinct scope. None certifies publication or Linux runtime.

The new plan replaces the previous canonical queue and all independent ordering
in the detail proposals. It carries the product definition of done, accepted
foundation contracts, implementation slices, qualification/release/adoption gates
and deferred boundary together. Existing contracts and ADRs remain normative.

## Document inventory

| Document | Current disposition |
| --- | --- |
| [storage-foundation-plan.md](storage-foundation-plan.md) | **Sole active work order.** Baseline, W01–W22, dependencies, acceptance and legacy traceability. |
| [gangcode-stow-boundary.md](gangcode-stow-boundary.md) | **Ownership/integration reference.** Storage facts and durable delivery belong to Stow; agent meaning, context and coordination belong to GangCode. No independent queue. |
| [plan.md](plan.md) | **Routing page.** Preserves old entry point/anchors; no separate queue. |
| [portable-workspace-usage.md](portable-workspace-usage.md) | **Current usage.** Concrete supported development APIs, commands and limits. |
| [implementation-2026-09-29.md](implementation-2026-09-29.md) | **Dated baseline evidence.** Earlier aggregate validation and costs; later evidence is separately linked. |
| [opencode-receiver-remediation-2026-09-29.md](opencode-receiver-remediation-2026-09-29.md) | **Dated later evidence.** Local meaningful model continuation complete; interruption/second-host/Linux remain W19. |
| [compatibility-coverage-2026-09-29.md](compatibility-coverage-2026-09-29.md) | **Evidence/coverage map.** Existing SDK/protocol coverage; uncovered work belongs to W06/W16/W20. |
| [portable-agent-workspace-goal.md](portable-agent-workspace-goal.md) | **Superseded milestone ordering; retained rationale.** Product/workflow/adoption acceptance absorbed into the new plan and W19/W21/W22. |
| [mcp-storage-integration-plan.md](mcp-storage-integration-plan.md) | **Superseded detail work order; retained design history.** Actual adapter/caller already implemented; remaining acceptance W19, usage/source govern concrete names. |
| [resumable-transfer-plan.md](resumable-transfer-plan.md) | **Retained transfer design reference.** Same-store SDK continuation W06; consumer recipe W21. No independent checklist. |
| [guarded-storage-plan.md](guarded-storage-plan.md) | **Retained contract/slice evidence.** Conditional forwarding implemented locally; richer identity/save W01/W02 and input basis W09. |
| [workspace-notifications-plan.md](workspace-notifications-plan.md) | **Retained notification design reference.** Core observation/readiness/codecs/diffs/webhooks W08–W11; portable state W12. Proposed API bodies are not shipment claims. |
| [protected-cache-plan.md](protected-cache-plan.md) | **Retained broad storage design reference.** Access W04/W05, encryption W07/W14/W15, host qualification W17/W18. Cache is a profile. |
| [portable-recovery-plan.md](portable-recovery-plan.md) | **Retained recovery design reference.** Holds/budgets W03, sections/takeover W12/W13 and envelopes W14/W15. Confirmed-takeover default is settled; proof is engineering work. |
| [foundation-review-2026-09-29.md](foundation-review-2026-09-29.md) | **Superseded review/order.** Findings/defaults absorbed into the new plan; no additional queue. |
| [storage-portability-design.md](storage-portability-design.md) | **Implemented-baseline design rationale.** Illustrative old shapes remain dated; usage/evidence govern actual behavior. New scoped work W05. |
| [pre-code-discovery-2026-09-29.md](pre-code-discovery-2026-09-29.md) | **Historical discovery.** P01–P16 local implementation is recorded in baseline evidence; old deficiencies are not automatically reopened. |
| [storage-plan-alignment-2026-09-29.md](storage-plan-alignment-2026-09-29.md) | **Historical review.** Previous consolidation/handover, superseded by the current plan. |
| [task-resumability-plan.md](task-resumability-plan.md) | **Superseded interpretation.** Agent-task continuation is not the clarified transfer-resume scope. |
| [realtime-multiplayer-plan.md](realtime-multiplayer-plan.md), [engineering](realtime-multiplayer-engineering.md), [review](realtime-multiplayer-plan-review.md), [evaluation](realtime-collaboration-evaluation.md), [boundary](realtime-repository-boundary.md) | **Transferred history.** Active scope/status live in the [standalone collaboration plan](https://github.com/chester-hill-solutions/gangcode/blob/main/docs/plan.md). No RT work queue in Stow. |
| [remediation-plan.md](remediation-plan.md), [agent-dx-plan.md](agent-dx-plan.md), [agentic-dx-10-plan.md](agentic-dx-10-plan.md), [agent-workspace-plan.md](agent-workspace-plan.md) | **Previously superseded work orders.** Baseline repairs retained; remaining storage/workload/release/consumer gates are in W01–W22. Old versions/checklists do not schedule work. |
| [deploy-anywhere-plan.md](deploy-anywhere-plan.md), [foss-readiness-plan.md](foss-readiness-plan.md) | **Previously superseded.** Narrow native/Workers/Railway scope W17/W18; release/docs/maintenance W20. No blanket host expansion. |
| [environment architecture](architecture/stow-environment.md), [environment implementation](architecture/environment-implementation.md) | **Superseded proposals/order.** Existing composition preserved; any ADR/API contradiction is W01, not silent resurrection of old milestones. |
| [work-session-plan.md](work-session-plan.md), [agent-isolate-exploration.md](agent-isolate-exploration.md) | **Retired execution proposals.** Execution/sandbox/control-plane work remains outside the storage plan. |
| [driver-facade-plan-status.md](driver-facade-plan-status.md) | **Retired design.** AWS SDK/shared corpus retained; custom first-party S3-client replacement remains rejected. |
| [history/plan-through-2026-09-28.md](history/plan-through-2026-09-28.md) | **Earlier frozen queue.** Original dates/status only. |
| [previous September 29 queue](history/plan-before-foundation-consolidation-2026-09-29.md), [previous disposition register](history/planning-index-before-foundation-consolidation-2026-09-29.md) | **Frozen before this consolidation.** Preserve earlier assertions and mappings; new plan owns present status. |
| [handoff-2026-09-27.md](handoff-2026-09-27.md) | **Obsolete handoff.** Do not use it as resume instructions. |
| [distribution-spike.md](distribution-spike.md), [assessment-2026-09-27.md](assessment-2026-09-27.md), [agent-workspace-pilot.md](agent-workspace-pilot.md) | **Dated experiments.** No independent work queue or current release claim. |
| [product assessment](product-assessment-2026-09-29.md), [continuity pilot](agent-continuity-pilot-2026-09-29.md), [assessment receipts](assessment-evidence/2026-09-29/README.md) | **Dated evidence.** Preserve failures/workarounds as history and link subsequent fixes; do not mark repaired defects open. |
| [competitive-landscape.md](competitive-landscape.md), [session baseline](benchmarks/session-baseline.md), [tool baseline](benchmarks/2026-09-27/tool-performance-baseline.md) | **Dated research/measurements.** Refresh relevant claims when executing W16/W22; not product or performance promises. |

## Reconciliation outcomes

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

Accepted [ADRs](adr/README.md), [shared admission](storage-admission-contract.md),
[S3 compatibility](compat-contract.md),
[workspace contract](workspace-contract.md), [preparation manifest](task-manifest.md),
[workspace vocabulary](workspace-contract-steps.md), [browser persistence](browser-persistence.md),
[running/probing](running-and-probing.md), [code standards](CODE_STANDARDS.md) and
[conformance](../conformance/CONFORMANCE.md) remain contracts/operating guidance.
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
