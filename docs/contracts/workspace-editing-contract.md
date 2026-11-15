# Native workspace file saves

**Status:** Native C/D and E.1/E.2 CLI/MCP/SDK locally qualified for macOS ARM64
`host_quiesced`, 2026-11-12.
C.1 owner/read APIs are [qualified locally](../reviews/2026-11-11-workspace-editor-observations.md),
as are [C.2 internal publication](../reviews/2026-11-11-workspace-editor-publication.md)
and [D.1 private live-owner request receipts](../reviews/2026-11-11-workspace-editor-durable-requests.md).
Public save/resolve, recovery-aware opening and capture/cleanup guards are
locally qualified in [D.2](../reviews/2026-11-11-workspace-editor-recovery.md).
The [CLI/MCP and SDK record](../reviews/2026-11-12-workspace-editing-mcp.md)
qualifies the opt-in binding; model editing remains a separate gate. The [design review](../reviews/2026-11-11-workspace-editing-contract.md)
and [C.1 record](../reviews/2026-11-11-workspace-editor-observations.md) retain
separate design and implementation evidence. The
[canonical plan](../development/storage-foundation-plan.md) owns status; the
[execution batches](../design/workspace-resource-plan.md#w26-prerequisite-audit-and-execution-boundary)
own assignments. This extends the local read-only resource profile, not its
permissions or defaults.

The C.1 API introduced bounded read observations. Tokens retain a stored-index
snapshot alongside current file identity
and digest; a host file without an index entry has an empty stored generation.
ReadForSave uses read scope; save admission independently requires
write scope and current write authority. C.1 refusal errors generally use
ErrWorkspaceEditorUnavailable, with separate closed, capacity, invalid-token,
byte-limit and context errors. C.2 introduced private comparison/publication;
D.1/D.2 add durable requests and public save-effect/error classification through
the same owner, Runtime and Store.

## Mission and acceptance

Allow managed clients to read a complete small file, compare against that basis,
publish a replacement and resolve a lost reply without publishing again. Keep
filesystem bytes, the natural object index and the owner's accounting coherent.

Acceptance requires:

1. Two clients with the same basis cannot both replace it successfully.
2. A confirmed save is visible as the same generation through file reads and
   natural object metadata; a lost reply resolves the original retained result.
3. Every partial publication has an honest committed, not_committed or unknown
   outcome. An ambiguous effect pauses new mutations and capture.
4. Admission preserves the owner's authority, quotas, path bounds and lifecycle.

This contract freezes the native foundation and opt-in MCP binding. Its local
qualification does not cover model editing, arbitrary host writers, protected
multi-tenant policy, Linux execution, portable request receipts or a product release.

## One owner and one coordination domain

The editor belongs to an existing owning **stow.Workspace**, its pinned Store,
primary bucket, Runtime and exclusive Session. One WorkspaceEditor may be active
per owner; multiple managed clients share it. A second process cannot create an
independent writer owner beside that session. This profile introduces no daemon,
distributed coordinator or detached writer over a registry ID.

The host must explicitly select **host_quiesced** coordination and keep every
writer outside the editor quiescent: direct filesystem tools, S3, direct Runtime
calls and other handles to the tree. Session liveness and advisory capture locks
do not fence those writers. An unsupported coordination request refuses. Managed
A→B→A changes are detected; an unobserved external A→B→A change is not covered.

Operations use this order, releasing in reverse:

    Workspace.lifecycleMu
      -> registry mutation gate
      -> workspace capture gate
      -> owning Instance.mu
      -> workspace Store.mu

Never acquire registry/capture locks from beneath Instance.mu or Store.mu. Reads
that mint a save basis use the same ordering. The gates are held per operation,
not throughout the human/model editing interval. Close cannot race publication.

Reuse AcquireMutationCapture and rooted confinement. Existing ordinary Runtime
mutations already use Instance.mu before Store.mu; the specialized admission must
not reverse that order. No call back into public Runtime methods while holding
its mutex, and no registry calls while holding Store.mu. Already-admitted paths
call internal helpers; they must not recursively call public capture APIs or
reacquire lifecycle/registry/capture gates. Verify this with contention tests.

## Path scope and authority

Only exact canonical natural paths in the primary workspace bucket are editable.
Do not expose secondary buckets, escaped keys or logical portable objects as
filesystem paths. Refuse collisions instead of falling back to an escaped object.
Use the read profile's component-aware roots, mandatory sensitive-path exclusions,
Unicode aliases, exact spelling checks and rooted regular-file confinement.

Read roots default to the existing local read profile. At least one explicit
writable root is required; at most 16 distinct roots of each kind are accepted.
Every writable root must lie within the declared read scope. Roots and file paths
are canonical and bounded to 1,024 UTF-8 bytes. A dot write root is a deliberate
whole-workspace selection, still subject to mandatory exclusions.

Parent directories must already exist as real directories. Expected absence may
create a regular leaf; this first profile creates no directories, symlinks or
special files. It supplies no rename, deletion, patch, append, chmod, multi-file
transaction or large-upload operation.

The retained owner must grant coarse ObjectRead and ObjectWrite for saves;
observe and resolve require read authority. Opening the editor requires read
authority; a read-only owner may resolve retained requests but cannot save.
First-profile editors refuse owners
with a configured resource Policy, including a currently permissive one. W04 has
not defined qualified workspace-path read/write operations. Object key-prefix
policy is not substituted for path authorization, and reopening cannot widen the
registered authority. Explicit roots are trusted host configuration, not an ACL.

## Native surface

C.1 implements OpenFileEditor, ReadForSave, ReleaseObservation and Close.
D.2 adds SaveFile, ResolveSave and explicit owning recovery:

    ResumeWorkspaceForFileRecovery(options WorkspaceOptions, id string) (*Workspace, error)
    (*Workspace).OpenFileEditor(WorkspaceEditOptions) (*WorkspaceEditor, error)
    (*WorkspaceEditor).ReadForSave(ctx, path) (WorkspaceFileRead, error)
    (*WorkspaceEditor).SaveFile(ctx, WorkspaceFileSaveRequest) (WorkspaceFileSaveResult, error)
    (*WorkspaceEditor).ResolveSave(ctx, path, requestKey) (WorkspaceFileSaveResult, error)
    (*WorkspaceEditor).ReleaseObservation(token) error
    (*WorkspaceEditor).Close() error

Options contain ReadRoots, WriteRoots, Coordination, MaxFileBytes,
MaxObservations and ObservationLifetime. Defaults and maxima are 64 KiB per file,
64 live observations and five minutes per observation; callers may lower them.
An editor's lifetime ends on its Close or the owner's Close. Closing it clears
volatile observations, not durable receipts or a pending recovery barrier.

WorkspaceFileRead returns path, complete bytes or observed absence, digest,
permission bits, generation and an opaque observation token. Only a missing leaf
under an accessible, valid parent establishes absence. An inaccessible parent,
unreadable file, changed root or unsupported node never becomes absence.

Tokens bind editor incarnation, registered workspace/root identity, canonical
path, existence, file identity, full content digest, size, nanosecond mtime,
permissions, relevant object metadata and the stored generation. Cache only the
bounded comparison state, not another copy of every body. Releasing/expiring a
token permits no mutation. A new owner/incarnation invalidates old live bases.

Save requests contain Path, Data, RequestKey and exactly one of ObservationToken
or Replace=true. Replacement is an explicit authorized choice. A regular
existing file keeps its permission bits; a new leaf uses 0644. Content type and
user metadata are retained for an existing natural entry; this API does not edit
them. Publication creates a new stored generation even for identical bytes.

WorkspaceFileSaveResult contains Outcome, Replayed, the original Generation and
Object metadata when known. Object.Data is empty; returned user metadata is
cloned. A refused save reports not_committed, while a missing or unresolved
receipt lookup reports unknown and cannot establish that no write happened.
Errors distinguish conflict, denied, invalid, unavailable, unsupported, capacity,
closed and uncertain recovery without leaking host paths or tokens.

## Admission and index coherence

Add a **workspace-file-scoped** internal Runtime admission path. It uses the same
owning Instance, mutation checks, quota/reservation calculations and accounting
transitions as existing writes, then invokes the workspace-specific publisher.
Do not copy a Usage check into MCP or construct a second Runtime over the Store.
The primary bucket/path check is mandatory before that delegation. In particular,
quota preflight must not call an adopting HeadObject path that changes an
unindexed file's stored generation before comparing its C.1 basis. Reuse the
quota arithmetic with a current non-adopting snapshot; unchanged input must not
conflict because admission itself adopted it.

Do not enable the store-global GuardedWriteStore or SaveRequestStore capability
merely to reach Runtime.SaveObject: that would advertise managed support for
secondary buckets and escaped forms outside this qualification. A narrow internal
storage interface and a private reference to the existing Instance are permitted;
the public Runtime wrapper must not gain an unqualified generic save capability.

Under Instance.mu, refresh usage, admit the payload/object delta, then compare and
publish under Store.mu. Reuse quota helpers rather than duplicate their rules.
Request lookup/reconciliation precedes fresh-write admission: replay must not
write again or fail because a later unrelated write consumed free capacity.
Current authority and path scope still apply before revealing any receipt.

Use pinned-root atomic replacement, preserving the planned permissions. Update
the primary bucket's natural ManifestEntry and folded-name index, persist the
index and reconcile Runtime usage before the operation becomes ready. Preserve
the complete index semantics, including generation, ETag, checksum, metadata,
size and modified time. Recompute checksums for the new content instead of
retaining an old value. Root replacement and alias collisions refuse.

File/index/journal failures must not escape as ordinary pre-effect refusals.
Runtime accounting becomes dirty and remains reconciled or paused; after a
known publication it must not subtract bytes as if no effect occurred. New saves
are refused while index or usage readiness remains uncertain.

## Durable request records

Use one versioned workspace-specific journal under **.stow/file-saves/**, with
its own identity marker and checksummed records bound to the registered workspace
and root lineage. Reuse atomic file/sync and validation primitives, not the FS
object backend's on-disk records. No payload bytes or credentials enter receipts.

Request keys use the existing trimmed UTF-8 convention, up to 256 bytes. Meaning
binds workspace/journal identity, canonical path, operation version, original
observation-token identity or explicit replacement, payload digest/length and
permission/metadata-preservation policy. Persist the resolved publication plan,
including its new generation, before effect. Same key with a changed path, body,
basis or mode policy is a request conflict, never a new operation.

Bounds are 1,024 terminal records, 8 MiB charged journal bytes and 64 KiB per
record, plus one pending slot in the same charged byte budget. Reserve the
worst-case pending/terminal transition space before publication. This is logical
accounting, not a physical disk-headroom guarantee. Full capacity refuses new requests while existing
receipts and resolution remain accessible. No automatic receipt eviction,
time-based forgetting or renewal/reclamation framework is introduced.

Serial pending intent blocks other managed writes. A retained terminal receipt
replays its original result even if newer bytes now occupy the path. After
reopen, clients use ResolveSave; a missing receipt reports unknown/not-found and
does not authorize repeating the write. Resolve performs no new payload write.

## Publication and restart outcomes

Persist the intent as prepared, then publishing before calling the file writer.
After a successful, synced file replacement, persist published evidence before
index completion. Finally persist the coherent terminal committed receipt and
clear the pending slot. A published record can carry an index-repair obligation;
it is not a ready terminal result.

| Last trustworthy evidence | Outcome and action |
| --- | --- |
| Refusal before durable intent/effect | not_committed; destination unchanged. |
| Prepared; publication has not been admitted | Resolve can persist not_committed without changing payload. |
| Publishing; publication cannot be proved | unknown; preserve intent and pause. Bytes matching the planned digest do not prove this request committed. |
| Writer returned an error with rename/sync phase unknown | unknown; never translate the generic error into a guaranteed unchanged file. |
| Published evidence durable; index incomplete | Validate root and recorded published file identity/content/mode under the gates. The index must match the recorded pre-entry or planned target entry. Install the planned generation/entry without rewriting payload; another entry or file state remains unknown. |
| Index coherent but terminal receipt not durable | Retain the pending obligation; resolve from trustworthy published evidence, never republish. |
| Terminal committed receipt durable | committed; lost reply/reopen/replay returns original result without touching later bytes. |
| Record corrupt, missing behind an existing marker, or unsupported | Fail closed; retain dependencies and refuse mutation/ready capture. |

This intentionally leaves a crash window between destination replacement and
durable published evidence. Neither byte equality nor an old index proves that
window's effect. The initial profile returns unknown and needs explicit operator
reconciliation; it does not invent a transactional filesystem or an automatic
abandon operation. A future proof-carrying publication mechanism needs its own
requirement and qualification.

Unknown or incomplete publication blocks new save observations, managed saves,
checkpoint capture and registered removal/collection of the dependent workspace.
This survives editor Close, owner death, observation expiry and reopen. Ordinary
read-only file resources may return current complete bytes without claiming a
save basis or ready version. Metadata operations must not serve a stale index or
silently rebuild it over a pending plan. Immutable older checkpoints remain
readable under their existing leases.

Qualified open/resume inspects the marker and journal **before** ordinary
Store initialization can write index state. Ordinary ResumeWith/OpenWorkspace
claims the Session before initialization and refuses any journal-bearing root,
including terminal-only state. The recovery-aware owning bootstrap claims the
existing session, pins registry/root identity, loads the journal/index without
adoption writes, and exposes resolution even while normal admission is paused.
Reuse registered limits and authority; do not open a second independent Store or
hide an unresolved journal by reinitializing it. The owning
helper is ResumeWorkspaceForFileRecovery(WorkspaceOptions, id), returning a
paused Workspace on which OpenFileEditor can resolve requests. It shares the
existing owner bootstrap, retained registry limits and session; it is not a
detached mutation API. Recovery initialization uses a read-only usage scan above
quota, then retains the original limits for fresh admission. It rebuilds only
the folded path map in memory and does not adopt object entries or flush
manifest/index state on Close. Registered roots and metadata directories must
already exist. New registrations persist the multipart cap; explicit recovery
narrows a missing legacy cap to one while ordinary unmarked resume keeps its
prior defaults.

Every qualified capture, lifecycle and mutation participant must honor that
durable barrier. Old binaries and arbitrary host writes are outside this profile.
Unknown-state operator intervention is a material availability limit, not a
successful automatic recovery claim.

## Opt-in MCP binding (E.1 implementation boundary)

The host selects --workspace-files --workspace-edit, explicit --write-root
directories and --coordination host_quiesced. Normal startup resumes the
registered owner with its retained authority and quotas. --recover-file-saves
explicitly selects the native owning recovery constructor; it requires editing
and does not fall back to normal initialization. Resource-only, selected saved
checkpoint, object and browser modes refuse editing. No editing-tool argument selects
the registry, root, workspace identity or bucket.

The four tools are stow_workspace_file_read_for_save, stow_workspace_file_save,
stow_workspace_file_resolve_save and stow_workspace_file_release_observation.
They reuse the one native editor; no second quota, receipt or observation cache
is added. Saves accept exactly one text or data_base64 body, bounded to 64 KiB,
and exactly one observation_token or replace=true. Empty bodies are valid.
Release preserves the native invalid/expired observation semantics.

Observe returns complete base64 bytes and optional UTF-8 text, plus path,
absence, digest, mode, time, generation and an opaque expiring token. Optional
text is omitted when necessary to fit the response budget. Save/resolve return
the original outcome, generation, replay flag and metadata without payload.
Encoded arguments and tool results are each bounded to 256 KiB. Error and
response-limit paths preserve known outcomes and disclose no host paths or
tokens. Failed observe responses release any undisclosed native observation.

Managed calls share existing workspace admission (eight active calls) and the
configured deadline, capped at ten seconds. Closing drains admitted calls and
releases the editor, owning Session and reader lease. Existing current-file
resources remain reads and never mint save bases. Checkpoint creation remains
an explicit separate caller action after a known committed save.

These bindings and the same-host SDK scenario are locally qualified. The
SDK scenario uses two real clients against one HTTP subprocess and explicitly
resolves a completed-response loss after SIGKILL/reopen. It does not qualify all
mid-publication MCP failure timings or a model editing turn.

## Verification and backout

Implement nearby native tests for scope/privacy/alias confinement, absence,
metadata and managed A→B→A, token expiry/close, exactly one competing commit,
quota boundary and unchanged index/list identity after success. Inject faults
before/after every intent, rename/sync, published marker, index and terminal
transition. Verify cancellation, disk-full/capacity and process death, with honest
unknown states and no second write after a lost reply.

Reopen tests must show that initialization does not erase evidence, resolution
does not mint a new basis or overwrite later bytes, and pending state blocks
capture/cleanup even without a live session. Include policy-configured profile refusal, missing-read-grant refusal before
disclosure, read-only save refusal before mutation and read-only receipt
resolution without a payload write. Qualified MCP tests
use two actual SDK clients sharing one owner and keep resources-only immutable.

Native and MCP readiness is limited to the C/D/E profiles that passed
independent review and the existing standards, conformance and agent gates. An
actual MCP-only model editing turn is separate qualification. macOS, Linux,
second-host and protected profiles retain separate evidence boundaries.

Backout disables the opt-in editor entry point. Preserve every receipt, marker,
pending intent and dependent workspace; do not erase recovery state to downgrade.
An older binary that ignores the marker is not a qualified downgrade path.
