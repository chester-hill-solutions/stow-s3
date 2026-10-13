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

Outbox files have a schema version. Stow rejects newer schemas and migrates unversioned files.
All writers sharing an outbox must use the same revision.
See the [compatibility contract](compat-contract.md#62-read-through-cache-semantics) for propagation rules
and the [server reference](../README.md#admin-and-metrics-routes) for authentication.
