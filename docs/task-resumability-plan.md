# Existing task resumability validation plan

**Superseded scope:** the user clarified that the requested resumability concerns
uploads/downloads. Use W06/W21 in [the consolidated plan](storage-foundation-plan.md);
the [transfer recipe](resumable-transfer-plan.md) is retained design detail.
The body below records the earlier agent-task interpretation and does not schedule
new implementation or consumer integration.

**Status:** proposed validation and adoption detail, corrected 2026-09-29.
Core resumability is already implemented in this development tree, and local
agent continuation has passed. This expands **S3-3 and
S4-1–S4-3** in the [canonical storage plan](plan.md); that plan retains scope,
ordering, status and release authority.

The goal is to let a person stop working with an agent and later continue with
a fresh agent, including on another supported computer, without repeating the
original explanation. Stow preserves the files and verifies their transfer. The
caller records enough task context to make those files useful and decides what
execution may safely happen next.

The first deliverable is stronger evidence and a clear usage recipe for the
existing OpenCode workflow. Use the current APIs and caller as they stand, and
change code only where a concrete exercise exposes a gap. GangCode and Canada411
are follow-up consumers, each with its own execution and progress rules.

## What must resume

Three different promises need different evidence:

| Promise | Meaning | Scope of this plan |
| --- | --- | --- |
| Data continuity | Saved files and supported object state can be reopened. | Reuse Stow's implemented portable checkpoint and adoption APIs. |
| Task continuity | A fresh worker understands the objective, verified progress, uncertainties and next useful step. | Existing notes-based continuation has local evidence; validate it on stronger tasks and consumers. |
| Process continuity | A running program resumes its exact memory and execution position. | Outside this milestone. |

The task survives through explicit saved information. Model memory, a private
chat history, credentials and external effects are not reconstructed. A receiver
must supply its own runner, model access and required toolchain.

## Existing foundation and missing evidence

The current development tree already has preparation, durable workspaces,
portable v2 checkpoints, integrity checks, export/adopt, saved working-directory
information and local MCP tools. The OpenCode controller has persisted prompt
admission, bounded capture retry and reconciliation of an uncertain save reply.
It writes execution facts to `STOW_PROGRESS.json` and records task-file changes.
Its `recover` operation reconciles a pending save; it does not resume task
execution or rerun the prompt.

The package already exposes Go `Resume`, `RestoreCheckpoint`, `ExportHandoff`
and `AdoptHandoff`, plus TypeScript/Python wrappers for workspace resume,
checkpoint, restore and handoff/adoption. The current OpenCode `run.mjs` can
execute a supplied continuation prompt against an adopted workspace with fresh
caller state. Neither a new storage API nor a new continuation entry point is
required to test task resumability.

The [local model pilot](opencode-receiver-remediation-2026-09-29.md) demonstrated
continuation from adopted files and notes, with independently checked output.
However, the receiver was given a task-specific prompt spelling out the next
calculation. This is narrower evidence than independently deriving the next
step from a complete saved handover.

Missing evidence includes interruption during real work, a fresh receiver using
only a generic continuation instruction, second-host continuation, and repeated
use on a meaningful task. The candidate remains unpublished. Existing release,
provider and platform gates remain separate and open.

## Ownership

Follow [ADR 0014](adr/0014-storage-product-caller-owned-execution.md).

| Owner | Responsibility |
| --- | --- |
| Stow | Store and capture supported state, verify checkpoint integrity, inspect differences, transfer/adopt, and enforce documented storage limits. |
| Caller integration | Maintain task context, coordinate writers, request and confirm saves, select the recovery point, construct the next agent's context, and control execution admission. |
| Agent | Propose progress notes, explain uncertainties, identify outputs and suggest the next step. Its claims require caller or independent verification where applicable. |
| User or application policy | Set task scope, authorize external actions, and resolve ambiguous effects or blocked continuation. |

Use ordinary captured files first. Do not add a Stow task registry, scheduler,
mandatory attempt model, chat codec or sandbox. No checkpoint/archive schema or
ADR amendment is needed for the initial recipe. Any later public context schema
requires its own compatibility and size/sensitivity design.

## The saved handover

Use the existing input manifest and ordinary captured files. Review whether the
consumer already saves the information below before introducing any new file.
Paths are example conventions, not required Stow API names or a new package
contract. Existing notes may combine the brief and progress in one file.

| File or data | Required information |
| --- | --- |
| `TASK.md` | Objective, scope, constraints, expected outputs, completion checks and authorization boundaries. |
| `STOW_NOTES.md` | Completed work with evidence; unfinished work; uncertainties; relevant decisions; output paths; the next useful step; prerequisites and any external action requiring reconciliation. |
| `STOW_PROGRESS.json` | Existing caller-observed execution outcome and changed-file facts; inspect and reuse it. |
| Task files and fixtures | Inputs, partial results, completed outputs and the exact small data set needed to continue. |
| Caller state outside the workspace | Original save request/options, last confirmed checkpoint and local admission/recovery state. This is operational state, not a credential-bearing portable handover. |

