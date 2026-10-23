# Asynchronous worker API

Run embedded object operations and bounded streams through an asynchronous binary host. Import from `@chester-hill-solutions/stow-s3/worker`.

## Defaults and limits

| Setting | Value |
| --- | --- |
| maxInFlight | 4 calls |
| maxInFlightBytes / maxResponseBytes | 64 MiB each, including metadata |
| Stream message | At most 256 KiB |
| transfer | false; copies caller buffers |
| Cleanup reserve | One call; at most 4 KiB metadata and no input body |

## Behavior

- AsyncEmbeddedStow.open uses a memory host.
- openPersistent uses a persistence-capable host.
- Stream uploads require the exact size.
- Complete-object reads obey response limits.
- Range reads use offset and length; separate ranges can observe different replacements.
- Close opened readers.
- Iteration closes a stream on completion or early exit.
- transfer: true detaches a full ordinary ArrayBuffer; partial views and shared buffers are rejected.

## Errors

- Over-limit calls reject with backpressure.
- Started mutations can commit despite cancellation or a lost reply.
- Maintain storage only when hostMaintenance is advertised; otherwise maintenance fails with unsupported_backend.
- Discarding an active stage fails with stage_active.
- Closing the host rejects pending calls.

## Example

```ts
import { AsyncEmbeddedStow } from "@chester-hill-solutions/stow-s3/worker";
import { loadBrowserPersistentWasmHost } from "@chester-hill-solutions/stow-s3/browser-wasm";

const host = loadBrowserPersistentWasmHost();
const store = await AsyncEmbeddedStow.openPersistent(host, { namespace: "files" });
try {
  await store.createBucket("assets");
  await store.putObjectStream("assets", "photo", chunks, { size: byteLength });
} finally {
  await store.close();
  host.close();
}
```

## Symbols

