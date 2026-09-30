# Portable workspace usage

The development tree adds portable object checkpoints, replayable capture requests,
a shared workspace S3 facade, local MCP tools and an experimental OpenCode caller.
The native package targets are macOS and Linux on arm64/x64; this local run
verifies macOS arm64. Windows workspace runtime is not supported by the current
rooted-read/capture implementation. These additions have not yet been published. See [the plan](plan.md) for release
and pilot gates.

## Workspace lifetime and S3

Go callers use `Workspace.Facade()` to borrow the existing runtime's loopback S3
endpoint. `Close` drains HTTP before closing the runtime and releasing liveness.
Concurrent facade calls return the same endpoint. `Workspace.Destroy` requires
`EnvironmentDestroy` in the handle's resolved authority, even after `Close`.
`ReadOnly` and `ReadWrite` withhold that permission; closing a handle does not grant
deletion. `DestroyRegisteredWorkspace` and the explicit CLI destroy command are
ambient registry administration for a trusted host. They preserve live/adopted/
protected-directory refusal and do not widen issued handles or rewrite saved grants.
Files written by the host are
reconciled before quota-sensitive API mutations; quotas cannot prevent arbitrary
host filesystem writes.

```sh
stow-s3 workspace serve --id WORKSPACE_ID --registry-dir /absolute/registry
```

This process owns the workspace until shutdown. Its readiness descriptor contains
local S3 credentials: treat it as a capability, not a public log. TypeScript
`serveWorkspace` and Python `serve_workspace` provide the same lifetime. Closing
these handles preserves the workspace. A second owner is refused.

## Conditional object saves

Public Go `PutOptions` now exposes `IfMatch` and `IfNoneMatch` through the existing
runtime/store write gate. Use the ETag from a read for `IfMatch`, or `IfNoneMatch: "*"`
for creation only when absent. A mismatch returns `ErrPreconditionFailed` without
replacing current bytes. Empty conditions retain unconditional writes.

`Runtime.Capabilities().ConditionalWrites` reports support. A supplied custom `Store`
must implement `ConditionalWriteStore` and return true only if it enforces conditions
atomically with its write. Otherwise conditional calls return
`ErrConditionalWritesUnsupported`; unconditional calls remain compatible. Memory and
workspace runtimes advertise support. Supplied stores, including filesystem
implementations, need explicit opt-in; existing internal filesystem condition support
does not qualify an arbitrary custom store.
The WASM/TypeScript embedded bridge exposes the same content-ETag contract and
refuses conditions against older hosts that do not advertise support.

These checks compare content ETags, not metadata or every intervening revision.
They do not coordinate arbitrary host writers or provide a committed-write replay
receipt after a lost response. A stronger native managed-save interface is described below. Native filesystem
receipts support bounded recovery; wrapper consumers and declared-input readiness
checks remain open in [W02/W09](storage-foundation-plan.md).

## Managed saves in native storage

The development Go API adds `Runtime.ReadForSave` and `Runtime.SaveObject` for
built-in memory storage and the owned filesystem profile below. Check
`Capabilities().GuardedSaves` before using them.
An observation includes an opaque `SaveCondition` bound to that runtime and exact
bucket/key. Saving compares the managed record generation, content and logical
metadata at the store write gate. A metadata-only or same-byte managed replacement
invalidates an earlier observation, even when the ETag remains unchanged.

```go
object, condition, err := runtime.ReadForSave(ctx, bucket, "report.txt")
if err != nil {
    return err
}
// Edit a caller-owned copy, or create when condition.ObservedAbsence() is true.
result, err := runtime.SaveObject(ctx, bucket, "report.txt", edited, stow.SaveOptions{
    Condition: condition,
    PutOptions: stow.PutOptions{ContentType: object.ContentType, Metadata: object.Metadata},
})
if errors.Is(err, stow.ErrSaveConflict) {
    // Reread and reconcile; current bytes were not replaced.
    return err
}
if err != nil {
    // result.Outcome distinguishes known refusal from an unknown effect.
    return err
}
_ = result.Object
```

