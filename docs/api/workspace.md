# TypeScript workspace API

Prepare directories, capture checkpoints, transfer archives and serve a workspace through S3. Import from `@chester-hill-solutions/stow-s3/workspace`.

## Defaults and limits

| Setting | Value |
| --- | --- |
| registryDir / team | Default local registry / no team partition |
| Archive maxBytes / maxFiles | 1 GiB / 100,000 files |
| serveWorkspace timeoutMs | 15,000 ms |
| WorkspaceJSON | A JSON object returned by the native operation |

## Behavior

- Keep registryDir and team consistent across operations.
- Pause writers before checkpoint capture.
- resume opens the original workspace; adopt creates a new one.
- Delta application verifies its named base.
- handoff can write a document to a file; the helper reads that file as its result.
- serveWorkspace owns the process until close, without deleting the workspace.

## Errors

- A nonzero native exit rejects the promise with command diagnostics.
- Invalid JSON or a non-object result is rejected.
- Archives can fail path, digest, size or file-count checks.
- Unknown save outcomes require resolution before retry.
- Destroy refuses live, adopted or protected directories.

## Example

```ts
import { prepareWorkspace, checkpointWorkspace } from "@chester-hill-solutions/stow-s3/workspace";

const task = await prepareWorkspace("task.json");
// Run your task in task.working_directory, then pause writers.
const saved = await checkpointWorkspace({ id: String(task.workspace_id) });
```

## Symbols

