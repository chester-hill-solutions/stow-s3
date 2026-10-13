# Binary and worker runtime

The v1 JSON/base64 bridge remains available. New hosts expose the v2 binary
bridge: JSON metadata and an optional `Uint8Array` travel separately, and the
response is `{ metadata: string, data?: Uint8Array }`. Metadata is a v2 response
envelope with the same structured errors and operation results as v1. Object
payloads are absent from its JSON. Go uses `CopyBytesToGo` and `CopyBytesToJS`;
this removes base64 work but still copies across the WASM memory boundary.

`EmbeddedStow` automatically chooses binary calls when the host provides
`callBinary`. Its methods remain synchronous. `AsyncEmbeddedStow` uses the same
response validation over an asynchronous binary host.

```ts
import { AsyncEmbeddedStow } from "@chester-hill-solutions/stow-s3/worker";
import { loadBrowserWasmHost } from "@chester-hill-solutions/stow-s3/browser-wasm";

const host = loadBrowserWasmHost({
  maxInFlight: 4,
  maxInFlightBytes: 64 * 1024 * 1024,
  maxResponseBytes: 64 * 1024 * 1024,
});
const runtime = await AsyncEmbeddedStow.open(host);
try {
  await runtime.createBucket("assets");
  const bytes = new Uint8Array([0, 128, 255]);
  await runtime.putObject("assets", "example.bin", bytes);
  const object = await runtime.getObject("assets", "example.bin");
} finally {
  await runtime.close();
  host.close();
}
```

The default worker hosts a memory runtime. `loadBrowserWasmBridge()` instead
loads Go in the current realm, allowing a custom worker module to instantiate
its own storage adapter. `loadBrowserWasmHost({ workerUrl })` connects to such a
module. `serveEmbeddedWorker(endpoint, ready)` accepts a Promise for an async
binary host and admits bounded concurrent operations after initialization. The Go runtime
and the persistence adapter govern their ordering. Storage functions
must run in that worker; functions cannot be passed through `postMessage`.

The worker and the application share no object buffers by default. Posting an
input copies it and leaves the caller's buffer usable. Passing
`{ transfer: true }` to `putObject` deliberately detaches the input's entire
ordinary `ArrayBuffer`; partial views and shared buffers are rejected. Worker
response buffers transfer to the caller. They are independent of the runtime's
stored bytes.

Admission limits count requests and the UTF-8 metadata plus input bytes. Calls
over either limit reject with `backpressure`; there is no unbounded client
queue. Responses include metadata in their byte limit. Server admission and
response limits also default to four calls and 64 MiB. A custom worker should
configure corresponding limits when the client changes them.

Cancellation rejects the caller immediately. It prevents queued execution when
the worker observes it before dispatch. A started operation can still commit:
the caller must reconcile uncertain writes. The cancelled call keeps its
admission allocation until its response arrives, so repeatedly cancelling
does not bypass the memory bound. Both client and server reserve one additional
cleanup call with at most 4 KiB of metadata and no input payload for stream abort,
reader close, or runtime close. Closing the host rejects pending calls; the
browser host also terminates its worker. Closing an individual runtime first
permits ordinary cleanup.

The ready-made persistent worker instantiates IndexedDB, the content store and
its bounded cache, and the host-backed Go runtime in the worker:

```ts
const host = loadBrowserPersistentWasmHost();
const runtime = await AsyncEmbeddedStow.openPersistent(host, {
  namespace: "design-project",
  databaseName: "my-design-tool",
  cacheBytes: 8 * 1024 * 1024,
  physicalQuotaBytes: 1024 * 1024 * 1024,
  maxBytes: 1024 * 1024 * 1024,
});
await runtime.putObjectStream("assets", "image.bin", inputChunks, {
  size: inputByteLength,
  ifNoneMatch: "*",
});
const opened = await runtime.getObjectStream("assets", "image.bin");
try {
  for await (const chunk of opened.chunks) consume(chunk);
} finally {
  await opened.close();
}
```

Import `loadBrowserPersistentWasmHost` from the same `browser-wasm` subpath.
Streams use at most 256 KiB per bridge message. Streaming puts require the exact
size up front, reserve runtime quotas, and publish only after a successful
commit. Their bounded internal copies leave source buffers usable. A streaming
source should itself produce bounded chunks. Input chunks must remain unchanged
until consumed. Cancellation or source failure aborts the staged publication;
cleanup can be unsuccessful after transport failure, so closing the runtime
remains the fallback. A commit whose reply is lost can still have published.

A streamed read pins the selected immutable content. Later replacements do not
change its bytes, and garbage collection retains its content until close. The
consumer must close an opened stream it never iterates. Iteration closes on
completion, error, or early exit. Each Go read rechecks authorization.

The ready-made persistent worker also exposes caller-owned storage maintenance.
Its open receipt advertises `capabilities().hostMaintenance === true`. Memory
and custom hosts reject these methods with `unsupported_backend` unless they
explicitly implement this capability. These are administrative operations on
the caller's namespace, separate from Go object authorization.

```ts
const recovery = await runtime.inspectRecovery();
// Show recovery.stages and recovery.protections without loading object payloads.
await runtime.discardAbandonedStage(abandonedStageId);
const usage = await runtime.collectGarbage();
```

Forced worker termination can leave unpublished staged chunks protected on disk.
After reopening the namespace, inspect recovery metadata and explicitly choose
an abandoned stage to discard. Discarding an active write's stage fails with
`stage_active`. Releasing an abandoned stage does not publish a partial object.
Collection then removes unreachable records and chunks, preserving published
heads, retained revisions, active streams, and durable protections. It clears
the worker's content cache and returns physical storage accounting. Inspection
and collection read metadata; they do not materialize object payloads. Closing
the runtime disables maintenance along with its object operations.

`getObject` remains a complete-object convenience operation and is subject to
the response limit. `getObjectRange(bucket, key, { offset, length })` reads at
most 256 KiB per call. It returns the object's metadata with the requested bytes;
`size` describes the entire object. A range bounds read memory, but several
independent range calls do not constitute a stable snapshot during concurrent
replacement. Callers must coordinate their selected revision.

The Node host exposes `callBinaryAsync` and `openHost`. Host-backed handles must
use asynchronous dispatch because persistence may await JavaScript promises;
synchronous dispatch rejects those handles rather than blocking the event loop.
