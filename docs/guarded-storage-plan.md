# Guarded read/edit/save plan

> **Consolidated on 2026-09-29; reference only.** The [new working-storage plan](storage-foundation-plan.md) is the sole work order. Conditional-write prerequisite evidence is retained below. Richer identity/save is W01/W02 and declared-input readiness W09; status/order live only in the new plan. Former checklists, phase numbers and next steps below do not schedule work.

**Status:** accepted direction from the 2026-09-29 interview. The conditional-write
prerequisite, native memory/filesystem guarded surfaces, retained filesystem
resolution and the native object MCP wrapper are implemented locally. Real consumer
adoption, further wrappers/backends, Linux execution and input-basis publication
remain open in the canonical queue. See [managed-save usage](portable-workspace-usage.md#managed-saves-in-native-storage)
and the [shared admission contract](storage-admission-contract.md). Expands S1-10 in the [canonical plan](plan.md).

## Implementation progress

The public Go runtime and WASM/TypeScript embedded options now carry existing
`IfMatch`/`IfNoneMatch` conditions to the store write gate. Mismatches expose typed
`ErrPreconditionFailed`/`precondition_failed` failures. Conditional support is explicit:
custom public stores opt in via `ConditionalWriteStore`; older bridges and undeclared
stores refuse conditional calls instead of silently discarding conditions. Legacy
unconditional writes remain supported.

This first slice guards content ETags only. It does not introduce read-bound tokens,
guarded defaults, metadata/history identities, input-basis ready publication or
lost-reply replay receipts. Existing backend locks qualify in-process writes; arbitrary
host writers and independent processes do not acquire a compare-and-swap guarantee.
See [current usage](portable-workspace-usage.md#conditional-object-saves).

### Verification of the conditional-write prerequisite

The local macOS arm64 slice covers real memory/workspace runtimes and a public
runtime supplied through the custom-store adapter: stale writes, expected absence,
unchanged bytes/metadata/accounting on refusal, competing concurrent writes, legacy
unconditional replacement and undeclared-store refusal. The real WASM bridge exercises
matching/stale/absence writes and typed errors; wrapper tests cover older-host refusal.

The full Go race suite and four local conformance profiles passed, as did the
`make standards` gate with existing installed npm dependencies reused via
`NPM_INSTALL='cd packages/stow-s3'`. A temporary Go build cache avoided restricted
default-cache writes; local HTTP test servers required approved host access. Initial
sandbox listener failures were rerun successfully with that access. This records
local evidence only, not Linux/live-provider or independent-process qualification.

## Agreed default

A new managed read/edit/save interface refuses a stale save by default. A caller
reads a resource with its comparison identity, edits its own copy, then submits that
expected identity when saving. If the resource no longer matches, return an explicit
conflict without overwriting current data. The caller decides whether to reread,
reconcile or explicitly request unconditional replacement. Replacement still requires
current resource-write authority; it is not an ACL bypass.

Creation needs an explicit expected-absence condition rather than an empty value that
implicitly means overwrite. Define the comparison identity, including namespace,
resource and content/version/metadata semantics, before implementing the public API.
Do not call an ETag a universal history/version identifier or promise detection of
every intermediate host edit that left the same final content.

Preserve existing S3 conditional/unconditional write behavior and legacy direct calls.
The guarded default belongs to the new read/edit/save interface; it does not silently
change every existing PutObject operation. This is storage conflict detection, not
automatic text merging, agent scheduling or a distributed collaborative editor.

## Existing mechanism and extension

S3 and the internal runtime/store paths already support conditional writes through
`If-Match` and `If-None-Match`. Workspace `putLocked` checks preconditions against the
resolved object before mutation. Public `pkg/stow.PutOptions`/`Runtime.PutObject`, the
WASM request bridge and TypeScript embedded options previously omitted those write
preconditions; the first implementation slice now forwards them with explicit support
and typed failures. The richer guarded identity/admission contract remains to implement.

Extend the existing public runtime, CLI/bridge and thin wrappers with the shared
comparison/conflict contract. Reuse store admission, authority, quota, atomic write and
commit-outcome handling; do not build wrapper-side check-then-write guards. Inspection
must not reveal forbidden current bytes. An unknown/lost save reply is not permission
to overwrite on retry; specify committed, refused and uncertain outcomes explicitly.

Ordinary direct filesystem edits remain caller-controlled. Qualify actual coordination
profiles across handles/processes and external edits; in-process locks alone do not
establish an arbitrary-host-write compare-and-swap guarantee. Report supported guard
capabilities rather than implying that every editor participates in Stow's write gate.

## Agreed declared-input checks

A caller may attach a bounded input basis: the resource/version identities it used
to produce selected output. When it requests current-input readiness, verify that
basis before admitting a ready report. A changed, missing, denied or unverifiable
required input blocks publication with a typed, permission-safe reason; preserve the
saved draft. Do not automatically recompute, overwrite or publish a stale result.

Providing a basis defaults to requiring those inputs to remain current. An explicit
snapshot-basis mode records intentional use of older immutable inputs without claiming
current-input freshness. Omitting a basis makes no claim about input freshness or a
complete record of reads. The caller declares reads; Stow checks the declared storage
identities and does not observe agent/model/tool activity or infer business correctness.

Bind basis references to namespace/resource/scope and expose only authorized provenance.
Reuse the guarded-read identity and saved-version model; do not create another task
registry or execution dependency graph. Current checkpoint provenance holds Git/origin
references, not this input-basis contract. Compare fresh resource identity at actual
publication admission and define atomicity with managed writers in the supported
profile; a wrapper-side check followed by an unrelated publish is insufficient.

Initial strict checks cover qualified managed-storage scopes. External filesystem
writers still require caller quiescence for a reliable capture/publication boundary.
Cross-origin or unsupported adapters cannot claim atomic multi-resource validation;
return unavailable coverage for a strict request rather than guess freshness. A stored
ETag or stale manifest entry alone is not evidence that arbitrary host-written bytes
are unchanged. Qualify content/version/metadata comparison semantics explicitly.

Publication checks establish the admitted report's declared basis at that boundary,
not that inputs remain current forever. Later changes remain ordinary notification
events and caller decisions. Include the admitted basis/mode in immutable report
meaning for request replay, subject to scope and the encoded-body budget. Basis
identities alone do not create indefinite retention holds on every mutable input;
declare any required saved-input dependencies through the existing recovery-set contract.

## Acceptance

Read the same resource through two real consumers, save from one, then prove the other
gets a conflict and preserves the first result. Cover expected absence, explicit
replacement, permission/namespace changes, metadata semantics, lost replies, host edits,
restart and supported coordination profiles. Exercise the actual Go/CLI/embedded public
surface, extending existing conditional-write tests instead of duplicating a backend.

Change an input after a draft is saved and before readiness publication; prove the
strict report is refused while the draft remains. Cover unchanged inputs, deletion,
permission changes, unstable/unverifiable inputs, supported concurrent managed writes,
explicit historical snapshot mode, omitted basis, replay and bounded disclosure.
Core does not infer what an agent read under ADR 0014/0015.