Reading a missing object in an existing bucket returns an explicit observed-absence
condition. An empty condition is invalid. For deliberate unconditional replacement,
set `SaveOptions.Condition` to `stow.ReplacementCondition()`; current write authority and quotas still apply.
Do not mix managed conditions with legacy `IfMatch`/`IfNoneMatch` options.

The result uses `SaveCommitted`, `SaveNotCommitted` or `SaveUnknown` independently
of the error reason. Memory commits are volatile and retain no save receipt.
Tokens are native values bound to the open runtime, cannot be serialized for a
bridge, and do not confer permission. Workspace and custom public stores refuse
with `ErrGuardedSavesUnsupported`, including stores qualified for ETag conditions.
The native object MCP profile below wraps these managed saves. WASM, in-process
TypeScript and S3 have no managed-save API in this slice. Resource ACLs and
declared-input publication remain target work in the
[shared contract](storage-admission-contract.md).

### Filesystem save receipts and recovery

`OpenFilesystem` owns an exclusively locked local directory on macOS or Linux.
It stores body and logical metadata in one object record. `Reset` is unavailable;
`Close` preserves the directory and releases ownership. Set quota and operation
mask options at open, and use normal bucket creation before the first save.

```go
runtime, err := stow.OpenFilesystem(stow.FilesystemOptions{
    Dir: "/absolute/owned-object-store",
    Options: stow.Options{MaxBytes: 1 << 30, MaxObjects: 10000},
})
if err != nil { return err }
defer runtime.Close()
// Persist this caller-generated key and intended save meaning BEFORE calling.
requestKey := "caller-generated-unique-key"
_, condition, err := runtime.ReadForSave(ctx, bucket, "report.txt")
if err != nil { return err }
result, err := runtime.SaveObject(ctx, bucket, "report.txt", edited, stow.SaveOptions{
    Condition: condition, RequestKey: requestKey,
})
if result.Outcome == stow.SaveUnknown {
    // Also available after closing/reopening this same owned directory.
    result, err = runtime.ResolveSave(ctx, bucket, "report.txt", requestKey)
}
```

Check `Capabilities().DurableSaveRequests`. A nonempty `RequestKey` requires this
capability and current read/write authority; `ResolveSave` needs current read
authority. Reusing a key with different resource, guard, payload or logical options
returns `ErrSaveRequestConflict`. Within the same runtime, replay with the original
condition returns retained original metadata and `Replayed: true` without replacing
newer bytes. Opening still refuses a directory whose current usage exceeds the
configured opening quota; use an admissible quota to obtain a resolution handle.
After restart, resolve the saved key: an old condition cannot bind to
the new runtime. Do not fabricate a fresh expectation to blindly retry an uncertain
save. Missing receipts return `SaveUnknown`, never proof of no effect.

Recovery verifies a matching publication before acknowledging commitment. An
unresolved publication pauses all managed mutations, including legacy puts,
deletes and multipart operations; reads and resolution remain available. Recovery
does not automatically apply the intended body. Receipts retain metadata, not a
historical body; fetching the object later returns its current contents.

Retention is bounded to 1024 terminal entries, 8 MiB of encoded bookkeeping
admission, 64 KiB per entry and one pending slot. New keyed saves refuse when
retention is full; required evidence is not automatically expired. This is a
logical reserve, not preallocated disk space. Files and receipts are plaintext.
Arbitrary host edits, directory copies, shared-host fencing, pre-enrollment ABA
history and physical power-loss qualification are outside this profile. Admission
refreshes authoritative object/multipart accounting by scanning the store; workload
costs still need measurement. Local evidence is recorded in
[the implementation receipt](implementation-2026-09-30.md).

## Portable capture and reconciliation

```sh
stow-s3 workspace checkpoint --id WORKSPACE_ID --registry-dir /absolute/registry \
  --portable --request-key CALLER_GENERATED_RANDOM_KEY --timeout 30s \
  --max-bytes 1073741824 --max-files 100000
```

