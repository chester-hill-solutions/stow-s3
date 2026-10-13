# Browser project and content API

Retain atomic multi-file revisions with shared immutable chunks. Import from `@chester-hill-solutions/stow-s3/project`.

## Defaults and limits

| Setting | Value |
| --- | --- |
| maxVersions | 50; minimum 2 |
| maxReadBytes | 16 MiB |
| maxManifestBytes | 1 MiB |
| ContentStore chunkBytes / cacheBytes | 1 MiB / 64 MiB |
| IndexedDbContentAdapter quotaBytes | 1 GiB; zero allows no accounted content |
| Database name | chs-stow-content |
| Range bounds | start inclusive, end exclusive; byte offsets |

## Behavior

- Open the adapter's namespace before using ContentStore.
- BrowserProject.commit applies all changes as one revision; put, delete and fixed-length replaceRange are supported.
- read returns full bytes; readRange returns a bounded allocation; stream pins the selected revision.
- checkout selects a retained revision.
- sync uploads chunks before a conditional manifest write.
- Closing a project does not close its content store or adapter.
- Release abandoned stages explicitly before collection.
- migrateFileHistory preserves the source and requires unresolved sync attempts to be reconciled first.

## Errors

- ProjectError exposes its code, including not_found, conflict, corrupt, invalid_options, history_boundary, read_too_large, remote_unavailable, uncertain_write and closed.
- Content adapters can raise BrowserPersistenceError or RangeError.
- This IndexedDB profile requires Web Locks.
- Unknown formats, invalid ranges and quota overflow fail without partial publication.

## Example

```ts
import { BrowserProject, ContentStore, IndexedDbContentAdapter } from "@chester-hill-solutions/stow-s3/project";

const adapter = new IndexedDbContentAdapter();
await adapter.open("my-app");
const content = new ContentStore(adapter);
const project = await BrowserProject.open(content, { projectId: "notes" });
await project.commit([{ type: "put", key: "note.txt", data: new TextEncoder().encode("hello") }]);
const bytes = await project.readRange("note.txt", 0, 5);
await project.close();
await adapter.close();
```

## Symbols

