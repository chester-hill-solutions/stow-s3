# Browser persistence and file API

Store objects in IndexedDB and retain file stages for undo/redo and explicit sync. Import from `@chester-hill-solutions/stow-s3/browser`.

## Defaults and limits

| Setting | Value |
| --- | --- |
| BrowserFileHistory.maxVersions | 50; minimum 2 |
| remoteBucket | Required for file history; remote transport is optional until needed |
| Persistence ownership | Exclusive per namespace; Web Locks across tabs |
| Save completion | After durable commit, not after memory mutation |

## Behavior

- openBrowserEmbeddedStow needs an EmbeddedHost and persistence adapter.
- read fetches remotely only on a local miss.
- save and saveReceipt never upload; saveReceipt omits returned file bytes.
- sync uploads the selected stage.
- undo/redo persist the cursor.
- A save after undo removes the redo branch.
- adoptRemote requires any uncertain attempt to be resolved first.
- File-history instances do not close their underlying store or host.

## Errors

- BrowserPersistenceError exposes the BrowserPersistenceErrorCode union.
- FileHistoryError exposes FileHistoryErrorCode, including conflict, history_boundary, history_corrupt and uncertain_write.
- Quota or persistence failures restore the previous committed state.
- Without Web Locks, the original adapter checks ownership only within one JavaScript realm.

## Example

```ts
import { BrowserFileHistory } from "@chester-hill-solutions/stow-s3/browser";

const files = await BrowserFileHistory.open(store, { remoteBucket: "files", remote });
await files.save("note.txt", new TextEncoder().encode("offline edit"));
await files.undo("note.txt");
await files.redo("note.txt");
const status = await files.sync("note.txt");
// store and remote are supplied by your application.
```

## Symbols

