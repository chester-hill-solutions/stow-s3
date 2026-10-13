# Browser project storage

`BrowserProject` stores documents and assets as atomic multi-file revisions.
A revision records the project's file manifest. Revisions share immutable content chunks.
Payloads load when needed.
Sync uploads content before publishing the project manifest.
Your application controls rendering, document formats and collaboration.

The development package provides:

- `/project`: content storage, revisions and file-history migration;
- `/worker`: asynchronous object operations and bounded streams;
- `/browser-wasm`: memory and persistent WASM worker loaders.

These APIs are implemented in the source checkout. The 0.3.0 package is unpublished.

## A project with reusable assets

```ts
import {
  BrowserProject, ContentStore, IndexedDbContentAdapter,
} from "@chester-hill-solutions/stow-s3/project";

const persistence = new IndexedDbContentAdapter({
  databaseName: "design-projects",
  quotaBytes: 2 * 1024 ** 3,
});
await persistence.open("my-project");
const content = new ContentStore(persistence, { cacheBytes: 8 * 1024 ** 2 });
const project = await BrowserProject.open(content, {
  projectId: "poster",
  maxVersions: 101,
});

await project.commit([
  { type: "put", key: "document.json", data: new TextEncoder().encode(JSON.stringify({ layers: [] })) },
  { type: "put", key: "assets/photo.bin", data: photoChunks }, // AsyncIterable<Uint8Array>
], { label: "Imported photo" });

// Only this document is processed; the photo remains a content reference.
await project.commit([
  { type: "put", key: "document.json", data: editedDocument },
], { label: "Moved layer" });
await project.undo();
await project.redo();

for await (const chunk of project.stream("assets/photo.bin")) {
  await consume(chunk);
}
await project.close();
content.clearCache();
await persistence.close();
```

The application supplies `photoChunks`, `editedDocument` and `consume`.
Run the project API in an application worker to keep metadata work off the UI thread.
The WASM worker loader below runs the shared object runtime in a dedicated worker.

Opening, listing and history inspection read metadata without loading payloads.
Commits return status receipts, not saved bytes.
`read()` and `readRange()` allocate returned bytes and default to a 16 MiB limit.
Use `stream()` for larger files. The payload cache defaults to 64 MiB;
set a separate cache limit or disable it.

`replaceRange` replaces bytes at an offset without changing file length.
It reads, hashes and stages only overlapping chunks.
The application implements insertions and format-specific edits.
Use an atomic multi-file commit for related layers, tiles or assets.

## Shared Go runtime in a persistent worker

```ts
import { AsyncEmbeddedStow } from "@chester-hill-solutions/stow-s3/worker";
import { loadBrowserPersistentWasmHost } from "@chester-hill-solutions/stow-s3/browser-wasm";

const host = loadBrowserPersistentWasmHost({ maxInFlightBytes: 1024 ** 2 });
const runtime = await AsyncEmbeddedStow.openPersistent(host, {
  namespace: "tool-assets",
  databaseName: "tool-storage",
  maxBytes: 1024 ** 3,
  physicalQuotaBytes: 2 * 1024 ** 3,
  cacheBytes: 8 * 1024 ** 2,
});
try {
  await runtime.createBucket("assets");
  await runtime.putObjectStream("assets", "photo", photoChunks, { size: photoSize });
  const reader = await runtime.getObjectStream("assets", "photo");
  try {
    for await (const chunk of reader.chunks) await consume(chunk);
  } finally {
    await reader.close();
  }
} finally {
  await runtime.close();
  host.close();
}
```

This profile reports `backend: "host"`.
The Go runtime checks lifecycle, logical quotas, authorization and write conditions.
Opening loads object metadata, without copying all payloads into a second memory store.
Reads pin immutable content until the reader finishes.
Writes reserve quota, stage outside the runtime mutex, and recheck admission before atomic publication.
Declare the exact stream size. Short or oversized streams cannot publish.

Bridge messages carry metadata separately from bytes, with at most 256 KiB per stream transfer.
Ordinary calls preserve input buffers. Explicitly transferred ArrayBuffers detach from the caller.
Worker request and response limits also apply. A reserved cleanup call allows stream abort or close.

Complete-object methods allocate their result. WASM calls still copy bytes across the memory boundary.
Cancellation can stop queued work or abort staged streams.
A started mutation can commit before cancellation or a lost reply reaches the caller.
Host-store reset is unsupported; authorized object deletion remains available.

