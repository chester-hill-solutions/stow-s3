# Planning index and disposition register

**Reconciled:** 2026-09-29 against `5485a36`, source inspection and the recorded
assessment. This register explains where old work went; it is not a second backlog.
The reconciliation itself changed documentation only. The subsequent local 0.3.0
implementation is recorded in [implementation evidence](implementation-2026-09-29.md);
no release has been certified.

## Start here

1. [Canonical storage plan](plan.md): the only engineering scope/order/status queue,
   with stable S0–S4 item IDs and the current release acceptance checklist.
2. [Product milestone](portable-agent-workspace-goal.md): workflow and adoption criteria.
3. [MCP/OpenCode integration](mcp-storage-integration-plan.md): S3 technical detail.
4. [Portable workspace usage](portable-workspace-usage.md): concrete development API and examples.
5. [Implementation evidence](implementation-2026-09-29.md): checks, costs and open gates.
6. [ADR index](adr/README.md): decisions in force, especially ADR 0014's storage boundary.

The milestone and MCP document expand the canonical plan; they do not schedule
independent work. Old release versions, unchecked boxes, “next steps” and claims of
authority are frozen historical text. Follow the destination below instead.

## Document inventory

| Document | Disposition and current destination |
| --- | --- |
| [portable-workspace-usage.md](portable-workspace-usage.md) | **Current usage.** Concrete local development APIs, commands and boundaries. |
| [implementation-2026-09-29.md](implementation-2026-09-29.md) | **Implementation evidence.** Local tests, performance and remaining release/pilot gates. |
| [compatibility-coverage-2026-09-29.md](compatibility-coverage-2026-09-29.md) | **Coverage map.** Shared SDK and focused protocol scenarios with explicit unsupported boundaries. |
| [plan.md](plan.md) | **Active work order.** S0 correctness, S1 coherent storage, S2 delivery, S3 MCP/OpenCode, S4 evidence. |
| [portable-agent-workspace-goal.md](portable-agent-workspace-goal.md) | **Active milestone.** Storage workflow and repeat-use decision. |
| [mcp-storage-integration-plan.md](mcp-storage-integration-plan.md) | **Active detail.** Expands S3, including separate workspace, selected context and bounded retry/failure. |
| [storage-portability-design.md](storage-portability-design.md) | **Design rationale.** Expands S0-7/S1; concrete implemented APIs and limits are in portable-workspace-usage.md. |
| [pre-code-discovery-2026-09-29.md](pre-code-discovery-2026-09-29.md) | **Discovery/handover record.** Verified source observations, proposed choices and slices mapped to canonical IDs; new findings require runtime regressions. |
| [storage-plan-alignment-2026-09-29.md](storage-plan-alignment-2026-09-29.md) | **Review record.** Explains consolidation and engineering handover; use plan IDs for status. |
| [remediation-plan.md](remediation-plan.md) | **Superseded.** R0–R11 sequence/version targets retired; contract acceptance and release requirements carried to S0 and S2. |
| [agent-dx-plan.md](agent-dx-plan.md) | **Superseded.** Revision backlogs, timings and sketches preserved as history; remaining storage/distribution work mapped below. |
| [agentic-dx-10-plan.md](agentic-dx-10-plan.md) | **Superseded.** Old version ladder, storage rewrite and broad integration commitments retired. |
| [agent-workspace-plan.md](agent-workspace-plan.md) | **Superseded.** Preparation/capture implementation record retained; residuals in S0–S4. |
| [deploy-anywhere-plan.md](deploy-anywhere-plan.md) | **Superseded.** Measurements retained; runtime/cache/persistence expansion conditional below. MCP now explicitly scheduled in S3. |
| [foss-readiness-plan.md](foss-readiness-plan.md) | **Superseded.** Completed contribution work retained; release/docs/maintenance residuals in S0/S2. |
| [architecture/stow-environment.md](architecture/stow-environment.md) | **Superseded proposal.** Reuse/composition rationale preserved. ADRs and implemented contracts govern; its roadmap has no authority. |
| [architecture/environment-implementation.md](architecture/environment-implementation.md) | **Superseded work order.** Completed requirements and stale intermediate tables retained as history; unapproved authority redesign deferred. |
| [work-session-plan.md](work-session-plan.md) | **Retired execution proposal.** E0 storage work carried forward; E1–E5 deferred under ADR 0014. |
| [agent-isolate-exploration.md](agent-isolate-exploration.md) | **Retired exploration.** No sandbox or runner commitment. Requires an explicit scope decision to reactivate. |
| [driver-facade-plan-status.md](driver-facade-plan-status.md) | **Retired design record.** Custom S3 client remains rejected; surviving corpus work in S0-4. |
| [history/plan-through-2026-09-28.md](history/plan-through-2026-09-28.md) | **Historical snapshot.** Superseded queue; measurements and implementation evidence retained. |
| [handoff-2026-09-27.md](handoff-2026-09-27.md) | **Obsolete handoff.** Never use it as instructions to resume current work. |
| [distribution-spike.md](distribution-spike.md) | **Historical experiment.** Packaging feasibility and old failures retained; current delivery work S2. |
| [assessment-2026-09-27.md](assessment-2026-09-27.md) | **Historical assessment.** Includes subsequent repair record; findings are not automatically reopened. |
| [agent-workspace-pilot.md](agent-workspace-pilot.md) | **Historical local pilot.** Remaining clean-install/cross-host/real-user proof lives in S2–S4. |
| [competitive-landscape.md](competitive-landscape.md) | **Dated research.** Recommendations do not schedule work; refresh material claims for S4 decisions. |
| [product-assessment-2026-09-29.md](product-assessment-2026-09-29.md) | **Dated assessment.** Current baseline evidence and limitations; actions owned by S0–S4. |
| [agent-continuity-pilot-2026-09-29.md](agent-continuity-pilot-2026-09-29.md) | **Dated experiment.** Synthetic same-host continuity, not real-agent or cross-host proof. |
| [assessment evidence](assessment-evidence/2026-09-29/README.md), [session baseline](benchmarks/session-baseline.md), [tool baseline](benchmarks/2026-09-27/tool-performance-baseline.md) | **Evidence only.** Preserve run dates, environments, failures and limitations; no performance promise or work queue. |

