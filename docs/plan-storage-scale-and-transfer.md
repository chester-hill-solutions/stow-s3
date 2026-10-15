# Plan: storage scale and transfer

**Date:** 2026-10-14. **Baseline:** `main` @ `9cb61c2`.

Three findings from the code evaluation need a design change rather than a
patch. Each is scoped here with the shape of the fix, the seam it lives behind,
and what would count as done. None is started.

---

## 1. Streaming `PutObject` in the runtime (unblocks COPY … REPLACE)

**Problem.** `s3api` `readSource` reads the whole source into memory for a
`COPY … REPLACE`, and the runtime's `StoreAdapter.PutObject` calls
`storage.BytesOf` (an `io.ReadAll`) for any body that is not a caller-owned
buffer. So a copy with replaced metadata, and any other non-owned reader,
buffers the object before the store sees it.

**Why a patch at one layer is not enough.** Streaming at the S3 layer only moves
the buffer; the runtime buffers next. The memory bound has to come from the
runtime and the store.

**Shape of the fix.**
- Give the runtime a `PutObjectStream`-shaped path that accepts an `io.Reader`
  with a known-or-unknown size and stages it (the filesystem store already has
  `StageObject`; the memory store is bounded by its own quota).
- Have `StoreAdapter.PutObject` use it for non-owned readers instead of
  `storage.BytesOf`.
- Have `s3api` `replaceMetadata` pass the source reader through after the
  precondition check, rather than `io.ReadAll`.
- Bound the staged read by the session byte budget, as `UploadPart` now does.

**Seam.** `storage.Store` already has staged-write concepts; this extends the
adapter, not the object model.

**Done when.** A `COPY … REPLACE` of an object larger than the session budget is
refused without allocating the whole object, and a normal copy streams.

---

## 2. Incremental listing (ordered index)

**Problem.** `fs.ListObjectsV2` walks the bucket, reads a record header per
candidate, collects matches, and only then paginates. Enumerating `N` objects
over `P` pages costs roughly `N × P` header reads, prefix filtering happens after
the read, and the walk holds the store's read lock. The run-through adapter
compounds it by gathering full local and upstream prefix listings before merging.

**Shape of the fix.**
- Maintain an ordered key index (key → record location), written on the same
  commit path that publishes a record, so a listing is a bounded range scan and a
  page costs the page, not the bucket.
- Keep the index crash-consistent with the records (it is derived state; rebuild
  on open if it is missing or its revision does not match).
- Push prefix and continuation-token bounds into the scan.
- Check the context during the scan (already done) and decide the lock scope: a
  range scan of an index should not need the write-blocking lock the walk holds.

**Done when.** Listing a bucket with a large object count is proportional to the
page, not to `N × P`, and a cancelled listing stops promptly.

---

## 3. Checkpoint cost (incremental capture)

**Problem.** A file checkpoint hashes and full-copies every included file, then
rescans the source for changes. That is good integrity work and it is not a
copy-on-write snapshot: frequent checkpoints of large, mostly-unchanged trees
cost full I/O and storage each time.

**Shape of the fix.**
- Content-address payloads already; make the checkpoint reference a payload the
  registry already holds instead of copying it again (deduplicate by digest).
- Detect unchanged files by (size, mtime, digest) against the parent checkpoint
  and reuse the parent's payload rather than re-copying.
- Keep the scan-before/scan-after and per-file digest verification: this is an
  optimization of the copy, not a weakening of the integrity check.

**Not in scope.** Freezing external writers. The capture refuses a detected
change; it cannot stop a process writing to the directory. That stays a documented
property: a checkpoint is a file/artifact checkpoint, not a snapshot of a running
process, its memory, or its open connections.

**Done when.** A second checkpoint of a large tree with few changes copies only
the changed payloads, and a corrupted parent payload is still detected.

---

## Sequencing

1 is independent and small; it unblocks a memory defect on a hot path. 2 is the
largest and touches the filesystem store's core. 3 is self-contained in
`pkg/stow` and pays off in a different workload. Recommended order: 1, then 3,
then 2.