## Local commits, retention and recovery

A save stages all referenced content, then atomically publishes the file manifest and history state.
A failed save preserves the previous head.
Undo/redo selects retained revisions. Saving after undo removes the redo branch.

`maxVersions` limits retained revision count.
Namespace quotas include unique content, revision descriptors and head/pin records.
Browser database overhead and disk quotas are separate limits.
Cache limits exclude caller buffers, full-file results, Go runtime memory and metadata records.
Immutable records are limited to 1 MiB, including references.

`collect()` removes content without references from heads, retained revisions, active reads, stages or durable protections.
It stores reachability marks in IndexedDB and processes one revision descriptor at a time.
Marking and removal share a transaction. Quota or abort failures preserve prior records and usage.
Collection does not load all payloads or history into memory.

Reads protect content for the handle's lifetime.
The next exclusive opener clears protections from dead handles; durable protections remain.
Inspect recovery with `listStages()` and `listProtections()`.
Release abandoned stage IDs explicitly before collection. Stow does not resume or publish them automatically.

The persistent worker provides `inspectRecovery()`, `discardAbandonedStage(stageId)` and `collectGarbage()`.
It rejects attempts to discard an active upload's stage.
After forced termination, reopen, inspect stages, release selected abandoned stages, then collect.
See [worker recovery](browser-worker-runtime.md).

This profile requires Web Locks. Opening fails if they are unavailable.
Unknown persistence formats also fail.

## Explicit project sync

Pass `ObjectSyncRemote` and `remoteBucket` to `BrowserProject.open`.
`S3ObjectSyncRemote` implements the S3 transport.
`sync()` uploads missing content to `stow-projects/<project-id>/chunks/<sha256>`.
It then writes `manifest.json`, conditional on the confirmed ETag or expected absence.

To reuse a remote chunk without downloading it, Stow checks its provider-verified full-object SHA-256 checksum and length.
Without that checksum, Stow downloads and hashes it.
User metadata alone cannot prove content identity.
S3 uploads request a provider-verified SHA-256 checksum.

Before network requests, Stow persists the attempt and revision.
It resolves lost replies using the receipt, exact manifest bytes and metadata.
Editing can continue during upload. Confirming an older revision leaves later edits pending.
Conflicts preserve local history.

Your application manages remote chunk collection; Stow does not automatically delete cloud data.
Keep the project's remote endpoint and account configuration stable.
Project manifests use a separate remote format. Single-file S3 sync still uploads complete files.
The application handles collaborative merge.

## Migration from browser file history

`migrateFileHistory(source, key, project)` copies one file's retained stages into a dedicated project.
It preserves bytes, content type, metadata, labels and selected cursor.
Persisted migration tokens make interrupted retries idempotent.
The destination must retain all source stages.
Migration rejects a changed source or an unrelated target.

Migration preserves the source and its cursor.
Resolve an uncertain source sync through the original file-history API before migrating.
Reading the legacy stages temporarily incurs that profile's full-replay memory cost.

## Qualification

[The recorded Chromium workload](benchmarks/browser-project-2026-10-12.json) stages 1 GiB of deterministic incompressible assets, retains an initial revision plus 100 document edits, reopens and reads a small asset range:

- 100 document edits stage 1,192 additional payload bytes.
- Retained content and revision descriptors account for 1,093,367,162 bytes, about 1.8% above the original asset size.
- Opening and listing after restart read zero payload chunks.
- A four-byte range reads one 1 MiB chunk; the cache holds 1 MiB under an 8 MiB configured limit.
- Reopening took about 6.8 ms on the measured host; timings are environment observations, not API promises.
- The supported main-thread long-task observer recorded zero long tasks during the storage workload. This excludes an application's rendering work.

The persistent Chromium profile stored approximately 1.01 GiB in IndexedDB. An ephemeral Chromium context hit the browser's own quota with this incompressible dataset. The benchmark exercises the JavaScript project facade without instantiating WASM. Its Chrome heap sample is coarse JavaScript-only evidence and excludes native allocations; it is not a total-memory measurement. Separate real-worker tests verify the persistent Go runtime and bounded streams.

Run the normal browser suite with `npm run test:browser`. Run the opt-in large workload with `STOW_LARGE_PROJECT=1 npm run test:browser` (temporary persistent Chromium profile). The large workload is intentionally excluded from ordinary CI timing gates. See [the execution record](browser-project-plan.md) and [worker details](browser-worker-runtime.md).
