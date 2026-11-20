---
status: proposed
decision_digest: b222bf9ac452f274
amended: 2026-11-20
relates_to: 0020, 0002, 0006
---

# Multipart checksums are part state and object state

## Context

AWS clients send `x-amz-checksum-algorithm: CRC64NVME` when they create a
multipart upload, a per-part checksum on each `UploadPart`, and a full-object
value with `x-amz-checksum-type: FULL_OBJECT` when they complete. The service
refuses the algorithm at create and refuses the type at complete, so a default
`aws s3 cp` above the 8 MiB single-PUT cap fails with `NotImplemented`.

Single-part checksums already work: `internal/storage/checksum.go` computes
CRC32, CRC32C, CRC64NVME, SHA1 and SHA256, with published vectors. What is
missing is where a multipart checksum would live. `storage.PartInfo` carries a
part number, entity tag, size and modification time and no checksum, so a
per-part value cannot be verified or retained, and completion assembles parts
without hashing the concatenation, so there is no full-object value to compare
or report.

The part record is persisted by three stores. Changing it is a format change,
which is why the decision is recorded before the code.

## Decision

A multipart checksum is state in two places, and the distinction is the
decision.

- **Part state.** `PartInfo` gains a checksum algorithm and value. `UploadPart`
  verifies a supplied per-part checksum against the bytes it receives, before
  the part is stored, and the store persists the verified value with the part.
  A part whose checksum does not match is refused and not stored.
- **Object state.** Completion computes the full-object checksum over the
  assembled bytes in the pass that already writes the object, compares it with
  the client's `FULL_OBJECT` value when one is supplied, and stores it on the
  object metadata together with the type marker. The complete response, `HEAD`
  and `GET` report `x-amz-checksum-<algorithm>` and `x-amz-checksum-type`.

The algorithm is fixed at initiation. Completion requires every part to carry
that algorithm, and refuses a mixed set rather than deriving a value from some
of the bytes.

The full-object value is computed from the concatenation, not combined from
part checksums. A CRC combine would be a second implementation of arithmetic
the assembly pass already has the bytes for, and the pass exists in all three
stores.

Part records gain the fields additively. A part written before this change has
no checksum, which is a valid state for a part in an upload that never declared
an algorithm; a completion that requires one refuses it. No existing record is
rewritten on open.

The three stores implement the same rules; a store that cannot persist a part
checksum refuses the initiation rather than accepting one it would forget.

Run-through forwards the algorithm and type upstream and caches the returned
values, so a proxied object reports what the upstream reported.

## Consequences

A default AWS client uploads an object larger than 8 MiB without a flag, and a
client that sends a wrong part or full-object value is told before publication
rather than after.

The part record changes shape. A pre-change fixture must still decode, and an
older reader must not be asked to read a new record: the record version is
bumped and the compatibility rule from ADR 0020 applies.

Completion pays one digest pass over the assembled bytes. It is the same pass
that writes them, so the cost is the hash, not another read.

A composite checksum type is not implemented. A client that asks for one is
refused explicitly rather than given a full-object value under a composite
label.

The corpus gains cases for create-with-CRC64NVME, complete-full-object, a
mismatched part and a mismatched full-object value, and the real-CLI profile
extends beyond 8 MiB so the original failure cannot return.

## Amendments

- 2026-11-20: Proposed. Not implemented. The interim refusal names
  `--checksum-algorithm CRC32` so a default client has a documented way forward
  until the part record and assembly pass carry the values.
