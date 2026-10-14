---
status: accepted
amended: 2026-10-14
decision_digest: 025d0e9da06ec082
relates_to: 0002, 0003, 0008
---

# Filesystem objects use protobuf metadata and raw payloads

## Context

The Railway comparison on October 13, 2026 found expensive filesystem mutations.
Before each mutation, quota refresh listed objects and decoded their JSON payloads.
A 1 MiB filesystem PUT allocated about 24 MiB in the embedded comparison.
CPU profiles showed JSON validation and base64 decoding dominating that path.

The user requested a performance fix and proposed protobuf storage.
The object-record directory remains distinct from an editable workspace.

## Decisions

New filesystem objects use record version 2 in one atomic file.
The file begins with `STOWOBJ2`, a four-byte little-endian protobuf-header length,
the protobuf header, and the raw payload.
The header stores logical metadata and the exact payload length.
The schema and generated Go source live in `internal/storage/fs/recordpb`.

HEAD, listing and quota refresh read the header and verify the file length.
GET streams payload bytes through the opened file, preserving that version across
managed atomic replacement and deletion. Windows materializes the captured payload
to retain its existing replacement behavior.
Operations requiring a content fingerprint still read the payload.

Publication keeps the existing temporary-file, file-sync, rename and directory-sync
sequence. Object paths, sharding, guards, save journals and admission remain intact.
Quota reconciliation runs on open, explicit refresh, changed managed filesystem
revisions, and failed mutations. Successful managed filesystem mutations use
incremental accounting. Generic stores and editable workspaces retain conservative
refresh. Pending publication is settled before reporting a filesystem revision.
An unresolved publication reports its stable revision and unknown effect.
Opening and receipt resolution may observe that state; mutation admission refuses it.
The runtime owns one accounting snapshot, including active staged reservations.
Ordinary unbound filesystem uploads prepare outside the runtime publication lock.
Unowned stream bytes stage in temporary files with incremental integrity checks.
Internally owned HTTP bodies keep their existing ownership handoff.
Publication rechecks conditions, recovery holds and runtime quotas.
Namespace admission, content guards and save receipts retain buffered publication.
Capacity binding requires all staged publications to finish or abort.
Ordinary copies stream a captured payload into the durable destination record.
Raw edits to object-record directories are outside the public managed profile.
Internal quiescent integrations reconcile through explicit `RefreshUsage`.

Version 1 JSON records remain readable, including in mixed directories.
An overwritten legacy object is written as version 2.
Opening a directory does not rewrite its objects.
Older Stow readers cannot read version 2 records.
Unknown versions, malformed headers and mismatched lengths are refused.

Protobuf field numbers must not be reused.
The schema generator and Go library are pinned.
Generated source is checked against a fresh schema build on Railway.
The S3 protocol, workspace layout and journal JSON formats do not change.

This narrows ADR 0008's statement that the filesystem record format stays unchanged.
Its separation of object records from editable workspaces remains in force.

## Amendments

2026-10-13: Successful managed filesystem mutations use revision-based accounting.
Ambiguous writes and pending-journal settlement still require reconciliation.
Publication fences ancestors once per directory lifetime in a handle; reopen,
failed fencing and directory removal require fresh fencing. File and immediate
parent sync remain mandatory for every publication.

2026-10-14: Ordinary filesystem streams stage outside the runtime lock.
Uploads and copies retain atomic record publication with bounded payload scratch.
Guarded and capacity-bound profiles keep their existing admission and journal paths.
