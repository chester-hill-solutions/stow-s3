# Checkpoint file resources

**Status:** W25 contract frozen 2026-11-11; [local profile qualified](../reviews/2026-11-11-checkpoint-resources.md). Cross-host
continuation remains open because a second machine is unavailable. This extends
the [workspace resource contract](workspace-resources-contract.md); it does not
qualify protected saved-version policy, Linux, editing or a release.

## Identity, scope and retention

The trusted local host selects one registry, workspace ID and checkpoint ID.
The workspace must be registered and the checkpoint manifest must name that
workspace. Working files need not exist. The selected view contains only
`manifest.Files` from file-v1 or portable-v2 checkpoints; v2 logical object payloads
remain a separate namespace and are not invented filesystem paths.

Static `ReadRoots` and mandatory exclusions use W24 path/segment semantics.
Roots are canonical existing directories in the manifest's implicit file tree;
nil means `.` and an empty nonnil list refuses. At most 16 distinct roots are
allowed. Navigation reveals only necessary ancestors. These are local connection
disclosure controls for file resources, not W05's protected capture/export/restore/diff
policy. Normal checkpoint tools retain their existing grants independently.

An attachment acquires the W23 shared workspace read lease before lookup, then a
shared checkpoint lease before resolving the manifest or payload. Direct and bulk
checkpoint deletion take matching exclusive checkpoint leases before removing
bytes. Workspace removal already takes the matching workspace exclusion before
its callback. Capture continues to use separate locks and is not blocked by a
saved-view attachment. Readers never acquire the registry/capture gate afterward.

These are ephemeral OS leases, independent of durable recovery holds. They consume
no recovery journal rows; process death releases them. Every reader has a fixed
absolute lifetime, default/max five minutes (callers may narrow it). An expiry
timer actively closes idle readers, and each operation has the earlier caller or
reader deadline. Close is idempotent, waits for active native reads, and releases
both leases and rooted handles. Filesystem calls cannot be forcibly preempted.
Expiry never permits a partially returned response; reconnect creates a new view.

All checkpoint deletion participants must support the lease guards. No new
registry, journal, heartbeat or recovery-hold renewal API is introduced. Existing
durable hold corruption/expiry behavior is unchanged; W03's broader gates remain.

## Native reader and limits

`OpenCheckpointReader(ctx, CheckpointReaderOptions)` accepts `RegistryDir`,
`WorkspaceID`, `CheckpointID`, `ReadRoots`, optional narrowed `Lifetime` and
`MaxVerifyBytes`. Its methods are `Reference() CheckpointReadReference`,
`ReadDirectory(ctx, path, offset, limit) (CheckpointReadDirectory, error)`,
`ReadFile(ctx, path) (CheckpointReadResult, error)`,
`ReadRange(ctx, path, offset, length) (CheckpointReadResult, error)`, and `Close()`.

`CheckpointReadReference` fields are `WorkspaceID`, `CheckpointID`, `Version`,
`ManifestSHA256`, `CreatedAt`, `ExpiresAt`, and `ReadRoots`. Directory fields are
`Entries`, `Offset` and `Total`; range offsets/lengths and file sizes are `int64`,
while directory offsets/limits are `int`. Native refusals use
`ErrCheckpointReadUnavailable`, `ErrCheckpointReadTooLarge`,
`ErrCheckpointReadExpired`, `ErrCheckpointReadClosed`; deletion conflicts expose
`ErrCheckpointInUse` as the matching workspace-storage sentinel.

Reference contains workspace/checkpoint IDs, manifest version/digest, capture time,
absolute expiry and a copied read-root list, with no host path. Directory results
contain copied `[]WorkspaceReadEntry`, offset and total; limits are 1–100 and
offsets 0–total. Metadata is immutable for this attachment. Implicit directories
are derived only from admitted manifest file paths; ambiguous file/directory
collisions refuse. Empty directories are not claimed by the checkpoint format.

The existing manifest decoder/validator is reused through a confined regular-file
read capped at 4 MiB; the legacy `LoadCheckpoint` API keeps its 16 MiB bound. Admit at most
10,000 manifest files and 4 MiB charged retained file/directory metadata (256 bytes
per entry plus UTF-8 name/path and file digest lengths, plus 256 bytes per view).
Refuse before retaining an oversized index. V2 objects are validated structurally
but not retained in the file-view index. These are charged bounds, not total RSS.

