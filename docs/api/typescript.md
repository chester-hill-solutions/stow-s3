# TypeScript process API

Start a private native S3 server, connect to an existing endpoint, or run a scoped session. Import from `@chester-hill-solutions/stow-s3`.

## Defaults and limits

| Setting | Value |
| --- | --- |
| Session maxBytes / maxObjects | 16 MiB / 1,000 objects |
| maxRequestBytes / maxConcurrentRequests | 8 MiB / 16 active requests |
| StartOptions.timeoutMs | 10,000 ms, including initial bucket creation |
| StartOptions.mode | Local unless run-through is explicitly selected; credentials alone do not select it |
| Credentials and port | Generated credentials; use the endpoint returned by startup |
| forcePathStyle | true; credential providers take precedence over static credentials |

## Behavior

- `withStow` closes its session after the callback.
- `openStow` requires an explicit close.
- Sessions strip inherited cloud settings.
- `Stow.start` preserves the child environment unless isolatedEnvironment is enabled.
- A session removes only its own temporary directory; a supplied dataDir remains.
- Closing a connection destroys its SDK client without stopping the remote server.

## Errors

- Startup can fail on cancellation, timeout, missing binaries or invalid readiness.
- StowProtocolError exposes code; StowOwnershipError rejects unsafe directory resets.
- SDK object requests use S3 errors.
- Workspace helpers reject failed commands.
- A callback failure remains observable if cleanup also fails.

## Example

```ts
import { withStow } from "@chester-hill-solutions/stow-s3";
import { PutObjectCommand } from "@aws-sdk/client-s3";

await withStow(async ({ s3, bucket }) => {
  await s3.send(new PutObjectCommand({ Bucket: bucket, Key: "note", Body: "hello" }));
});
```

## Symbols

