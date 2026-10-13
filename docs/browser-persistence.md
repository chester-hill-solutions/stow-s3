# Browser persistence profile

`openBrowserEmbeddedStow` stores the embedded WASM runtime's objects through a persistence adapter.
It restores the last committed generation when opened.
`EmbeddedStow` and the Node WASM API remain synchronous; this persistence API is asynchronous.

Mutations run in arrival order. Each promise resolves after its durable commit.
If the commit fails, the coordinator restores the previous generation and returns `persistence_error`.
If restoration also fails, the profile stops accepting operations.

```ts
import {
  IndexedDbPersistenceAdapter,
  openBrowserEmbeddedStow,
} from "@chester-hill-solutions/stow-s3/browser";

const persistence = new IndexedDbPersistenceAdapter({
  databaseName: "my-app-stow",
});

const stow = await openBrowserEmbeddedStow(wasmHost, {
  namespace: "app-profile",
  persistence,
  maxBytes: 10_000_000,
  maxObjects: 1_000,
});

await stow.createBucket("assets");
await stow.putObject(
  "assets",
  "hello.txt",
  new TextEncoder().encode("hello"),
  { contentType: "text/plain" },
);
// The mutation is committed before this promise resolves.
await stow.close();
```

The IndexedDB adapter stores a manifest, buckets and object records.
Object keys use `["namespace", "bucket", "key"]`; values hold `Uint8Array` bytes.
A database can contain several namespaces.
Unknown formats and stale generations return `persistence_corrupt`.

Only one opener can own a namespace. A competing opener returns `already_open`.
Web Locks enforce ownership across tabs. Without Web Locks, ownership checks cover only the current JavaScript realm.

`reset()` clears persisted and live state while retaining namespace ownership.
`close()` drains accepted operations, closes the runtime and adapter, and preserves committed data.
It is terminal and safe to repeat. It does not close the caller-supplied host.

Capabilities report `backend: "indexeddb"`, `persistent: true`, `multipart: false` and `upstream: false`.
Persistence errors do not cause a fallback to memory-only storage.

To use another store, implement `PersistenceAdapter` with `open`, `load`, `commit`, `clear` and `close`.
Each commit includes an expected generation and typed changes.
Apply them in one transaction and reject stale writers.

## Known limits

- Replay restores bytes, buckets, metadata and content types.
  It does not restore creation dates, ETags and last-modified times exactly;
  the synchronous replay API does not accept those values.
- Cross-tab ownership requires Web Locks.
- This profile buffers whole objects and replays the namespace into memory. It does not support streaming uploads.
  Use [persistent workers](browser-worker-runtime.md) for bounded streams.

<!-- repo-only:start -->
Unit tests cover the coordinator with a fake host and transactional adapter.
[Chromium tests](../packages/stow-s3/scripts/browser-tests/storage.spec.mjs) cover real IndexedDB commits,
transaction rollback, WASM quotas, reload and Web Locks ownership.
<!-- repo-only:end -->
`writeBatch()` commits related object puts/deletes in one transaction and rolls
back the whole batch on either a WASM operation failure or a persistence failure.
The [file history API](browser-file-history.md) uses this batch operation.
The [editor example](../examples/browser-editor/README.md) exercises real WASM,
IndexedDB, reload recovery, offline access and explicit sync to a native S3 fixture.