Whole reads are at most 64 KiB. Ranges request 1–65,536 bytes, offset 0–file size;
EOF clamps the actual count, while past-EOF/negative/overflow requests refuse.
`CheckpointReadResult` contains `Path`, `Data`, `Offset`, full `Size` and `SHA256`.
Every read hashes the entire selected payload with a 64 KiB scratch buffer while
capturing only the requested window from that same pass. The manifest's complete
size/digest must match before any bytes return. Hash work is capped at 64 MiB per
file (callers may narrow); larger files refuse explicitly. No unbounded body
allocation or unverifiable partial-hash claim is allowed.

Pin the checkpoint root and revalidate its identity and the original manifest
digest before reads. Payload reads use no-symlink regular-file handles, expected
stored modes, bounded size and before/after metadata checks. Cancellation, expiry,
wrong workspace, replacement, corrupt payloads and changed manifests return no
partial bytes. Reuse W24 sensitive-parent/Unicode comparisons without rewriting
literal paths. Unsupported safe-open/lock platforms refuse.

## MCP and CLI

`--workspace-files --checkpoint-id CHECKPOINT_ID` selects this saved view instead
of the mutable file view. It still requires `--workspace-id` and registry. Existing
flags/defaults are unchanged; `--resources-only` still advertises no tools/prompts.
The selected profile refuses `--browser`; no W25 UI work is included. Normal mode
may retain the existing checkpoint tools, without adding a file/range tool.

`Config.CheckpointID` is host-owned. `NewWorkspaceFiles` routes to the saved reader
when selected and reuses the existing admitted resource lifetime/close machinery.
Its exact URI middleware allows only the configured checkpoint namespace (plus
the existing guide in normal mode). W24 live routes are not silently redirected.

| Resource | URI |
| --- | --- |
| Overview | `stow://workspaces/{workspace}/checkpoints/{checkpoint}/v1` |
| Directory first page | `.../tree/v1/{base64url_path}` |
| Directory continuation | `.../tree/v1/{base64url_path}/{offset}` |
| Whole file | `.../files/{base64url_path}` |
| Byte range | `.../ranges/v1/{base64url_path}/{offset}/{length}` |

All routes are exact, canonical, query/fragment-free and at most 2,048 bytes.
Offsets/lengths are canonical decimal integers. Immutable manifest ordering needs
no separate directory observation cache or cursor journal. Continuations advance
by the actual encoded page count, at most 100 entries and 256 KiB for the complete
encoded MCP result. The existing eight-read admission and min(timeout,10s) deadline
apply, further narrowed by the reader expiry. No empty nonterminal page is valid.

JSON overview/tree/range bodies use `version:1`, `workspace_id`, `checkpoint_id`,
`view:"checkpoint files"`, `manifest_sha256`, `created_at` and `expires_at` where
relevant. Overview includes `root_uri`, read roots, filtered-view semantics and all
bounds (`max_entries`, `max_file_bytes`, `max_range_bytes`,
`max_verify_file_bytes`, `max_manifest_files`, `max_metadata_bytes`). Tree includes
path, entries (W24 name/path/kind/size/URI shape; files additionally return
`range_uri` for their first 64 KiB window), and
`page:{offset,count,next_uri?}`. A range includes path, full file size/SHA-256,
offset, actual length, and either UTF-8 `text` for safe text media or `blob_base64`;
`next_uri` is present when more bytes remain. Whole files keep normal MCP text/blob
contents. Missing, forbidden, malformed and foreign references refuse path-free.

## Local qualification and remaining gates

Local tests must prove saved/working divergence, >64 KiB verified ranges, corruption
outside a requested window, bounds, scope, v1/v2 file views, active/idle expiry,
close/setup failure/process-kill release, and read-versus-prune/forget/removal while
capture still succeeds. An independent TypeScript client traverses and reconstructs
bytes from the actual native stdio server. Preserve W23/W24 regressions and run the
existing standards/conformance/agent gates on frozen sources.

The continuation recipe reuses export/adoption, removes source access and launches
a new receiver-local resource server using the returned local workspace ID. It
labels source checkpoint versus receiver working identity. No existing server is
silently widened to that receiver. Same-machine relocation is local evidence only;
second-machine/Linux and meaningful fresh-model continuation remain explicit gates.
No provider calls, paid fallback, deployment or publication are needed for this
local implementation phase.
