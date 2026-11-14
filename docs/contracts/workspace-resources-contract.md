# Workspace file resources

**Version:** 1. **Status:** W23/W24 implemented and qualified for the declared local development profiles, 2026-11-11.
This contract defines the initial native read-only profile. Implementation and
qualification status live in the [canonical queue](../development/storage-foundation-plan.md).
The [design plan](../design/workspace-resource-plan.md) retains later product gates.
The [qualification record](../reviews/2026-11-11-workspace-resources.md) identifies
the tested native profile and remaining platform/product gates.
The [W24 qualification](../reviews/2026-11-11-workspace-resource-hosts.md) adds scoped
paging, the browser fixture and the actual OpenCode resource-only task.

## Identity and lifetime

The host selects an existing registry and exact workspace ID. Opening a reader
does not call `OpenWorkspace`, acquire a writer session, update last-used/index
state, create workspace files or change identity. It resolves the registered
workspace root, verifies its existing manifest ID/bucket and pins a confined root.
The registry is host-trusted under the existing registry contract.

Every reader first acquires a shared advisory lease in the registry's private
reader lock namespace, then resolves and validates the workspace. Registered
removal acquires the matching exclusive lease while holding its existing mutation
and capture gates, before invoking removal or forgetting identity. Contention
refuses removal as in-use. Failed reader construction releases its lease.
The lock files remain inert; their presence is not liveness. OS locks release on
close or process death. Hosts without supported advisory locks or safe rooted
regular-file opening refuse reader construction.

Reader leases are separate from exclusive writer-session locks. Multiple readers
and one existing writer session can coexist. Closing a writer leaves reader
protection intact. Closing a reader releases only its handles/lease and never
deletes workspace files. Checkpoint capture remains possible while readers live.
External host deletion or arbitrary modifications of the trusted registry are
outside this protection. Older binaries do not know this new lease namespace;
all processes that remove a workspace while readers are attached must use a
revision that enforces these leases.

## Public native reader

The proposed Go entry point for this slice is:

```go
OpenWorkspaceReader(WorkspaceReaderOptions{
    RegistryDir: registry,
    WorkspaceID: id,
    MaxFileBytes: 65536,
    MaxEntries: 100,
})
```

Zero bounds select those defaults; negative or greater bounds refuse. The returned
`WorkspaceReader` exposes `Reference() WorkspaceReference`,
`ReadFile(context.Context, string) (WorkspaceReadFile, error)`,
`ReadDirectory(context.Context, string) ([]WorkspaceReadEntry, error)` and `Close()`.
`WorkspaceReadFile` has `Path`, `Data`, `ContentType` and `Modified` fields.
`WorkspaceReadEntry` has `Name`, `Path`, `Kind` (`file` or `directory`) and `Size`.
These operations share a confined reader implementation, rather than another
object store. The reference may contain host paths for an embedding host; MCP
responses expose only the configured ID/bucket and relative paths.

Native sentinels distinguish closed, unavailable/forbidden/invalid,
too-large and changed reads. They disclose no forbidden path existence. The
initial profile uses explicit host trust and exclusions, not per-client protected
policy or authentication claims. Read observations are not guarded-save tokens.

## Paths and visibility

Paths are slash-separated UTF-8 relative paths. `.` names only the directory root.
Reject absolute paths, backslashes, NUL/control characters, empty/dot/dot-dot
segments, noncanonical spelling and paths longer than 1,024 UTF-8 bytes. Do not
normalize Unicode or case, or collapse distinct names. Invalid names encountered
in a directory refuse that directory observation rather than aliasing them.

Always exclude `.stow`, `.git`, `.ssh` and `.aws` path segments, case-insensitively,
and the existing `sensitiveSeedPath` file patterns from preparation/checkpoints.
Apply exclusions to every segment so a directory named `.env` cannot expose
children. Exclusion comparisons fold Unicode case aliases such as `.ſtow`; the
actual read path and resource identity remain unchanged. These connection
defaults are not an assertion that every secret can be
recognized. The operator explicitly opts into exposing the remaining workspace.
Private entries are absent from directory results; direct reads refuse without
revealing existence. Symlinks and nonregular/nondirectory entries are never listed
or served. Symlink roots/components and replaced roots refuse through rooted I/O.
Object keys stored under Stow's escaped layout are not filesystem resources.

The initial directory result is a complete bounded observation of direct visible
children, sorted by their literal name, without a continuation cursor. At most
100 visible children and 10,000 examined directory entries are admitted; enforce
both while iterating bounded batches, before unbounded materialization. Overflow
returns too-large, never an apparently complete partial result. There is no
recursive scan in W23. W24 below adds separate paged resources and static scope.