Keep the brief and notes next to the recorded working directory, or explicitly
record how to locate them from it. Use relative references. Record repository
base and dependencies through existing provenance and declared files; `.git`
and installed tools do not automatically travel.

Use Objective, Verified progress, Outputs, Remaining work, Decisions and
uncertainties, External actions, Prerequisites, and Next step as a review checklist
for existing notes. Add a template or bounded reader only if a reproduced missing
context problem makes it necessary. Missing evidence must be stated as unknown,
not presented as completed work. The first pilot uses only local file effects.

The checkpoint captures the handover and task bytes together after known writers
have stopped. The current checkpoint ID is recorded by the caller after commit,
not inserted retroactively into the captured files. Any captured previous ID
remains historical. Imported old session IDs are provenance and never reconnect
the receiver to the sender's agent.

## Save and continuation procedure

1. Prepare a workspace with the brief, notes template and selected inputs. Save
   an initial portable checkpoint before starting work.
2. Admit a turn through the existing caller. At a planned boundary, have the
   agent update its notes while it still has context. Wait for it to settle,
   stop known writers and record caller-observed execution facts.
3. Validate required handover sections and references. If notes are stale or
   absent, preserve recoverable data but mark task continuation as needing
   review; do not call a storage-only save a complete handover.
4. Persist the capture request and exact options. Use existing capture/resolve
   with bounded retries. Report a save only after `outcome=committed`, and keep
   admission closed if the outcome remains uncertain.
5. Continue locally from an explicitly selected confirmed checkpoint, or export
   and adopt a portable bundle. Preserve modified original work until an explicit
   decision chooses restoration or a new capture; never silently overwrite it.
6. Inspect the restored handover, exclusions, working directory and prerequisites.
   Validate referenced required outputs against the selected checkpoint. Stop
   before a model call if required state is missing or incompatible.
7. Establish fresh local caller state and capture a recipient baseline before
   creating a local parent chain. Use a new agent session and receiver-provided
   credentials. Interpret old execution facts as history, not as an active lock
   or an instruction to replay a prompt.
8. Give the new agent a generic instruction to read the brief and notes, verify
   relevant outputs, and continue the next permitted step. Independently check
   its result, update the handover, and confirm a new checkpoint.

On abrupt interruption, use the last confirmed checkpoint unless the caller can
confirm writer shutdown and explicitly capture the remaining files. Partial
files and old notes may disagree; the receiver must inspect them and cannot
infer completion from file existence alone. If shutdown cannot be established,
hold admission and report the unresolved owner instead of pretending the
workspace is ready.

External actions need an application-specific receipt or lookup mechanism.
Record known, unknown and pending effects, but do not add generic exactly-once
execution to Stow. A saved instruction to send a message is not evidence that
the message was or was not sent. Ambiguous effects block dependent actions
until the caller reconciles them.

## Validation sequence

Each slice starts with the existing implementation and produces acceptance
evidence. A failed exercise must identify whether the problem is missing task
information, storage behavior, runner behavior, environment setup or model output.
Only then make the smallest necessary change in the owning layer. Preserve current
capture/retry semantics and extend existing tests rather than duplicate the controller.

| Slice | Work and likely location | Acceptance |
| --- | --- | --- |
| 1 Inventory existing behavior | Map exported package operations, the caller's progress/notes and current tests. Document the existing resume procedure and its evidence. | Existing functionality is clearly distinguished from unknown behavior and actual missing features. No new task format or reader is assumed necessary. |
| 2 Verify coherent saves | Exercise `controller.mjs`, `run.mjs`, existing file-change checks and capture receipts using current notes. Preserve the no-op review guard. | Settled work, notes and execution facts are captured together. Save failure blocks another prompt. A storage retry never executes the agent again. The report distinguishes a saved checkpoint from the adequacy of its task context. |
| 3 Strengthen the fresh-agent proof | Use existing preparation, adoption and `run.mjs` with fresh caller state and a generic continuation prompt. Add a stronger fixture/test rather than a new resume subsystem. | Original tree, registry and agent state can be removed. A new session completes the next step using only saved task information and receiver configuration. |
| 4 Exercise interruptions | Extend current controller, native continuity and real-model coverage for the remaining interruption and receiver failure scenarios. Fix only reproduced gaps. | Known-stop partial work remains labelled partial. Unknown owner/effects prevent unsafe continuation. Confirmed receipts resolve without prompt replay. |
| 5 Move to another host | Run the unedited bundle on a second physical host, including Linux runtime evidence. Verify toolchain/base requirements and destination-local identity. | Independently checked work continues without sender paths, caller state or storage credentials. Source-build evidence and published-install evidence are reported separately. |
| 6 Test actual consumers | Pilot GangCode, then compare against Canada411's existing resume mechanism using small synthetic data. Keep adapters in their consumer repos. | Demonstrate useful recovery with measurable effort/cost; preserve each consumer's execution rules. Expand only after a recorded benefit. |

