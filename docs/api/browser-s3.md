# Browser S3 sync transport API

Use an AWS SDK S3Client as an ObjectSyncRemote. Import from `@chester-hill-solutions/stow-s3/browser-s3`.

## Defaults and limits

| Setting | Value |
| --- | --- |
| Client | Caller-supplied S3Client with endpoint and scoped credentials |
| Missing object | headObject and getObject return null |
| Write condition | Required ETag or expected absence |
| Receipt metadata | Reserved stow-save-id key |

## Behavior

- Configure CORS for conditional requests, checksum headers and receipt metadata.
- Expose ETag and reconciliation metadata.
- The transport enforces If-Match or If-None-Match and requests SHA-256 verification when supplied.
- The application owns the SDK client's lifetime.

## Errors

- 403 remains an access failure, not a missing object.
- A definitive precondition failure reports a conflict.
- 409 and transport failures can leave an uncertain write.
- The provider must enforce conditions atomically.

## Example

```ts
import { S3ObjectSyncRemote } from "@chester-hill-solutions/stow-s3/browser-s3";
const remote = new S3ObjectSyncRemote(s3Client);
const object = await remote.getObject("files", "note.txt");
// Supply remote to BrowserFileHistory or BrowserProject.
```

## Symbols

[S3ObjectSyncRemote](#s3objectsyncremote)

## S3ObjectSyncRemote

Configure the client's endpoint, scoped credentials and timeouts in the host application.

```ts
class S3ObjectSyncRemote  implements ObjectSyncRemote {
  constructor(client: S3Client);
  headObject(bucket: string, key: string): Promise<{ etag: string; size: number; metadata?: Record<string, string> | undefined; sha256?: string | undefined; } | null>;
  getObject(bucket: string, key: string): Promise<SyncRemoteObject | null>;
  putObject(bucket: string, key: string, data: Uint8Array<ArrayBufferLike>, options: SyncRemoteWrite): Promise<{ etag: string; }>;
}
```

### S3ObjectSyncRemote.constructor

function Object() { [native code] }

```ts
constructor(client: S3Client)
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `client` | `S3Client` | Yes | — | Caller-owned or handle-owned AWS SDK client; follow the interface's close rules. |

Returns: `S3ObjectSyncRemote`.

### S3ObjectSyncRemote.headObject

Return object metadata without reading the body into the result.

```ts
headObject(bucket: string, key: string): Promise<{ etag: string; size: number; metadata?: Record<string, string> | undefined; sha256?: string | undefined; } | null>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<{ etag: string; size: number; metadata?: Record<string, string> | undefined; sha256?: string | undefined; } | null>`.

### S3ObjectSyncRemote.getObject

Return complete object bytes and metadata.

```ts
getObject(bucket: string, key: string): Promise<SyncRemoteObject | null>
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `bucket` | `string` | Yes | — | Bucket name. Create it before storing objects. |
| `key` | `string` | Yes | — | Object or file key within the selected bucket or project. |

Returns: `Promise<SyncRemoteObject | null>`.

### S3ObjectSyncRemote.putObject

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
