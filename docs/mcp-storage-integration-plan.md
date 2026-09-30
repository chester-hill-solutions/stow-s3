# MCP storage integration and turn checkpoints

> **Consolidated on 2026-09-29; reference only.** The [new working-storage plan](storage-foundation-plan.md) is the sole work order. Concrete local adapter/caller APIs are implemented; remaining acceptance is W19. The retained pre-code names, SDK choices and checklist are dated design history; use current usage/source for actual contracts. Former checklists, phase numbers and next steps below do not schedule work.

**Status:** local experimental implementation, 2026-09-29. The stdio adapter and
caller-controlled OpenCode example are implemented; successful real-model and
cross-host acceptance remain open. [Usage](portable-workspace-usage.md),
[caller instructions](../examples/opencode/README.md) and
[implementation evidence](implementation-2026-09-29.md) describe the concrete API.
The design below preserves discovery rationale; proposed names are illustrative
where they differ from the implementation. The [canonical plan](plan.md) owns
acceptance status and ordering. [ADR 0014](adr/0014-storage-product-caller-owned-execution.md)
continues to govern storage versus caller-owned execution.

## Native object profile extension

The separate [object MCP contract](object-mcp-contract.md) extends W02 managed saves
and retained resolution over one configured filesystem bucket. It uses existing
MCP negotiation and the native core. Its object result and observation lifetime
are separate from checkpoint requests and editable workspaces. The workspace
adapter/caller history below remains unchanged; actual consumer acceptance still
requires its own evidence.

## Purpose

Expose Stow's existing workspace/checkpoint operations to MCP-capable agents and
hosts, with a consistent save/reopen recipe. This is the first concrete agent
integration surface. It does not add a Stow runner, scheduler, model broker or
execution-attempt registry.