Persist the key and exact options before calling. Pause all writers first. Use
`--resolve` with the same arguments after an uncertain reply; it creates no new
checkpoint. The response carries `version`, `result` and a typed `error` on failure.
Success is `result.outcome=committed`, with `result.checkpoint.id`. Inspect the
exit status as well as the JSON. Scope/options conflicts and permanent failures
must not be retried as new captures. Receipts remain valid only while retained.

The shared Go APIs are `CaptureCheckpoint` and `ResolveCheckpoint`, with
`CheckpointRequest{Key, Options}`. `CheckpointOptions.PortableObjects=true` selects
the new format. Plain `CheckpointOf` and `Workspace.CreateCheckpoint` still accept
file-only capture by default. All capture paths share locking and publication.

Publication synchronizes staged payloads, manifest, receipt and directories before
acknowledging durability on supported local filesystems. An error after rename
can have an unknown durable outcome; resolution verifies the saved payloads and
reestablishes the synchronization barrier. Locking failure is an error. This is
not a snapshot primitive for actively changing arbitrary filesystems.

## What travels

| Profile | Preserved |
| --- | --- |
| v1 file checkpoint | Selected regular files, modes and hashes |
| v2 portable checkpoint | Files, logical object bytes, bucket membership (including empty buckets), content type, user metadata, valid checksums, relative working directory and declared Git provenance |

Private `.stow` state, credentials, multipart staging, `.git`, process memory and
external effects do not travel. Sensitive-path exclusions apply to object keys as
well as files. Host edits invalidate stale checksums while declared content type
and user metadata are retained. Restore reconstructs destination storage state;
original object version IDs and multipart ETags are not guaranteed.

The archive contains a versioned manifest and safe generated payload names.
Readers refuse unsupported formats and malformed references. v1 stays readable.
`ComparePortableCheckpoints` reports file, bucket and object/metadata changes.
File-only diff and delta APIs refuse v2 instead of dropping object state. Enhanced
delta transport is deferred.

Existing CLI handoff/export/adopt commands accept v2. A file-output handoff stores
its archive reference relative to the document; move both together unchanged.
Go `ExportHandoff` creates a new self-contained bundle directory atomically;
`AdoptHandoff` reports a retained import if later destination restoration fails.
Handoff operations are not automatically retried.

For Git-aware continuation, `RestoreCheckpointWithGit` accepts explicit local
repository mappings containing the recorded commits. It recreates HEAD/index
without checking base files out over captured deletions. No transported URL triggers
a fetch. Restore records origin provenance under a new workspace identity; capture
a local baseline before starting a new parent chain.

## Retention

`SetRegistryPolicy` records standing workspace-count, checkpoint-count and payload
byte caps. Zero means unlimited. `GetRegistryPolicy` reads them. Registration,
capture, import and delta publication share admission. No automatic eviction is
performed. `DeleteCheckpoint` refuses retained descendants and coordinates with
active capture/read operations; deleting a checkpoint deletes its replay receipt.
These policy/deletion APIs are currently Go APIs. Collection remains caller-scheduled.

## Native object MCP

The object profile gives MCP callers guarded object editing and retained resolution
through the existing native binary. It owns one filesystem object store and one
configured bucket. Use an object-store directory, with the object-record layout
shown above.

```sh
stow-s3 mcp --object-dir /absolute/owned-object-store --bucket reports \
  --create-bucket --max-bytes 1073741824 --max-objects 10000
```

`--create-bucket` explicitly creates the configured bucket if missing. Omit it when
requiring an existing bucket. `--read-only` permits reading and resolving prior
saves; it refuses saves and cannot be combined with bucket creation. Object mode
refuses workspace registry/ID, export/adopt and capture-only flags. MCP clients
choose object keys within the configured bucket; they cannot select host paths,
other buckets, backends or authority.