[AsyncEmbeddedHost](#asyncembeddedhost) · [AsyncEmbeddedStow](#asyncembeddedstow) · [AsyncWasmBridge](#asyncwasmbridge) · [EMBEDDED_STREAM_CHUNK_BYTES](#embedded_stream_chunk_bytes) · [EmbeddedObjectStream](#embeddedobjectstream) · [EmbeddedStreamPutOptions](#embeddedstreamputoptions) · [HostObjectStoreBridge](#hostobjectstorebridge) · [PersistentAsyncEmbeddedOptions](#persistentasyncembeddedoptions) · [serveEmbeddedWorker](#serveembeddedworker) · [WorkerCallOptions](#workercalloptions) · [WorkerEmbeddedHost](#workerembeddedhost) · [WorkerEndpoint](#workerendpoint) · [WorkerHostOptions](#workerhostoptions)

## AsyncEmbeddedHost



```ts
interface AsyncEmbeddedHost {
  callBinary(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined, options?: WorkerCallOptions | undefined): Promise<EmbeddedBinaryResponse>;
}
```

### AsyncEmbeddedHost.callBinary



```ts
callBinary(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined, options?: WorkerCallOptions | undefined): Promise<EmbeddedBinaryResponse>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `metadata` | `string` | Yes | — | Serialized binary-bridge request or response metadata. |
| `data` | `Uint8Array<ArrayBufferLike> \| undefined` | No | — | Object bytes. Input ownership and size rules depend on the method. |
| `options` | `WorkerCallOptions \| undefined` | No | — | Configuration for this operation; the parameter type lists the accepted settings. |

Returns: `Promise<EmbeddedBinaryResponse>`.

## AsyncEmbeddedStow



```ts
class AsyncEmbeddedStow {
  static open(host: AsyncEmbeddedHost, options?: EmbeddedStowOptions): Promise<AsyncEmbeddedStow>;
  static openPersistent(host: AsyncEmbeddedHost, options: PersistentAsyncEmbeddedOptions): Promise<AsyncEmbeddedStow>;
  static attach(host: AsyncEmbeddedHost, handle: number, capabilities: EmbeddedCapabilities): AsyncEmbeddedStow;
  capabilities(): EmbeddedCapabilities;
  collectGarbage(): Promise<ContentUsage>;
  inspectRecovery(): Promise<{ stages: string[]; protections: ContentProtection[]; }>;
  discardAbandonedStage(stageId: string): Promise<void>;
  usage(options?: WorkerCallOptions | undefined): Promise<EmbeddedUsage>;
  createBucket(bucket: string): Promise<void>;
  deleteBucket(bucket: string): Promise<void>;
  deleteObject(bucket: string, key: string): Promise<void>;
  reset(): Promise<void>;
  listBuckets(): Promise<EmbeddedBucket[]>;
  putObject(bucket: string, key: string, data: Uint8Array<ArrayBufferLike>, options?: EmbeddedPutOptions, call?: WorkerCallOptions | undefined): Promise<EmbeddedObject>;
  getObject(bucket: string, key: string, options?: WorkerCallOptions | undefined): Promise<EmbeddedObject>;
  getObjectRange(bucket: string, key: string, range: { offset: number; length: number; }, options?: WorkerCallOptions | undefined): Promise<EmbeddedObject>;
  readForSave(bucket: string, key: string, call?: WorkerCallOptions | undefined): Promise<EmbeddedSaveObservation>;
  saveObject(bucket: string, key: string, data: Uint8Array<ArrayBufferLike>, options?: EmbeddedSaveOptions, call?: WorkerCallOptions | undefined): Promise<EmbeddedSaveResult>;
  resolveSave(bucket: string, key: string, requestKey: string, call?: WorkerCallOptions | undefined): Promise<EmbeddedSaveResult>;
  releaseObservation(observationToken: string, call?: WorkerCallOptions | undefined): Promise<void>;
  putObjectStream(bucket: string, key: string, source: Uint8Array<ArrayBufferLike> | AsyncIterable<Uint8Array<ArrayBufferLike>>, options: EmbeddedStreamPutOptions): Promise<EmbeddedObject>;
  getObjectStream(bucket: string, key: string, options?: { signal?: AbortSignal | undefined; }): Promise<EmbeddedObjectStream>;
  headObject(bucket: string, key: string): Promise<EmbeddedObject>;
  listObjects(bucket: string, list?: EmbeddedListOptions): Promise<EmbeddedObjectPage>;
  copyObject(sourceBucket: string, sourceKey: string, destinationBucket: string, destinationKey: string): Promise<EmbeddedObject>;
  close(): Promise<void>;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `handle` | `number` | No | Open runtime handle issued by the WASM bridge. |

### AsyncEmbeddedStow.open

Open a handle. Retain it until operations finish, then close it.

```ts
open(host: AsyncEmbeddedHost, options?: EmbeddedStowOptions): Promise<AsyncEmbeddedStow>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `host` | `AsyncEmbeddedHost` | Yes | — | Bind address; native default is 127.0.0.1. |
| `options` | `EmbeddedStowOptions` | No | {} | Configuration for this operation; the parameter type lists the accepted settings. |

Returns: `Promise<AsyncEmbeddedStow>`.

### AsyncEmbeddedStow.openPersistent

Open a host-backed runtime in the selected namespace.

```ts
openPersistent(host: AsyncEmbeddedHost, options: PersistentAsyncEmbeddedOptions): Promise<AsyncEmbeddedStow>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `host` | `AsyncEmbeddedHost` | Yes | — | Bind address; native default is 127.0.0.1. |
| `options` | `PersistentAsyncEmbeddedOptions` | Yes | — | Configuration for this operation; the parameter type lists the accepted settings. |

Returns: `Promise<AsyncEmbeddedStow>`.

### AsyncEmbeddedStow.attach

Wrap an existing open receipt without allocating a second runtime.

```ts
attach(host: AsyncEmbeddedHost, handle: number, capabilities: EmbeddedCapabilities): AsyncEmbeddedStow
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `host` | `AsyncEmbeddedHost` | Yes | — | Bind address; native default is 127.0.0.1. |
| `handle` | `number` | Yes | — | Open runtime handle issued by the WASM bridge. |
| `capabilities` | `EmbeddedCapabilities` | Yes | — |  |

Returns: `AsyncEmbeddedStow`.

### AsyncEmbeddedStow.capabilities

Return the operations and limits supported by this handle.

```ts
capabilities(): EmbeddedCapabilities
```

Returns: `EmbeddedCapabilities`.

### AsyncEmbeddedStow.collectGarbage

Remove unreachable content and return physical usage.

```ts
collectGarbage(): Promise<ContentUsage>
```

Returns: `Promise<ContentUsage>`.

### AsyncEmbeddedStow.inspectRecovery

List staged writes and protections without loading payloads.

```ts
inspectRecovery(): Promise<{ stages: string[]; protections: ContentProtection[]; }>
```

Returns: `Promise<{ stages: string[]; protections: ContentProtection[]; }>`.

### AsyncEmbeddedStow.discardAbandonedStage

Release an abandoned stage; an active upload's stage is refused.

```ts
discardAbandonedStage(stageId: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `stageId` | `string` | Yes | — | Identity of unpublished staged content, used for publication or explicit cleanup. |

Returns: `Promise<void>`.

### AsyncEmbeddedStow.usage

Return current storage accounting.

```ts
usage(options?: WorkerCallOptions | undefined): Promise<EmbeddedUsage>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `options` | `WorkerCallOptions \| undefined` | No | — | Configuration for this operation; the parameter type lists the accepted settings. |

Returns: `Promise<EmbeddedUsage>`.

### AsyncEmbeddedStow.createBucket

Create the named bucket before storing objects.

```ts
createBucket(bucket: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |

Returns: `Promise<void>`.

### AsyncEmbeddedStow.deleteBucket

Remove an empty bucket; a nonempty bucket is refused.

```ts
deleteBucket(bucket: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |

Returns: `Promise<void>`.

### AsyncEmbeddedStow.deleteObject

Delete the selected object; deleting a missing object is idempotent.

```ts
deleteObject(bucket: string, key: string): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<void>`.

### AsyncEmbeddedStow.reset

Clear storage state where the backend supports reset.

```ts
reset(): Promise<void>
```

Returns: `Promise<void>`.

### AsyncEmbeddedStow.listBuckets

Return the buckets visible to the handle.

```ts
listBuckets(): Promise<EmbeddedBucket[]>
```

Returns: `Promise<EmbeddedBucket[]>`.

### AsyncEmbeddedStow.putObject

Store complete bytes and metadata, subject to write conditions and quotas.

```ts
putObject(bucket: string, key: string, data: Uint8Array<ArrayBufferLike>, options?: EmbeddedPutOptions, call?: WorkerCallOptions | undefined): Promise<EmbeddedObject>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `data` | `Uint8Array<ArrayBufferLike>` | Yes | — | Object bytes. Input ownership and size rules depend on the method. |
| `options` | `EmbeddedPutOptions` | No | {} | Configuration for this operation; the parameter type lists the accepted settings. |
| `call` | `WorkerCallOptions \| undefined` | No | — |  |

Returns: `Promise<EmbeddedObject>`.

### AsyncEmbeddedStow.getObject

Return complete object bytes and metadata.

```ts
getObject(bucket: string, key: string, options?: WorkerCallOptions | undefined): Promise<EmbeddedObject>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `options` | `WorkerCallOptions \| undefined` | No | — | Configuration for this operation; the parameter type lists the accepted settings. |

Returns: `Promise<EmbeddedObject>`.

### AsyncEmbeddedStow.getObjectRange

Read a range using offset and length, at most 256 KiB per call.

```ts
getObjectRange(bucket: string, key: string, range: { offset: number; length: number; }, options?: WorkerCallOptions | undefined): Promise<EmbeddedObject>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `range` | `{ offset: number; length: number; }` | Yes | — |  |
| `options` | `WorkerCallOptions \| undefined` | No | — | Configuration for this operation; the parameter type lists the accepted settings. |

Returns: `Promise<EmbeddedObject>`.

### AsyncEmbeddedStow.readForSave



```ts
readForSave(bucket: string, key: string, call?: WorkerCallOptions | undefined): Promise<EmbeddedSaveObservation>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `call` | `WorkerCallOptions \| undefined` | No | — |  |

Returns: `Promise<EmbeddedSaveObservation>`.

### AsyncEmbeddedStow.saveObject



```ts
saveObject(bucket: string, key: string, data: Uint8Array<ArrayBufferLike>, options?: EmbeddedSaveOptions, call?: WorkerCallOptions | undefined): Promise<EmbeddedSaveResult>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `data` | `Uint8Array<ArrayBufferLike>` | Yes | — | Object bytes. Input ownership and size rules depend on the method. |
| `options` | `EmbeddedSaveOptions` | No | {} | Configuration for this operation; the parameter type lists the accepted settings. |
| `call` | `WorkerCallOptions \| undefined` | No | — |  |

Returns: `Promise<EmbeddedSaveResult>`.

### AsyncEmbeddedStow.resolveSave



```ts
resolveSave(bucket: string, key: string, requestKey: string, call?: WorkerCallOptions | undefined): Promise<EmbeddedSaveResult>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `requestKey` | `string` | Yes | — | Unique persisted request identity for capture or save recovery. |
| `call` | `WorkerCallOptions \| undefined` | No | — |  |

Returns: `Promise<EmbeddedSaveResult>`.

### AsyncEmbeddedStow.releaseObservation



```ts
releaseObservation(observationToken: string, call?: WorkerCallOptions | undefined): Promise<void>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `observationToken` | `string` | Yes | — |  |
| `call` | `WorkerCallOptions \| undefined` | No | — |  |

Returns: `Promise<void>`.

### AsyncEmbeddedStow.putObjectStream

Stage exactly options.size bytes and atomically publish after validation.

```ts
putObjectStream(bucket: string, key: string, source: Uint8Array<ArrayBufferLike> | AsyncIterable<Uint8Array<ArrayBufferLike>>, options: EmbeddedStreamPutOptions): Promise<EmbeddedObject>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `source` | `Uint8Array<ArrayBufferLike> \| AsyncIterable<Uint8Array<ArrayBufferLike>>` | Yes | — | Complete bytes or an asynchronous sequence of bounded byte chunks. |
| `options` | `EmbeddedStreamPutOptions` | Yes | — | Configuration for this operation; the parameter type lists the accepted settings. |

Returns: `Promise<EmbeddedObject>`.

### AsyncEmbeddedStow.getObjectStream

Open a reader pinned to immutable object content; close it when finished.

```ts
getObjectStream(bucket: string, key: string, options?: { signal?: AbortSignal | undefined; }): Promise<EmbeddedObjectStream>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |
| `options` | `{ signal?: AbortSignal \| undefined; }` | No | {} | Configuration for this operation; the parameter type lists the accepted settings. |

Returns: `Promise<EmbeddedObjectStream>`.

### AsyncEmbeddedStow.headObject

Return object metadata without reading the body into the result.

```ts
headObject(bucket: string, key: string): Promise<EmbeddedObject>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<EmbeddedObject>`.

### AsyncEmbeddedStow.listObjects

Return one page. Pass nextCursor to continue when truncated is true.

```ts
listObjects(bucket: string, list?: EmbeddedListOptions): Promise<EmbeddedObjectPage>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `list` | `EmbeddedListOptions` | No | {} |  |

Returns: `Promise<EmbeddedObjectPage>`.

### AsyncEmbeddedStow.copyObject

Copy source bytes and metadata to the destination object.

```ts
copyObject(sourceBucket: string, sourceKey: string, destinationBucket: string, destinationKey: string): Promise<EmbeddedObject>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `sourceBucket` | `string` | Yes | — | Bucket containing the source object. |
| `sourceKey` | `string` | Yes | — | Source object key. |
| `destinationBucket` | `string` | Yes | — | Bucket that receives the copy. |
| `destinationKey` | `string` | Yes | — | Object key that receives the copy. |

Returns: `Promise<EmbeddedObject>`.

### AsyncEmbeddedStow.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## AsyncWasmBridge



```ts
interface AsyncWasmBridge  extends EmbeddedHost {
  callBinaryAsync(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined): Promise<EmbeddedBinaryResponse>;
  openHost(metadata: string, store: HostObjectStoreBridge): Promise<EmbeddedBinaryResponse>;
  exit(): void;
}
```

### AsyncWasmBridge.callBinaryAsync



```ts
callBinaryAsync(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined): Promise<EmbeddedBinaryResponse>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `metadata` | `string` | Yes | — | Serialized binary-bridge request or response metadata. |
| `data` | `Uint8Array<ArrayBufferLike> \| undefined` | No | — | Object bytes. Input ownership and size rules depend on the method. |

Returns: `Promise<EmbeddedBinaryResponse>`.

### AsyncWasmBridge.openHost



```ts
openHost(metadata: string, store: HostObjectStoreBridge): Promise<EmbeddedBinaryResponse>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `metadata` | `string` | Yes | — | Serialized binary-bridge request or response metadata. |
| `store` | `HostObjectStoreBridge` | Yes | — | Open underlying object or content store supplied by the application. |

Returns: `Promise<EmbeddedBinaryResponse>`.

### AsyncWasmBridge.exit



```ts
exit(): void
```

Returns: `void`.

## EMBEDDED_STREAM_CHUNK_BYTES



```ts
const EMBEDDED_STREAM_CHUNK_BYTES: number;
```

## EmbeddedObjectStream



```ts
interface EmbeddedObjectStream {
  object: EmbeddedObject;
  chunks: AsyncIterable<Uint8Array<ArrayBufferLike>>;
  close(): Promise<void>;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `object` | `EmbeddedObject` | No |  |
| `chunks` | `AsyncIterable<Uint8Array<ArrayBufferLike>>` | No | Ordered chunk descriptors, or an async byte iterator for stream readers. |

### EmbeddedObjectStream.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): Promise<void>
```

Returns: `Promise<void>`.

## EmbeddedStreamPutOptions



```ts
interface EmbeddedStreamPutOptions  extends EmbeddedPutOptions {
  size: number;
  signal?: AbortSignal | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `size` | `number` | No | Total object or content size in bytes; stream uploads require an exact declaration. |
| `signal` | `AbortSignal \| undefined` | Yes | AbortSignal for cancellation. A started write can still commit. |
| `contentType` | `string \| undefined` | Yes | MIME content type attached to the object or file. |
| `metadata` | `Record<string, string> \| undefined` | Yes | User metadata included in storage and sync comparisons. |
| `ifMatch` | `string \| undefined` | Yes | Write only when the current content ETag matches; empty means unset. |
| `ifNoneMatch` | `string \| undefined` | Yes | Use * to create only when absent; empty means unset. |

## HostObjectStoreBridge



```ts
interface HostObjectStoreBridge {
  call(metadata: string, data: Uint8Array<ArrayBufferLike>): Promise<EmbeddedBinaryResponse>;
}
```

### HostObjectStoreBridge.call



```ts
call(metadata: string, data: Uint8Array<ArrayBufferLike>): Promise<EmbeddedBinaryResponse>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `metadata` | `string` | Yes | — | Serialized binary-bridge request or response metadata. |
| `data` | `Uint8Array<ArrayBufferLike>` | Yes | — | Object bytes. Input ownership and size rules depend on the method. |

Returns: `Promise<EmbeddedBinaryResponse>`.

## PersistentAsyncEmbeddedOptions



```ts
interface PersistentAsyncEmbeddedOptions  extends EmbeddedStowOptions {
  namespace: string;
  databaseName?: string | undefined;
  cacheBytes?: number | undefined;
  physicalQuotaBytes?: number | undefined;
  maxStreams?: number | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `namespace` | `string` | No | Nonempty local persistence namespace; ownership is exclusive. |
| `databaseName` | `string \| undefined` | Yes | IndexedDB database name; namespaces partition records within it. |
| `cacheBytes` | `number \| undefined` | Yes | Maximum cached payload bytes; zero disables the payload cache. |
| `physicalQuotaBytes` | `number \| undefined` | Yes | Persistent worker content and bookkeeping byte budget; default is 1 GiB. |
| `maxStreams` | `number \| undefined` | Yes | Maximum active host-store readers and uploads; default is 16. |
| `maxBytes` | `number \| undefined` | Yes | Maximum managed object bytes; zero means unlimited. |
| `maxObjects` | `number \| undefined` | Yes | Maximum object count; zero means unlimited. |

## serveEmbeddedWorker

Cancellation stops calls awaiting initialization; started Go operations finish.

```ts
serveEmbeddedWorker(endpoint: WorkerEndpoint, host: Promise<EmbeddedHost | AsyncEmbeddedHost>, options?: WorkerHostOptions): () => void
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `endpoint` | `WorkerEndpoint` | Yes | — | S3 service URL, including scheme and port. |
| `host` | `Promise<EmbeddedHost \| AsyncEmbeddedHost>` | Yes | — | Bind address; native default is 127.0.0.1. |
| `options` | `WorkerHostOptions` | No | {} | Configuration for this operation; the parameter type lists the accepted settings. |

Returns: `() => void`.

## WorkerCallOptions



```ts
interface WorkerCallOptions {
  signal?: AbortSignal | undefined;
  transfer?: boolean | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `signal` | `AbortSignal \| undefined` | Yes | AbortSignal for cancellation. A started write can still commit. |
| `transfer` | `boolean \| undefined` | Yes | Detach the whole input ArrayBuffer instead of copying; defaults to false. |

## WorkerEmbeddedHost

Bounds admitted calls instead of retaining an unbounded queue of copied payloads.

```ts
class WorkerEmbeddedHost  implements AsyncEmbeddedHost {
  constructor(endpoint: WorkerEndpoint, options?: WorkerHostOptions);
  callBinary(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined, options?: WorkerCallOptions): Promise<EmbeddedBinaryResponse>;
  close(): void;
}
```

### WorkerEmbeddedHost.constructor

function Object() { [native code] }

```ts
constructor(endpoint: WorkerEndpoint, options?: WorkerHostOptions)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `endpoint` | `WorkerEndpoint` | Yes | — | S3 service URL, including scheme and port. |
| `options` | `WorkerHostOptions` | No | {} | Configuration for this operation; the parameter type lists the accepted settings. |

Returns: `WorkerEmbeddedHost`.

### WorkerEmbeddedHost.callBinary



```ts
callBinary(metadata: string, data?: Uint8Array<ArrayBufferLike> | undefined, options?: WorkerCallOptions): Promise<EmbeddedBinaryResponse>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `metadata` | `string` | Yes | — | Serialized binary-bridge request or response metadata. |
| `data` | `Uint8Array<ArrayBufferLike> \| undefined` | No | — | Object bytes. Input ownership and size rules depend on the method. |
| `options` | `WorkerCallOptions` | No | {} | Configuration for this operation; the parameter type lists the accepted settings. |

Returns: `Promise<EmbeddedBinaryResponse>`.

### WorkerEmbeddedHost.close

Release this handle. Do not submit new operations after closing it.

```ts
close(): void
```

Returns: `void`.

## WorkerEndpoint



```ts
interface WorkerEndpoint {
  postMessage(message: unknown, transfer?: Transferable[] | undefined): void;
  addEventListener(type: "message", listener: (event: MessageEvent<unknown>) => void): void;
  removeEventListener(type: "message", listener: (event: MessageEvent<unknown>) => void): void;
}
```

### WorkerEndpoint.postMessage



```ts
postMessage(message: unknown, transfer?: Transferable[] | undefined): void
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `message` | `unknown` | Yes | — | Human-readable error message. |
| `transfer` | `Transferable[] \| undefined` | No | — | Detach the whole input ArrayBuffer instead of copying; defaults to false. |

Returns: `void`.

### WorkerEndpoint.addEventListener



```ts
addEventListener(type: "message", listener: (event: MessageEvent<unknown>) => void): void
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `type` | `"message"` | Yes | — |  |
| `listener` | `(event: MessageEvent<unknown>) => void` | Yes | — |  |

Returns: `void`.

### WorkerEndpoint.removeEventListener



```ts
removeEventListener(type: "message", listener: (event: MessageEvent<unknown>) => void): void
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `type` | `"message"` | Yes | — |  |
| `listener` | `(event: MessageEvent<unknown>) => void` | Yes | — |  |

Returns: `void`.

## WorkerHostOptions



```ts
interface WorkerHostOptions {
  maxInFlight?: number | undefined;
  maxInFlightBytes?: number | undefined;
  maxResponseBytes?: number | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `maxInFlight` | `number \| undefined` | Yes | Maximum admitted worker calls; default is 4. |
| `maxInFlightBytes` | `number \| undefined` | Yes | Maximum worker request metadata plus payload bytes; default is 64 MiB. |
| `maxResponseBytes` | `number \| undefined` | Yes | Maximum worker response metadata plus payload bytes; default is 64 MiB. |
