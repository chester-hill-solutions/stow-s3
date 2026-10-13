# Durable upstream write outbox

The native run-through adapter commits a supported mutation locally before
propagating it to an S3-compatible upstream. A durable per-key outbox records
the exact local object version and captured upstream state. This is separate
from the browser APIs' explicit file and project sync.

## Enable propagation deliberately

Run-through mode selects the upstream. Live-write consent separately permits
mutations. A `mirrorWrites` routing policy does not supply that consent.

Use `--mode run-through` with configured upstream credentials, and opt into
mutations with `--allow-live-writes` or `STOW_ALLOW_LIVE_WRITES=true`.
See the [run-through guide](run-through.md) for configuration and offline reads.

## Local commit and remote conditions

A prepared intent is reconciled with the committed local version before it
becomes eligible for propagation. Failed local mutations do not leave a write
that may later reach the provider. Entries for one key are processed in order;
file-backed workers coordinate through expiring claims and a filesystem lock.

Propagation uses the upstream validator captured from read provenance or an
observation made before preparing the local mutation. A confirmed missing key
uses an expected-absence condition. If the upstream changed, propagation stops
with a terminal conflict. If an upstream baseline cannot be established, the
local mutation still commits, but unsafe propagation is refused.

Local data remains available. A conflict needs a new caller decision; retrying
the same precondition cannot resolve it.

## Retry and lost replies

Transient failures retry with bounded backoff. Deterministic failures remain
available for inspection rather than being retried indefinitely.

An attempted put is acknowledged during recovery only when the upstream ETag,
content type, normalized user metadata and requested checksum match the immutable
local version. Matching bytes alone are insufficient for a metadata change.
An attempted delete is acknowledged when the upstream object is already absent.

If recovery cannot establish that state, another conditional attempt may be
needed. This is not an exactly-once delivery guarantee. A newer local version
does not silently replace the version recorded by the original intent.

## Inspection and intervention

`/_stow/inspect` reports object and upload state. The
`/_stow/outbox/retry` and `/_stow/outbox/discard` routes change live propagation
and always require the configured admin token, including on loopback. With no
admin token, those mutation routes are unavailable.

Outbox files have a schema version. Newer schemas are refused at open;
unversioned files migrate. All writers sharing one outbox must use the same
revision. See the [compatibility contract](compat-contract.md#62-read-through-cache-semantics)
for the normative propagation behavior, and the
[server reference](../README.md#admin-and-metrics-routes) for admin authentication.
