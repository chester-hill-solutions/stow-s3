# The filesystem store

`internal/storage/fs` is the durable, on-disk implementation of `storage.Store`. It owns a whole
data directory, and this page states the contract that any change to it is obliged to keep. The
object-record format is defined by [ADR 0020](adr/0020-binary-object-records.md); this page covers
the obligations around it.

A workspace is a different backend with its own layout, described in the
[workspace contract](workspace-contract.md).

## The data directory

| Path | What it holds |
|---|---|
| `buckets/<bucket>/bucket.json` | Bucket metadata. |
| `buckets/<bucket>/objects/...` | One file per object. |
| `.multipart/` | In-flight multipart uploads. |
| `.save-requests/` | The durable save-request journal. |
| `.save-pending.json`, `.save-identity.json` | Journal state and the store's save identity. |
| `.recovery-holds.json` | Outstanding recovery holds. |
| `.capacity-binding.json` | The capacity-namespace binding, when the store is bound to one. |
| `.stow.lock` | The exclusive lock. |
| `.stow-owner` | The ownership marker. |

`buckets/` and `.multipart/` are created when the store opens. A directory that carries someone
else's ownership marker is refused rather than adopted.

## Object records

An object's bytes are preceded by a record header:

```
"STOWOBJ2"  8 bytes   magic
<uint32>    4 bytes   little-endian header length
<protobuf>  N bytes   metadata header
<payload>   rest      the raw object bytes
```

The reader accepts exactly record version 2 and refuses any other version. That is deliberate: a
format change without a reader for the old records makes an existing data directory **unreadable**
rather than partially readable, which is the diagnosable failure rather than a silent one. Records
written before the binary format carry no magic and are read through the legacy path.

The generated protobuf types live in the `recordpb` subpackage and are checked in. A field-number
change is caught by the regeneration gate rather than by a failing read.

## Ownership and concurrency

**One process owns a data directory at a time.** The store takes an exclusive lock at construction
and returns `errors.ErrUnsupported` on platforms where it cannot, so construction fails rather than
proceeding unguarded.

**The ownership marker is what makes deletion safe.** `.stow-owner` is the only thing that
distinguishes a directory stow created from one a developer happened to point at, so a directory
without it is never deleted on a caller's behalf. The marker's name is also read outside Go, by
`packages/stow-s3/src/ownership.ts`, and `scripts/check-version.mjs` fails if the two drift:
renaming it is a change in two languages.

Failing to write the marker is deliberately not fatal. Its absence only makes later cleanup refuse
to delete the directory, which is the safe direction to fail in.

## Durability capabilities are platform-gated

`SupportsGuardedWrites` is true only on darwin and linux, and `SupportsDurableSaveRequests` and
`SupportsRecoveryHolds` both defer to it. On another platform those capabilities are silently
absent, so a caller must ask the store rather than assume them from the type.

## Construction

Only `fs.NewFilesystemStore` and `fs.NewFilesystemStoreWithNamespace` construct a store.
`internal/runtime` discovers the package's optional capabilities by type assertion and is forbidden
from importing it, so the package's behaviour is part of the runtime's contract without appearing
in any type the runtime can name.
