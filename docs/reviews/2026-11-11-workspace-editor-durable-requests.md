# Internal workspace durable save requests

**Date:** 2026-11-11. **Status:** D.1 qualified for the local private native
macOS ARM64 same-live-owner profile, using GOFLAGS=-p=1 for integrated Go gates.
This record covers a private native implementation. Public SaveFile/ResolveSave,
CLI/MCP editing, recovery-aware owner startup and capture/cleanup qualification
remain disabled or open. The [canonical plan](../development/storage-foundation-plan.md)
owns status and the [contract](../contracts/workspace-editing-contract.md) owns
the persistence and coordination rules.

## Implemented boundary

The journal uses .stow/file-saves/identity.json, pending.json and hashed request
receipt names. Versioned checksummed records bind the workspace, bucket, journal
and native root identity. They contain planned index fields, hashes, file identity
and result metadata, never payload or live token bodies. Native identity uses
typed device/inode fields on Darwin/Linux; this record qualifies only the local
macOS profile. Other journal platforms refuse rather than infer identity.

The shared charged budget includes identity, terminal and pending files: 1,024
terminal records, one pending slot, 64 KiB per record and 8 MiB total. Capacity is
reserved before effect. Bounded directory enumeration and remaining-byte limits
apply during load. Full capacity preserves lookup of retained results, including
a physical exact-8MiB/1,024-receipt fixture. There is no eviction or receipt
reclamation scheme.

The publisher fixes a new generation and logical entry before effect. It persists
prepared intent, then publishing intent, performs the existing-parent atomic
replacement, and persists published root/parent/file identity, full content,
permission and nanosecond-time proof before index installation. The terminal
receipt precedes pending cleanup. Index modification time uses the published
time truncated to seconds; returned metadata retains the full observed time.
New private namespace entries are fenced through the pinned workspace root.

A prepared request can finish not_committed without writing payload. Publishing
stays unknown even when current bytes match the planned digest. Published
resolution validates the complete file proof and requires the index to match
either the recorded pre-entry or planned target; it repairs only that index.
The rename-to-durable-published-proof crash gap remains unknown and needs operator
reconciliation. This design does not infer a transaction from byte equality.

Retained lookup precedes live-observation and fresh-quota admission. Request
meaning binds the original token digest or explicit replacement, canonical path,
payload hash/length and fixed preservation policy. Terminal replay returns the
original cloned metadata even after a later save changes the path's bytes. The
same existing Runtime owns authority, limits, multipart reservations and usage.
Save/replay requires current read/write authority; resolution requires current
read authority and performs no payload write. Scope and privacy checks precede
disclosure. Configured resource policies remain unsupported.

Cancellation before publishing leaves payload unchanged; admitted publication
finishes bounded bookkeeping without a canceled read erasing evidence. A known
terminal effect is preserved if later cleanup fails. Pending or uncertain journal
state pauses new observations and managed mutations through UsageRevision;
resolving a trustworthy obligation restores readiness. The earlier C.2 opaque
writer-error barrier remains separate and cannot be cleared by journal refresh.

Ordinary Store.New refuses any existing journal marker before initialization
writes, including an invalid or aliased marker. This protects staged evidence;
it is not recovery-aware opening. D.2 must still claim ownership before loading,
preserve pending state across death/reopen, and qualify capture/removal/collection
barriers. Public native save/resolve remains absent until those gates pass.

## Review and local fault coverage

The format review corrected unbounded directory/aggregate loading, the exact-full
journal edge, an invalid publishing-to-not_committed transition, lossy UTF-8 and
metadata-key comparisons, timestamp/reservation boundaries, invalid multipart
target fields and directory-creation fencing. Seven focused journal tests and
the independent recheck passed before integration.

Twenty-nine new regression methods cover journal, backend, Runtime and private
editor behavior. Focused races and independent review passed in all layers.
Tests exercise actual publication branches, retained meaning, replay after token
release/expiry/new editor incarnation and later saves, cancellation, scope and
authority, quota/reservation accounting, index-only repair and shared readiness.
An actual index-file failure is repaired through a new editor on the same owner
without changing payload inode or modification time, followed by a fresh save.
All integrated required gates passed on the unchanged-source rerun. Local fault
seams distinguish a returned atomic writer error from durable phase evidence;
these tests do not qualify process death or power-loss behavior.

## Verification

The first integrated race attempt exhausted host disk space: existing workspace
fixtures could not write payloads, the public package could not link and a quality
test could not write its log. The volume had 120 MiB free. Removing 4.75 GiB of
older regenerable entries from this task's dedicated Go cache restored 9.4 GiB
free. Source, qualified binaries, evidence and assertions were unchanged; the
same gate passed on rerun. The failed and successful logs are both retained.

| Check | Result |
| --- | --- |
| Focused behavior and races | 29 new regression methods: 7 journal, 13 backend, 5 Runtime and 4 private-editor methods; affected race runs passed. |
| Independent review | Accepted format fixes, backend/Runtime and final editor integration. |
| Full standards and Go race suite | Passed on unchanged-source rerun after ENOSPC cleanup. No assertion, timeout or baseline changed. |
| Coverage gate | 77.82%, 15,680/20,148 statements under the configured measurement. |
| Scripts, extension and agents | 246 script tests, 27 extension tests and 40 agent tests passed; one opt-in model test explicitly skipped. |
| Conformance | Memory, filesystem, runtime-memory and runtime-filesystem passed. |
| Builds/API/generated output | Native/WASM builds, 13-page API generation/verification and fresh package reproduction passed. |

The [receipt](../assessment-evidence/2026-11-11/workspace-editor-durable-requests.json)
retains 21 source-file hashes, artifacts, the frozen 842-file Go source manifest
hash and exact qualification boundaries. The dedicated native artifact is saved
locally; the earlier C.2 artifact/source snapshot remains preserved. No provider
call, commit or deployment occurred. Validation recreated distribution output;
the exact 270 pre-existing omissions are restored afterward.

## Remaining work and backout

D.2 supplies the recovery-aware owner bootstrap, capture/cleanup guards and
process-kill/reopen qualification before public native editing. MCP and caller
qualification follow the native API. Linux execution, second-host use and model
editing retain separate gates; prior failed model receipts are unchanged.

Retain journal records on any uncertain result. Disabling the private surface
must not delete evidence or authorize an old binary to rewrite a journal-bearing
workspace. The previously qualified C.2 binary/source snapshot is preserved for
comparison, not automatic downgrade of roots containing the new journal.