MCP makes tools discoverable and callable, and supports reusable prompt templates.
It does not by itself establish a universal host turn-completion callback. The
integration must explicitly identify who requests each save. Protocol references:
[MCP tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools),
[MCP prompts](https://modelcontextprotocol.io/specification/2025-11-25/server/prompts).

## Confirmed first integration

Decisions agreed on 2026-09-29:

- Target an OpenCode agent first.
- Launch the first pilot through a small caller-side integration command that owns
  prompt admission. Hold the next prompt until checkpoint publication is confirmed;
  after retry exhaustion or unresolved outcome, return failure and keep admission
  closed until an explicit caller decision. User approved this pilot shape on
  2026-09-29. Ordinary OpenCode UI integration requires separate verification.
- Run it in a separate Stow-prepared workspace; leave the source project in place.
- Preserve files, required object metadata and explicitly saved progress/context.
  Full conversation restoration is outside the first milestone. Required metadata
  preservation depends on the S1 storage contract; file capture alone is insufficient.
- Retry transient checkpoint failures with bounded backoff, then return failure.
  A failed save must not silently advance the automatic workflow.

### Pinned OpenCode discovery and selected pilot shape

Installed binary: **2.0.16**. Inspected tag source:
`3a103fe0aff726a4edc7492f03f7b88195d9e4c9`. This is a discovery pin, not a tested
compatibility certification. [Source manifest](assessment-evidence/2026-09-29/pre-code-source-manifest.json)
records exact files and hashes.

The public [SDK guide](https://opencode.ai/docs/sdk/) describes the older
`@opencode-ai/sdk` surface. The pinned v2 source instead defines `@opencode/client`
and a different prompt lifecycle. Do not build against an unversioned tutorial.
[Pinned client package](https://github.com/anomalyco/opencode/blob/3a103fe0aff726a4edc7492f03f7b88195d9e4c9/packages/client/package.json)

| Source observation | Integration consequence |
| --- | --- |
| `POST /api/session/:sessionID/prompt` returns admitted input and schedules execution unless `resume=false`. | Awaiting submission is not awaiting the final answer. |
| `POST /api/experimental/session/:sessionID/wait` waits for the execution loop to become idle. | Candidate completion primitive; experimental and version-pinned. |
| Coordinator settlement can follow coalesced input and successor execution. | One busy period can contain multiple prompts; exclusive admission is required for one checkpoint per caller prompt. |
| Interrupt acknowledgement can precede cleanup; idle wait observes the local coordinator, not every arbitrary process. | Wait after interruption and establish known-writer quiescence separately. |
| Public v2 session hooks include prompt/context/request hooks, but no public post-settlement save barrier in the inspected interface. | Do not claim a plugin notification can gate every turn. |

Sources: [protocol endpoints](https://github.com/anomalyco/opencode/blob/3a103fe0aff726a4edc7492f03f7b88195d9e4c9/packages/protocol/src/groups/session.ts),
[prompt/wait implementation](https://github.com/anomalyco/opencode/blob/3a103fe0aff726a4edc7492f03f7b88195d9e4c9/packages/core/src/session/session.ts),
[execution coordinator](https://github.com/anomalyco/opencode/blob/3a103fe0aff726a4edc7492f03f7b88195d9e4c9/packages/core/src/session/run-coordinator.ts),
[plugin session interface](https://github.com/anomalyco/opencode/blob/3a103fe0aff726a4edc7492f03f7b88195d9e4c9/packages/plugin/src/promise/session.ts).

**Agreed pilot profile; reliability still requires runtime verification:** a caller-side controller owns prompt
admission to a dedicated OpenCode session, waits for settled execution, checks its
terminal result and known writers, saves context and captures before admitting
another prompt. No second UI/client may submit to that session under this contract.
The initial profile excludes detached work and untracked subagents; enabling them
requires tracking/awaiting their writers. These are integration constraints, not
OS containment. Stow still owns storage only.

Define a turn in this profile as one admitted caller prompt and its settled work.
Retain both caller request and OpenCode message/session IDs in caller state. An idle
wait response alone does not establish task success. After interruption, do not
advance until cleanup and a confirmed recovery save or explicit failure decision.

The remaining spike is runtime verification of this pinned path, including queued
input, failure and interruption. Confirm v2 local MCP configuration and negotiated
protocol at runtime as well; the earlier [MCP setup guide](https://opencode.ai/docs/mcp-servers/)
is background documentation, not a verified v2 configuration fixture.

## Initial adapter

Use a local stdio server sharing the existing Go workspace semantics. Keep it
stateless with respect to task/turn orchestration: existing registries and
checkpoints remain authoritative. Reuse `pkg/stow` directly where possible and
existing CLI composition for operations not exposed through an equivalent public
API; do not duplicate archive or handoff behavior in the adapter. Pin the selected
MCP SDK/protocol support when implementation begins.

Recommended adapter location: `internal/mcpstorage`, composed by a proposed stdio
subcommand in the existing binary. Keep protocol dependencies out of `pkg/stow`.
Extract typed shared handoff composition instead of maintaining CLI subprocess
parsing as the permanent implementation. Stdio stdout is reserved for protocol;
diagnostics go to stderr and carry no secrets.

The first client attaches to a workspace the caller prepared. Creation can remain
in the existing CLI for the initial example. The proposed tool surface is:

| Proposed tool | Existing operation to expose |
| --- | --- |
| `stow_workspace_inspect` | Read-only workspace lookup and recorded checkpoint retention limits |
| `stow_checkpoint_create` | `CheckpointOf` with explicit workspace/parent and capture limits |
| `stow_checkpoint_resolve` | New shared reconciliation of a capture request key; no new logical capture |
| `stow_checkpoint_inspect` | Load the saved manifest, exclusions and bounded file listing |
| `stow_checkpoint_diff` | Compare two explicit checkpoint IDs |
| `stow_handoff_export` | Existing checkpoint archive and handoff publication |
| `stow_handoff_adopt` | Verify/import into an explicit new workspace under configured destinations |

These names and schemas are proposals, not shipped APIs. Inspection/diff results
should be bounded and machine-readable, with clear error codes and human summaries.
Capture reports success only after checkpoint publication and returns the checkpoint
ID, parent, file/byte totals and exclusions. No tool runs arbitrary commands,
selects a model, grants upstream-write consent or silently destroys a workspace.

`LookupWorkspace` currently exposes identity, directory, bucket and checkpoint
retention caps, not all effective runtime quotas/authority. If inspection needs
more, extend a shared bounded read-only descriptor; do not resume/open a live
workspace just to inspect it. Ambiguous capture reconciliation likewise needs a
supported way to identify a committed result: loading a known checkpoint ID alone
does not resolve a lost reply containing an unknown newly generated ID.

Server configuration determines allowed workspace/registry scope and permissible
export/adoption roots. Tool arguments cannot widen that scope. The process still
has its host OS privileges; input validation and configured scope are not a
sandbox. Keep provider secrets out of tool results and handoff documents.

### Proposed bounded tool contracts

Names/fields below are pre-code proposals. Reject unknown fields and validate IDs
against configured scope before filesystem lookup. Resolve registry/team once from
trusted configuration; never accept a registry path or environment override from
the model. Check loaded checkpoint/workspace identity as well as the filename.

| Tool | Proposed input | Result |
| --- | --- | --- |
| workspace inspect | `workspace_id` | Read-only identity/location, recorded retention and locking capability. |
| checkpoint create | `workspace_id`, random `request_key`, optional `parent_id`, narrower limits | Confirmed checkpoint summary, exclusions, `replayed`, commit/durability outcome. |
| checkpoint resolve | same identity/key and options fingerprint | `committed`, `not_found`, `in_progress`, `request_conflict`, or `outcome_unknown`. |
| checkpoint inspect | `checkpoint_id`, limit/cursor | Summary and bounded files/objects/exclusions. |
| checkpoint diff | `from_id`, `to_id`, limit/cursor | Bounded changes; explicitly refuse unsupported enhanced-object diff until available. |
| handoff export | scoped workspace/checkpoint, relative new bundle destination | Artifact identifiers, relative paths and digest. |
| handoff adopt | relative allowed bundle path, relative new destination, narrower limits | New local descriptor and origin provenance; explicit partial outcomes. |

Proposed page default 100, maximum 1,000 entries, plus a serialized response-byte
cap. Bind cursors to immutable checkpoint/diff identity and server scope. Tool
arguments may narrow configured byte/file/archive/sensitive-data bounds, never
widen them. Use a result envelope with schema version, outcome and bounded payload.
Return `structuredContent` and matching text for compatibility; domain failures
are tool results with `isError`, while malformed protocol requests remain protocol
errors. Follow the [MCP tool contract](https://modelcontextprotocol.io/specification/2025-11-25/server/tools).

Read-only workspace lookup currently creates a missing registry via `openRegistry`;
change it to the existing read-only open path. Inspection must neither create
directories nor acquire another caller's exclusive workspace handle.

### SDK/toolchain choice

The repository pins Go 1.24.13. Official Go MCP SDK
[v1.4.0 declares Go 1.24](https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.4.0/go.mod),
whereas [v1.7.0 requires Go 1.25](https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.7.0/go.mod).
Its [compatibility table](https://github.com/modelcontextprotocol/go-sdk) also differs
by protocol release. Prefer the current supported SDK after a separate toolchain
compatibility check; use an older compatible pin only after reviewing intervening
fixes and proving negotiation with the selected OpenCode release. Do not silently
upgrade the repository or install an unqualified latest dependency. This remains
an explicit pre-implementation validation item.

## Two ways to request a turn checkpoint

### Host-controlled: the target for reliable every-turn saves

The caller controller owns admission and a verified lifecycle boundary. It:

1. Waits for relevant tools and background writers to stop or pause.
2. Saves any selected task/progress/context files inside the workspace.
3. Calls the MCP capture tool (or the same Go/CLI operation directly).
4. Records the returned checkpoint against its own turn identity only on success.
5. Allows the next turn, or reports that no save was confirmed if capture failed.
   Distinguish a confirmed failure from an unknown publication outcome.

This is a host integration contract, not a standard MCP turn event. Verify the
OpenCode integration can implement it and document exactly when capture occurs relative to
the agent's final response. If a response/transcript is required, the host must save
it before capture; a model calling a tool before its final answer cannot capture an
answer that has not yet been emitted.

The prepare CLI closes its workspace handle before returning. Holding OpenCode's
cwd alone therefore provides no Stow liveness protection. The integration must
hold an explicit caller-owned workspace claim until work/save completes. Do not
make an inspection tool secretly take that claim. For the JavaScript example, the
caller starts the S1-1 native workspace-serving process, awaits readiness, runs
OpenCode separately, and stops the storage holder last. An interim no-TTL/no-collection
profile must be labelled as a caller constraint, not deletion protection.

Conceptual caller sequence; these helper names are not existing APIs:

```text
acquire caller-owned workspace lifetime
for each caller prompt (serial admission):
    persist caller intent
    submit to pinned OpenCode API with stable message identity
    wait for execution settlement; inspect terminal outcome
    await all declared writers; refuse unsupported background work
    write selected progress/context files
    persist capture request key + immutable options in caller state
    capture or reconcile with bounded retries
    if outcome is not confirmed committed: return failure; keep admission closed
    persist returned checkpoint as last confirmed
    allow next prompt
release workspace lifetime only after cleanup
```

The caller state can live in its own session store outside the captured working
set; Stow does not create an attempt database. Do not automatically rerun a prompt
whose reply was lost. The OpenCode submission identity and Stow capture request
identity represent different operations.

### Agent-requested: useful guidance, best effort

A model receives a recipe to save progress and call the capture tool before ending
its work. This works with a broader range of clients but can be skipped, interrupted
or invoked while tools are still writing. Describe it as an agent-requested save;
do not advertise guaranteed every-turn coverage.

The guide must say: wait for writes, save objective/progress/remaining work, request
a checkpoint, inspect exclusions, and report the returned ID. On failure report
that saving was not confirmed and identify the previous completed checkpoint. A saved
checkpoint does not imply tests passed or the task succeeded.

## Guidance delivery

Provide concise tool descriptions and a versioned checkpoint workflow guide as an
MCP resource, with an optional user-selected prompt for setup/resume. Also ship the
same recipe in `skills/stow-s3/SKILL.md` and `site/agent.md` during implementation.
Clients differ in how they surface instructions, resources and prompts; verify the
chosen client's behavior rather than assume merely registering a prompt makes the
model follow it. Guidance is not a mechanism for enforcing turn boundaries.

Context starts as ordinary caller-written files captured by the existing format.
No new persistent turn identifier or context manifest field is required for this
adapter. A later typed attachment remains subject to the versioning decision in
the canonical plan.

## Failure and lifecycle behavior

- The caller owns the finite retry budget, exponential backoff with jitter and
  overall deadline. Retry only transient failures while the writer barrier remains
  held; permanent validation/permission failures return immediately. Cancellation
  stops retries. Proposed initial policy: three total attempts, full jitter over
  250 ms then 500 ms backoff windows, capped at 2 seconds if configured for more
  attempts. Require a positive caller-supplied overall deadline initially; select a
  default from capture measurements. OpenCode's tool-discovery timeout is unrelated.
- After exhausting the budget, return a structured failure with attempt count,
  error category and last confirmed checkpoint ID, if any. Keep the turn marked
  not confirmed saved and require an explicit caller decision before further work. A storage
  retry never automatically reruns the model prompt or its external side effects.
- A changing tree or confirmed pre-publication failure is not a completed save.
  Cancellation or disk errors around/after publication require reconciliation;
  report confirmed committed state or an unknown outcome, never assume absence.
- A lost MCP reply can leave the host uncertain even if publication succeeded.
  Define reconciliation before automatic retry; do not claim exactly-once capture
  from JSON-RPC request IDs or add an unbounded retry loop. Expose existing committed
  state as needed and document any API gap before extending it.
  If reconciliation cannot establish the outcome, return `outcome_unknown` rather
  than blindly retrying or asserting that no checkpoint exists.
- The caller controls writers. A caller assertion that writes are paused does not
  make Stow enforce that assertion against arbitrary processes.
- Closing the MCP connection releases adapter resources; it must not destroy the
  durable workspace or its checkpoints.
- Handoff transport retains its explicit scope, integrity checks and sensitive-path
  behavior. Repair the known absolute archive-reference defect before accepting
  relocated MCP handoffs as working.

### Capture request identity and reconciliation

Recommended new shared storage API: capture and resolve by a caller-generated
random request key, scoped to one workspace and canonical registry. Existing
`CheckpointOf`/handle entry points remain additive and use the same lock/publication
core. Do not derive identity from a JSON-RPC connection's request number.

Derive a stable candidate checkpoint ID from a domain-separated hash of workspace
identity and request key. Publish a bounded local receipt inside the checkpoint
directory together with its manifest/payload. Store the full request digest and
options digest even if the human-readable ID truncates a hash. Options bind parent,
limits, snapshot profile and sensitive-file choice. Different options under the
same key are a conflict. The receipt stays local and is excluded from portable
exports; it records a storage operation, not an agent turn.

Under the common capture gate:

1. Validate scope and request identity.
2. If a complete matching receipt exists, verify the checkpoint and return it before
   checking capacity for a new save. Never overwrite a conflicting/corrupt result.
3. Otherwise capture, validate, admit capacity and publish receipt/manifest/payload
   as one unit with the defined durability boundary.
4. On reconnect, resolve first. Contention is `in_progress`; a complete matching
   result can be `committed` only after the required durability barrier is established;
   absence is only established after checking under the gate.

A receipt and valid bytes do not prove a previous directory sync succeeded.
Resolution must re-establish the required sync barrier (at least the published
parent directory under the proposed ordered protocol) before durable acknowledgement.
Sync failure remains `outcome_unknown`. Reconciliation creates no new logical
checkpoint but may perform durability synchronization.

A lost connection around publication is not evidence of absence. If resolution
cannot establish a durable outcome, return `outcome_unknown`. After restart,
re-establish caller ownership and quiescence; never infer the previous writer
barrier survived. If the old operation cannot be reconciled, require an explicit
new capture decision rather than silently checkpointing a later state as that turn.

Replay is guaranteed only while the checkpoint and receipt are retained. Coordinate
cleanup with active/reconciling callers. After deliberate pruning, absence cannot
prove a request never succeeded; permanent exactly-once history would require an
additional bounded receipt-retention design and is not claimed here.

### Typed failure policy

| Category | Handling |
| --- | --- |
| Capture contention | Bounded retry while caller owns the barrier. |
| Workspace changed | Quiescence failure; establish a new stable boundary before retrying. |
| Invalid scope/ID/options, missing workspace, parent/request mismatch | Immediate failure. |
| Capture/retention limit, disk full, permission denied, locking unavailable | Immediate failure requiring capacity/policy/operator action. |
| Corrupt checkpoint/archive, unsupported format | Refuse; do not overwrite or retry as a new save. |
| Cancellation/deadline before commit | Stop; report confirmed failure only if non-publication is known. |
| Transport failure or error around commit | Resolve first; unresolved is `outcome_unknown`. |
| Explicitly identified transient I/O | Retry only after outcome is known; do not treat every filesystem error as transient. |

Return typed `code`, `phase`, `attempts`, `retryable` and `outcome`. The caller adds
its own last-confirmed-checkpoint association; the adapter must not invent which
checkpoint belonged to a previous turn. Initial handoff export/adopt has no
automatic retry because partial publication/import currently requires separate
reconciliation.

## Delivery and acceptance

1. Verify the pinned OpenCode admission/wait/terminal-outcome sequence with the
   smallest caller integration. Confirm protocol negotiation and foreground-writer
   profile. Resolve lifecycle ownership and capture deadline using measured cases.
2. Complete shared capture locking, cancellation/commit semantics, request receipt/
   resolve and typed handoff/read-only descriptor prerequisites (S0-7/S1).
3. Implement the stdio adapter and reuse shared storage contract cases to verify
   that MCP results match the Go/CLI surfaces.
4. Ship the guide and an OpenCode local MCP setup. Verify discoverability, structured
   errors, bounded output and connection cleanup.
5. Demonstrate both request modes and label them correctly. For host-controlled saves,
   verify a read-only turn, a writing turn, a turn with background work, an interrupted
   turn, capture refusal, retry exhaustion, cancellation during backoff, a lost reply,
   and restart without loss of completed saves. Prove pending prompts cannot bypass
   the save barrier and failed saves are surfaced without silently continuing.
   Unsupported detached/untracked writers must produce refusal with admission held.
6. Move a captured workspace to a second supported host and let an external agent
   continue using its own execution environment and credentials.

MCP work follows the storage contracts in S0/S1 and is packaged through S2. Design can
begin alongside those repairs; acceptance depends on their correctness. A hosted
remote MCP service and its authentication/multi-tenant operations are deferred.

**Done:** an existing agent/host can discover Stow, explicitly save and inspect its
working state, and hand it off through the same storage semantics as other callers.
Only a verified host integration may promise automatic checkpoints for its defined
turn lifecycle. The MCP adapter alone makes no such guarantee.
