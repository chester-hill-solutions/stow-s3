# Product goal: portable working storage for agents and tools

> **Consolidated on 2026-09-29; reference only.** The [new working-storage plan](storage-foundation-plan.md) is the sole work order. Product workflow and definition of done are consolidated into the new plan, including W19/W21/W22. The milestone body is retained rationale, not another schedule. Former checklists, phase numbers and next steps below do not schedule work.

**Status:** current storage milestone, aligned 2026-09-29. [The canonical plan](plan.md) owns implementation order; [ADR 0014](adr/0014-storage-product-caller-owned-execution.md) owns the product boundary. Earlier runner-oriented wording is superseded.

This document defines success for S0–S4; it is not an independent backlog. The
[planning index](planning-index.md) records which old plans were retired and where
their remaining work was carried forward.

The [local 0.3.0 implementation](implementation-2026-09-29.md) now includes portable
object payloads and metadata, shared capture/publication guarantees, local MCP and
an experimental OpenCode caller. Public release, successful real-model cross-host
continuation and independent adoption remain open. The goal below defines acceptance
for the completed product; [usage](portable-workspace-usage.md) describes the current
API and limitations.

## Goal

**Make a working set of files and object data easy to prepare, checkpoint, inspect, move and reopen—and demonstrate that two independent teams repeatedly choose Stow for that workflow.**

Agents, developer tools, applications and tests are consumers of this one storage product. The first proof should pair an S3 fixture workflow with one thin integration into an existing agent runner. Stow need not launch or supervise the agent.

The customer promise is: preserve the data and selected context needed to pick up work elsewhere, with explicit supported semantics and no reconstruction of the sender's paths or storage credentials.

## Ownership

| Stow owns | The caller or execution environment owns |
| --- | --- |
| Declared input preparation and durable workspace identity | Task execution, toolchain installation and process lifecycle |
| Supported directory and S3 access | Agent/model selection and model credentials |
| Verified capture, diff, export/adopt and restore | Turn detection and quiescing writers |
| Storage retention, quotas at their documented enforcement points, cleanup | CPU/memory/network enforcement and sandboxing |
| Preservation of explicitly saved context as data | Interpretation of progress, task success and external side effects |

Existing scoped sessions may still start a native S3 server. That process is the storage service, not an agent executor.

## The complete workflow

1. Install supported Stow artifacts outside the source checkout.
2. Prepare a working directory and selected object inputs with their required metadata. Record source identity and the supported runtime requirements as data.
3. Use the ordinary filesystem and the completed workspace S3 facade. An external application, test or agent runner performs the work.
4. At a caller-declared stable boundary, save selected progress/context files and request a checkpoint. The caller marks it saved only after publication succeeds.
5. Inspect changed files and outputs, including deletions.
6. Export, move and adopt the saved state into a new directory/registry on a supported destination.
7. Reopen the storage and continue using the caller's own execution tools. Captured inputs require no sender storage credentials or source location.

For coding work, exact Git base acquisition and reconstruction must be explicit; current checkpoints exclude `.git`. For object fixtures, object metadata preservation must be specified; file checkpoints are not automatically complete S3 snapshots.

## Every-turn checkpointing

This is an optional caller integration, not a Stow scheduler. The first recipe can use ordinary declared files for task brief, progress and saved context, followed by the existing checkpoint operation. Reuse `CheckpointOf` or its CLI wrapper; it already captures a workspace another process holds.

The [local MCP storage adapter](mcp-storage-integration-plan.md) makes these operations discoverable and provides a common workflow guide. Host-triggered saves use an explicit runner hook and can support a verified every-turn contract. Agent-requested saves based on tool descriptions or prompts are best effort. MCP itself does not detect turn completion or stop writers.

The caller is responsible for a stable writer boundary. Stow retains changed-tree refusal and returns capture failure explicitly. The prior completed checkpoint remains the recovery point. No successful checkpoint implies model success, process-memory capture, reversal of remote actions, or containment.