### References that remain active

These are contracts or operating instructions, not obsolete plans:

- [S3 compatibility](compat-contract.md), [workspace contract](workspace-contract.md),
  [preparation manifest](task-manifest.md), [workspace step vocabulary](workspace-contract-steps.md).
- [Browser persistence](browser-persistence.md), [running and probing](running-and-probing.md),
  [code standards](CODE_STANDARDS.md), [conformance](../conformance/CONFORMANCE.md).
- README, CONTEXT, package docs, contribution/security/support docs, site and agent skill.

Preserve distinctions between specified and shipped behavior, including implemented-but-unpublished portable storage and experimental agent integration. References to old plans in ADR context or evidence remain
historical citations; the current scheduling authority is ADR 0014 and `plan.md`.
The retired driver-facade document records an external OpenCode plan path unavailable
in this checkout; its documented findings are accounted for without claiming that
external file was inspected or modified.

## Carry-forward map

All destinations are IDs in [the canonical plan](plan.md). Rows group equivalent
findings across old documents so engineering fixes one implementation once.

| Old source/items | Disposition and destination |
| --- | --- |
| Sept 29 assessment checksum/metadata/handoff failures | Confirmed open → **S0-1**; no workaround accepted as closure. |
| Remediation R7/R9; workspace Phases 0–1; work-session E0 provider cases | Preserve implemented production composition and authority paths; scope/conflict/restart/provider evidence → **S0-2, S2-3**. |
| Workspace Phase 3; DX safety sections; FOSS G1–G3; work-session E0 filesystem hardening | Existing ownership and leaf-symlink fixes retained; supported path/race boundaries and residual audit → **S0-3**. |
| Retired driver surviving findings; remediation R1/R4–R6; DX W2; FOSS C1/C2 | Existing harnesses retained; shared SDK corpus gaps → **S0-4**. Local `--base-host` and range cases already exist. |
| DX §0.6/W6/§7; 10-plan Phase 1 endpoint policy | Body cap/timeouts and runtime quotas exist; configurable effective limits, transient allocation/concurrency and endpoint-policy decision → **S0-5**. |
| Remediation R-Standards; FOSS E1/E2/F3/F4 | Gates and schema checks exist. Red file-size gate, residual decomposition and meaningful coverage work → **S0-6**. |
| FOSS B3; workspace facade contract; environment composition rationale | Same-runtime workspace endpoint → **S1-1**. No custom S3 client or duplicate storage. |
| Workspace Phases 3–4; history checkpoint/archive/delta rows; work-session E0 | Portable metadata/modes/deletions/sensitive exclusions/Git reconstruction → **S1-2, S1-3**. Existing delta CLI and language transports are retained. |
| History retention row; work-session E0 standing-policy item | Explicit standing registry policy and retained cap/collection contracts → **S1-4**. No implicit cleanup daemon. |
| DX §11 soak/performance; history performance evidence; FOSS E3; local pilot limits | Reuse completed local lifecycle soak; long-lived disk, max-object/concurrency memory and repeated capture measurements → **S1-4, S1-5, S2-2**. Old scale/time targets are unverified, not release promises. |
| Remediation R8/R10/R11; DX W14/W15 and §12; FOSS A2/A3/A5; distribution spike §7 | Publication/accounts, immutable tags, version alignment, clean installs and fresh release verification → **S2-1, S2-2, S2-3**. |
| FOSS A4/F1/F2; DX W13; workspace Phase 6 | Working documentation destination, newcomer/install and agent guidance → **S2-4, S3-2**. |
| FOSS D5; remediation legacy-format decision | Actionable upgrade diagnostics and release notes → **S2-5**. No migration silently added. |
| FOSS D1–D4 | Contribution/security/templates/dependency updates and removal of inert permission already delivered; maintain through **S2-5**, do not rebuild. |
| DX W17; workspace Phase 5; deploy-anywhere Phase 7 MCP | Earlier broad tool proposals replaced by approved local MCP/OpenCode detail → **S3-1–S3-3**. |
| DX W16 displacement/pilots; workspace Phase 6; Sept 29 product assessment | Replace speculative campaign with fixture/agent comparison and independent repeat use → **S4-1, S4-2**. |
| Earlier research and runner/isolate recommendations | Storage-first decision supersedes automatic execution investment → **S4-3** and deferred register below. |

