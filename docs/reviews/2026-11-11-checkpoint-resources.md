# Local checkpoint file resources

**Date:** 2026-11-11. **Status:** Local profile qualified. W25 remains partial until
cross-host/fresh-model qualification.
The user has no second machine available. The
[canonical plan](../development/storage-foundation-plan.md) owns status.

## Implemented local profile

The [contract](../contracts/checkpoint-resources-contract.md) fixes a selected
registered workspace/checkpoint file view, static roots and mandatory exclusions,
immutable manifest pages, complete reads up to 64 KiB and verified ranges.
Every range hashes the complete file up to 64 MiB while retaining only its
requested window. File-v1 and portable-v2 `manifest.Files` work; logical v2 object
payloads retain their separate namespace. Working directories may be absent.

Ephemeral checkpoint leases extend the existing OS lock mechanism rather than
consuming durable recovery-hold rows. Direct and bulk deletion acquire matching
exclusion; shared workspace attachment protects workspace removal. Capture can
continue. A fixed five-minute timer closes idle readers, and process death
releases both leases. W03's broader recovery behavior and W05's protected policy
remain unqualified by this local profile.

The opt-in `--workspace-files --checkpoint-id ID` profile reuses the MCP server's
admission, timeout and close machinery. Resource-only mode still has no tools or
prompts; normal mode keeps existing checkpoint grants. Mutable URI meanings and
defaults remain intact. No new UI, editing tool or language wrapper was added.

## Review and repairs

Source audits established both checkpoint deletion sites and the lock order.
Independent B/C review accepted constructor identity/lease ordering, bounded
indexing, v1/v2 file-only scope, full-file verification and expiry/close behavior.
Parent review caught a hash loop that originally read to EOF before checking
size. It now reads at most the expected size plus one sentinel byte and refuses
growth; tests prove the bound and discard captured bytes on cancellation.
Focused independent recheck accepted that repair.

Independent D review accepted exact namespace routing, saved-only startup without
a working root, empty selector refusal, encoded page packing, range/empty payloads,
expiry and generic errors. E review accepted source removal, receiver identity,
exact byte verification and cleanup without a model or cross-host claim.

The first combined affected race run hit the old W24 response-packing fixture's
one-second deadline during concurrent builds. The exact case passed alone in
0.55 seconds. That non-timing fixture now uses the existing ten-second read cap;
production bounds are unchanged. The complete MCP race suite then passed.

## Local adoption receipt

The [receipt](../assessment-evidence/2026-11-11/checkpoint-local.json) names the
exact native artifact and retains source/imported/receiver checkpoint identities.
The [recipe](../../examples/workspace-resources/receiver-continuation.mjs) exports a
portable checkpoint, removes the source workspace, registry and input tree, adopts
the archive, and captures a new receiver-local checkpoint. A fresh resource-only
server reads that receiver checkpoint after a later working-tree edit. Exact
contents, byte counts and SHA-256 of both progress and CSV inputs match.

The base adoption recipe is same-machine relocation and resource verification
without provider calls. The separate model attempt below does not substitute for
a second physical host.
Usage and bounds are in the [guide](../guides/checkpoint-resources.md).

## Verification

| Check | Result |
| --- | --- |
| Native reader/hash/index focused races | Passed, including corruption outside a range, growth bound, cancellation, expiry, process-kill release and capture/deletion coordination. |
| Affected package races | Storage, rooted, public stow and CLI suites passed; full MCP suite passed after the fixture deadline repair. |
| Independent TypeScript stdio client | Passed tree paging, 180,000-byte reconstruction/hash, saved/working divergence, missing working root, scope and corruption refusal. |
| Receiver recipe and automated extension test | Passed locally with exact bytes/hashes and explicit receiver-local root. |
| Native/WASM build and API generation | Passed. |
| Integrated standards | Passed full race, quality, documentation/API, TypeScript, generated-output and browser gates; existing baselines unchanged. Coverage: 74.78% (14,320/19,150 statements). |
| Extension and agent fixtures | 27 extension tests passed; final frozen example sources passed 40 agent tests, with one separate opt-in interrupted-turn model test skipped. |
| Four conformance profiles | Memory, filesystem, runtime-memory and runtime-filesystem passed. |

