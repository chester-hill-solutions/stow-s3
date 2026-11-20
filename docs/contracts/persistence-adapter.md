# Persistence adapter contract

A persistence adapter is the smallest thing a platform can implement to give
Stow somewhere to put objects. It has five operations. Everything else the
product does — buckets, copies, multipart, checksums, quotas, receipts,
checkpoints — is built above them.

This is a contract for hosts that already have storage and want Stow's object
semantics, not a replacement for the S3 `Store` interface. The full interface
remains available through `stow.Options.Store` for a caller that wants to
implement multipart or conditional copy itself.

## The five operations

```go
type Adapter interface {
	Get(ctx context.Context, bucket, key string) (*Object, error)
	Put(ctx context.Context, bucket, key string, body io.Reader, options PutOptions) (*Meta, error)
	ConditionalPut(ctx context.Context, bucket, key string, body io.Reader, options ConditionalPutOptions) (*Meta, error)
	List(ctx context.Context, bucket string, options ListOptions) (*ListResult, error)
	Delete(ctx context.Context, bucket, key string) error
}
```

| Operation | Rule |
| --- | --- |
| `Get` | Returns the bytes and metadata of one version. A missing object is `ErrNotFound`, not an empty body. `Object.Body` is closed by the caller. |
| `Put` | Publishes the bytes as a new version and returns its metadata. The returned `ETag` must be non-empty and identifies the stored bytes; rewriting identical bytes may return the same tag, which both reference adapters do. |
| `ConditionalPut` | The same publication, refused with `ErrPreconditionFailed` unless the condition holds. `IfMatch` compares the current version's `ETag`; `IfAbsent` requires that no version exists. An implementation that cannot do this atomically must not claim it: the bridge advertises conditional writes from this method alone. |
| `List` | Returns keys in lexicographic order, honouring `Prefix`, `Delimiter`, `MaxKeys` and an opaque `ContinuationToken`. Groups collapsed by `Delimiter` appear in `CommonPrefixes`. A truncated page carries a non-empty `NextContinuationToken`. |
| `Delete` | Removes the object. A missing object is `ErrNotFound`; the S3 layer treats that as a successful idempotent delete. |

`Meta` carries `Key`, `Size`, `ETag`, `Modified`, `ContentType`, `Metadata` and, when the adapter persists them, `ChecksumAlgorithm` and `ChecksumValue`.
A host that stores object metadata must return what it stored; one that does not
returns what it can, and the bridge reports what it knows.

Two optional interfaces cover what a real platform usually has anyway:

- `Header` answers `Head` without reading the body. Without it the bridge reads
  and closes the object, which is correct but costs a body read.
- `BucketAdmin` creates, deletes and lists buckets. Without it buckets are
  process-local names and the adapter is expected to accept any namespace.

## What the bridge provides

`stow.OpenAdapter(options, adapter, buckets...)` opens an embedded runtime over
a `persistence.Adapter`. The bridge supplies:

- bucket lifecycle, on top of `BucketAdmin` when present, in-process otherwise;
- `CopyObject` as get-and-put, with source preconditions evaluated against the
  version read;
- `DeleteObject` idempotence;
- conditional writes, because the adapter contract includes them.

The bridge does not supply:

- multipart uploads, which are an optional interface and stay refused;
- checksum persistence, so a checksum a client sends is verified at the request
  and not reported by a later `HEAD`;
- cross-process bucket existence for adapters without `BucketAdmin`.

Those are capability differences, not contract violations, and they are visible
in `Capabilities()`.

## Reference implementations

- **Directory:** `pkg/stow/persistence/directory` wraps the filesystem store, so
  the reference is the engine that ships rather than a second copy of it. It
  implements `Header` and `BucketAdmin`.
- **IndexedDB:** `IndexedDbAdapter` in `packages/stow-s3` implements the same five
  operations over one IndexedDB object store. It is exported from the `browser`
  entry and qualified by the Playwright browser suite.

  ```ts
  import { IndexedDbAdapter } from "@chester-hill-solutions/stow-s3/browser";

  const adapter = await IndexedDbAdapter.open({ databaseName: "my-app" });
  const meta = await adapter.put("bucket", "notes/today.md", bytes, {
    contentType: "text/markdown",
    metadata: { owner: "editor" },
  });
  await adapter.conditionalPut("bucket", "notes/today.md", next, {
    condition: { ifMatch: meta.etag },
  });
  const page = await adapter.list("bucket", { prefix: "notes/", maxKeys: 100 });
  await adapter.delete("bucket", "notes/today.md");
  adapter.close();
  ```

  The entity tag is SHA-256 of the bytes. Listing orders keys by IndexedDB's
  compound-key comparison, which is UTF-16 code-unit order; the Go directory
  reference orders by byte. Both are deterministic, and a host that needs byte
  order must say so in its own adapter.

  The browser profile also ships `IndexedDbPersistenceAdapter` for hosts that
  persist the embedded runtime as whole snapshots. That is a different,
  higher-level contract and not a substitute for the five operations.

## Verifying an implementation

`pkg/stow/persistence/adaptertest` is the shared suite. A host runs it against
its own adapter in its own repository:

```go
func TestMyAdapter(t *testing.T) {
	adaptertest.Run(t, func(t *testing.T) persistence.Adapter {
		return myAdapter(t)
	})
}
```

The suite covers round-trips, missing objects, entity-tag change on overwrite,
create-if-absent, update-if-match, stale and absent conditions, delete,
metadata and content-type round-trip, bucket isolation, prefix and delimiter
listings, and pagination to exhaustion.

The S3-visible corpus is this repository's lane, not a host-importable package.
`conformance/corpus` imports `internal/...` for its bridge, signing and server,
so a separate module cannot import it; Go refuses that at build time.
`corpus.RunAdapter` still starts a private S3 server over the bridge for each
case, and `make test-conformance` runs it as the `adapter` backend, which is how
the bridge is qualified against the same cases every SDK driver runs. Multipart
cases skip and say so, because the five operations do not include it.
`corpus.Run` drives the cases against an existing endpoint from inside this
module. A host that already serves S3 can drive its own endpoint with the AWS
SDK directly; the importable qualification for a host's adapter is
`pkg/stow/persistence/adaptertest`.

## Using it

```go
adapter, err := directory.Open("/var/lib/myapp/objects")
if err != nil {
	return err
}
runtime, err := stow.OpenAdapter(stow.Options{Backend: stow.BackendFilesystem}, adapter, "my-bucket")
if err != nil {
	return err
}
defer runtime.Close()
```

`Options.Store` and an adapter cannot both be supplied; the constructor refuses
rather than choosing.
