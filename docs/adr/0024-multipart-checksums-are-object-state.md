---
status: accepted
decision_digest: 95c7a725f5bd1c7e
amended: 2026-11-20
relates_to: 0020, 0002, 0006
---

# A multipart checksum is object state at completion

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

A multipart checksum is object state, verified at completion.

`UploadPart` already verifies a per-part checksum against the bytes it receives
and refuses a mismatch before the part is stored. The verified part value is not
persisted: completion identifies the parts by entity tag, and the integrity
guarantee a client asks for is the full-object value, which is checked against
the bytes that are about to be published. Persisting part values would be
reporting state without a reader that needs it, and it would change the part
record in three stores.

Completion accepts a `FULL_OBJECT` claim, applies it to the properties fixed at
initiation, computes the checksum over the assembled bytes in the pass that
already writes the object, and refuses a mismatch before publication. The
result is stored on the object metadata. The complete response reports the value
and `x-amz-checksum-type: FULL_OBJECT`; `HEAD` and `GET` report the value. The
type is not stored on the object, because a single-part checksum carries no type
and inventing one for every object would be a metadata change with no reader.

The algorithm is fixed at initiation and the claim must name the same one.
Completion accepts an algorithm-only claim from the upload when the client sends
only a value, and refuses a claim that contradicts the initiation.

The full-object value is computed from the concatenation, not combined from
part checksums. A CRC combine would be a second implementation of arithmetic the
assembly pass already has the bytes for, and the pass exists in all three
stores.

A store that cannot verify a completion claim refuses the completion rather than
publishing a value it did not check. The interface is optional, so the refusal
is explicit and not a capability the caller discovers from a wrong object.

Run-through forwards the algorithm and type upstream and caches the returned
values, so a proxied object reports what the upstream reported.

## Consequences

A default AWS client uploads an object larger than 8 MiB without a flag, and a
client that sends a wrong part or full-object value is told before publication
rather than after.

No part record changes shape, so existing uploads remain readable and no
compatibility fixture is needed.

Completion pays one digest pass over the assembled bytes. It is the same pass
that writes them, so the cost is the hash, not another read.

A composite checksum type is not implemented. A client that asks for one is
refused explicitly rather than given a full-object value under a composite
label.

The conformance suite gains a lifecycle case for create-with-CRC64NVME,
complete-full-object and a mismatched value, in the Go drivers and the API
tests. A shared-corpus case would also run the Node driver, and is the next
addition rather than a claim of this change.

## Amendments

- 2026-11-20: Accepted. Implemented as object state at completion; per-part
  values stay verified-but-unpersisted, which removes the part-record change
  this record originally proposed. The refusal it replaced named
  `--checksum-algorithm CRC32` and no longer exists for CRC64NVME.
