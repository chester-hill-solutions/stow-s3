# Browser project storage remediation

The browser storage path will keep durable content authoritative, load payloads on demand, share immutable chunks between revisions, and publish coherent project versions. Existing embedded and file-history APIs remain compatible. The caller owns document semantics and rendering.

## Execution stages

- [x] Scope IndexedDB reads and deletion to the requested namespace; structurally share unchanged private payloads; remove forced write readback.
- [x] Verify binary WASM bridge and bounded worker transport with explicit ownership and cancellation.
- [x] Verify storage-backed asynchronous runtime and metadata-only browser startup; bounded range/stream reads and cache.
- [x] Verify content-addressed chunks, atomic project revisions, changed-range saves, retained history and garbage collection.
- [x] Verify remote chunk reuse, conditional manifest publication, persisted attempts and continued editing during upload.
- [x] Verify resumable migration, real browser failures/reloads and representative large-workload evidence; regenerate distribution and run standards.

## Required guarantees

A reported local save has committed its revision and references. Failed publication cannot expose half a project. Quotas include staged and retained storage. Uncertain remote attempts pin their revision until reconciled. Garbage collection preserves reachable chunks and active stages. Memory budgets cover library caches and in-flight chunks; full-file convenience reads and caller document buffers are measured separately.

## Qualification workload

The qualification uses a 1 GiB asset set, a small editable document and 100 retained edits. It records payload reads at open, staged bytes per edit, physical storage, peak library cache, sampled JavaScript heap and main-thread long tasks. This workload exercises the JavaScript project facade without instantiating WASM; separate isolate and real-worker tests cover the Go runtime, and these observations do not establish a combined application memory bound. Remote tests count writes and verify chunk reuse and reply reconciliation. Real IndexedDB tests exercise worker termination, quota failure, competing tabs, commit abort, lost replies and retained attempt pruning.

## Representation

The host supplies immutable chunk persistence and transactional head/revision records. Project revisions name file metadata and content references. Project state records retain history, cursor and unfinished remote attempts. Whole-file saves remain available; range replacement and separate asset saves let callers avoid processing unchanged content. OPFS is deferred until IndexedDB workload measurements justify a second persistence medium.

## Verified implementation

Binary v2, asynchronous host-backed Go objects and 256 KiB public streams preserve legacy entry points. Browser project saves share immutable chunks, atomically publish related files/history and support fixed-length range replacement. Remote publication persists attempts, reuses only verified content and permits local edits during upload. Legacy migration copies retained history idempotently and retains uncertain source operations. Real Chromium tests cover transactions, quotas, corruption, namespace ownership, reload, handle pins, staging, sync and database schema upgrade.

The incompressible 1 GiB/100-edit qualification also runs bounded mark/sweep collection: reachability marks live in IndexedDB, only one revision descriptor is fetched at a time, payloads are unread, and abort/quota faults rollback the entire collection. See the recorded [receipt](benchmarks/browser-project-2026-10-12.json).

Validation passed: all 154 package tests, 154 repository script tests, 33 normal Chromium tests, the separate large-workload qualification, native Go race tests and real WASM tests. The live provider suite remains opt-in and was not run. Required formatting, vet, quality/size/coverage ratchets, version/site/install/documentation/ADR checks and generated artifact reproducibility all passed. No quality baseline was increased. A final checksum fallback adjustment passed 20 focused remote/project tests and repeated TypeScript and generated-output gates.
