# Storage portability: design rationale

**Status:** design implemented in the local 0.3.0 candidate on 2026-09-29, with
remaining acceptance tracked by [the canonical plan](plan.md). The text below
preserves pre-code reasoning and illustrative shapes. For actual names, defaults,
format boundaries and measured limitations use [portable workspace usage](portable-workspace-usage.md)
and [implementation evidence](implementation-2026-09-29.md). It is not a release
certification; self-contained Git base bundles remain deferred.

## 1. One workspace runtime, optional S3 access

Implement the workspace contract's proposed idempotent `Workspace.Facade()` over
`runtime.NewStoreAdapter(w.Runtime.inner)`. Reuse the existing authenticated S3
server, bind loopback only, and return the actual endpoint and credentials through
a descriptor. Do not pass `w.store` through native store construction: that would
create another runtime and accounting state.

The server currently closes its store on shutdown. The preferred contained change
is a private borrowed adapter embedding `*runtime.StoreAdapter` and overriding
`Close`; preserve its optional interfaces, including multipart support. Workspace
owns runtime shutdown. Concurrent open/facade/close/destroy operations must be
synchronized and have one deterministic result.

Lifecycle:

```text
caller owns Workspace handle + session claim
    ├── ordinary workspace files
    ├── existing embedded runtime
    └── optional borrowed loopback S3 facade

Close: stop HTTP admission → drain → close runtime → release session claim
Destroy: explicit ownership-aware removal after lifecycle closure
```

A proposed CLI serving surface resumes and owns one workspace handle. It must
refuse when another owner holds that session, rather than bypass its lock. A caller
already holding the handle uses the in-process facade. Never infer upstream mode
from ambient provider variables.

### Usage accounting

Prepare currently creates the runtime before copying seed files. The runtime's
initial usage therefore predates those writes; direct host writes also bypass its
mutation accounting. First reconcile seeded contents before returning prepare.
Define a refresh at safe boundaries before later quota-sensitive S3 mutations,
with a revision/change check so refresh cannot overwrite concurrent accounting.
Measure the cost before selecting a watcher/index. A watcher alone is not a proof
of complete accounting. Hard prevention of arbitrary filesystem writes remains
outside this product.

Acceptance includes seeded quota exhaustion, host-added/deleted/resized files,
simultaneous API mutations, stale metadata, startup failure, repeated facade calls,
read-only authority and close during active requests.

## 2. Two explicit checkpoint profiles

| Profile | Preserves | Boundary |
| --- | --- | --- |
| Existing file checkpoint v1 | Regular selected files and recorded modes/hashes | No complete object-state or Git-history claim. |
| Proposed portable checkpoint v2 | Selected regular files, logical objects including escaped/secondary-bucket payloads, required metadata and bounded provenance | No private runtime/identity/locks/credentials, incomplete multipart state, process memory or remote side effects. |

Read existing v1 without silently enriching its guarantee. For the first enhanced
writer, emit v2 whenever portable objects/provenance are required; old readers
must refuse. Keep an explicit file-only mode if needed for existing callers.

### Proposed shape (illustrative, names not yet public)

```text
CheckpointManifestV2
  version, id, workspace_id, parent_id, created
  files[]: relative_path, payload_ref, size, sha256, mode
  buckets[]: name
  objects[]:
    bucket, key, payload_ref, size, sha256
    content_type, user_metadata
    verified_checksums[]: algorithm, value, checksum_type
  primary_bucket
  working_directory: relative path
  provenance:
    repositories[]: relative_destination, resolved_commit, source_label
    origin_checkpoint_id?  // provenance, never a cross-workspace parent
  exclusions[]: kind, logical identifier, reason
```

Natural primary-bucket objects may reference the same captured payload as a file.
Escaped keys and secondary-bucket objects require their own payloads. Payload IDs
are generated safe identifiers; a logical object key is never used directly as an
archive path. Preserve explicit bucket membership, including empty secondary
buckets; absence versus empty must not be inferred from objects.

Keep identity/ownership, registry paths, locks, credentials, upstream endpoints and
multipart staging out of the portable representation. Reconstruct destination
private state through the workspace backend's own import logic. Do not copy raw
`.stow` or teach the checkpoint layer a second implementation of its disk layout.

### Metadata and validators

Initial required fidelity: bucket/key membership, bytes, content type and user
metadata. Normalize portable user metadata to bare lowercase names with bounded
sizes, explicitly handling legacy internal prefixed forms at conversion.