The first integration targets **OpenCode in a separate Stow-prepared workspace**.
The user approved launching it through a small caller-side integration command
that holds the next prompt until the save succeeds. Ordinary OpenCode UI integration
requires separate verification; the command is a caller integration example within
the existing storage-first boundary.
Its saved state includes files, required object metadata and explicitly written
progress/context; full chat restoration is deferred. Transient checkpoint failures
receive bounded retries with backoff, then return failure. Automatic continuation
must wait for a confirmed save or an explicit caller decision after failure. The
OpenCode turn barrier remains an integration question to verify, not a shipped
guarantee; see the detailed MCP plan.

If typed caller metadata becomes necessary, design a bounded versioned extension with explicit old-reader behavior and preservation across transports before implementation. Do not require TaskSpec/Attempt/Turn registries first.

## Definition of done

| Requirement | Acceptance evidence |
| --- | --- |
| Published installation | Version-matched macOS arm64 and Linux x64 consumers install without repository-specific binary overrides |
| Supported SDK behavior | Default-client checksum, metadata and required object operations work through the declared compatibility profile |
| Coherent storage surfaces | Workspace directory and S3 facade share one runtime/store and preserve documented object/file semantics |
| Movable saved state | Adoption works after changing bundle parent and removing access to original sender paths, without editing references |
| Complete reconstruction | Required bytes, supported metadata, file modes, additions and deletions survive; Git source requirements are explicit |
| Stable capture and failure | Changing/corrupt input, interrupted publication, retention bounds and unavailable prerequisites yield explicit outcomes; completed checkpoints stay usable |
| Thin agent integration | One real external runner saves at safe boundaries and uses transferred state on Linux CI with fresh model context; caller owns the runner lifecycle |
| MCP access and guidance | One real client discovers the local storage tools and workflow guide; storage semantics match Go/CLI, and host-triggered versus agent-requested save guarantees are stated and verified separately |
| Useful S3 fixture | An independent application/test uses the same storage lifecycle to keep and reopen a meaningful fixture |
| Repeat use | Two independent teams each choose at least three genuine save/reopen/review events over two weeks and retain the integration |

Technical checks and adoption targets are separate. The team thresholds are proposed learning criteria, not existing evidence or a statistical PMF claim. Fresh-agent task failures must be distinguished from missing or corrupted stored state.

## Suggested learning timebox

Use six weeks after implementation begins as a planning timebox, not an effort estimate or release commitment. Establish the complete local storage path first, then published installation and the supported second host. Begin recruiting willing pilot participants through a user-authorized outreach process while engineering work is underway; reserve time for independent use.

If the schedule overruns, reduce supported workload breadth. Do not trade away data-integrity checks or consume all pilot time by adding an executor. Repeat checkpoints should be measured for storage cost and retention behavior before selecting deduplication or another backend.

## Compare with the user's current workflow

Measure hands-on setup/recovery effort, missing-input incidents, storage correctness, capture/transfer cost and voluntary repeat use. Compare with the user's actual baseline: Git plus files, CI artifacts, existing S3 fixtures, or provider-native snapshots. The combination must earn its integration cost; another archive format by itself is insufficient.

## Decision after the milestone

- Deepen storage if independent users repeatedly rely on inspection, portability or recovery.
- Narrow the initial audience if S3 fixtures or agent working data demonstrate substantially stronger use.
- Rework the proposition if ordinary Git/artifacts or existing storage tools meet the need at lower effort.
- Reconsider execution ownership only if repeated integrations reveal a common blocking need and the user explicitly approves a new product scope. A runner or sandbox is not the automatic next phase.

A future paid offer needs evidence of recurring team needs such as shared retention, distribution, access control or support. Neither hosted execution nor a control plane is required to validate this milestone.

## Starting evidence

The [assessment](product-assessment-2026-09-29.md) records passing storage checks and current defects. The [continuity pilot](agent-continuity-pilot-2026-09-29.md) demonstrates two deterministic worker processes using offline inputs and transferred files on one host, with an archive-reference workaround. It did not demonstrate fresh-agent or cross-host continuation. Those are validation tasks, not existing product claims.