[BROWSER_PERSISTENCE_FORMAT_VERSION](#browser_persistence_format_version) · [BrowserEmbeddedStow](#browserembeddedstow) · [BrowserFileHistory](#browserfilehistory) · [BrowserFileHistoryOptions](#browserfilehistoryoptions) · [BrowserObjectMutation](#browserobjectmutation) · [BrowserPersistenceError](#browserpersistenceerror) · [BrowserPersistenceErrorCode](#browserpersistenceerrorcode) · [DeleteBucketChange](#deletebucketchange) · [DeleteObjectChange](#deleteobjectchange) · [FileHistoryError](#filehistoryerror) · [FileHistoryErrorCode](#filehistoryerrorcode) · [FileHistoryExport](#filehistoryexport) · [FileHistoryStatus](#filehistorystatus) · [FileReadResult](#filereadresult) · [FileSaveOptions](#filesaveoptions) · [FileSaveReceipt](#filesavereceipt) · [FileVersion](#fileversion) · [IndexedDbPersistenceAdapter](#indexeddbpersistenceadapter) · [IndexedDbPersistenceAdapterOptions](#indexeddbpersistenceadapteroptions) · [LockManagerLike](#lockmanagerlike) · [ObjectSyncRemote](#objectsyncremote) · [openBrowserEmbeddedStow](#openbrowserembeddedstow) · [PersistenceAdapter](#persistenceadapter) · [PersistenceChange](#persistencechange) · [PersistenceSnapshot](#persistencesnapshot) · [PersistentEmbeddedStowOptions](#persistentembeddedstowoptions) · [PutBucketChange](#putbucketchange) · [PutObjectChange](#putobjectchange) · [RemoteObjectBasis](#remoteobjectbasis) · [SyncRemoteObject](#syncremoteobject) · [SyncRemoteWrite](#syncremotewrite)

## BROWSER_PERSISTENCE_FORMAT_VERSION

Supported IndexedDB object format version: 1.

```ts
const BROWSER_PERSISTENCE_FORMAT_VERSION: 1;
```

## BrowserEmbeddedStow



```ts
class BrowserEmbeddedStow {
  static open(host: EmbeddedHost, options: PersistentEmbeddedStowOptions): Promise<BrowserEmbeddedStow>;
  static create(host: EmbeddedHost, adapter: PersistenceAdapter, namespace: string, options?: EmbeddedStowOptions): Promise<BrowserEmbeddedStow>;
  readonly handle: number;
  capabilities(): Promise<EmbeddedCapabilities>;
  usage(): Promise<EmbeddedUsage>;
  createBucket(bucket: string): Promise<void>;
  deleteBucket(bucket: string): Promise<void>;
  listBuckets(): Promise<EmbeddedBucket[]>;
  putObject(bucket: string, key: string, data: Uint8Array<ArrayBufferLike>, options?: EmbeddedPutOptions): Promise<EmbeddedObject>;
  getObject(bucket: string, key: string): Promise<EmbeddedObject>;
  headObject(bucket: string, key: string): Promise<EmbeddedObject>;
  listObjects(bucket: string, options?: EmbeddedListOptions): Promise<EmbeddedObjectPage>;
  deleteObject(bucket: string, key: string): Promise<void>;
  writeBatch(mutations: BrowserObjectMutation[]): Promise<void>;
  copyObject(sourceBucket: string, sourceKey: string, destinationBucket: string, destinationKey: string): Promise<EmbeddedObject>;
  reset(): Promise<void>;
  close(): Promise<void>;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `handle` | `number` | No | Open runtime handle issued by the WASM bridge. |

### BrowserEmbeddedStow.open

Open a handle. Retain it until operations finish, then close it.

```ts
open(host: EmbeddedHost, options: PersistentEmbeddedStowOptions): Promise<BrowserEmbeddedStow>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `host` | `EmbeddedHost` | Yes | — | Bind address; native default is 127.0.0.1. |
| `options` | `PersistentEmbeddedStowOptions` | Yes | — |  |

Returns: `Promise<BrowserEmbeddedStow>`.

### BrowserEmbeddedStow.create



```ts
create(host: EmbeddedHost, adapter: PersistenceAdapter, namespace: string, options?: EmbeddedStowOptions): Promise<BrowserEmbeddedStow>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `host` | `EmbeddedHost` | Yes | — | Bind address; native default is 127.0.0.1. |
| `adapter` | `PersistenceAdapter` | Yes | — | Content persistence adapter; its namespace must be open before operations. |
| `namespace` | `string` | Yes | — | Nonempty local persistence namespace; ownership is exclusive. |
| `options` | `EmbeddedStowOptions` | No | {} |  |

Returns: `Promise<BrowserEmbeddedStow>`.

### BrowserEmbeddedStow.capabilities

Return the operations and limits supported by this handle.

```ts
capabilities(): Promise<EmbeddedCapabilities>
```

Returns: `Promise<EmbeddedCapabilities>`.

### BrowserEmbeddedStow.usage

Return current storage accounting.

```ts
usage(): Promise<EmbeddedUsage>
```

Returns: `Promise<EmbeddedUsage>`.

### BrowserEmbeddedStow.createBucket

Create the named bucket before storing objects.

```ts
createBucket(bucket: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |

Returns: `Promise<void>`.

### BrowserEmbeddedStow.deleteBucket

Remove an empty bucket; a nonempty bucket is refused.

```ts
deleteBucket(bucket: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |

Returns: `Promise<void>`.

### BrowserEmbeddedStow.listBuckets

Return the buckets visible to the handle.

```ts
listBuckets(): Promise<EmbeddedBucket[]>
```

Returns: `Promise<EmbeddedBucket[]>`.

### BrowserEmbeddedStow.putObject

Store complete bytes and metadata, subject to write conditions and quotas.

```ts
putObject(bucket: string, key: string, data: Uint8Array<ArrayBufferLike>, options?: EmbeddedPutOptions): Promise<EmbeddedObject>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `data` | `Uint8Array<ArrayBufferLike>` | Yes | — | Object bytes. Input ownership and size rules depend on the method. |
| `options` | `EmbeddedPutOptions` | No | {} |  |

Returns: `Promise<EmbeddedObject>`.

### BrowserEmbeddedStow.getObject

Return complete object bytes and metadata.

```ts
getObject(bucket: string, key: string): Promise<EmbeddedObject>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<EmbeddedObject>`.

### BrowserEmbeddedStow.headObject

Return object metadata without reading the body into the result.

```ts
headObject(bucket: string, key: string): Promise<EmbeddedObject>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<EmbeddedObject>`.

### BrowserEmbeddedStow.listObjects

Return one page. Pass nextCursor to continue when truncated is true.

```ts
listObjects(bucket: string, options?: EmbeddedListOptions): Promise<EmbeddedObjectPage>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `options` | `EmbeddedListOptions` | No | {} |  |

Returns: `Promise<EmbeddedObjectPage>`.

### BrowserEmbeddedStow.deleteObject

Delete the selected object; deleting a missing object is idempotent.

```ts
deleteObject(bucket: string, key: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<void>`.

### BrowserEmbeddedStow.writeBatch

Commit all related object mutations in one durable transaction or restore the previous state.

```ts
writeBatch(mutations: BrowserObjectMutation[]): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `mutations` | `BrowserObjectMutation[]` | Yes | — |  |

Returns: `Promise<void>`.

### BrowserEmbeddedStow.copyObject

Copy source bytes and metadata to the destination object.

```ts
copyObject(sourceBucket: string, sourceKey: string, destinationBucket: string, destinationKey: string): Promise<EmbeddedObject>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `sourceBucket` | `string` | Yes | — |  |
| `sourceKey` | `string` | Yes | — |  |
| `destinationBucket` | `string` | Yes | — |  |
| `destinationKey` | `string` | Yes | — |  |

Returns: `Promise<EmbeddedObject>`.

### BrowserEmbeddedStow.reset

Clear storage state where the backend supports reset.

```ts
reset(): Promise<void>
```

Returns: `Promise<void>`.

### BrowserEmbeddedStow.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## BrowserFileHistory

Durable file stages over the browser store.

```ts
class BrowserFileHistory {
  static open(store: BrowserEmbeddedStow, options: BrowserFileHistoryOptions): Promise<BrowserFileHistory>;
  read(key: string): Promise<FileReadResult>;
  save(key: string, data: Uint8Array<ArrayBufferLike>, options?: FileSaveOptions): Promise<FileReadResult>;
  saveReceipt(key: string, data: Uint8Array<ArrayBufferLike>, options?: FileSaveOptions): Promise<FileSaveReceipt>;
  status(key: string): Promise<FileHistoryStatus>;
  exportRetained(key: string, visit: (version: FileVersion, bytes: Uint8Array<ArrayBufferLike>, history: FileHistoryExport) => Promise<void>): Promise<FileHistoryExport>;
  undo(key: string): Promise<FileReadResult>;
  redo(key: string): Promise<FileReadResult>;
  sync(key: string): Promise<FileHistoryStatus>;
  adoptRemote(key: string): Promise<FileReadResult>;
}
```

### BrowserFileHistory.open

Open a handle. Retain it until operations finish, then close it.

```ts
open(store: BrowserEmbeddedStow, options: BrowserFileHistoryOptions): Promise<BrowserFileHistory>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `store` | `BrowserEmbeddedStow` | Yes | — | Open underlying object or content store supplied by the application. |
| `options` | `BrowserFileHistoryOptions` | Yes | — |  |

Returns: `Promise<BrowserFileHistory>`.

### BrowserFileHistory.read

Read the selected file's bytes.

```ts
read(key: string): Promise<FileReadResult>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<FileReadResult>`.

### BrowserFileHistory.save

Commit a complete local file stage before resolving.

```ts
save(key: string, data: Uint8Array<ArrayBufferLike>, options?: FileSaveOptions): Promise<FileReadResult>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `data` | `Uint8Array<ArrayBufferLike>` | Yes | — | Object bytes. Input ownership and size rules depend on the method. |
| `options` | `FileSaveOptions` | No | {} |  |

Returns: `Promise<FileReadResult>`.

### BrowserFileHistory.saveReceipt

Commit a local stage and return its receipt without copying file bytes back.

```ts
saveReceipt(key: string, data: Uint8Array<ArrayBufferLike>, options?: FileSaveOptions): Promise<FileSaveReceipt>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `data` | `Uint8Array<ArrayBufferLike>` | Yes | — | Object bytes. Input ownership and size rules depend on the method. |
| `options` | `FileSaveOptions` | No | {} |  |

Returns: `Promise<FileSaveReceipt>`.

### BrowserFileHistory.status

Return the selected revision, retained history and pending sync state.

```ts
status(key: string): Promise<FileHistoryStatus>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<FileHistoryStatus>`.

### BrowserFileHistory.exportRetained

Call visit for each retained stage while holding source history stable.

```ts
exportRetained(key: string, visit: (version: FileVersion, bytes: Uint8Array<ArrayBufferLike>, history: FileHistoryExport) => Promise<void>): Promise<FileHistoryExport>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `visit` | `(version: FileVersion, bytes: Uint8Array<ArrayBufferLike>, history: FileHistoryExport) => Promise<void>` | Yes | — |  |

Returns: `Promise<FileHistoryExport>`.

### BrowserFileHistory.undo

Select the preceding retained revision; fail at the history boundary.

```ts
undo(key: string): Promise<FileReadResult>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<FileReadResult>`.

### BrowserFileHistory.redo

Select the next retained revision; fail at the history boundary.

```ts
redo(key: string): Promise<FileReadResult>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<FileReadResult>`.

### BrowserFileHistory.sync

Publish the selected revision with the saved remote condition; preserve local work on conflict.

```ts
sync(key: string): Promise<FileHistoryStatus>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<FileHistoryStatus>`.

### BrowserFileHistory.adoptRemote

Record the current remote bytes as a confirmed stage after reconciliation.

```ts
adoptRemote(key: string): Promise<FileReadResult>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<FileReadResult>`.

## BrowserFileHistoryOptions



```ts
interface BrowserFileHistoryOptions {
  remoteBucket: string;
  remote?: ObjectSyncRemote | undefined;
  historyBucket?: string | undefined;
  maxVersions?: number | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `remoteBucket` | `string` | No | Remote bucket used by file or project sync. |
| `remote` | `ObjectSyncRemote \| undefined` | Yes | Application-supplied authenticated transport for conditional remote sync. |
| `historyBucket` | `string \| undefined` | Yes | Dedicated local history bucket; do not modify it through another API. |
| `maxVersions` | `number \| undefined` | Yes | Retained revision count. Defaults to 50; must be at least 2. |

## BrowserObjectMutation

All changes in a batch commit together, including failures in the WASM runtime.

```ts
type BrowserObjectMutation = {
    type: "put";
    bucket: string;
    key: string;
    data: Uint8Array;
    options?: EmbeddedPutOptions;
} | {
    type: "delete";
    bucket: string;
    key: string;
};
```

## BrowserPersistenceError



```ts
class BrowserPersistenceError  extends Error {
  readonly code: BrowserPersistenceErrorCode;
  constructor(code: BrowserPersistenceErrorCode, message: string);
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `code` | `BrowserPersistenceErrorCode` | No | Structured error code; inspect the code rather than matching message text. |
| `name` | `string` | No |  |
| `message` | `string` | No |  |
| `stack` | `string \| undefined` | Yes |  |
| `cause` | `unknown` | Yes |  |

### BrowserPersistenceError.constructor

function Object() { [native code] }

```ts
constructor(code: BrowserPersistenceErrorCode, message: string)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `code` | `BrowserPersistenceErrorCode` | Yes | — | Structured error code; inspect the code rather than matching message text. |
| `message` | `string` | Yes | — |  |

Returns: `BrowserPersistenceError`.

## BrowserPersistenceErrorCode



```ts
type BrowserPersistenceErrorCode = "persistence_unsupported" | "persistence_denied" | "already_open" | "persistence_error" | "persistence_corrupt" | "quota_exceeded" | "closed";
```

## DeleteBucketChange



```ts
type DeleteBucketChange = {
    type: "deleteBucket";
    bucket: string;
};
```

## DeleteObjectChange



```ts
type DeleteObjectChange = {
    type: "deleteObject";
    bucket: string;
    key: string;
};
```

## FileHistoryError



```ts
class FileHistoryError  extends Error {
  constructor(code: FileHistoryErrorCode, message: string);
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `code` | `FileHistoryErrorCode` | No | Structured error code; inspect the code rather than matching message text. |
| `name` | `string` | No |  |
| `message` | `string` | No |  |
| `stack` | `string \| undefined` | Yes |  |
| `cause` | `unknown` | Yes |  |

### FileHistoryError.constructor

function Object() { [native code] }

```ts
constructor(code: FileHistoryErrorCode, message: string)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `code` | `FileHistoryErrorCode` | Yes | — | Structured error code; inspect the code rather than matching message text. |
| `message` | `string` | Yes | — |  |

Returns: `FileHistoryError`.

## FileHistoryErrorCode



```ts
type FileHistoryErrorCode = "not_found" | "conflict" | "history_boundary" | "remote_unavailable" | "history_corrupt" | "invalid_options" | "uncertain_write";
```

## FileHistoryExport



```ts
interface FileHistoryExport {
  key: string;
  remoteBucket: string;
  versions: FileVersion[];
  currentVersionId: string;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `key` | `string` | No | Object or file key within the selected bucket or project. |
| `remoteBucket` | `string` | No | Remote bucket used by file or project sync. |
| `versions` | `FileVersion[]` | No | Retained revisions in history order. |
| `currentVersionId` | `string` | No | Selected local stage or project revision ID. |

## FileHistoryStatus



```ts
interface FileHistoryStatus {
  key: string;
  versions: FileVersion[];
  currentVersionId: string;
  pending: boolean;
  canUndo: boolean;
  canRedo: boolean;
  remoteETag?: string | undefined;
  hasUncertainWrite: boolean;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `key` | `string` | No | Object or file key within the selected bucket or project. |
| `versions` | `FileVersion[]` | No | Retained revisions in history order. |
| `currentVersionId` | `string` | No | Selected local stage or project revision ID. |
| `pending` | `boolean` | No | Whether the selected revision still needs remote confirmation. |
| `canUndo` | `boolean` | No | Whether an earlier retained revision can be selected. |
| `canRedo` | `boolean` | No | Whether a later retained revision can be selected. |
| `remoteETag` | `string \| undefined` | Yes | ETag of the last confirmed remote file. |
| `hasUncertainWrite` | `boolean` | No | Whether a saved remote attempt needs reconciliation. |

## FileReadResult



```ts
interface FileReadResult {
  data: Uint8Array<ArrayBufferLike>;
  version: FileVersion;
  status: FileHistoryStatus;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `data` | `Uint8Array<ArrayBufferLike>` | No | Object bytes. Input ownership and size rules depend on the method. |
| `version` | `FileVersion` | No |  |
| `status` | `FileHistoryStatus` | No |  |

## FileSaveOptions



```ts
interface FileSaveOptions  extends Pick<EmbeddedPutOptions, "contentType" | "metadata"> {
  label?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `label` | `string \| undefined` | Yes | Optional revision label; it does not prevent retention from removing the revision. |
| `contentType` | `string \| undefined` | Yes | MIME content type attached to the object or file. |
| `metadata` | `Record<string, string> \| undefined` | Yes | User metadata included in storage and sync comparisons. |

## FileSaveReceipt

A committed save without materializing the saved file again.

```ts
type FileSaveReceipt = Omit<FileReadResult, "data">;
```

## FileVersion



```ts
interface FileVersion {
  id: string;
  createdAt: string;
  label?: string | undefined;
  contentType?: string | undefined;
  metadata?: Record<string, string> | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `id` | `string` | No | Stable identity of this workspace, file stage, revision or immutable record. |
| `createdAt` | `string` | No | Revision creation timestamp. |
| `label` | `string \| undefined` | Yes | Optional revision label; it does not prevent retention from removing the revision. |
| `contentType` | `string \| undefined` | Yes | MIME content type attached to the object or file. |
| `metadata` | `Record<string, string> \| undefined` | Yes | User metadata included in storage and sync comparisons. |

## IndexedDbPersistenceAdapter



```ts
class IndexedDbPersistenceAdapter  implements PersistenceAdapter {
  constructor(options?: IndexedDbPersistenceAdapterOptions);
  open(namespace: string): Promise<void>;
  load(namespace: string): Promise<PersistenceSnapshot | null>;
  commit(namespace: string, expectedGeneration: number, changes: PersistenceChange[]): Promise<number>;
  clear(namespace: string, expectedGeneration: number): Promise<number>;
  close(): Promise<void>;
}
```

### IndexedDbPersistenceAdapter.constructor

function Object() { [native code] }

```ts
constructor(options?: IndexedDbPersistenceAdapterOptions)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `IndexedDbPersistenceAdapterOptions` | No | {} |  |

Returns: `IndexedDbPersistenceAdapter`.

### IndexedDbPersistenceAdapter.open

Open a handle. Retain it until operations finish, then close it.

```ts
open(namespace: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `namespace` | `string` | Yes | — | Nonempty local persistence namespace; ownership is exclusive. |

Returns: `Promise<void>`.

### IndexedDbPersistenceAdapter.load



```ts
load(namespace: string): Promise<PersistenceSnapshot | null>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `namespace` | `string` | Yes | — | Nonempty local persistence namespace; ownership is exclusive. |

Returns: `Promise<PersistenceSnapshot | null>`.

### IndexedDbPersistenceAdapter.commit

Publish all requested file changes as one project revision.

```ts
commit(namespace: string, expectedGeneration: number, changes: PersistenceChange[]): Promise<number>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `namespace` | `string` | Yes | — | Nonempty local persistence namespace; ownership is exclusive. |
| `expectedGeneration` | `number` | Yes | — | Generation that must still match when publishing. |
| `changes` | `PersistenceChange[]` | Yes | — |  |

Returns: `Promise<number>`.

### IndexedDbPersistenceAdapter.clear



```ts
clear(namespace: string, expectedGeneration: number): Promise<number>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `namespace` | `string` | Yes | — | Nonempty local persistence namespace; ownership is exclusive. |
| `expectedGeneration` | `number` | Yes | — | Generation that must still match when publishing. |

Returns: `Promise<number>`.

### IndexedDbPersistenceAdapter.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## IndexedDbPersistenceAdapterOptions



```ts
interface IndexedDbPersistenceAdapterOptions {
  indexedDB?: IDBFactory | undefined;
  databaseName?: string | undefined;
  manifestStoreName?: string | undefined;
  bucketStoreName?: string | undefined;
  objectStoreName?: string | undefined;
  lockManager?: LockManagerLike | undefined;
  lockName?: ((namespace: string) => string) | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `indexedDB` | `IDBFactory \| undefined` | Yes | Optional IDBFactory override for the storage adapter. |
| `databaseName` | `string \| undefined` | Yes | Defaults to chs-stow. |
| `manifestStoreName` | `string \| undefined` | Yes |  |
| `bucketStoreName` | `string \| undefined` | Yes |  |
| `objectStoreName` | `string \| undefined` | Yes |  |
| `lockManager` | `LockManagerLike \| undefined` | Yes | Web Locks implementation used for namespace ownership. |
| `lockName` | `((namespace: string) => string) \| undefined` | Yes |  |

## LockManagerLike



```ts
interface LockManagerLike {
  request(name: string, options: { mode: "exclusive"; ifAvailable?: boolean | undefined; }, callback: (lock: unknown) => Promise<void>): Promise<void>;
}
```

### LockManagerLike.request



```ts
request(name: string, options: { mode: "exclusive"; ifAvailable?: boolean | undefined; }, callback: (lock: unknown) => Promise<void>): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `name` | `string` | Yes | — |  |
| `options` | `{ mode: "exclusive"; ifAvailable?: boolean \| undefined; }` | Yes | — |  |
| `callback` | `(lock: unknown) => Promise<void>` | Yes | — |  |

Returns: `Promise<void>`.

## ObjectSyncRemote

Host-owned credentials, networking and conditional object writes.

```ts
interface ObjectSyncRemote {
  headObject(bucket: string, key: string): Promise<{ etag: string; size: number; metadata?: Record<string, string> | undefined; sha256?: string | undefined; } | null>;
  getObject(bucket: string, key: string): Promise<SyncRemoteObject | null>;
  putObject(bucket: string, key: string, data: Uint8Array<ArrayBufferLike>, options: SyncRemoteWrite): Promise<{ etag: string; }>;
}
```

### ObjectSyncRemote.headObject

Return object metadata without reading the body into the result.

```ts
headObject(bucket: string, key: string): Promise<{ etag: string; size: number; metadata?: Record<string, string> | undefined; sha256?: string | undefined; } | null>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<{ etag: string; size: number; metadata?: Record<string, string> | undefined; sha256?: string | undefined; } | null>`.

### ObjectSyncRemote.getObject

Return complete object bytes and metadata.

```ts
getObject(bucket: string, key: string): Promise<SyncRemoteObject | null>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<SyncRemoteObject | null>`.

### ObjectSyncRemote.putObject

Store complete bytes and metadata, subject to write conditions and quotas.

```ts
putObject(bucket: string, key: string, data: Uint8Array<ArrayBufferLike>, options: SyncRemoteWrite): Promise<{ etag: string; }>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `data` | `Uint8Array<ArrayBufferLike>` | Yes | — | Object bytes. Input ownership and size rules depend on the method. |
| `options` | `SyncRemoteWrite` | Yes | — |  |

Returns: `Promise<{ etag: string; }>`.

## openBrowserEmbeddedStow



```ts
openBrowserEmbeddedStow(host: EmbeddedHost, options: PersistentEmbeddedStowOptions): Promise<BrowserEmbeddedStow>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `host` | `EmbeddedHost` | Yes | — | Bind address; native default is 127.0.0.1. |
| `options` | `PersistentEmbeddedStowOptions` | Yes | — |  |

Returns: `Promise<BrowserEmbeddedStow>`.

## PersistenceAdapter



```ts
interface PersistenceAdapter {
  open(namespace: string): Promise<void>;
  load(namespace: string): Promise<PersistenceSnapshot | null | undefined>;
  commit(namespace: string, expectedGeneration: number, changes: PersistenceChange[]): Promise<number>;
  clear(namespace: string, expectedGeneration: number): Promise<number>;
  close(): Promise<void>;
}
```

### PersistenceAdapter.open

Open a handle. Retain it until operations finish, then close it.

```ts
open(namespace: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `namespace` | `string` | Yes | — | Nonempty local persistence namespace; ownership is exclusive. |

Returns: `Promise<void>`.

### PersistenceAdapter.load



```ts
load(namespace: string): Promise<PersistenceSnapshot | null | undefined>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `namespace` | `string` | Yes | — | Nonempty local persistence namespace; ownership is exclusive. |

Returns: `Promise<PersistenceSnapshot | null | undefined>`.

### PersistenceAdapter.commit

Publish all requested file changes as one project revision.

```ts
commit(namespace: string, expectedGeneration: number, changes: PersistenceChange[]): Promise<number>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `namespace` | `string` | Yes | — | Nonempty local persistence namespace; ownership is exclusive. |
| `expectedGeneration` | `number` | Yes | — | Generation that must still match when publishing. |
| `changes` | `PersistenceChange[]` | Yes | — |  |

Returns: `Promise<number>`.

### PersistenceAdapter.clear



```ts
clear(namespace: string, expectedGeneration: number): Promise<number>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `namespace` | `string` | Yes | — | Nonempty local persistence namespace; ownership is exclusive. |
| `expectedGeneration` | `number` | Yes | — | Generation that must still match when publishing. |

Returns: `Promise<number>`.

### PersistenceAdapter.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## PersistenceChange



```ts
type PersistenceChange = PutBucketChange | DeleteBucketChange | PutObjectChange | DeleteObjectChange;
```

## PersistenceSnapshot



```ts
interface PersistenceSnapshot {
  formatVersion: 1;
  generation: number;
  buckets: EmbeddedBucket[];
  objects: EmbeddedObject[];
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `formatVersion` | `1` | No |  |
| `generation` | `number` | No | Committed storage generation used to detect stale publication. |
| `buckets` | `EmbeddedBucket[]` | No | Buckets to create before startup resolves, or buckets returned by inspection. |
| `objects` | `EmbeddedObject[]` | No | Objects returned by the listing or persistence snapshot. |

## PersistentEmbeddedStowOptions



```ts
interface PersistentEmbeddedStowOptions  extends EmbeddedStowOptions {
  namespace: string;
  persistence: PersistenceAdapter;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `namespace` | `string` | No | Nonempty local persistence namespace; ownership is exclusive. |
| `persistence` | `PersistenceAdapter` | No | Adapter used for atomic durable object generations. |
| `maxBytes` | `number \| undefined` | Yes | Maximum managed object bytes; zero means unlimited. |
| `maxObjects` | `number \| undefined` | Yes | Maximum object count; zero means unlimited. |

## PutBucketChange



```ts
type PutBucketChange = {
    type: "putBucket";
    bucket: EmbeddedBucket;
};
```

## PutObjectChange



```ts
type PutObjectChange = {
    type: "putObject";
    object: EmbeddedObject;
};
```

## RemoteObjectBasis

The remote must enforce this condition atomically with its write.

```ts
type RemoteObjectBasis = {
    etag: string;
} | {
    absent: true;
};
```

## SyncRemoteObject



```ts
interface SyncRemoteObject {
  data: Uint8Array<ArrayBufferLike>;
  etag: string;
  contentType?: string | undefined;
  metadata?: Record<string, string> | undefined;
  saveId?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `data` | `Uint8Array<ArrayBufferLike>` | No | Object bytes. Input ownership and size rules depend on the method. |
| `etag` | `string` | No | Content validator returned by the store or provider. |
| `contentType` | `string \| undefined` | Yes | MIME content type attached to the object or file. |
| `metadata` | `Record<string, string> \| undefined` | Yes | User metadata included in storage and sync comparisons. |
| `saveId` | `string \| undefined` | Yes | Unique remote-write receipt, stored as reserved stow-save-id metadata. |

## SyncRemoteWrite



```ts
interface SyncRemoteWrite {
  basis: RemoteObjectBasis;
  saveId: string;
  contentType?: string | undefined;
  metadata?: Record<string, string> | undefined;
  sha256?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `basis` | `RemoteObjectBasis` | No | Confirmed remote ETag or expected absence, enforced atomically on PUT. |
| `saveId` | `string` | No | Unique remote-write receipt, stored as reserved stow-save-id metadata. |
| `contentType` | `string \| undefined` | Yes | MIME content type attached to the object or file. |
| `metadata` | `Record<string, string> \| undefined` | Yes | User metadata included in storage and sync comparisons. |
| `sha256` | `string \| undefined` | Yes | Full-object SHA-256 in hexadecimal, verified by the provider. |