| Tool | Caller workflow |
| --- | --- |
| `stow_object_read_for_save` | Read bounded bytes/metadata or observed absence and obtain an opaque edit token. |
| `stow_object_save` | Supply exactly one `observation_token` or `replace: true`, plus the persisted `request_key`, key, base64 data and logical options. |
| `stow_object_resolve_save` | Resolve the original key/request key after an uncertain reply or host reopen; it starts no new save. |
| `stow_object_release_observation` | Release an edit token after inspection or terminal handling; object data and receipts remain. |
| `stow_object_capabilities` | Inspect persistence, read-only state and adapter limits. |

Persist a unique request key and the complete intended save before calling. Live
retries preserve the original token, key, bytes and options. After token expiry or
host restart, resolve the original request key. A new read is a new observation;
it does not prove an earlier uncertain save had no effect. Deliberate replacement
is explicit and still checks current authority and quota.

The version-1 object result keeps effect outcome separate from its error reason and
MCP `isError`. Only `outcome: "committed"` proves a save. An absent valid effect
result, including MCP validation/transport failure, leaves a keyed save unknown.
`available` reports whether requested read/receipt information is included; receipts
retain original metadata, while later reads return current bytes. No old body is held.

Bodies are limited to 64 KiB, observations to 256 with a 15-minute lifetime, admitted
tool calls to eight, tool names to 128 bytes, raw tool arguments to 256 KiB and complete encoded tool results
to 256 KiB. Hosts can narrow
body/observation limits with `--max-object-bytes` and `--max-observations`. Stdio
requests have a 1 MiB line limit. These payload limits do not imply an RSS ceiling
or streaming filesystem decoding. See the [object MCP contract](object-mcp-contract.md)
for scope, result semantics and evidence. This is a native object-record profile;
resource ACLs, encrypted storage and workspace-host-edit coordination remain open.

## Local MCP

```sh
stow-s3 mcp --registry-dir /absolute/registry --workspace-id WORKSPACE_ID \
  --timeout 30s --max-bytes 1073741824 --max-files 100000 \
  --export-root /absolute/existing-transfer-dir \
  --adopt-root /absolute/existing-destination-dir
```

The optional roots enable handoff export/adopt. Without them, the server offers
workspace inspection, capture, resolution, checkpoint inspection and diff. Tool
arguments cannot replace the configured registry or widen limits. Transfer names
are direct children of configured directories, and imported team values cannot
change registry scope. Treat these directories as caller-owned; adapter scope
checks do not isolate the host OS against another process changing paths.

Tool output includes structured data and matching text, bounded pages and typed
failure information. Stdio stdout contains protocol only. The workflow guide is
available at `stow://guides/checkpoints/v1`. The official Go MCP SDK is pinned at
1.8.0, using Go 1.25.6. Closing the connection does not destroy storage.

Reliable turn saves require a host admission barrier. The
[OpenCode example](../examples/opencode/README.md) implements the first experimental
caller profile. An agent choosing to call the MCP save tool remains best effort.

## Cost and scope

Captures currently store full payload copies. With tests stopped on the local
macOS arm64 host, five-run measurements were:

| Working set | Median | Observed range |
| --- | --- | --- |
| 256 files / 8 MiB | 129 ms | 124–185 ms |
| 4,096 files / 64 MiB | 1.74 s | 1.53–11.10 s |

The large run included an 11.10-second first capture; a median is not a latency
bound. The earlier ~31-second result was dominated by repeated directory scans,
which have been removed. Capture now uses at most eight copy workers and sixteen
file flushes, and waits for each barrier before verifying/publishing. All content
and mutation checks remain. Retaining five checkpoints still costs roughly five
payload copies plus manifests. A tested parent-file sharing experiment was removed
because it made saves slower on this filesystem.

Measure the actual working set before turn-by-turn use and choose a bounded
deadline accordingly. Transfer time, peak RSS and a second host remain separate
checks. These measurements are not service-level promises.

Per-request body limits and storage quotas do not imply a process memory ceiling.
Stow's filesystem checks protect supported storage operations; arbitrary processes
with host access remain outside this boundary. See [implementation evidence](implementation-2026-09-29.md)
for exact validation and outstanding resource limits.