[AwsSdkV3ConfigOptions](#awssdkv3configoptions) · [checkpointWorkspace](#checkpointworkspace) · [collectWorkspaces](#collectworkspaces) · [ConnectOptions](#connectoptions) · [default](#default) · [DEFAULT_SESSION_MAX_BYTES](#default_session_max_bytes) · [DEFAULT_SESSION_MAX_OBJECTS](#default_session_max_objects) · [destroyWorkspace](#destroyworkspace) · [diffWorkspaces](#diffworkspaces) · [EMBEDDED_PROTOCOL_VERSION](#embedded_protocol_version) · [EmbeddedBucket](#embeddedbucket) · [EmbeddedCapabilities](#embeddedcapabilities) · [EmbeddedHost](#embeddedhost) · [EmbeddedListOptions](#embeddedlistoptions) · [EmbeddedObject](#embeddedobject) · [EmbeddedObjectPage](#embeddedobjectpage) · [EmbeddedPutOptions](#embeddedputoptions) · [EmbeddedStow](#embeddedstow) · [EmbeddedStowError](#embeddedstowerror) · [EmbeddedStowOptions](#embeddedstowoptions) · [EmbeddedUsage](#embeddedusage) · [EphemeralStowOptions](#ephemeralstowoptions) · [exportWorkspaceCheckpoint](#exportworkspacecheckpoint) · [handoffWorkspace](#handoffworkspace) · [importWorkspaceCheckpoint](#importworkspacecheckpoint) · [loadNodeWasmHost](#loadnodewasmhost) · [NodeWasmHost](#nodewasmhost) · [ObjectSnapshot](#objectsnapshot) · [openStow](#openstow) · [parseReadyLine](#parsereadyline) · [parseReadyMessage](#parsereadymessage) · [prepareWorkspace](#prepareworkspace) · [pruneWorkspaces](#pruneworkspaces) · [PutFixtureOptions](#putfixtureoptions) · [READY_PROTOCOL_VERSION](#ready_protocol_version) · [resetOwnedData](#resetowneddata) · [resolveStowBinary](#resolvestowbinary) · [restoreWorkspaceCheckpoint](#restoreworkspacecheckpoint) · [resumeWorkspace](#resumeworkspace) · [ResumeWorkspaceOptions](#resumeworkspaceoptions) · [runWorkspaceCommand](#runworkspacecommand) · [ServedWorkspace](#servedworkspace) · [serveWorkspace](#serveworkspace) · [ServeWorkspaceOptions](#serveworkspaceoptions) · [StartOptions](#startoptions) · [Stow](#stow) · [STOW_OWNER_MARKER](#stow_owner_marker) · [stowBinaryAvailable](#stowbinaryavailable) · [StowBinaryNotFoundError](#stowbinarynotfounderror) · [StowConnection](#stowconnection) · [StowInstance](#stowinstance) · [StowMode](#stowmode) · [StowOwnershipError](#stowownershiperror) · [StowProtocolError](#stowprotocolerror) · [StowReady](#stowready) · [StowReadyCapabilities](#stowreadycapabilities) · [StowSession](#stowsession) · [StowSessionCapabilities](#stowsessioncapabilities) · [UpstreamConfig](#upstreamconfig) · [upstreamFromEnv](#upstreamfromenv) · [withStow](#withstow) · [WorkspaceArchiveOptions](#workspacearchiveoptions) · [WorkspaceCheckpointOptions](#workspacecheckpointoptions) · [WorkspaceJSON](#workspacejson)

## AwsSdkV3ConfigOptions



```ts
type AwsSdkV3ConfigOptions = (AwsSdkV3ConfigBase & {
    accessKeyId: string;
    secretAccessKey: string;
    sessionToken?: string;
    provider?: AwsCredentialIdentityProvider;
}) | (AwsSdkV3ConfigBase & {
    provider: AwsCredentialIdentityProvider;
    accessKeyId?: string;
    secretAccessKey?: string;
    sessionToken?: string;
});
```

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

## ConnectOptions



```ts
type ConnectOptions = AwsSdkV3ConfigOptions;
```

## default

The same Stow helper object as the named export.

```ts
const Stow: { start(options?: StartOptions | undefined): Promise<StowInstance>; connect(options: AwsSdkV3ConfigOptions): StowConnection; awsSdkV3Config(options: StowInstance | AwsSdkV3ConfigOptions): S3ClientConfig; upstream: { fromEnv(): UpstreamConfig | null; }; };
```

### default.start

Start a native server and create the requested buckets before returning its handle.

```ts
start(options?: StartOptions | undefined): Promise<StowInstance>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `StartOptions \| undefined` | No | — |  |

Returns: `Promise<StowInstance>`.

### default.connect

Create an owned SDK client for an existing endpoint; disconnect it when finished.

```ts
connect(options: AwsSdkV3ConfigOptions): StowConnection
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `AwsSdkV3ConfigOptions` | Yes | — |  |

Returns: `StowConnection`.

### default.awsSdkV3Config

Return AWS SDK connection settings with local credentials and path-style addressing by default.

```ts
awsSdkV3Config(options: StowInstance | AwsSdkV3ConfigOptions): S3ClientConfig
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `StowInstance \| AwsSdkV3ConfigOptions` | Yes | — |  |

Returns: `S3ClientConfig`.

## DEFAULT_SESSION_MAX_BYTES

Default session storage limit: 16 MiB.

```ts
const DEFAULT_SESSION_MAX_BYTES: number;
```

## DEFAULT_SESSION_MAX_OBJECTS

Default session object count: 1,000.

```ts
const DEFAULT_SESSION_MAX_OBJECTS: 1000;
```

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

## EMBEDDED_PROTOCOL_VERSION



```ts
const EMBEDDED_PROTOCOL_VERSION: 1;
```

## EmbeddedBucket



```ts
interface EmbeddedBucket {
  name: string;
  creationDate?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `name` | `string` | No |  |
| `creationDate` | `string \| undefined` | Yes |  |

## EmbeddedCapabilities



```ts
interface EmbeddedCapabilities {
  backend: "memory" | "indexeddb" | "host";
  maxBytes: number;
  maxObjects: number;
  persistent: boolean;
  multipart: boolean;
  upstream: boolean;
  conditionalWrites?: boolean | undefined;
  hostMaintenance?: boolean | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `backend` | `"memory" \| "indexeddb" \| "host"` | No | Storage implementation selected or reported by this handle. |
| `maxBytes` | `number` | No | Maximum managed object bytes; zero means unlimited. |
| `maxObjects` | `number` | No | Maximum object count; zero means unlimited. |
| `persistent` | `boolean` | No | Whether this backend retains data after the handle closes. |
| `multipart` | `boolean` | No | Whether this handle supports multipart operations. |
| `upstream` | `boolean` | No | Whether this handle can use an upstream provider. |
| `conditionalWrites` | `boolean \| undefined` | Yes | Whether atomic ETag write conditions are supported. |
| `hostMaintenance` | `boolean \| undefined` | Yes | Whether namespace recovery inspection and garbage collection are supported. |

## EmbeddedHost



```ts
interface EmbeddedHost {
  call(request: string): string;
  callBinary(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined): EmbeddedBinaryResponse;
}
```

### EmbeddedHost.call



```ts
call(request: string): string
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `request` | `string` | Yes | — |  |

Returns: `string`.

### EmbeddedHost.callBinary

Binary calls preserve caller ownership; returned bytes belong to the caller.

```ts
callBinary(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined): EmbeddedBinaryResponse
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `metadata` | `string` | Yes | — | Serialized binary-bridge request or response metadata. |
| `data` | `Uint8Array<ArrayBufferLike> \| undefined` | No | — | Object bytes. Input ownership and size rules depend on the method. |

Returns: `EmbeddedBinaryResponse`.

## EmbeddedListOptions



```ts
interface EmbeddedListOptions {
  prefix?: string | undefined;
  cursor?: string | undefined;
  limit?: number | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `prefix` | `string \| undefined` | Yes | Only list keys beginning with this prefix. |
| `cursor` | `string \| undefined` | Yes | Opaque continuation value returned by the previous page. |
| `limit` | `number \| undefined` | Yes | Maximum results per page; limits and defaults depend on the listing API. |

## EmbeddedObject



```ts
interface EmbeddedObject {
  bucket: string;
  key: string;
  data?: Uint8Array<ArrayBufferLike> | undefined;
  size: number;
  etag: string;
  contentType?: string | undefined;
  metadata?: Record<string, string> | undefined;
  lastModified?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `bucket` | `string` | No | Bucket name. Create it before storing objects. |
| `key` | `string` | No | Object or file key within the selected bucket or project. |
| `data` | `Uint8Array<ArrayBufferLike> \| undefined` | Yes | Object bytes. Input ownership and size rules depend on the method. |
| `size` | `number` | No | Total object or content size in bytes; stream uploads require an exact declaration. |
| `etag` | `string` | No | Content validator returned by the store or provider. |
| `contentType` | `string \| undefined` | Yes | MIME content type attached to the object or file. |
| `metadata` | `Record<string, string> \| undefined` | Yes | User metadata included in storage and sync comparisons. |
| `lastModified` | `string \| undefined` | Yes |  |

## EmbeddedObjectPage



```ts
interface EmbeddedObjectPage {
  objects: EmbeddedObject[];
  truncated: boolean;
  nextCursor?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `objects` | `EmbeddedObject[]` | No | Objects returned by the listing or persistence snapshot. |
| `truncated` | `boolean` | No | Whether additional matching objects remain. |
| `nextCursor` | `string \| undefined` | Yes | Continuation value for the next page when truncated is true. |

## EmbeddedPutOptions



```ts
interface EmbeddedPutOptions {
  contentType?: string | undefined;
  metadata?: Record<string, string> | undefined;
  ifMatch?: string | undefined;
  ifNoneMatch?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `contentType` | `string \| undefined` | Yes | MIME content type attached to the object or file. |
| `metadata` | `Record<string, string> \| undefined` | Yes | User metadata included in storage and sync comparisons. |
| `ifMatch` | `string \| undefined` | Yes | Write only when the current content ETag matches; empty means unset. |
| `ifNoneMatch` | `string \| undefined` | Yes | Use * to create only when absent; empty means unset. |

## EmbeddedStow



```ts
class EmbeddedStow {
  static open(host: EmbeddedHost, options?: EmbeddedStowOptions): EmbeddedStow;
  readonly handle: number;
  capabilities(): EmbeddedCapabilities;
  usage(): EmbeddedUsage;
  createBucket(bucket: string): void;
  deleteBucket(bucket: string): void;
  listBuckets(): EmbeddedBucket[];
  putObject(bucket: string, key: string, data: Uint8Array<ArrayBufferLike>, options?: EmbeddedPutOptions): EmbeddedObject;
  getObject(bucket: string, key: string): EmbeddedObject;
  headObject(bucket: string, key: string): EmbeddedObject;
  listObjects(bucket: string, options?: EmbeddedListOptions): EmbeddedObjectPage;
  deleteObject(bucket: string, key: string): void;
  copyObject(sourceBucket: string, sourceKey: string, destinationBucket: string, destinationKey: string): EmbeddedObject;
  reset(): void;
  close(): void;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `handle` | `number` | No | Open runtime handle issued by the WASM bridge. |

### EmbeddedStow.open

Open a handle. Retain it until operations finish, then close it.

```ts
open(host: EmbeddedHost, options?: EmbeddedStowOptions): EmbeddedStow
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `host` | `EmbeddedHost` | Yes | — | Bind address; native default is 127.0.0.1. |
| `options` | `EmbeddedStowOptions` | No | {} |  |

Returns: `EmbeddedStow`.

### EmbeddedStow.capabilities

Return the operations and limits supported by this handle.

```ts
capabilities(): EmbeddedCapabilities
```

Returns: `EmbeddedCapabilities`.

### EmbeddedStow.usage

Return current storage accounting.

```ts
usage(): EmbeddedUsage
```

Returns: `EmbeddedUsage`.

### EmbeddedStow.createBucket

Create the named bucket before storing objects.

```ts
createBucket(bucket: string): void
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |

Returns: `void`.

### EmbeddedStow.deleteBucket

Remove an empty bucket; a nonempty bucket is refused.

```ts
deleteBucket(bucket: string): void
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |

Returns: `void`.

### EmbeddedStow.listBuckets

Return the buckets visible to the handle.

```ts
listBuckets(): EmbeddedBucket[]
```

Returns: `EmbeddedBucket[]`.

### EmbeddedStow.putObject

Store complete bytes and metadata, subject to write conditions and quotas.

```ts
putObject(bucket: string, key: string, data: Uint8Array<ArrayBufferLike>, options?: EmbeddedPutOptions): EmbeddedObject
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `data` | `Uint8Array<ArrayBufferLike>` | Yes | — | Object bytes. Input ownership and size rules depend on the method. |
| `options` | `EmbeddedPutOptions` | No | {} |  |

Returns: `EmbeddedObject`.

### EmbeddedStow.getObject

Return complete object bytes and metadata.

```ts
getObject(bucket: string, key: string): EmbeddedObject
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `EmbeddedObject`.

### EmbeddedStow.headObject

Return object metadata without reading the body into the result.

```ts
headObject(bucket: string, key: string): EmbeddedObject
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `EmbeddedObject`.

### EmbeddedStow.listObjects

Return one page. Pass nextCursor to continue when truncated is true.

```ts
listObjects(bucket: string, options?: EmbeddedListOptions): EmbeddedObjectPage
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `options` | `EmbeddedListOptions` | No | {} |  |

Returns: `EmbeddedObjectPage`.

### EmbeddedStow.deleteObject

Delete the selected object; deleting a missing object is idempotent.

```ts
deleteObject(bucket: string, key: string): void
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `void`.

### EmbeddedStow.copyObject

Copy source bytes and metadata to the destination object.

```ts
copyObject(sourceBucket: string, sourceKey: string, destinationBucket: string, destinationKey: string): EmbeddedObject
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `sourceBucket` | `string` | Yes | — |  |
| `sourceKey` | `string` | Yes | — |  |
| `destinationBucket` | `string` | Yes | — |  |
| `destinationKey` | `string` | Yes | — |  |

Returns: `EmbeddedObject`.

### EmbeddedStow.reset

Clear storage state where the backend supports reset.

```ts
reset(): void
```

Returns: `void`.

### EmbeddedStow.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): void
```

Returns: `void`.

## EmbeddedStowError



```ts
class EmbeddedStowError  extends Error {
  readonly code: string;
  constructor(code: string, message: string);
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `code` | `string` | No | Structured error code; inspect the code rather than matching message text. |
| `name` | `string` | No |  |
| `message` | `string` | No |  |
| `stack` | `string \| undefined` | Yes |  |
| `cause` | `unknown` | Yes |  |

### EmbeddedStowError.constructor

function Object() { [native code] }

```ts
constructor(code: string, message: string)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `code` | `string` | Yes | — | Structured error code; inspect the code rather than matching message text. |
| `message` | `string` | Yes | — |  |

Returns: `EmbeddedStowError`.

## EmbeddedStowOptions



```ts
interface EmbeddedStowOptions {
  maxBytes?: number | undefined;
  maxObjects?: number | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `maxBytes` | `number \| undefined` | Yes | Maximum managed object bytes; zero means unlimited. |
| `maxObjects` | `number \| undefined` | Yes | Maximum object count; zero means unlimited. |

## EmbeddedUsage



```ts
interface EmbeddedUsage {
  bytes: number;
  objects: number;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `bytes` | `number` | No | Current managed object payload bytes. |
| `objects` | `number` | No | Objects returned by the listing or persistence snapshot. |

## EphemeralStowOptions



```ts
interface EphemeralStowOptions {
  readonly maxRequestBytes?: number | undefined;
  maxConcurrentRequests?: number | undefined;
  readonly maxBytes?: number | undefined;
  readonly maxObjects?: number | undefined;
  readonly bucket?: string | undefined;
  readonly timeoutMs?: number | undefined;
  readonly signal?: AbortSignal | undefined;
  readonly dataDir?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `maxRequestBytes` | `number \| undefined` | Yes | Positive HTTP request-body byte limit; native default is 8 MiB. |
| `maxConcurrentRequests` | `number \| undefined` | Yes | Positive active HTTP handler limit; native default is 16. |
| `maxBytes` | `number \| undefined` | Yes | Maximum managed object bytes; zero means unlimited. |
| `maxObjects` | `number \| undefined` | Yes | Maximum object count; zero means unlimited. |
| `bucket` | `string \| undefined` | Yes | Bucket name. Create it before storing objects. |
| `timeoutMs` | `number \| undefined` | Yes | Startup deadline in milliseconds; see the defaults table for this interface. |
| `signal` | `AbortSignal \| undefined` | Yes | AbortSignal for cancellation. A started write can still commit. |
| `dataDir` | `string \| undefined` | Yes | Native data directory. A supplied session directory is preserved on close. |

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

## loadNodeWasmHost



```ts
loadNodeWasmHost(): Promise<NodeWasmHost>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |

Returns: `Promise<NodeWasmHost>`.

## NodeWasmHost



```ts
interface NodeWasmHost  extends EmbeddedHost {
  callBinaryAsync(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined): Promise<EmbeddedBinaryResponse>;
  openHost(metadata: string, store: HostObjectStoreBridge): Promise<EmbeddedBinaryResponse>;
  close(): Promise<void>;
}
```

### NodeWasmHost.callBinaryAsync



```ts
callBinaryAsync(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined): Promise<EmbeddedBinaryResponse>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `metadata` | `string` | Yes | — | Serialized binary-bridge request or response metadata. |
| `data` | `Uint8Array<ArrayBufferLike> \| undefined` | No | — | Object bytes. Input ownership and size rules depend on the method. |

Returns: `Promise<EmbeddedBinaryResponse>`.

### NodeWasmHost.openHost



```ts
openHost(metadata: string, store: HostObjectStoreBridge): Promise<EmbeddedBinaryResponse>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `metadata` | `string` | Yes | — | Serialized binary-bridge request or response metadata. |
| `store` | `HostObjectStoreBridge` | Yes | — | Open underlying object or content store supplied by the application. |

Returns: `Promise<EmbeddedBinaryResponse>`.

### NodeWasmHost.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## ObjectSnapshot



```ts
interface ObjectSnapshot {
  key: string;
  size: number;
  etag?: string | undefined;
  lastModified?: Date | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `key` | `string` | No | Object or file key within the selected bucket or project. |
| `size` | `number` | No | Total object or content size in bytes; stream uploads require an exact declaration. |
| `etag` | `string \| undefined` | Yes | Content validator returned by the store or provider. |
| `lastModified` | `Date \| undefined` | Yes |  |

## openStow

Start a private S3 session and return its owned handle. Call close when finished.

```ts
openStow(options?: EphemeralStowOptions): Promise<StowSession>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `EphemeralStowOptions` | No | {} |  |

Returns: `Promise<StowSession>`.

## parseReadyLine

Parse the pre-protocol single-line readiness announcement.

```ts
parseReadyLine(line: string): ReadyLine | null
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `line` | `string` | Yes | — |  |

Returns: `ReadyLine | null`.

## parseReadyMessage

Parse the single readiness object.

```ts
parseReadyMessage(line: string): StowReady
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `line` | `string` | Yes | — |  |

Returns: `StowReady`.

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

## PutFixtureOptions



```ts
interface PutFixtureOptions {
  contentType?: string | undefined;
  metadata?: Record<string, string> | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `contentType` | `string \| undefined` | Yes | MIME content type attached to the object or file. |
| `metadata` | `Record<string, string> \| undefined` | Yes | User metadata included in storage and sync comparisons. |

## READY_PROTOCOL_VERSION



```ts
const READY_PROTOCOL_VERSION: 1;
```

## resetOwnedData

Delete a stow data directory that stow created, and nothing else.

```ts
resetOwnedData(dataDir: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `dataDir` | `string` | Yes | — | Native data directory. A supplied session directory is preserved on close. |

Returns: `Promise<void>`.

## resolveStowBinary



```ts
resolveStowBinary(): string
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |

Returns: `string`.

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

## StartOptions



```ts
interface StartOptions {
  dataDir?: string | undefined;
  backend?: "memory" | "filesystem" | undefined;
  buckets?: string[] | undefined;
  port?: number | undefined;
  host?: string | undefined;
  timeoutMs?: number | undefined;
  signal?: AbortSignal | undefined;
  region?: string | undefined;
  baseHost?: string | undefined;
  allowPublicAdmin?: boolean | undefined;
  resetOwnedData?: boolean | undefined;
  cleanSlate?: boolean | undefined;
  accessKey?: string | undefined;
  secretKey?: string | undefined;
  mode?: StowMode | "auto" | undefined;
  cacheDir?: string | undefined;
  cacheMaxBytes?: number | undefined;
  cacheMaxObjects?: number | undefined;
  cacheTtlSeconds?: number | undefined;
  allowLiveWrites?: boolean | undefined;
  allowInsecureUpstream?: boolean | undefined;
  maxRequestBytes?: number | undefined;
  maxConcurrentRequests?: number | undefined;
  maxBytes?: number | undefined;
  maxObjects?: number | undefined;
  isolatedEnvironment?: boolean | undefined;
  parentPid?: number | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `dataDir` | `string \| undefined` | Yes | Native data directory. A supplied session directory is preserved on close. |
| `backend` | `"memory" \| "filesystem" \| undefined` | Yes | Storage implementation selected or reported by this handle. |
| `buckets` | `string[] \| undefined` | Yes | Buckets to create before startup resolves, or buckets returned by inspection. |
| `port` | `number \| undefined` | Yes | Requested native TCP port; zero selects an ephemeral port. |
| `host` | `string \| undefined` | Yes | Bind address; native default is 127.0.0.1. |
| `timeoutMs` | `number \| undefined` | Yes | Startup deadline in milliseconds; see the defaults table for this interface. |
| `signal` | `AbortSignal \| undefined` | Yes | AbortSignal for cancellation. A started write can still commit. |
| `region` | `string \| undefined` | Yes | Region used for S3 signing; native default is us-east-1. |
| `baseHost` | `string \| undefined` | Yes | Hostname used for virtual-hosted S3 requests. |
| `allowPublicAdmin` | `boolean \| undefined` | Yes | Deprecated and ignored; admin-token rules still apply. |
| `resetOwnedData` | `boolean \| undefined` | Yes | Clear only a Stow-owned data directory before startup; protected paths are refused. |
| `cleanSlate` | `boolean \| undefined` | Yes | Deprecated alias for resetOwnedData, with identical ownership checks. |
| `accessKey` | `string \| undefined` | Yes | Configured S3 access key ID. |
| `secretKey` | `string \| undefined` | Yes | Configured S3 signing secret. |
| `mode` | `StowMode \| "auto" \| undefined` | Yes | Select local or run-through explicitly. Legacy auto does not select an upstream from credentials alone. |
| `cacheDir` | `string \| undefined` | Yes | Directory for upstream-derived cached objects, separate from dataDir. |
| `cacheMaxBytes` | `number \| undefined` | Yes | Maximum cache payload bytes. |
| `cacheMaxObjects` | `number \| undefined` | Yes | Maximum cached object count. |
| `cacheTtlSeconds` | `number \| undefined` | Yes | Cache entry lifetime in seconds, measured from publication. |
| `allowLiveWrites` | `boolean \| undefined` | Yes | Explicit consent to propagate native run-through writes upstream. |
| `allowInsecureUpstream` | `boolean \| undefined` | Yes | Allow HTTP upstream transport beyond literal loopback addresses. |
| `maxRequestBytes` | `number \| undefined` | Yes | Positive HTTP request-body byte limit; native default is 8 MiB. |
| `maxConcurrentRequests` | `number \| undefined` | Yes | Positive active HTTP handler limit; native default is 16. |
| `maxBytes` | `number \| undefined` | Yes | Maximum managed object bytes; zero means unlimited. |
| `maxObjects` | `number \| undefined` | Yes | Maximum object count; zero means unlimited. |
| `isolatedEnvironment` | `boolean \| undefined` | Yes | Remove inherited STOW_*, S3_* and AWS_* variables from the child environment. |
| `parentPid` | `number \| undefined` | Yes | Stop the native server when this parent process dies. |

## Stow

Server startup, endpoint connection and AWS SDK configuration helpers.

```ts
const Stow: { start(options?: StartOptions | undefined): Promise<StowInstance>; connect(options: AwsSdkV3ConfigOptions): StowConnection; awsSdkV3Config(options: StowInstance | AwsSdkV3ConfigOptions): S3ClientConfig; upstream: { fromEnv(): UpstreamConfig | null; }; };
```

### Stow.start

Start a native server and create the requested buckets before returning its handle.

```ts
start(options?: StartOptions | undefined): Promise<StowInstance>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `StartOptions \| undefined` | No | — |  |

Returns: `Promise<StowInstance>`.

### Stow.connect

Create an owned SDK client for an existing endpoint; disconnect it when finished.

```ts
connect(options: AwsSdkV3ConfigOptions): StowConnection
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `AwsSdkV3ConfigOptions` | Yes | — |  |

Returns: `StowConnection`.

### Stow.awsSdkV3Config

Return AWS SDK connection settings with local credentials and path-style addressing by default.

```ts
awsSdkV3Config(options: StowInstance | AwsSdkV3ConfigOptions): S3ClientConfig
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `StowInstance \| AwsSdkV3ConfigOptions` | Yes | — |  |

Returns: `S3ClientConfig`.

## STOW_OWNER_MARKER

The file a stow server writes into a data directory it has initialized.

```ts
const STOW_OWNER_MARKER: ".stow-owner";
```

## stowBinaryAvailable



```ts
stowBinaryAvailable(): boolean
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |

Returns: `boolean`.

## StowBinaryNotFoundError

StowBinaryNotFoundError reports that no runnable server binary was found.

```ts
class StowBinaryNotFoundError  extends Error {
  readonly code: "binary_not_found";
  constructor(resolved: string);
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `code` | `"binary_not_found"` | No | Structured error code; inspect the code rather than matching message text. |
| `name` | `string` | No |  |
| `message` | `string` | No |  |
| `stack` | `string \| undefined` | Yes |  |
| `cause` | `unknown` | Yes |  |

### StowBinaryNotFoundError.constructor

function Object() { [native code] }

```ts
constructor(resolved: string)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `resolved` | `string` | Yes | — |  |

Returns: `StowBinaryNotFoundError`.

## StowConnection



```ts
interface StowConnection {
  endpoint: string;
  accessKeyId?: string | undefined;
  secretAccessKey?: string | undefined;
  sessionToken?: string | undefined;
  provider?: AwsCredentialIdentityProvider | undefined;
  region: string;
  client: S3Client;
  awsSdkV3Config(): S3ClientConfig;
  disconnect(): void;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `endpoint` | `string` | No | S3 service URL, including scheme and port. |
| `accessKeyId` | `string \| undefined` | Yes | Local S3 access key ID. |
| `secretAccessKey` | `string \| undefined` | Yes | Local S3 signing secret. |
| `sessionToken` | `string \| undefined` | Yes | Temporary AWS credential session token, when required. |
| `provider` | `AwsCredentialIdentityProvider \| undefined` | Yes | AWS credential provider; takes precedence over static credentials. |
| `region` | `string` | No | Region used for S3 signing; native default is us-east-1. |
| `client` | `S3Client` | No | Caller-owned or handle-owned AWS SDK client; follow the interface's close rules. |

### StowConnection.awsSdkV3Config

Return AWS SDK connection settings with local credentials and path-style addressing by default.

```ts
awsSdkV3Config(): S3ClientConfig
```

Returns: `S3ClientConfig`.

### StowConnection.disconnect



```ts
disconnect(): void
```

Returns: `void`.

## StowInstance



```ts
interface StowInstance {
  endpoint: string;
  accessKeyId: string;
  secretAccessKey: string;
  region: string;
  mode: StowMode;
  dataDir: string;
  stop(): Promise<void>;
  awsSdkV3Config(): S3ClientConfig;
  createBucket(name: string): Promise<void>;
  emptyBucket(name: string): Promise<void>;
  putFixture(bucket: string, key: string, body: any, options?: PutFixtureOptions | undefined): Promise<void>;
  snapshotObjects(bucket: string, prefix?: string | undefined): Promise<ObjectSnapshot[]>;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `endpoint` | `string` | No | S3 service URL, including scheme and port. |
| `accessKeyId` | `string` | No | Local S3 access key ID. |
| `secretAccessKey` | `string` | No | Local S3 signing secret. |
| `region` | `string` | No | Region used for S3 signing; native default is us-east-1. |
| `mode` | `StowMode` | No |  |
| `dataDir` | `string` | No | Native data directory. A supplied session directory is preserved on close. |

### StowInstance.stop



```ts
stop(): Promise<void>
```

Returns: `Promise<void>`.

### StowInstance.awsSdkV3Config

Return AWS SDK connection settings with local credentials and path-style addressing by default.

```ts
awsSdkV3Config(): S3ClientConfig
```

Returns: `S3ClientConfig`.

### StowInstance.createBucket

Create the named bucket before storing objects.

```ts
createBucket(name: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `name` | `string` | Yes | — |  |

Returns: `Promise<void>`.

### StowInstance.emptyBucket



```ts
emptyBucket(name: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `name` | `string` | Yes | — |  |

Returns: `Promise<void>`.

### StowInstance.putFixture



```ts
putFixture(bucket: string, key: string, body: any, options?: PutFixtureOptions | undefined): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `body` | `any` | Yes | — | Complete object payload for the selected native SDK helper. |
| `options` | `PutFixtureOptions \| undefined` | No | — |  |

Returns: `Promise<void>`.

### StowInstance.snapshotObjects



```ts
snapshotObjects(bucket: string, prefix?: string | undefined): Promise<ObjectSnapshot[]>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `prefix` | `string \| undefined` | No | — | Only list keys beginning with this prefix. |

Returns: `Promise<ObjectSnapshot[]>`.

## StowMode



```ts
type StowMode = "local" | "run-through";
```

## StowOwnershipError

A directory stow did not create is never deleted on a caller's behalf.

```ts
class StowOwnershipError  extends Error {
  readonly code: "ownership";
  constructor(dataDir: string, reason: string);
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `code` | `"ownership"` | No | Structured error code; inspect the code rather than matching message text. |
| `dataDir` | `string` | No | Native data directory. A supplied session directory is preserved on close. |
| `name` | `string` | No |  |
| `message` | `string` | No |  |
| `stack` | `string \| undefined` | Yes |  |
| `cause` | `unknown` | Yes |  |

### StowOwnershipError.constructor

function Object() { [native code] }

```ts
constructor(dataDir: string, reason: string)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `dataDir` | `string` | Yes | — | Native data directory. A supplied session directory is preserved on close. |
| `reason` | `string` | Yes | — |  |

Returns: `StowOwnershipError`.

## StowProtocolError



```ts
class StowProtocolError  extends Error {
  readonly code: StowErrorCode;
  constructor(code: StowErrorCode, message: string);
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `code` | `StowErrorCode` | No | Structured error code; inspect the code rather than matching message text. |
| `name` | `string` | No |  |
| `message` | `string` | No |  |
| `stack` | `string \| undefined` | Yes |  |
| `cause` | `unknown` | Yes |  |

### StowProtocolError.constructor

function Object() { [native code] }

```ts
constructor(code: StowErrorCode, message: string)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `code` | `StowErrorCode` | Yes | — | Structured error code; inspect the code rather than matching message text. |
| `message` | `string` | Yes | — |  |

Returns: `StowProtocolError`.

## StowReady



```ts
interface StowReady {
  protocolVersion: number;
  binaryVersion: string;
  endpoint: string;
  region: string;
  accessKeyId: string;
  secretAccessKey: string;
  mode: string;
  backend: string;
  capabilities: StowReadyCapabilities;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `protocolVersion` | `number` | No | Readiness protocol version supported by this client. |
| `binaryVersion` | `string` | No | Native binary version reported by readiness. |
| `endpoint` | `string` | No | S3 service URL, including scheme and port. |
| `region` | `string` | No | Region used for S3 signing; native default is us-east-1. |
| `accessKeyId` | `string` | No | Local S3 access key ID. |
| `secretAccessKey` | `string` | No | Local S3 signing secret. |
| `mode` | `string` | No |  |
| `backend` | `string` | No | Storage implementation selected or reported by this handle. |
| `capabilities` | `StowReadyCapabilities` | No |  |

## StowReadyCapabilities



```ts
interface StowReadyCapabilities {
  persistent: boolean;
  multipart: boolean;
  upstream: boolean;
  conditionalWrites: boolean;
  presignedUrls: boolean;
  maxBytes: number;
  maxObjects: number;
  maxRequestBytes: number;
  maxConcurrentRequests?: number | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `persistent` | `boolean` | No | Whether this backend retains data after the handle closes. |
| `multipart` | `boolean` | No | Whether this handle supports multipart operations. |
| `upstream` | `boolean` | No | Whether this handle can use an upstream provider. |
| `conditionalWrites` | `boolean` | No | Whether atomic ETag write conditions are supported. |
| `presignedUrls` | `boolean` | No |  |
| `maxBytes` | `number` | No | Maximum managed object bytes; zero means unlimited. |
| `maxObjects` | `number` | No | Maximum object count; zero means unlimited. |
| `maxRequestBytes` | `number` | No | Positive HTTP request-body byte limit; native default is 8 MiB. |
| `maxConcurrentRequests` | `number \| undefined` | Yes | Positive active HTTP handler limit; native default is 16. |

## StowSession



```ts
interface StowSession {
  readonly bucket: string;
  readonly endpoint: string;
  readonly dataDir: string;
  readonly s3: S3Client;
  awsSdkV3Config(): S3ClientConfig;
  capabilities(): StowSessionCapabilities;
  handoff(): Record<string, string>;
  close(): Promise<void>;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `bucket` | `string` | No | Bucket name. Create it before storing objects. |
| `endpoint` | `string` | No | S3 service URL, including scheme and port. |
| `dataDir` | `string` | No | Native data directory. A supplied session directory is preserved on close. |
| `s3` | `S3Client` | No | Session-owned S3 client, destroyed on close. |

### StowSession.awsSdkV3Config

Return AWS SDK connection settings with local credentials and path-style addressing by default.

```ts
awsSdkV3Config(): S3ClientConfig
```

Returns: `S3ClientConfig`.

### StowSession.capabilities

Return the operations and limits supported by this handle.

```ts
capabilities(): StowSessionCapabilities
```

Returns: `StowSessionCapabilities`.

### StowSession.handoff

A fresh environment mapping for a child process that needs to reach this session.

```ts
handoff(): Record<string, string>
```

Returns: `Record<string, string>`.

### StowSession.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## StowSessionCapabilities



```ts
interface StowSessionCapabilities {
  backend: string;
  persistent: boolean;
  multipart: boolean;
  upstream: boolean;
  maxBytes: number;
  maxObjects: number;
  maxRequestBytes: number;
  maxConcurrentRequests: number;
  protocolVersion: number;
  binaryVersion: string;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `backend` | `string` | No | Storage implementation selected or reported by this handle. |
| `persistent` | `boolean` | No | Whether this backend retains data after the handle closes. |
| `multipart` | `boolean` | No | Whether this handle supports multipart operations. |
| `upstream` | `boolean` | No | Whether this handle can use an upstream provider. |
| `maxBytes` | `number` | No | Maximum managed object bytes; zero means unlimited. |
| `maxObjects` | `number` | No | Maximum object count; zero means unlimited. |
| `maxRequestBytes` | `number` | No | Positive HTTP request-body byte limit; native default is 8 MiB. |
| `maxConcurrentRequests` | `number` | No | Positive active HTTP handler limit; native default is 16. |
| `protocolVersion` | `number` | No | Readiness protocol version supported by this client. |
| `binaryVersion` | `string` | No | Native binary version reported by readiness. |

## UpstreamConfig



```ts
interface UpstreamConfig {
  endpoint: string;
  accessKey: string;
  secretKey: string;
  sessionToken?: string | undefined;
  region?: string | undefined;
  bucket?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `endpoint` | `string` | No | S3 service URL, including scheme and port. |
| `accessKey` | `string` | No | Configured S3 access key ID. |
| `secretKey` | `string` | No | Configured S3 signing secret. |
| `sessionToken` | `string \| undefined` | Yes | Temporary AWS credential session token, when required. |
| `region` | `string \| undefined` | Yes | Region used for S3 signing; native default is us-east-1. |
| `bucket` | `string \| undefined` | Yes | Bucket name. Create it before storing objects. |

## upstreamFromEnv

Resolve upstream credentials from environment variables.

```ts
upstreamFromEnv(): UpstreamConfig | null
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |

Returns: `UpstreamConfig | null`.

## withStow

Run the callback in a private session and always close it.

```ts
withStow<T>(use: (session: StowSession) => T | Promise<T>, options?: EphemeralStowOptions): Promise<T>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `use` | `(session: StowSession) => T \| Promise<T>` | Yes | — |  |
| `options` | `EphemeralStowOptions` | No | {} |  |

Returns: `Promise<T>`.

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

## WorkspaceJSON



```ts
type WorkspaceJSON = Readonly<Record<string, unknown>>;
```