Reads cap decoded bytes at 64 KiB. Size preflight and bounded reads enforce the
limit even if the file grows. Compare handle metadata before/after and recheck root
identity; detected modification returns changed. A live read is not a snapshot:
arbitrary in-place host edits may evade metadata checks. Pause writers and capture
a checkpoint for immutable or multi-file consistency. Check cancellation between
I/O steps; filesystem syscalls themselves are not preemptible by context expiry.

## MCP resources

The opt-in launcher flags are `--workspace-files` and `--resources-only`.
The latter requires the former and is incompatible with object-store mode.
Both use the existing configured workspace/registry and positive timeout.
`--workspace-files` adds resources to normal checkpoint tools; `--resources-only`
registers no tools, prompts, capture/export effects or App UI. Existing defaults
are unchanged. Transport shutdown closes the reader; disconnecting one HTTP
request does not close a server-owned reader used by other requests.

| Resource | URI |
| --- | --- |
| Overview | `stow://workspaces/{id}/v1` |
| Directory template | `stow://workspaces/{id}/tree/{path}` |
| File template | `stow://workspaces/{id}/files/{path}` |

`path` is canonical unpadded base64url of the UTF-8 relative path (`.` for the
directory root). Reject noncanonical encodings, unknown IDs/query/fragment,
trailing path syntax and URIs over 2,048 bytes. Resource names contain readable
relative paths. Root overview includes version, workspace ID, bucket, working-view
label, `read_only: true` for file access, resource roots, implemented features and
bounds; it discloses no local directories or credentials.

Root resources appear in `resources/list`; templates are discovered through
`resources/templates/list`. Directory content is JSON with version, workspace ID,
path, entries, `complete: true` and mutable-view labeling. Each entry supplies a
directory or file resource URI. Failed observations contain no partial entries.
File resources return UTF-8 text for textual media, otherwise binary. Do not
silently truncate, execute or render file contents as host code. Empty files are
represented successfully. Whole encoded resource results are capped at 256 KiB.

Admit at most eight concurrent reads and bound each operation by the narrower of
the request context and the host timeout (10 seconds for reads unless narrowed by
the existing configured timeout). Reject saturation without waiting indefinitely.
No subscription or change-list capability is promised for live file changes.
MCP error messages map unavailable/forbidden/invalid together without paths;
too-large, changed, closed and busy have distinct bounded messages. Host logs must
not copy file contents, bearer tokens or excluded names.

## Required acceptance

Meaningful regressions must prove simultaneous writer/readers, collection and
administrative deletion refusal before and after writer close, explicit reader
close and process-kill lease release, and checkpoint capture with readers live.
Construction failure must leave identity/index bytes unchanged and release leases.
Readers must reject manifest/registry identity mismatch and root replacement.

Protocol tests cover ordinary host-written bytes, private/traversal/symlink/special
file refusal, text/binary/empty/oversize content, bounded directory overflow,
canonical URI scope, encoded response bounds, capability negotiation, stdio
shutdown and existing object/checkpoint behavior. Concurrency/close tests run
under the race detector. Unsupported platforms fail closed.

W23 is local native qualification. W24 below qualifies host usability, paged
navigation and static scope for its declared profiles; checkpoint resources/ranges and portable
continuation remain W25; guarded editing remains W26. This profile provides no
mount, execution, encryption or protected multi-tenant service guarantee.

## W24 additive implementation contract

**Status:** Implemented and qualified for the declared local profiles on 2026-11-11;
see the [W24 qualification](../reviews/2026-11-11-workspace-resource-hosts.md).
The existing W23 qualification remains separate; unverified host/platform claims remain open.

### Static scope and observations

`WorkspaceReaderOptions.ReadRoots []string` defaults to the existing whole view
when absent. At most 16 distinct sorted canonical existing relative directory
roots are permitted. Mandatory exclusions always win. File reads require a root
or descendant; navigation may include only ancestors necessary to reach roots.
Apply scope before child metadata inspection. Roots are immutable for the reader
lifetime; changing them requires a new server/reader and invalidates cursors.
Literal segment matching must not widen `inputs` into `inputs-private`. Existing
case-alias, traversal and symlink defenses remain mandatory.

The native API is `ObserveDirectory(ctx, path) (*WorkspaceDirectoryObservation,
error)` with `Page(ctx, offset, limit) ([]WorkspaceReadEntry, error)`, `Len() int`,
`MetadataBytes() int64` and `ObservedAt() time.Time`. Page limits are 1–100 and
offsets 0–Len inclusive. Returned slices are independent. The observation keeps
the reader, observed parent identity and immutable sorted metadata, no file bodies,
extra leases or retained file descriptors. Page validates reader/root/parent and
the current immutable scope. Directory replacement refuses; child edits do not
change captured metadata. File URIs continue to read current bytes.

