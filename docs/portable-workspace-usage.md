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
Concurrent facade calls return the same endpoint. Files written by the host are
reconciled before quota-sensitive API mutations; quotas cannot prevent arbitrary
host filesystem writes.

```sh
stow-s3 workspace serve --id WORKSPACE_ID --registry-dir /absolute/registry
```

This process owns the workspace until shutdown. Its readiness descriptor contains
local S3 credentials: treat it as a capability, not a public log. TypeScript
`serveWorkspace` and Python `serve_workspace` provide the same lifetime. Closing
these handles preserves the workspace. A second owner is refused.

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