[adoptWorkspaceHandoff](#adoptworkspacehandoff) · [applyWorkspaceDelta](#applyworkspacedelta) · [checkpointWorkspace](#checkpointworkspace) · [collectWorkspaces](#collectworkspaces) · [createWorkspaceDelta](#createworkspacedelta) · [decodeWorkspaceJSON](#decodeworkspacejson) · [destroyWorkspace](#destroyworkspace) · [diffWorkspaces](#diffworkspaces) · [exportWorkspaceCheckpoint](#exportworkspacecheckpoint) · [handoffWorkspace](#handoffworkspace) · [importWorkspaceCheckpoint](#importworkspacecheckpoint) · [listWorkspaces](#listworkspaces) · [prepareWorkspace](#prepareworkspace) · [pruneWorkspaces](#pruneworkspaces) · [restoreWorkspaceCheckpoint](#restoreworkspacecheckpoint) · [resumeWorkspace](#resumeworkspace) · [ResumeWorkspaceOptions](#resumeworkspaceoptions) · [runWorkspaceCommand](#runworkspacecommand) · [ServedWorkspace](#servedworkspace) · [serveWorkspace](#serveworkspace) · [ServeWorkspaceOptions](#serveworkspaceoptions) · [WorkspaceAdoptOptions](#workspaceadoptoptions) · [WorkspaceApplyOptions](#workspaceapplyoptions) · [WorkspaceArchiveOptions](#workspacearchiveoptions) · [WorkspaceCheckpointOptions](#workspacecheckpointoptions) · [WorkspaceDeltaOptions](#workspacedeltaoptions) · [WorkspaceJSON](#workspacejson)

## adoptWorkspaceHandoff

Verify a portable handoff and restore it under a new workspace identity.

```ts
adoptWorkspaceHandoff(options: WorkspaceAdoptOptions): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `WorkspaceAdoptOptions` | Yes | — |  |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## applyWorkspaceDelta

Verify the named base and optional document digest before publishing a new checkpoint.

```ts
applyWorkspaceDelta(options: WorkspaceApplyOptions): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `WorkspaceApplyOptions` | Yes | — |  |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## checkpointWorkspace

Capture an immutable checkpoint of paused writers.

```ts
checkpointWorkspace(options: WorkspaceCheckpointOptions): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `WorkspaceCheckpointOptions` | Yes | — |  |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## collectWorkspaces

Collect expired workspaces that are unused and eligible for deletion.

```ts
collectWorkspaces(registryDir?: string | undefined): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `registryDir` | `string \| undefined` | No | — | Native registry directory for workspace metadata. |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## createWorkspaceDelta

Write a verified change document between two checkpoints from the same workspace.

```ts
createWorkspaceDelta(options: WorkspaceDeltaOptions): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `WorkspaceDeltaOptions` | Yes | — |  |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## decodeWorkspaceJSON

Run the native workspace command contract without invoking a shell.

```ts
decodeWorkspaceJSON(raw: string): Readonly<Record<string, unknown>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `raw` | `string` | Yes | — |  |

Returns: `Readonly<Record<string, unknown>>`.

## destroyWorkspace

Explicitly destroy a registered, eligible workspace.

```ts
destroyWorkspace(id: string, registryDir?: string | undefined): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `id` | `string` | Yes | — | Stable identity of this workspace, file stage, revision or immutable record. |
| `registryDir` | `string \| undefined` | No | — | Native registry directory for workspace metadata. |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## diffWorkspaces

Compare two checkpoints and return changed paths.

```ts
diffWorkspaces(from: string, to: string, registryDir?: string | undefined, team?: string | undefined): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `from` | `string` | Yes | — |  |
| `to` | `string` | Yes | — |  |
| `registryDir` | `string \| undefined` | No | — | Native registry directory for workspace metadata. |
| `team` | `string \| undefined` | No | — | Registry team partition; it is not an authorization identity. |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## exportWorkspaceCheckpoint

Write a portable checkpoint archive to the selected file.

```ts
exportWorkspaceCheckpoint(checkpointId: string, output: string, options?: WorkspaceArchiveOptions): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `checkpointId` | `string` | Yes | — | Immutable checkpoint identity. |
| `output` | `string` | Yes | — | Destination path for the transfer document or archive. |
| `options` | `WorkspaceArchiveOptions` | No | {} |  |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## handoffWorkspace

Create a handoff reference, optionally with a portable archive.

```ts
handoffWorkspace(id: string, options?: { readonly registryDir?: string | undefined; readonly team?: string | undefined; readonly checkpointId?: string | undefined; readonly archive?: string | undefined; readonly output?: string | undefined; }): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `id` | `string` | Yes | — | Stable identity of this workspace, file stage, revision or immutable record. |
| `options` | `{ readonly registryDir?: string \| undefined; readonly team?: string \| undefined; readonly checkpointId?: string \| undefined; readonly archive?: string \| undefined; readonly output?: string \| undefined; }` | No | {} |  |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## importWorkspaceCheckpoint

Validate and import a portable checkpoint archive.

```ts
importWorkspaceCheckpoint(archive: string, options?: WorkspaceArchiveOptions): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `archive` | `string` | Yes | — | Path to a portable checkpoint archive. |
| `options` | `WorkspaceArchiveOptions` | No | {} |  |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## listWorkspaces

List registered workspaces, including records whose directories are missing.

```ts
listWorkspaces(options?: { readonly registryDir?: string | undefined; readonly team?: string | undefined; }): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `{ readonly registryDir?: string \| undefined; readonly team?: string \| undefined; }` | No | {} |  |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## prepareWorkspace

Create a workspace from a task manifest and return its launch descriptor.

```ts
prepareWorkspace(manifestPath: string): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `manifestPath` | `string` | Yes | — | Path to the task manifest used to prepare a workspace. |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## pruneWorkspaces

Remove registry records for missing directories; preserve existing directories.

```ts
pruneWorkspaces(options?: { readonly registryDir?: string | undefined; readonly team?: string | undefined; readonly includeAdopted?: boolean | undefined; }): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `{ readonly registryDir?: string \| undefined; readonly team?: string \| undefined; readonly includeAdopted?: boolean \| undefined; }` | No | {} |  |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## restoreWorkspaceCheckpoint

Restore a checkpoint into a new workspace directory.

```ts
restoreWorkspaceCheckpoint(checkpointId: string, root: string, registryDir?: string | undefined): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `checkpointId` | `string` | Yes | — | Immutable checkpoint identity. |
| `root` | `string` | Yes | — |  |
| `registryDir` | `string \| undefined` | No | — | Native registry directory for workspace metadata. |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## resumeWorkspace

Open the workspace selected by ID or a same-machine handoff.

```ts
resumeWorkspace(options: ResumeWorkspaceOptions): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `ResumeWorkspaceOptions` | Yes | — |  |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## ResumeWorkspaceOptions



```ts
interface ResumeWorkspaceOptions {
  readonly id?: string | undefined;
  readonly handoffPath?: string | undefined;
  readonly registryDir?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `id` | `string \| undefined` | Yes | Stable identity of this workspace, file stage, revision or immutable record. |
| `handoffPath` | `string \| undefined` | Yes | Path to a same-machine or portable handoff document. |
| `registryDir` | `string \| undefined` | Yes | Native registry directory for workspace metadata. |

## runWorkspaceCommand

Run the native workspace command without a shell and decode its JSON object.

```ts
runWorkspaceCommand(args: readonly string[]): Promise<Readonly<Record<string, unknown>>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `args` | `readonly string[]` | Yes | — |  |

Returns: `Promise<Readonly<Record<string, unknown>>>`.

## ServedWorkspace



```ts
interface ServedWorkspace {
  readonly ready: Readonly<Record<string, unknown>>;
  close(): Promise<void>;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `ready` | `Readonly<Record<string, unknown>>` | No |  |

### ServedWorkspace.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## serveWorkspace

Own a workspace S3 server until the returned handle closes; preserve workspace data.

```ts
serveWorkspace(options: ServeWorkspaceOptions): Promise<ServedWorkspace>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `ServeWorkspaceOptions` | Yes | — |  |

Returns: `Promise<ServedWorkspace>`.

## ServeWorkspaceOptions



```ts
interface ServeWorkspaceOptions {
  readonly maxRequestBytes?: number | undefined;
  maxConcurrentRequests?: number | undefined;
  readonly id: string;
  readonly registryDir?: string | undefined;
  readonly team?: string | undefined;
  readonly maxBytes?: number | undefined;
  readonly maxObjects?: number | undefined;
  readonly timeoutMs?: number | undefined;
  readonly signal?: AbortSignal | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `maxRequestBytes` | `number \| undefined` | Yes | Positive HTTP request-body byte limit; native default is 8 MiB. |
| `maxConcurrentRequests` | `number \| undefined` | Yes | Positive active HTTP handler limit; native default is 16. |
| `id` | `string` | No | Stable identity of this workspace, file stage, revision or immutable record. |
| `registryDir` | `string \| undefined` | Yes | Native registry directory for workspace metadata. |
| `team` | `string \| undefined` | Yes | Registry team partition; it is not an authorization identity. |
| `maxBytes` | `number \| undefined` | Yes | Maximum managed object bytes; zero means unlimited. |
| `maxObjects` | `number \| undefined` | Yes | Maximum object count; zero means unlimited. |
| `timeoutMs` | `number \| undefined` | Yes | Startup deadline in milliseconds; see the defaults table for this interface. |
| `signal` | `AbortSignal \| undefined` | Yes | AbortSignal for cancellation. A started write can still commit. |

## WorkspaceAdoptOptions



```ts
interface WorkspaceAdoptOptions  extends WorkspaceArchiveOptions {
  readonly handoffPath: string;
  readonly root: string;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `handoffPath` | `string` | No | Path to a same-machine or portable handoff document. |
| `root` | `string` | No |  |
| `registryDir` | `string \| undefined` | Yes | Native registry directory for workspace metadata. |
| `team` | `string \| undefined` | Yes | Registry team partition; it is not an authorization identity. |
| `maxBytes` | `number \| undefined` | Yes | Maximum managed object bytes; zero means unlimited. |
| `maxFiles` | `number \| undefined` | Yes | Maximum files accepted by the archive operation. |
| `includeSensitive` | `boolean \| undefined` | Yes | Explicitly permit paths rejected by the sensitive-filename filter. |

## WorkspaceApplyOptions

The base a delta applies to, and the document that says what changes.

```ts
interface WorkspaceApplyOptions  extends WorkspaceArchiveOptions {
  readonly delta: string;
  readonly base: string;
  readonly expectSHA256?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `delta` | `string` | No |  |
| `base` | `string` | No |  |
| `expectSHA256` | `string \| undefined` | Yes | The digest `createWorkspaceDelta` reported for the document. |
| `registryDir` | `string \| undefined` | Yes | Native registry directory for workspace metadata. |
| `team` | `string \| undefined` | Yes | Registry team partition; it is not an authorization identity. |
| `maxBytes` | `number \| undefined` | Yes | Maximum managed object bytes; zero means unlimited. |
| `maxFiles` | `number \| undefined` | Yes | Maximum files accepted by the archive operation. |
| `includeSensitive` | `boolean \| undefined` | Yes | Explicitly permit paths rejected by the sensitive-filename filter. |

## WorkspaceArchiveOptions



```ts
interface WorkspaceArchiveOptions {
  readonly registryDir?: string | undefined;
  readonly team?: string | undefined;
  readonly maxBytes?: number | undefined;
  readonly maxFiles?: number | undefined;
  readonly includeSensitive?: boolean | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `registryDir` | `string \| undefined` | Yes | Native registry directory for workspace metadata. |
| `team` | `string \| undefined` | Yes | Registry team partition; it is not an authorization identity. |
| `maxBytes` | `number \| undefined` | Yes | Maximum managed object bytes; zero means unlimited. |
| `maxFiles` | `number \| undefined` | Yes | Maximum files accepted by the archive operation. |
| `includeSensitive` | `boolean \| undefined` | Yes | Explicitly permit paths rejected by the sensitive-filename filter. |

## WorkspaceCheckpointOptions



```ts
interface WorkspaceCheckpointOptions {
  readonly id: string;
  readonly registryDir?: string | undefined;
  readonly team?: string | undefined;
  readonly parent?: string | undefined;
  readonly maxBytes?: number | undefined;
  readonly maxFiles?: number | undefined;
  readonly includeSensitive?: boolean | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `id` | `string` | No | Stable identity of this workspace, file stage, revision or immutable record. |
| `registryDir` | `string \| undefined` | Yes | Native registry directory for workspace metadata. |
| `team` | `string \| undefined` | Yes | Registry team partition; it is not an authorization identity. |
| `parent` | `string \| undefined` | Yes |  |
| `maxBytes` | `number \| undefined` | Yes | Maximum managed object bytes; zero means unlimited. |
| `maxFiles` | `number \| undefined` | Yes | Maximum files accepted by the archive operation. |
| `includeSensitive` | `boolean \| undefined` | Yes | Explicitly permit paths rejected by the sensitive-filename filter. |

## WorkspaceDeltaOptions



```ts
interface WorkspaceDeltaOptions  extends WorkspaceArchiveOptions {
  readonly from: string;
  readonly to: string;
  readonly output: string;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `from` | `string` | No |  |
| `to` | `string` | No |  |
| `output` | `string` | No | Destination path for the transfer document or archive. |
| `registryDir` | `string \| undefined` | Yes | Native registry directory for workspace metadata. |
| `team` | `string \| undefined` | Yes | Registry team partition; it is not an authorization identity. |
| `maxBytes` | `number \| undefined` | Yes | Maximum managed object bytes; zero means unlimited. |
| `maxFiles` | `number \| undefined` | Yes | Maximum files accepted by the archive operation. |
| `includeSensitive` | `boolean \| undefined` | Yes | Explicitly permit paths rejected by the sensitive-filename filter. |

## WorkspaceJSON



```ts
type WorkspaceJSON = Readonly<Record<string, unknown>>;
```
