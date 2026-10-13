# Browser project storage

Stow can persist a file manipulation tool's documents and assets as atomic project revisions. Immutable content chunks are shared between revisions, payloads load on demand, and explicit synchronization publishes a manifest after its content is available. The application owns rendering, document semantics and collaboration.

The development package adds three entry points:

- `@chester-hill-solutions/stow-s3/project`: content persistence, project revisions and legacy-history migration.
- `@chester-hill-solutions/stow-s3/worker`: asynchronous embedded object operations and bounded streams.
- `@chester-hill-solutions/stow-s3/browser-wasm`: ready-made memory or persistent WASM worker loaders.

Existing `/embedded`, `/browser`, Node process and S3 interfaces keep their contracts. These additions are implemented in the development tree; publication is a separate release operation.

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

`photoChunks`, `editedDocument` and `consume` are application-owned values. Run the project facade inside an application worker when its metadata work should be off the UI thread. The dedicated WASM workers below provide a ready-made worker transport for the shared object runtime.

Opening, listing files and history inspection read metadata without replaying payloads. Commits return a status receipt rather than materializing the saved content. `read()` and `readRange()` materialize bytes and default to a 16 MiB limit; use `stream()` for larger files. The payload cache defaults to 64 MiB and can be disabled or bounded independently of durable storage.

`replaceRange` replaces bytes at an offset without changing file length. Only overlapping chunks are read, hashed and staged. Insertions, arbitrary file format deltas and semantic editing operations remain application representations; callers can save separate layers, tiles or assets through an atomic multi-file commit.

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

This profile reports `backend: "host"`. The injected store uses the same Go runtime for lifecycle, logical quotas, authorization and write predicates. Opening enumerates object metadata; it never seeds a second memory store. Reads pin an immutable object while it is consumed. Streaming writes reserve quota, stage outside the runtime mutex and recheck admission before atomic publication. Declared sizes are mandatory and short or oversized streams cannot publish.

The binary bridge carries metadata separately from bytes. The public stream protocol uses at most 256 KiB per transfer. Ordinary calls preserve caller buffers; explicitly transferred full ArrayBuffers become unavailable to the caller. Worker admission and response limits are enforced, with a bounded cleanup lane for stream abort/close.

Complete-object convenience methods still allocate their result. WASM still copies across its memory boundary. Cancellation stops queued work and aborts staged streams where possible; a started mutation may commit before cancellation or a lost acknowledgement reaches the caller. Reset of an externally owned host store is explicitly unsupported; deletion is an ordinary authorized operation.

## Local commits, retention and recovery

A project save atomically publishes its immutable file manifest and new history state after all referenced content is staged. A failed save keeps the previous head. Undo/redo selects retained project revisions; a save after undo releases the redo branch from history. Retention is a version count, while namespace quotas include unique content, revision descriptors and accounted head/pin bookkeeping. Browser-managed database overhead and actual disk quotas remain host constraints.

`collect()` reclaims content that no head, retained revision, active read, staging operation or durable protection references. Collection keeps reachability marks in IndexedDB and processes one revision descriptor at a time; it does not load all history or payloads into memory. Marking and sweeping are one transaction, so quota or abort failures preserve the previous records and usage. Reads use handle-lifetime protections. The next exclusive opener clears protections left by a dead handle; durable protections survive. `listStages()` and `listProtections()` allow explicit recovery inspection. Abandoned staged writes remain protected until the owner explicitly releases their stage IDs; they are never resumed or published automatically.

The ready-made persistent worker exposes the same maintenance through `inspectRecovery()`, `discardAbandonedStage(stageId)` and `collectGarbage()`. It refuses to discard a stage owned by an active upload. After forced worker termination, reopen the namespace, inspect abandoned stages and explicitly release the selected stages before collection. See [worker recovery](browser-worker-runtime.md).

Namespace ownership requires Web Locks and fails explicitly when unavailable. The persistence format is versioned and unknown formats fail. Cache limits bound cached payloads; caller document buffers, full-file convenience results, the Go runtime's fixed memory and metadata records are separate costs. Immutable records are limited to 1 MiB including their references.

## Explicit project sync

Supply a host-owned `ObjectSyncRemote` and `remoteBucket` to `BrowserProject.open`. The existing `S3ObjectSyncRemote` is one implementation. `sync()` publishes missing content under `stow-projects/<project-id>/chunks/<sha256>`, then conditionally writes `manifest.json` using its previously confirmed ETag or expected absence.

Existing remote chunks require a verified full-object provider SHA-256 checksum and length to avoid a payload download. Providers without that checksum are verified with GET and a local hash. User metadata alone is never proof of content identity. S3 uploads request a provider-verified SHA-256 checksum.

The sync attempt and its revision are persisted before network operations. Lost replies are reconciled against the receipt, exact manifest bytes and metadata. Local editing continues during upload, and acknowledging an older revision leaves later edits pending. Conflicts preserve local history. Remote chunk garbage collection is caller-owned; Stow does not delete published cloud data implicitly. The same local project must retain its intended remote endpoint/account configuration.

Project manifests are a separate remote representation. Ordinary single-file S3 publication continues to upload a complete file. This profile supplies coherent publication and conflict detection; collaborative merge remains an application concern.

## Migration from browser file history

`migrateFileHistory(source, key, project)` copies one legacy file's retained stages into a dedicated project. It preserves file bytes, content type, metadata, labels and selected cursor. Persisted migration tokens make retry idempotent after interruption. The destination must retain all source stages, and a changed source or unrelated target is refused.

The source is retained and its cursor is not advanced. An uncertain source sync must be reconciled through the original file-history facade before migration, so its pending remote operation is never dropped or reinterpreted as a project publication. Migration temporarily inherits the source profile's full-replay costs while reading that legacy data.

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
