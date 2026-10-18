# Durable upstream write outbox

The native run-through adapter commits a mutation locally, then sends it to the upstream.
A durable outbox records the local object version and the upstream state used for its write condition.
Browser file and project sync use separate APIs.

## Enable propagation deliberately

Configure upstream credentials and use `--mode run-through`.
Enable writes with `--allow-live-writes` or `STOW_ALLOW_LIVE_WRITES=true`.
`mirrorWrites` routing policy does not enable writes.
See the [run-through guide](run-through.md) for configuration and offline reads.

## Local commit and remote conditions

An outbox intent can propagate only after Stow confirms its local commit.
A failed local mutation cannot produce a later upstream write.
Stow processes entries for each key in order.
Filesystem workers use expiring claims and a filesystem lock to coordinate.

The write condition uses the upstream validator captured during a read or before the local mutation.
A confirmed missing key uses an expected-absence condition.
If the upstream changes, propagation stops with a terminal conflict.
If Stow cannot establish a baseline, the local mutation commits but does not propagate.

Local data stays available. Resolve a conflict by choosing what to publish;
retrying the same condition cannot resolve it.

## Retry and lost replies

Filesystem outbox updates return an error if journal replacement cannot complete its durability fence.
The replacement may already be visible when that error occurs; it does not prove the old journal was restored.
Reads that need to record upstream provenance fail until it can be saved durably.
Unchanged native provenance saves may retain the journal file, but still sync its
data and parent directory. Legacy migration and repair fallbacks replace it.

A missing journal path is allowed when creating an outbox. An existing zero-byte
journal is rejected as corrupt. Restore a valid journal before retrying; Stow does
not reconstruct a truncated journal automatically.
Legacy JSON journal snapshots must have an object root; `null` is rejected.
Once a live handle has read or successfully written a journal, a missing-file reload
returns an error and preserves its last known state. This per-handle check does not
infer history across restarts or detect loss hidden by a valid replacement.

Transient failures retry with bounded backoff. Deterministic failures remain available for inspection.

After a lost put reply, recovery compares the upstream object with the recorded local version.
Its ETag, content type, normalized user metadata and requested checksum must match.
Matching bytes alone do not confirm a metadata change.
A delete is confirmed when the upstream object is absent.

If recovery cannot confirm the result, Stow may make another conditional attempt.
Delivery is not guaranteed to occur exactly once.
A newer local version does not replace the version recorded by the original intent.

## Inspection and intervention

`/_stow/inspect` reports object and upload state.
`/_stow/outbox/retry` and `/_stow/outbox/discard` change propagation.
Both require the configured admin token, including on loopback.
Without an admin token, these mutation routes are unavailable.

Outbox snapshots use framed protobuf schema 4. Stow rejects unsupported versions and unknown fields.
Existing JSON schemas 0–3 remain readable and convert on the next successful write.
Configured paths keep their names, including any `.json` suffix, while rewritten contents are binary.
All writers sharing an outbox must use the same revision; older binaries cannot read rewritten snapshots.
File-backed operations read and decode the complete journal under the shared lock.
Their cost grows with retained provenance and queued work; cache byte/object limits
apply to cached objects. See the [Railway measurements](reviews/2026-10-18-outbox-read-cost.md)
for measured latency, allocations and process memory, with their workload limits.

See the [compatibility contract](compat-contract.md#62-read-through-cache-semantics) for propagation rules
and the [server reference](../README.md#admin-and-metrics-routes) for authentication.