## Deferred and rejected work

Deferred does not mean scheduled after S4. Reopening requires the trigger below,
an explicit entry in the canonical plan and any necessary contract/ADR amendment.

| Work | Disposition / trigger |
| --- | --- |
| Work-session E1–E5, execution TaskSpec/Attempt/Turn registries, model broker, CPU/memory allocation API, sandbox backend | **Deferred outside scope.** Demonstrated consumer need plus explicit new product decision. Existing input manifest remains supported. |
| Full chat/process-memory restoration, typed caller context, richer delta authorship | **Deferred.** Selected ordinary context files first; justify a consumer and versioned bounded transport before extending schemas. |
| Hosted/remote MCP, accounts, scheduler, shared control plane | **Deferred.** Validated remote/team need and explicit operational scope decision. Local stdio MCP is already scheduled. |
| Additional edge/persistence backend; in-process TS/Python workspace handles; async Python; pools | **Deferred.** Named host/application requirement not satisfied by supported storage surfaces. Existing browser persistence stays supported. |
| WASM runtime/compiler replacement; incremental cache index; streaming/storage-v3 rewrite; deduplication | **Deferred.** Measured memory/latency/disk requirement that current implementation cannot meet; preserve existing performance evidence. |
| SSE-S3 wire acceptance | **Deferred.** Demonstrated SDK compatibility requirement and explicit semantic contract; header acceptance alone is not encryption. |
| Per-caller authority/multi-environment descriptor and tri-state capability redesign | **Deferred.** Concrete API consumer need and enforcement/versioning design; do not reopen completed environment corrections. |
| Offline uncached-read error change | **Deferred compatibility decision.** Document current refusal/unknown-versus-absent limitation; change only with an explicit client requirement. |
| Broad Windows/edge process qualification, submodules/LFS | **Deferred expansion.** Concrete workload and validation budget. Preserve existing supported targets and narrower CI checks. |
| Unified first-party TypeScript S3 client / replacement of existing entry points | **Retired design.** Pinned AWS SDK compatibility and additive session APIs remain constraints; revival requires a new architecture decision. |
| Mandatory migration tool, old release ladders/deadlines, unconditional competitor integrations/displacement campaign | **Retired commitments.** No active obligation; new proposals need evidence and scope. |

## Preventing another set of competing plans

- New implementation proposals must identify the canonical item they expand.
- Record completion with code/evidence at that item; do not update historical bodies
  to look current or infer completion from an old checkbox.
- A new scope decision updates the canonical plan and affected contracts/ADRs together.
- Keep old paths and headings stable for links. Their leading retirement notice
  applies to the entire retained body, including previously authoritative language.
- Evidence remains dated. Re-run required validation for the candidate release;
  documentation reconciliation does not turn historical passes into current ones.

The later September 29 discovery adds S0-7 capture/publication prerequisites and
clarifies S1 object payload preservation, OpenCode v2 lifecycle and S2 registry
requirements. It expands existing scope through the named active details above;
retired plan bodies remain frozen.