Slices 1–4 expand S3-3; slice 5 also supplies relevant S2-2 evidence, without
closing its publication gate. Slice 6 expands S4-1; repeated independent use and
the investment decision remain S4-2/S4-3. None overrides S0–S2 dependencies.

## First acceptance task

Use a synthetic data-analysis task with at least two dependent stages. The sender
validates input and writes a checked intermediate result plus a handover. The
receiver verifies that result, finishes the remaining analysis and writes a
report. Include an intentionally unfinished item so restarting from scratch or
claiming everything is done is observable.

The receiver prompt must not repeat the calculation, expected answer or remaining
task details. It may identify the handover files and instruct the agent to
continue within the recorded scope. Expected results stay in the independent
test oracle, outside the agent's context. Keep the existing arithmetic pilot as
a narrower regression rather than relabel it as this stronger proof.

Pass requires correct final output, preservation of prior verified work, no
duplicate simulated external action, a new confirmed checkpoint, and exact
restoration of its expected artifacts. A terminal `succeeded` value or changed
file alone does not satisfy task acceptance. Existing successful read-only/no-op
turns remain review-gated until a separately specified evidence rule supports them.

## Failure and recovery checks

| Situation | Required behavior |
| --- | --- |
| Agent stops during a file edit | Retain the last confirmed checkpoint; treat newer files as partial until inspected and explicitly captured. |
| Capture commits but its reply is lost | Resolve the original request and checkpoint; never replay the agent prompt. |
| Quota, disk or capture deadline prevents saving | Show the last confirmed recovery point and block the next turn. |
| Brief, notes or referenced output is absent | Preserve available data and report the missing handover; do not start unattended continuation. |
| Saved notes disagree with task outputs | Require inspection/reconciliation; agent claims cannot overrule evidence. |
| Archive is corrupt or required input is excluded | Refuse corruption; report exclusions and missing requirements before execution. |
| Recipient has no required toolchain or Git base | Report the exact missing prerequisite without fetching or installing from transported instructions automatically. |
| Work changed after the selected checkpoint | Preserve that work and require an explicit source-state choice. |
| An external action may already have happened | Reconcile with the owning service or caller; block dependent replay while unknown. |
| Multiple agents still write | Require caller-controlled settlement of all known writers; file inactivity is insufficient. |

## Consumer pilots

**GangCode:** use the public Stow CLI/API from the standalone collaboration repo.
At a confirmed global pause, write a room handover with objective, participant
contributions, dependencies, pending work and verified outputs; save it with the
shared files. Reopening creates fresh participants and local runtime state.
Presence, cursors, leases, queued controls and observation coverage are not live
merely because a historical file contains them. Start with a completed work
boundary before testing coordinator interruption. This is a proposed consumer
adapter, not a transfer of collaboration ownership back into Stow.

**Canada411:** preserve a small coherent set of scraper progress files, outputs
and required inputs after the scraper stops. Restore it elsewhere and let the
existing scraper choose which items remain. Use recorded/synthetic HTTP fixtures
first. Compare a Stow bundle with copying those files directly. Do not replace
the scraper's existing resumability, package the entire raw-data collection, or
claim browser/network execution is restored. The adapter is worthwhile only if
integrity, relocation or inspection improves the existing workflow.

Campaign Brain and BranchWeave remain later candidates. Their application state
and execution contracts need a separate consumer-specific design. S3 fixture
adoption in CHS packages or CallCaster remains useful but is a distinct proof.

## Verification and adoption decision

Use existing Node/controller regressions, native portable continuity tests and
the opt-in real-model acceptance target. Add tests only for uncovered resumability
claims or reproduced failures. Run `make test-agent` for caller changes and the current
required storage/quality gates when shared code changes; run `make test-all` and
`make standards` before a candidate release. Record required model/provider
tests as unavailable when no profile exists, not as a successful skip.

For each pilot record revision, platform, runner/model, task, checkpoint/bundle
identity, independent result checks, interventions, resume time, repeated work,
capture time, retained bytes and the tool the user would otherwise have used.
Keep credentials and private caller state out of the evidence and archive.

Build on the already passed local fresh-agent continuation: strengthen its task
context proof, verify an interrupted task, then qualify second-host continuation.
Product acceptance still needs
two independent teams repeatedly choosing the workflow. If users still have to
re-explain the task, identify which saved information was missing. If plain
folders and existing progress files work just as well, narrow the product claim
before building more infrastructure.