Creation admits 10,000 examined/visible entries and 4 MiB charged metadata. Charge
256 bytes plus UTF-8 name/path lengths per retained entry, plus 256 bytes per
observation, before retention. Duplicate names and observable parent changes
refuse. A complete scan is not an atomic filesystem snapshot. Cancellation and
limits return no partial observation. Existing `ReadDirectory` remains a complete
at-most-100-entry operation with its existing overflow behavior.

### Paged resources

The additive routes are `stow://workspaces/{id}/tree-pages/v1/{path}` and the same
URI followed by `/{cursor}` for continuation. Path encoding and 2,048-byte URI
ceiling are unchanged. Strict routing/middleware accepts only those exact forms;
invalid, foreign and unavailable resources keep the existing path-free category.

The JSON body has `version: 1`, `workspace_id`, `path`, `view`, `observation_id`,
`observed_at`, `observation_complete: true`, `entries` and `page`. Entries retain
name/path/kind/size/URI; directory URIs use the paged route. `page` contains
`offset`, actual `count` and optional `next_uri`. Follow that URI verbatim. Page
packing returns at most 100 entries and measures the complete encoded MCP result
under 256 KiB; reduce count when needed without gaps/empty continuation pages.

The server-owned cache admits eight observations and 16 MiB of charged metadata,
with five-minute absolute expiry. One scan may run at a time; reserve its 4 MiB
allowance before creation within cache admission and evict expired/least-recently-
used observations as needed. No filesystem I/O occurs under the cache mutex.
Admitted page requests may retain an immutable evicted observation until their
bounded response completes. Measure such transient copies separately from cache
charge. Closing the server clears the cache and preserves existing read shutdown.

A cursor is canonical base64url of a version, unpredictable observation ID and
offset. The server map binds that ID to reader/server lifetime, scope and path;
native pages validate directory/root identity. Forged/out-of-range/expired/evicted
cursors refuse. This is ephemeral navigation, not a grant or durable journal.
Overview adds canonical sorted `read_roots`, `paged_root_uri`, `max_observation_entries: 10000`,
`max_observation_bytes: 4194304`, `max_observations: 8`, `max_cache_bytes: 16777216`,
`observation_ttl_seconds: 300` and implemented feature labels. V1 fields remain.

### Optional workspace browser

`--read-root` is repeatable only with `--workspace-files`. `--browser` remains
unchanged in object mode and is additionally valid with workspace file resources;
it refuses with resource-only mode or a workspace without file resources.
`WorkspaceFiles.EnableBrowser()` is a host registration operation that refuses a
resource-only or closed server; it introduces no actor tool for enabling features.

The optional `stow_workspace_browser` App launch tool returns
`{mode: "workspace", overview_uri: "stow://workspaces/{id}/v1"}` and the usual UI
metadata. Its UI URI is `ui://stow/workspace-browser-v1`. The same shared HTML bundle
is served with an escaped JSON `stow-bootstrap` script injected before `</head>`.
The script has the same mode/overview payload; it contains no host path or secret.
The App SDK singleton is shared, and the existing bucket view stays unchanged.

UI reads are admitted once through the existing bounded read lifetime. The
generated bundle is at most 512 KiB compressed / 2 MiB HTML; the entire encoded
UI-resource result is at most 4 MiB. UI is a separately bounded resource, not a
64 KiB workspace file preview. Keep CSP self-contained with no external assets.
Unknown UI URIs and UI requests in unconfigured profiles refuse path-free.

The workspace view uses vendored CHS primitives and a scoped React mount with
Tailwind theme/utilities only, without global preflight. It consumes overview,
paged roots, next URIs and file resources; no page tool duplicates those APIs.
References are attached only by explicit selection, preserving host context and
the existing ten-reference bound. Missing host attachment capability preserves
browsing. Visible status, keyboard/focus, touch targets, zoom and safe previews
are required by the W24 plan.

### Real-host profile

The available native host is OpenCode v2.2.7. Its new isolated recipe will verify
that exact API/config and resource-only permissions without changing the legacy
v2.0.16 pilot. User-selected provider/model is OpenRouter `openrouter/free`; the
OpenCode selector is `openrouter/openrouter/free`. Only this free route is allowed,
including background/title/summary model activity; no paid fallback is authorized.
The pilot has at most two turns and five minutes and uses synthetic inputs.
The isolated recipe pins root/title/compaction/summary models and uses a loopback
request guard that forwards only the exact free route to fixed OpenRouter endpoints.
It refuses model fallback fields. Only the guard holds the real provider key;
OpenCode receives a placeholder and the Stow subprocess receives an empty override.

The private host JSON selects binaries, expected version, model, credential env/
file references, deadline, turn limit and `allowPaidModels: false`. Read credentials
inside the runner, never into output/fixtures. Missing credentials or unsupported
free-model controls leave the actual-host gate open while local implementation
continues. No source/SDK mock is a live-model pass.