Verify checksums against captured bytes; record the checksum type. Preserve only
validators whose semantics remain valid. Reconstructing multipart data by ordinary
PUT does not preserve a multipart ETag. Source ETag/version may be provenance, but
destination identity/version fidelity is not promised. A consumer needing exact
multipart ETag behavior requires a separate accepted compatibility case.

For ordinary host file edits, recompute content-derived hashes/validators. Preserve
declared content type/user metadata only under an explicit documented rule; do not
inherit an invalid checksum because size and second-resolution mtime match.
Metadata-only mutations must be included in snapshot consistency checks.

### Boundedness and validation

Reuse existing capture/archive limits, extended to unique payload bytes, file/object
counts and total metadata/provenance size. Count shared payloads once for disk
admission; expose logical object count separately. Validate bounds before allocating
or materializing payloads. Reject duplicate logical objects, conflicting file
references, dangling/unreferenced payloads, invalid checksums, duplicate archive
entries, traversal and unsupported format/features.

The exact maximum metadata/provenance constants should follow the existing store
and archive bounds, with a recorded upper bound in the schema; they must not be
unbounded strings/maps accepted from an MCP client.

Apply one selection and sensitive-exclusion policy across files, logical objects
and shared payload references. A file excluded by path must not reappear through
an object alias, escaped key or secondary-bucket entry. Report exclusions with
their logical references; importing cannot widen receiver consent.

## 3. Coherent capture and durable publication

Factor a read-only logical snapshot reader beside workspace layout/manifest code.
External capture must not call `workspace.New`, which can write private state or
claim ownership. Read and verify payload content plus a canonical metadata and
membership inventory before/after capture. Refuse changed state. This detects
observed changes; it is not a filesystem snapshot against arbitrary active writers.

Caller quiescence is still required for the reliable turn profile. Hold the common
cross-process capture gate through retention admission and publication. Use the
same gate from handle and external APIs; do not rely on two unrelated mutexes.

Proposed commit boundary on supported local filesystems:

1. Validate target, options, parent and any capture request identity.
2. Acquire required locks in the documented common order.
3. Reconcile existing receipt before consuming another retention slot.
4. Scan logical state, admit bounded capacity, stage and verify payloads.
5. Write manifest/optional receipt; sync payloads, files and required staged dirs.
6. Check cancellation before entering publication, publish without overwriting a
   conflicting identity, and sync the checkpoint parent directory.
7. Acknowledge only the established outcome; release locks.

Specify errors after rename but before directory sync as uncertain durability,
not guaranteed absence. Cancellation after publication does not undo a save.
Unsupported filesystem durability/locking must be surfaced as a capability or
refusal, never silently upgraded to the reliable profile. The MCP request/resolve
protocol uses this same publication path.

## 4. Archive, delta and restore compatibility

Version-dispatch checkpoint readers explicitly. Audit the archive envelope as well
as manifest and payload names; retaining an envelope version is acceptable only if
its existing contract can represent the new entries and old readers refuse them.
Otherwise increment it. A new reader must validate all referenced payloads before
destination publication.

Current deltas describe files and construct a fresh manifest from selected fields.
Initially refuse delta creation/application involving enhanced checkpoints until
a versioned object/provenance-aware delta is implemented. Preserve existing v1
file delta behavior. This refusal is an explicit supported-profile boundary, not
a reason to silently strip object state or claim full v2 delta parity.

Inspection/diff is part of the complete workflow and must understand v2: report
file changes, bucket membership, object additions/deletions and metadata-only
changes. Extend the shared Go comparison layer with a versioned result shape;
existing file-only result types must explicitly refuse enhanced comparisons until
callers select that shape. The MCP adapter paginates those results rather than
implementing its own comparison. This is required for S1/S3 acceptance even though
enhanced delta transport can remain unsupported initially.

Preserve ordinary file permission bits on supported macOS/Linux hosts with explicit
chmod after writing, accounting for umask. Do not promise ownership, ACLs or xattrs.
Current `0755` masking must be reconciled with manifest modes. Reject unsupported
file kinds rather than follow links during materialization.

For MCP export, prefer a new immutable bundle directory containing archive and
handoff document with relative references. Stage together and publish the bundle
as one unit. Keep existing CLI file-output behavior compatible while fixing its
reference base. Validate membership before writing. Adoption must preflight
destination, identity and bounds, and verify the same archive bytes it imports
using one handle/stream or an equivalent immutable staging step.

