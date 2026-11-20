---
status: accepted
decision_digest: 83ea1efd37553987
amended: 2026-11-19
relates_to: 0003, 0010, 0014
---

# A persistence adapter is five operations

## Context

The product already accepted a caller-supplied store through
`Options.Store`, and `internal/embedded` adapts its optional capabilities. That
seam is the full object-store interface: buckets, copies, deletes, close, and
optional multipart, staging and conditional-write interfaces. It is the right
contract for a platform that wants to replace the storage engine. It is the
wrong contract for a platform that already has storage and wants Stow's
semantics on top of it, because implementing it means reimplementing S3-shaped
behaviour before the first object is stored.

Two reference points existed at different levels: the filesystem store is the
directory implementation, and the browser profile persists through
`IndexedDbPersistenceAdapter`, which commits whole snapshots rather than single
objects. Neither is presented as an integration contract, and nothing tells a
host which operations it must implement for the product to work.

## Decision

A persistence adapter is five operations: get, put, list, delete and conditional
put, over a bucket namespace and an opaque key. `pkg/stow/persistence` declares
them; `pkg/stow` bridges them to the embedded runtime.

The bridge, not the adapter, supplies buckets, copies, idempotent deletes and
capability reporting. Multipart, checksum persistence and cross-process bucket
existence for adapters without `BucketAdmin` remain outside the contract and are
reported as missing rather than emulated silently.

`ConditionalPut` is one of the five because a persistence layer that cannot
refuse a stale write cannot host a workspace or an agent edit. It is separate
from `Put` rather than optional, so a host cannot implement the easy four and
discover the missing one at the first guarded save.

The directory reference wraps the existing filesystem store rather than
implementing the five operations a second time. `adaptertest` is the shared
suite a host runs against its own adapter, and the S3 corpus runs through the
bridge as one more backend, so the same cases that qualify the shipped store
qualify a host's.

The full `Store` seam stays. It is how a platform replaces the engine; the
adapter is how a platform keeps its own engine and adds Stow.

## Consequences

A host implements five methods and gets the runtime, quotas, receipts and the S3
surface. The cost is that the product now has two integration levels, and a
reader has to know which one they are looking at. The names say it: `Store` is
the engine, `Adapter` is persistence.

The adapter cannot express multipart, so an adapter-backed environment refuses
multipart uploads. That refusal is a documented capability rather than a bug,
and the corpus records it as a skip rather than a pass.

Checksums a client sends are verified at the request and are not persisted by an
adapter that does not store them, so a later `HEAD` cannot report them. A host
that needs that reports the metadata itself.

An adapter that stores metadata but lies about `ETag` breaks conditional writes
and cannot pass `adaptertest`. That is deliberate: an entity tag is the only
currency the five operations have for "the version I read".

## Amendments

- 2026-11-19: Accepted. The contract is the five operations plus two optional
  interfaces (`Header`, `BucketAdmin`); the directory reference reuses the
  filesystem store. `IndexedDbAdapter` binds the same operations to IndexedDB and
  is qualified by the Playwright browser lane rather than by Node, which has no
  IndexedDB. The shared corpus is importable so a host runs it against its own
  adapter instead of trusting this repository's run.
- 2026-11-20: Correction. The S3 corpus is **not** host-importable:
  `conformance/corpus` imports `internal/...` for its bridge, signing and server,
  and Go forbids a separate module from importing those.
  `pkg/stow/persistence/adaptertest` is the importable host suite; the S3 corpus
  stays this repository's lane and runs as the `adapter` backend in
  `make test-conformance`.
