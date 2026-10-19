# Stow v0.3.2

This patch fixes concurrent stream cleanup, stale workspace multipart identities,
and late preparation/checkpoint budget checks. The public runtime API is unchanged.

## Distribution

Native downloads target Linux and macOS on x86_64 and ARM64. Verify each archive
against `SHA256SUMS.txt` from the release. npm and PyPI publication remain
deferred; build those clients from a source checkout.

## Changes

- Queue bounded worker cleanup requests so concurrently closing readers releases
  every stream slot and persistent content pin. Failed cleanup remains visible
  through the Go/WASM storage boundary and reader close can be retried. Cancelling
  an undispatched cleanup request immediately frees its queue slot.
- Store a separate content fingerprint for workspace multipart objects. A host
  edit invalidates the old object identity even when size and timestamps match;
  a stale conditional write is refused. Portable snapshots use the same metadata
  reconciliation as live reads, independently of prior object access.
- Admit local input sizes and counts before copying, and checkpoint limits
  before hashing. Bound the verification pass to the captured inventory.
- Reserve the selected Git tree and account for fetched Git metadata before
  checkout. Keep final accounting and failed-workspace cleanup as safeguards:
  this is admission control, not an operating-system disk quota for Git.
- Account for multipart staging and completion against namespace capacity.

## Compatibility

New workspace index entries include an additive `content_etag` field for multipart
objects. Existing multipart entries lack this fingerprint, so the first access
re-derives their identity from current bytes. This can change their ETag/version
and reset saved object metadata even if the bytes have not changed. Refresh
conditional-write ETags after upgrading and reapply required object metadata.
New fingerprinted entries retain multipart identity and metadata when only the
host timestamp changes. After an actual byte edit, portable snapshots now drop
stale saved metadata and derive content type, matching live workspace reads.

The filesystem restriction from v0.3.0 still applies: older Stow versions cannot
read version-2 filesystem records. Preserve a backup if you need to downgrade.

## Validation

The [patch benchmark report](../benchmarks/2026-10-15-patch-validation/report.md)
compares the candidate with commit `29897c8` on one host. It records startup,
memory, transfer throughput, workspace capture, and exact WASM size. These local
measurements do not establish production capacity or a service-level guarantee.
The review fixes follow that benchmark: their WASM asset is 4,617,652 bytes,
1,798 bytes (0.04%) larger than the measured candidate. Those timing measurements
have not been repeated for the review fixes.