## Fresh-context model attempt

The [model receipt](../assessment-evidence/2026-11-11/checkpoint-model-local.json)
retains a **failed strict pilot**, not a new qualification. A fresh isolated
OpenCode v2.2.7 process used the selected receiver checkpoint after all sender
workspace, registry and input paths were removed. The synthetic saved notes record
three entries totaling 46 units; the saved batch adds 7 and 13. The receiver's
mutable batch had already changed to 700 and 1300. The agent returned the correct
66 units and five entries, citing both exact saved-file URIs. Successful resource
responses match the saved fixture's SHA-256 and byte length.

The eight-call resource trace includes one rejected request to an invented
`/files/` directory before the model recovered through the returned tree and file
URIs. The frozen verifier rejects any failed or noncanonical call, so the overall
outcome remains `failed / evidence_rejected`. Independent review accepted the
narrower answer and exact-byte-delivery subclaims without closing that gate. Nine
completion requests used the guarded `openrouter/free` selector and returned HTTP
200; routed model IDs are retained. These records verify routing, not an
independent price guarantee. The first attempt used one top-level prompt and no
automatic retry.

The attempted prompt and five harness digests are preserved separately from the
later prompt correction. That correction now directs the agent to copy overview
`root_uri`, tree-entry `uri` and `next_uri` verbatim. Syntax and final agent checks
passed. The subsequent user-authorized corrected attempt is recorded below.
Output was not written or checkpointed by the caller. Prior progress was seeded,
not produced by an interrupted model turn.

One independent verifier review and focused repairs added canonical URI checks,
positive resource-result validation and exact returned-text digests. Replaying the
prior actual W24 events caught an incorrect interpretation of OpenCode's
`providerResult.executed: false`: successful local reads also contain that flag.
The repaired parser accepts all eight prior successful events and explicit MCP
errors still refuse. Focused W24/H regressions passed 13/13.

The first integrated run lacked the pre-existing missing distribution files;
building the package prerequisite resolved those setup failures. Standards then
passed, including 246 script tests. A final agent-only run after the prompt edit
passed 40/41 with one explicit opt-in model skip. Native/WASM/UI source behavior
is unchanged from the qualified base profile. Temporary pilot directories and
matching child processes were confirmed absent. The 270 pre-existing distribution
omissions are preserved after validation.

## Corrected fresh-context attempt

After the user authorized continuation, the [second receipt](../assessment-evidence/2026-11-11/checkpoint-model-local-attempt2.json)
records one run of the corrected prompt, using the same native artifact and
unchanged strict verifier. All six resource calls completed successfully with
canonical returned URIs. Progress and next-batch text matched their saved byte
lengths and SHA-256 values; all six completion requests used the guarded
Free Router selector and returned HTTP 200.

The final assistant message contains the correct 66-unit/five-entry result and
exact source URIs in one fenced JSON object, followed by explanatory processing
prose. The frozen parser requires the entire terminal message to be JSON, so
this attempt also remains **failed / evidence_rejected**. Extracting the embedded
object was diagnostic only; it did not turn the receipt into a pass. The URI
guidance correction worked for this run; strict terminal-format compliance is
still unqualified. No third attempt or automatic retry ran.

Further qualification should first establish whether the pinned host and free
route can enforce a structured terminal response. Do not keep tightening prompts
or relax the verifier solely to mark a recorded failure green. Any different
output contract must be explicit and separately reviewed. Both failed receipts
remain intact; these are two top-level prompts across two separately authorized
attempts, not proof of repeat-use reliability or an interrupted model turn.

The [W26 native save contract](../contracts/workspace-editing-contract.md) is now
frozen after independent design review, with [implementation batches](../design/workspace-resource-plan.md#w26-prerequisite-audit-and-execution-boundary).
It is planning evidence only; no workspace editing code was implemented here.

## Remaining gates

Second-machine and Linux execution, meaningful fresh-model continuation, protected
saved-version policy and guarded workspace editing remain open. Local lifetimes
do not claim filesystem syscall preemption. All checkpoint deletion participants
must support the new leases. No commit, publication or deployment occurred.