A failed adoption may currently leave an imported checkpoint. Report phase and
partial outcome; do not automatically retry until shared typed handoff operations
have a defined reconcile contract.

## 5. Git and destination lineage

Persist a relative cwd and repository destination plus resolved commit. Sender
absolute paths are optional diagnostic origin data, not resolution instructions.
Transported URLs must not cause automatic network access or credential use.

Two supported reconstruction profiles are proposed:

- **File continuation:** fully captured files/context; Git unavailable is explicit.
- **Git-aware continuation:** destination caller supplies a local repository mapping
  that contains the recorded commit. Verify it, construct fresh Git metadata using
  the existing safe preparation path, then materialize the captured tree as the
  authoritative working tree. Do not overlay it and resurrect deleted base files.

Use Git-aware continuation for the coding pilot when CI already has the project.
The recipient supplies its own source/model access. Self-contained Git history/base
transport is a separate bounded bundle spike; raw `.git`, hooks/config, submodules
and LFS expansion remain excluded until explicitly supported.

Keep `parent_id` local to one workspace. Adoption creates a new workspace, so an
imported source checkpoint is provenance rather than a valid local parent. Create
a destination baseline checkpoint and start its local chain there, retaining origin
as bounded data. Do not relax parent validation to accept arbitrary cross-workspace
links.

## 6. Registry policy and lifetime

Store a versioned policy in a reserved metadata directory, outside the existing
`*.json` workspace-entry enumeration. Proposed initial policy covers registry
workspace count, retained checkpoint count/payload bytes and optional defaults for
new workspaces. Existing entries retain their recorded policy; call options may
narrow hard maxima. Zero/unlimited is allowed only where explicitly specified.

Use a simple coarse registry mutation/admission lock first. Fix one order across
all writers: registry mutation/admission → workspace capture/deletion coordination;
never reacquire them in reverse order from cleanup. Existing live-session claims
are checked non-blockingly where required; do not wait for a live owner while
holding the global gate. Refine lock scope/reservations only after measuring cost.

Apply admission to prepare, capture, import and delta publication, including imports
without a local workspace entry. Count only committed records; recognize all
reserved staging directories. Corrupt/unreadable entries block exact accounting
with actionable diagnostics. No automatic eviction is proposed. Collection stays
caller-scheduled and protects live/adopted data.

Reliable OpenCode usage needs a caller-owned live storage claim throughout the run.
For JavaScript callers, the long-lived native workspace-serving process from S1-1
owns the Workspace/session handle. The caller waits for storage readiness, separately
controls OpenCode, and stops the storage holder last. Go callers can hold the handle
directly. MCP inspection must not seize someone else's claim. A no-TTL/no-collection pilot
profile can be a temporary explicit constraint, but is not liveness enforcement.

Retention must include a safe explicit checkpoint-deletion contract: coordinate
active readers/export/capture, local lineage and capture-request receipts. A cap
error telling the user to delete a checkpoint is insufficient without a supported
operation. Preserve unresolved receipts while a caller is reconciling; the first
request protocol guarantees replay only while its checkpoint/receipt is retained.

## 7. Acceptance matrix and decisions still needing measurement

| Concern | Required cases |
| --- | --- |
| Object preservation | Natural/escaped keys, secondary buckets, same key in two buckets, case collision, zero-byte objects, empty buckets, empty-key refusal, metadata-only changes, host edits/deletions and exclusions through aliases/shared payloads. |
| Capture admission | Handle plus external capture, parallel prepare at last slot, imports/deltas at cap, unsupported/failed lock, corrupt records and all staging names. |
| Publication | Cancellation during scan/copy/before commit/after rename, disk full, sync failure, lost reply, restart, cleanup racing capture/export. |
| Restore | Unedited moved bundle, identity/credential exclusion, modes despite umask, unknown version, duplicate/missing payload, enhanced delta refusal. |
| Git | Exact destination commit, missing commit, captured deletion/untracked edit, source untouched, destination baseline lineage. |
| Facade | Shared usage, seed/host-write reconciliation, read-only permission, multipart, repeated open, close/drain failure and no ambient upstream. |
| Cost | Representative small/large working sets, max object sizes, concurrent API sessions, repeated captures, retained disk and lock hold time. |

Still requiring measurement: no-follow/path behavior on supported hosts; snapshot
and usage-refresh cost; practical capture deadline; exact multipart-validator needs;
and whether a consumer actually needs self-contained Git base transport. These
are explicit spike inputs, not reasons to add speculative backends or an executor.
