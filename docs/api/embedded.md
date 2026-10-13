# TypeScript embedded API

Run object operations synchronously inside a loaded WASM host. Import from `@chester-hill-solutions/stow-s3/embedded`.

## Defaults and limits

| Setting | Value |
| --- | --- |
| maxBytes / maxObjects | 0 means no configured limit |
| Backend | Memory |
| Payloads | Uint8Array; complete objects are buffered |
| List cursor | Return nextCursor unchanged on the next call |

## Behavior

- Create a bucket before writing objects.
- Check capabilities before conditional writes.
- ifMatch compares the content ETag; ifNoneMatch: "*" requires absence.
- Empty conditions are unconditional.
- headObject returns metadata without body bytes.
- reset clears stored state.
- Close the runtime before closing its host.

## Errors

- EmbeddedStowError exposes code and message.
- Common codes include bucket_not_found, object_not_found, quota_exceeded, precondition_failed, conditional_write_unsupported and closed.
- A failed precondition preserves the current object.
- Unknown response versions and malformed responses are rejected.

## Example

```ts
import { EmbeddedStow } from "@chester-hill-solutions/stow-s3/embedded";
import { loadNodeWasmHost } from "@chester-hill-solutions/stow-s3/node-wasm";

const host = await loadNodeWasmHost();
const store = EmbeddedStow.open(host);
try {
  store.createBucket("files");
  store.putObject("files", "note.txt", new TextEncoder().encode("hello"));
  const file = store.getObject("files", "note.txt");
} finally {
  store.close();
  await host.close();
}
```

## Symbols

[BridgeObject](#bridgeobject) · [decodeEmbeddedResponse](#decodeembeddedresponse) · [EMBEDDED_BINARY_PROTOCOL_VERSION](#embedded_binary_protocol_version) · [EMBEDDED_PROTOCOL_VERSION](#embedded_protocol_version) · [EmbeddedBinaryResponse](#embeddedbinaryresponse) · [EmbeddedBucket](#embeddedbucket) · [EmbeddedCapabilities](#embeddedcapabilities) · [EmbeddedHost](#embeddedhost) · [EmbeddedListOptions](#embeddedlistoptions) · [EmbeddedObject](#embeddedobject) · [EmbeddedObjectPage](#embeddedobjectpage) · [EmbeddedPutOptions](#embeddedputoptions) · [EmbeddedStow](#embeddedstow) · [EmbeddedStowError](#embeddedstowerror) · [EmbeddedStowOptions](#embeddedstowoptions) · [EmbeddedUsage](#embeddedusage) · [fromBridgeObject](#frombridgeobject)

## BridgeObject



```ts
interface BridgeObject  extends Omit<EmbeddedObject, "data"> {
  data?: string | Uint8Array<ArrayBufferLike> | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `data` | `string \| Uint8Array<ArrayBufferLike> \| undefined` | Yes | Object bytes. Input ownership and size rules depend on the method. |
| `bucket` | `string` | No | Bucket name. Create it before storing objects. |
| `key` | `string` | No | Object or file key within the selected bucket or project. |
| `size` | `number` | No | Total object or content size in bytes; stream uploads require an exact declaration. |
| `etag` | `string` | No | Content validator returned by the store or provider. |
| `contentType` | `string \| undefined` | Yes | MIME content type attached to the object or file. |
| `metadata` | `Record<string, string> \| undefined` | Yes | User metadata included in storage and sync comparisons. |
| `lastModified` | `string \| undefined` | Yes |  |

## decodeEmbeddedResponse



```ts
decodeEmbeddedResponse<T>(parsed: unknown, version: number, payload?: Uint8Array<ArrayBufferLike> | undefined): T
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `parsed` | `unknown` | Yes | — |  |
| `version` | `number` | Yes | — |  |
| `payload` | `Uint8Array<ArrayBufferLike> \| undefined` | No | — |  |

Returns: `T`.

## EMBEDDED_BINARY_PROTOCOL_VERSION



```ts
const EMBEDDED_BINARY_PROTOCOL_VERSION: 2;
```

## EMBEDDED_PROTOCOL_VERSION



```ts
const EMBEDDED_PROTOCOL_VERSION: 1;
```

## EmbeddedBinaryResponse



```ts
interface EmbeddedBinaryResponse {
  metadata: string;
  data?: Uint8Array<ArrayBufferLike> | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `metadata` | `string` | No | User metadata included in storage and sync comparisons. |
| `data` | `Uint8Array<ArrayBufferLike> \| undefined` | Yes | Object bytes. Input ownership and size rules depend on the method. |

## EmbeddedBucket



```ts
interface EmbeddedBucket {
  name: string;
  creationDate?: string | undefined;
}
```

| Field | Type | Optional | Description |
| --- | --- | --- | --- |
| `name` | `string` | No | Name identifying this bucket, head, protection or error class. |
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
| `options` | `EmbeddedStowOptions` | No | {} | Configuration for this operation; the parameter type lists the accepted settings. |

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
| `options` | `EmbeddedPutOptions` | No | {} | Configuration for this operation; the parameter type lists the accepted settings. |

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
| `options` | `EmbeddedListOptions` | No | {} | Configuration for this operation; the parameter type lists the accepted settings. |

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
| `sourceBucket` | `string` | Yes | — | Bucket containing the source object. |
| `sourceKey` | `string` | Yes | — | Source object key. |
| `destinationBucket` | `string` | Yes | — | Bucket that receives the copy. |
| `destinationKey` | `string` | Yes | — | Object key that receives the copy. |

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
| `name` | `string` | No | Name identifying this bucket, head, protection or error class. |
| `message` | `string` | No | Human-readable error message. |
| `stack` | `string \| undefined` | Yes | JavaScript stack trace, when available. |
| `cause` | `unknown` | Yes | Underlying error cause, when provided. |

### EmbeddedStowError.constructor

function Object() { [native code] }

```ts
constructor(code: string, message: string)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `code` | `string` | Yes | — | Structured error code; inspect the code rather than matching message text. |
| `message` | `string` | Yes | — | Human-readable error message. |

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

## fromBridgeObject



```ts
fromBridgeObject(object: BridgeObject): EmbeddedObject
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `object` | `BridgeObject` | Yes | — |  |

Returns: `EmbeddedObject`.