[BrowserProject](#browserproject) · [BrowserProjectOptions](#browserprojectoptions) · [ContentAdapter](#contentadapter) · [ContentChunk](#contentchunk) · [ContentHead](#contenthead) · [ContentObjectHost](#contentobjecthost) · [ContentPage](#contentpage) · [ContentProtection](#contentprotection) · [ContentPublication](#contentpublication) · [ContentRecord](#contentrecord) · [ContentReference](#contentreference) · [ContentStageOptions](#contentstageoptions) · [ContentStore](#contentstore) · [ContentStoreOptions](#contentstoreoptions) · [ContentUsage](#contentusage) · [IndexedDbContentAdapter](#indexeddbcontentadapter) · [IndexedDbContentOptions](#indexeddbcontentoptions) · [migrateFileHistory](#migratefilehistory) · [ProjectChange](#projectchange) · [ProjectCommitOptions](#projectcommitoptions) · [ProjectError](#projecterror) · [ProjectFile](#projectfile) · [ProjectInput](#projectinput) · [ProjectRevision](#projectrevision) · [ProjectStatus](#projectstatus) · [StagedContent](#stagedcontent)

## BrowserProject

Durable project revisions over lazy immutable content.

```ts
class BrowserProject {
  readonly retentionLimit: number;
  static open(store: ContentStore, options: BrowserProjectOptions): Promise<BrowserProject>;
  status(): Promise<ProjectStatus>;
  listFiles(): Promise<ProjectFile[]>;
  commit(changes: ProjectChange[], options?: ProjectCommitOptions): Promise<ProjectStatus>;
  read(key: string): Promise<Uint8Array<ArrayBufferLike>>;
  readRange(key: string, start: number, end: number): Promise<Uint8Array<ArrayBufferLike>>;
  stream(key: string, options?: { signal?: AbortSignal | undefined; }): AsyncIterable<Uint8Array<ArrayBufferLike>>;
  undo(): Promise<ProjectStatus>;
  redo(): Promise<ProjectStatus>;
  checkout(revisionId: string): Promise<ProjectStatus>;
  migrationProgress(token: string): Promise<string | undefined>;
  sync(): Promise<ProjectStatus>;
  close(): Promise<void>;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `retentionLimit` | `number` | No | Configured maximum retained project revision count. |

### BrowserProject.open

Open a handle. Retain it until operations finish, then close it.

```ts
open(store: ContentStore, options: BrowserProjectOptions): Promise<BrowserProject>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `store` | `ContentStore` | Yes | — | Open underlying object or content store supplied by the application. |
| `options` | `BrowserProjectOptions` | Yes | — |  |

Returns: `Promise<BrowserProject>`.

### BrowserProject.status

Return the selected revision, retained history and pending sync state.

```ts
status(): Promise<ProjectStatus>
```

Returns: `Promise<ProjectStatus>`.

### BrowserProject.listFiles

Return the selected project's file manifest without reading payloads.

```ts
listFiles(): Promise<ProjectFile[]>
```

Returns: `Promise<ProjectFile[]>`.

### BrowserProject.commit

Publish all requested file changes as one project revision.

```ts
commit(changes: ProjectChange[], options?: ProjectCommitOptions): Promise<ProjectStatus>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `changes` | `ProjectChange[]` | Yes | — |  |
| `options` | `ProjectCommitOptions` | No | {} |  |

Returns: `Promise<ProjectStatus>`.

### BrowserProject.read

Read the selected file's bytes.

```ts
read(key: string): Promise<Uint8Array<ArrayBufferLike>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<Uint8Array<ArrayBufferLike>>`.

### BrowserProject.readRange

Read byte offsets from start inclusive to end exclusive.

```ts
readRange(key: string, start: number, end: number): Promise<Uint8Array<ArrayBufferLike>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `start` | `number` | Yes | — |  |
| `end` | `number` | Yes | — |  |

Returns: `Promise<Uint8Array<ArrayBufferLike>>`.

### BrowserProject.stream

Yield bounded content chunks; finish or close the reader to release protection.

```ts
stream(key: string, options?: { signal?: AbortSignal | undefined; }): AsyncIterable<Uint8Array<ArrayBufferLike>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `options` | `{ signal?: AbortSignal \| undefined; }` | No | {} |  |

Returns: `AsyncIterable<Uint8Array<ArrayBufferLike>>`.

### BrowserProject.undo

Select the preceding retained revision; fail at the history boundary.

```ts
undo(): Promise<ProjectStatus>
```

Returns: `Promise<ProjectStatus>`.

### BrowserProject.redo

Select the next retained revision; fail at the history boundary.

```ts
redo(): Promise<ProjectStatus>
```

Returns: `Promise<ProjectStatus>`.

### BrowserProject.checkout

Select the retained revision identified by revisionId.

```ts
checkout(revisionId: string): Promise<ProjectStatus>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `revisionId` | `string` | Yes | — |  |

Returns: `Promise<ProjectStatus>`.

### BrowserProject.migrationProgress

Return the retained destination revision ID for a migration token, if present.

```ts
migrationProgress(token: string): Promise<string | undefined>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `token` | `string` | Yes | — |  |

Returns: `Promise<string | undefined>`.

### BrowserProject.sync

Publish the selected revision with the saved remote condition; preserve local work on conflict.

```ts
sync(): Promise<ProjectStatus>
```

Returns: `Promise<ProjectStatus>`.

### BrowserProject.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## BrowserProjectOptions



```ts
interface BrowserProjectOptions {
  projectId: string;
  maxVersions?: number | undefined;
  maxReadBytes?: number | undefined;
  maxManifestBytes?: number | undefined;
  remote?: ObjectSyncRemote | undefined;
  remoteBucket?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `projectId` | `string` | No | Stable project identifier used in local state and remote manifest paths. |
| `maxVersions` | `number \| undefined` | Yes | Retained revision count. Defaults to 50; must be at least 2. |
| `maxReadBytes` | `number \| undefined` | Yes | Maximum allocated read/range result; project default is 16 MiB. |
| `maxManifestBytes` | `number \| undefined` | Yes | Maximum encoded project metadata bytes; default is 1 MiB. |
| `remote` | `ObjectSyncRemote \| undefined` | Yes | Application-supplied authenticated transport for conditional remote sync. |
| `remoteBucket` | `string \| undefined` | Yes | Remote bucket used by file or project sync. |

## ContentAdapter



```ts
interface ContentAdapter {
  open(namespace: string): Promise<void>;
  getHead(name: string): Promise<ContentHead>;
  getRecord(id: string): Promise<ContentRecord | null>;
  listRecords(options?: { after?: string | undefined; limit?: number | undefined; } | undefined): Promise<ContentPage>;
  getChunk(hash: string): Promise<Uint8Array<ArrayBufferLike> | null>;
  stageChunk(stageId: string, hash: string, data: Uint8Array<ArrayBufferLike>): Promise<void>;
  releaseStage(stageId: string): Promise<void>;
  listStages(): Promise<string[]>;
  publish(publication: ContentPublication): Promise<ContentHead>;
  protect(name: string, recordIds: string[], options?: { lifetime?: "handle" | "durable" | undefined; } | undefined): Promise<void>;
  listProtections(): Promise<ContentProtection[]>;
  collect(options?: { retainRecordIds?: string[] | undefined; } | undefined): Promise<ContentUsage>;
  usage(): Promise<ContentUsage>;
  close(): Promise<void>;
}
```

### ContentAdapter.open

Open a handle. Retain it until operations finish, then close it.

```ts
open(namespace: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `namespace` | `string` | Yes | — | Nonempty local persistence namespace; ownership is exclusive. |

Returns: `Promise<void>`.

### ContentAdapter.getHead



```ts
getHead(name: string): Promise<ContentHead>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `name` | `string` | Yes | — |  |

Returns: `Promise<ContentHead>`.

### ContentAdapter.getRecord



```ts
getRecord(id: string): Promise<ContentRecord | null>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `id` | `string` | Yes | — | Stable identity of this workspace, file stage, revision or immutable record. |

Returns: `Promise<ContentRecord | null>`.

### ContentAdapter.listRecords



```ts
listRecords(options?: { after?: string | undefined; limit?: number | undefined; } | undefined): Promise<ContentPage>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `{ after?: string \| undefined; limit?: number \| undefined; } \| undefined` | No | — |  |

Returns: `Promise<ContentPage>`.

### ContentAdapter.getChunk



```ts
getChunk(hash: string): Promise<Uint8Array<ArrayBufferLike> | null>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `hash` | `string` | Yes | — | Lowercase hexadecimal SHA-256 identity of a content chunk. |

Returns: `Promise<Uint8Array<ArrayBufferLike> | null>`.

### ContentAdapter.stageChunk



```ts
stageChunk(stageId: string, hash: string, data: Uint8Array<ArrayBufferLike>): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `stageId` | `string` | Yes | — | Identity of unpublished staged content, used for publication or explicit cleanup. |
| `hash` | `string` | Yes | — | Lowercase hexadecimal SHA-256 identity of a content chunk. |
| `data` | `Uint8Array<ArrayBufferLike>` | Yes | — | Object bytes. Input ownership and size rules depend on the method. |

Returns: `Promise<void>`.

### ContentAdapter.releaseStage



```ts
releaseStage(stageId: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `stageId` | `string` | Yes | — | Identity of unpublished staged content, used for publication or explicit cleanup. |

Returns: `Promise<void>`.

### ContentAdapter.listStages



```ts
listStages(): Promise<string[]>
```

Returns: `Promise<string[]>`.

### ContentAdapter.publish



```ts
publish(publication: ContentPublication): Promise<ContentHead>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `publication` | `ContentPublication` | Yes | — |  |

Returns: `Promise<ContentHead>`.

### ContentAdapter.protect



```ts
protect(name: string, recordIds: string[], options?: { lifetime?: "handle" | "durable" | undefined; } | undefined): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `name` | `string` | Yes | — |  |
| `recordIds` | `string[]` | Yes | — | Records protected from garbage collection. |
| `options` | `{ lifetime?: "handle" \| "durable" \| undefined; } \| undefined` | No | — |  |

Returns: `Promise<void>`.

### ContentAdapter.listProtections



```ts
listProtections(): Promise<ContentProtection[]>
```

Returns: `Promise<ContentProtection[]>`.

### ContentAdapter.collect



```ts
collect(options?: { retainRecordIds?: string[] | undefined; } | undefined): Promise<ContentUsage>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `{ retainRecordIds?: string[] \| undefined; } \| undefined` | No | — |  |

Returns: `Promise<ContentUsage>`.

### ContentAdapter.usage

Return current storage accounting.

```ts
usage(): Promise<ContentUsage>
```

Returns: `Promise<ContentUsage>`.

### ContentAdapter.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## ContentChunk



```ts
interface ContentChunk {
  hash: string;
  size: number;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `hash` | `string` | No | Lowercase hexadecimal SHA-256 identity of a content chunk. |
| `size` | `number` | No | Total object or content size in bytes; stream uploads require an exact declaration. |

## ContentHead



```ts
interface ContentHead {
  name: string;
  generation: number;
  recordId: string | null;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `name` | `string` | No |  |
| `generation` | `number` | No | Committed storage generation used to detect stale publication. |
| `recordId` | `string \| null` | No | Immutable record selected by a head; null means no selected record. |

## ContentObjectHost

Host persistence beneath the shared Go object runtime; bytes are never replayed at open.

```ts
class ContentObjectHost  implements HostObjectStoreBridge {
  checkConditions?: ((metadata: string) => string) | undefined;
  constructor(store: ContentStore, options?: { name?: string | undefined; maxStreams?: number | undefined; });
  hasActiveStage(stageId: string): boolean;
  call(metadata: string, data: Uint8Array<ArrayBufferLike>): Promise<EmbeddedBinaryResponse>;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `checkConditions` | `((metadata: string) => string) \| undefined` | Yes | Installed by the Go host-backed runtime to evaluate its shared atomic write predicates. |
| `store` | `ContentStore` | No | Open underlying object or content store supplied by the application. |

### ContentObjectHost.constructor

function Object() { [native code] }

```ts
constructor(store: ContentStore, options?: { name?: string | undefined; maxStreams?: number | undefined; })
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `store` | `ContentStore` | Yes | — | Open underlying object or content store supplied by the application. |
| `options` | `{ name?: string \| undefined; maxStreams?: number \| undefined; }` | No | {} |  |

Returns: `ContentObjectHost`.

### ContentObjectHost.hasActiveStage



```ts
hasActiveStage(stageId: string): boolean
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `stageId` | `string` | Yes | — | Identity of unpublished staged content, used for publication or explicit cleanup. |

Returns: `boolean`.

### ContentObjectHost.call



```ts
call(metadata: string, data: Uint8Array<ArrayBufferLike>): Promise<EmbeddedBinaryResponse>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `metadata` | `string` | Yes | — | Serialized binary-bridge request or response metadata. |
| `data` | `Uint8Array<ArrayBufferLike>` | Yes | — | Object bytes. Input ownership and size rules depend on the method. |

Returns: `Promise<EmbeddedBinaryResponse>`.

## ContentPage



```ts
interface ContentPage {
  records: ContentRecord[];
  next?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `records` | `ContentRecord[]` | No |  |
| `next` | `string \| undefined` | Yes |  |

## ContentProtection



```ts
interface ContentProtection {
  name: string;
  recordIds: string[];
  lifetime: "handle" | "durable";
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `name` | `string` | No |  |
| `recordIds` | `string[]` | No | Records protected from garbage collection. |
| `lifetime` | `"handle" \| "durable"` | No | Protection lifetime: handle-bound or durable across reopen. |

## ContentPublication



```ts
interface ContentPublication {
  head: string;
  expectedGeneration: number;
  record: ContentRecord;
  additionalRecords?: ContentRecord[] | undefined;
  stageId: string;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `head` | `string` | No |  |
| `expectedGeneration` | `number` | No | Generation that must still match when publishing. |
| `record` | `ContentRecord` | No |  |
| `additionalRecords` | `ContentRecord[] \| undefined` | Yes |  |
| `stageId` | `string` | No | Identity of unpublished staged content, used for publication or explicit cleanup. |

## ContentRecord



```ts
interface ContentRecord {
  id: string;
  metadata: Uint8Array<ArrayBufferLike>;
  contents: ContentReference[];
  references: string[];
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `id` | `string` | No | Stable identity of this workspace, file stage, revision or immutable record. |
| `metadata` | `Uint8Array<ArrayBufferLike>` | No | Encoded immutable record metadata bytes, included in its size limit. |
| `contents` | `ContentReference[]` | No | Immutable content references retained by this record. |
| `references` | `string[]` | No | Referenced immutable record IDs used for reachability and retention. |

## ContentReference



```ts
interface ContentReference {
  size: number;
  chunks: ContentChunk[];
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `size` | `number` | No | Total object or content size in bytes; stream uploads require an exact declaration. |
| `chunks` | `ContentChunk[]` | No | Ordered chunk descriptors, or an async byte iterator for stream readers. |

## ContentStageOptions



```ts
interface ContentStageOptions {
  stageId?: string | undefined;
  signal?: AbortSignal | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `stageId` | `string \| undefined` | Yes | Identity of unpublished staged content, used for publication or explicit cleanup. |
| `signal` | `AbortSignal \| undefined` | Yes | AbortSignal for cancellation. A started write can still commit. |

## ContentStore



```ts
class ContentStore {
  constructor(adapter: ContentAdapter, options?: ContentStoreOptions);
  stage(input: Uint8Array<ArrayBufferLike> | AsyncIterable<Uint8Array<ArrayBufferLike>>, options?: ContentStageOptions): Promise<StagedContent>;
  stream(content: ContentReference, options?: { start?: number | undefined; end?: number | undefined; signal?: AbortSignal | undefined; }): AsyncIterable<Uint8Array<ArrayBufferLike>>;
  readRange(content: ContentReference, start?: number, end?: number): Promise<Uint8Array<ArrayBufferLike>>;
  usage(): Promise<ContentUsage>;
  clearCache(): void;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `adapter` | `ContentAdapter` | No | Content persistence adapter; its namespace must be open before operations. |

### ContentStore.constructor

function Object() { [native code] }

```ts
constructor(adapter: ContentAdapter, options?: ContentStoreOptions)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `adapter` | `ContentAdapter` | Yes | — | Content persistence adapter; its namespace must be open before operations. |
| `options` | `ContentStoreOptions` | No | {} |  |

Returns: `ContentStore`.

### ContentStore.stage

Write immutable chunks without publishing a head; retain the stage ID for publication or cleanup.

```ts
stage(input: Uint8Array<ArrayBufferLike> | AsyncIterable<Uint8Array<ArrayBufferLike>>, options?: ContentStageOptions): Promise<StagedContent>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `input` | `Uint8Array<ArrayBufferLike> \| AsyncIterable<Uint8Array<ArrayBufferLike>>` | Yes | — |  |
| `options` | `ContentStageOptions` | No | {} |  |

Returns: `Promise<StagedContent>`.

### ContentStore.stream

Yield bounded content chunks; finish or close the reader to release protection.

```ts
stream(content: ContentReference, options?: { start?: number | undefined; end?: number | undefined; signal?: AbortSignal | undefined; }): AsyncIterable<Uint8Array<ArrayBufferLike>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `content` | `ContentReference` | Yes | — | Immutable content reference for this file or staged result. |
| `options` | `{ start?: number \| undefined; end?: number \| undefined; signal?: AbortSignal \| undefined; }` | No | {} |  |

Returns: `AsyncIterable<Uint8Array<ArrayBufferLike>>`.

### ContentStore.readRange

Read byte offsets from start inclusive to end exclusive.

```ts
readRange(content: ContentReference, start?: number, end?: number): Promise<Uint8Array<ArrayBufferLike>>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `content` | `ContentReference` | Yes | — | Immutable content reference for this file or staged result. |
| `start` | `number` | No | 0 |  |
| `end` | `number` | No | content.size |  |

Returns: `Promise<Uint8Array<ArrayBufferLike>>`.

### ContentStore.usage

Return current storage accounting.

```ts
usage(): Promise<ContentUsage>
```

Returns: `Promise<ContentUsage>`.

### ContentStore.clearCache

Release cached payload bytes without changing durable storage.

```ts
clearCache(): void
```

Returns: `void`.

## ContentStoreOptions



```ts
interface ContentStoreOptions {
  chunkBytes?: number | undefined;
  cacheBytes?: number | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `chunkBytes` | `number \| undefined` | Yes | Content chunk size in bytes; default is 1 MiB. |
| `cacheBytes` | `number \| undefined` | Yes | Maximum cached payload bytes; zero disables the payload cache. |

## ContentUsage



```ts
interface ContentUsage {
  physicalBytes: number;
  chunks: number;
  records: number;
  bookkeepingBytes?: number | undefined;
  cacheBytes?: number | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `physicalBytes` | `number` | No | Unique stored content/record bytes accounted by the adapter. |
| `chunks` | `number` | No | Number of unique persisted payload chunks. |
| `records` | `number` | No | Number of persisted immutable records. |
| `bookkeepingBytes` | `number \| undefined` | Yes | Accounted head, staging and protection records. |
| `cacheBytes` | `number \| undefined` | Yes | Payload bytes currently held in the ContentStore cache. |

## IndexedDbContentAdapter



```ts
class IndexedDbContentAdapter  implements ContentAdapter {
  constructor(options?: IndexedDbContentOptions);
  open(namespace: string): Promise<void>;
  getHead(name: string): Promise<ContentHead>;
  getRecord(id: string): Promise<ContentRecord | null>;
  listRecords(options?: { after?: string | undefined; limit?: number | undefined; }): Promise<ContentPage>;
  getChunk(hash: string): Promise<Uint8Array<ArrayBufferLike> | null>;
  stageChunk(stageId: string, hash: string, bytes: Uint8Array<ArrayBufferLike>): Promise<void>;
  listStages(): Promise<string[]>;
  releaseStage(stageId: string): Promise<void>;
  publish(publication: ContentPublication): Promise<ContentHead>;
  listProtections(): Promise<ContentProtection[]>;
  protect(name: string, recordIds: string[], options?: { lifetime?: "handle" | "durable" | undefined; }): Promise<void>;
  collect(options?: { retainRecordIds?: string[] | undefined; }): Promise<ContentUsage>;
  usage(): Promise<ContentUsage>;
  close(): Promise<void>;
}
```

### IndexedDbContentAdapter.constructor

function Object() { [native code] }

```ts
constructor(options?: IndexedDbContentOptions)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `IndexedDbContentOptions` | No | {} |  |

Returns: `IndexedDbContentAdapter`.

### IndexedDbContentAdapter.open

Open a handle. Retain it until operations finish, then close it.

```ts
open(namespace: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `namespace` | `string` | Yes | — | Nonempty local persistence namespace; ownership is exclusive. |

Returns: `Promise<void>`.

### IndexedDbContentAdapter.getHead



```ts
getHead(name: string): Promise<ContentHead>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `name` | `string` | Yes | — |  |

Returns: `Promise<ContentHead>`.

### IndexedDbContentAdapter.getRecord



```ts
getRecord(id: string): Promise<ContentRecord | null>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `id` | `string` | Yes | — | Stable identity of this workspace, file stage, revision or immutable record. |

Returns: `Promise<ContentRecord | null>`.

### IndexedDbContentAdapter.listRecords



```ts
listRecords(options?: { after?: string | undefined; limit?: number | undefined; }): Promise<ContentPage>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `{ after?: string \| undefined; limit?: number \| undefined; }` | No | {} |  |

Returns: `Promise<ContentPage>`.

### IndexedDbContentAdapter.getChunk



```ts
getChunk(hash: string): Promise<Uint8Array<ArrayBufferLike> | null>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `hash` | `string` | Yes | — | Lowercase hexadecimal SHA-256 identity of a content chunk. |

Returns: `Promise<Uint8Array<ArrayBufferLike> | null>`.

### IndexedDbContentAdapter.stageChunk



```ts
stageChunk(stageId: string, hash: string, bytes: Uint8Array<ArrayBufferLike>): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `stageId` | `string` | Yes | — | Identity of unpublished staged content, used for publication or explicit cleanup. |
| `hash` | `string` | Yes | — | Lowercase hexadecimal SHA-256 identity of a content chunk. |
| `bytes` | `Uint8Array<ArrayBufferLike>` | Yes | — | Current managed object payload bytes. |

Returns: `Promise<void>`.

### IndexedDbContentAdapter.listStages



```ts
listStages(): Promise<string[]>
```

Returns: `Promise<string[]>`.

### IndexedDbContentAdapter.releaseStage



```ts
releaseStage(stageId: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `stageId` | `string` | Yes | — | Identity of unpublished staged content, used for publication or explicit cleanup. |

Returns: `Promise<void>`.

### IndexedDbContentAdapter.publish



```ts
publish(publication: ContentPublication): Promise<ContentHead>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `publication` | `ContentPublication` | Yes | — |  |

Returns: `Promise<ContentHead>`.

### IndexedDbContentAdapter.listProtections



```ts
listProtections(): Promise<ContentProtection[]>
```

Returns: `Promise<ContentProtection[]>`.

### IndexedDbContentAdapter.protect



```ts
protect(name: string, recordIds: string[], options?: { lifetime?: "handle" | "durable" | undefined; }): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `name` | `string` | Yes | — |  |
| `recordIds` | `string[]` | Yes | — | Records protected from garbage collection. |
| `options` | `{ lifetime?: "handle" \| "durable" \| undefined; }` | No | {} |  |

Returns: `Promise<void>`.

### IndexedDbContentAdapter.collect



```ts
collect(options?: { retainRecordIds?: string[] | undefined; }): Promise<ContentUsage>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `{ retainRecordIds?: string[] \| undefined; }` | No | {} |  |

Returns: `Promise<ContentUsage>`.

### IndexedDbContentAdapter.usage

Return current storage accounting.

```ts
usage(): Promise<ContentUsage>
```

Returns: `Promise<ContentUsage>`.

### IndexedDbContentAdapter.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## IndexedDbContentOptions



```ts
interface IndexedDbContentOptions {
  indexedDB?: IDBFactory | undefined;
  databaseName?: string | undefined;
  lockManager?: LockManagerLike | undefined;
  quotaBytes?: number | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `indexedDB` | `IDBFactory \| undefined` | Yes | Optional IDBFactory override for the storage adapter. |
| `databaseName` | `string \| undefined` | Yes | Defaults to chs-stow-content. |
| `lockManager` | `LockManagerLike \| undefined` | Yes | Web Locks implementation used for namespace ownership. |
| `quotaBytes` | `number \| undefined` | Yes | Namespace content and bookkeeping byte budget; zero allows no accounted content. |

## migrateFileHistory

Copy retained legacy stages into a dedicated project without changing the source or discarding uncertain sync attempts.

```ts
migrateFileHistory(source: Pick<BrowserFileHistory, "exportRetained">, key: string, target: BrowserProject): Promise<ProjectStatus>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `source` | `Pick<BrowserFileHistory, "exportRetained">` | Yes | — | Complete bytes or an asynchronous sequence of bounded byte chunks. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `target` | `BrowserProject` | Yes | — |  |

Returns: `Promise<ProjectStatus>`.

## ProjectChange



```ts
type ProjectChange = {
    type: "put";
    key: string;
    data: ProjectInput;
    contentType?: string;
    metadata?: Record<string, string>;
} | {
    type: "delete";
    key: string;
} | {
    type: "replaceRange";
    key: string;
    offset: number;
    data: Uint8Array;
};
```

## ProjectCommitOptions



```ts
interface ProjectCommitOptions {
  label?: string | undefined;
  signal?: AbortSignal | undefined;
  migrationToken?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `label` | `string \| undefined` | Yes | Optional revision label; it does not prevent retention from removing the revision. |
| `signal` | `AbortSignal \| undefined` | Yes | AbortSignal for cancellation. A started write can still commit. |
| `migrationToken` | `string \| undefined` | Yes | Persisted token used to make a migration retry idempotent. |

## ProjectError



```ts
class ProjectError  extends Error {
  constructor(code: ProjectErrorCode, message: string);
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `code` | `ProjectErrorCode` | No | Structured error code; inspect the code rather than matching message text. |
| `name` | `string` | No |  |
| `message` | `string` | No |  |
| `stack` | `string \| undefined` | Yes |  |
| `cause` | `unknown` | Yes |  |

### ProjectError.constructor

function Object() { [native code] }

```ts
constructor(code: ProjectErrorCode, message: string)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `code` | `ProjectErrorCode` | Yes | — | Structured error code; inspect the code rather than matching message text. |
| `message` | `string` | Yes | — |  |

Returns: `ProjectError`.

## ProjectFile



```ts
interface ProjectFile {
  key: string;
  content: ContentReference;
  contentType?: string | undefined;
  metadata?: Record<string, string> | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `key` | `string` | No | Object or file key within the selected bucket or project. |
| `content` | `ContentReference` | No | Immutable content reference for this file or staged result. |
| `contentType` | `string \| undefined` | Yes | MIME content type attached to the object or file. |
| `metadata` | `Record<string, string> \| undefined` | Yes | User metadata included in storage and sync comparisons. |

## ProjectInput



```ts
type ProjectInput = Uint8Array | AsyncIterable<Uint8Array>;
```

## ProjectRevision



```ts
interface ProjectRevision {
  id: string;
  createdAt: string;
  label?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `id` | `string` | No | Stable identity of this workspace, file stage, revision or immutable record. |
| `createdAt` | `string` | No | Revision creation timestamp. |
| `label` | `string \| undefined` | Yes | Optional revision label; it does not prevent retention from removing the revision. |

## ProjectStatus



```ts
interface ProjectStatus {
  projectId: string;
  generation: number;
  versions: ProjectRevision[];
  currentVersionId: string | null;
  pending: boolean;
  canUndo: boolean;
  canRedo: boolean;
  hasUncertainWrite: boolean;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `projectId` | `string` | No | Stable project identifier used in local state and remote manifest paths. |
| `generation` | `number` | No | Committed storage generation used to detect stale publication. |
| `versions` | `ProjectRevision[]` | No | Retained revisions in history order. |
| `currentVersionId` | `string \| null` | No | Selected local stage or project revision ID. |
| `pending` | `boolean` | No | Whether the selected revision still needs remote confirmation. |
| `canUndo` | `boolean` | No | Whether an earlier retained revision can be selected. |
| `canRedo` | `boolean` | No | Whether a later retained revision can be selected. |
| `hasUncertainWrite` | `boolean` | No | Whether a saved remote attempt needs reconciliation. |

## StagedContent



```ts
interface StagedContent {
  stageId: string;
  content: ContentReference;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `stageId` | `string` | No | Identity of unpublished staged content, used for publication or explicit cleanup. |
| `content` | `ContentReference` | No | Immutable content reference for this file or staged result. |
