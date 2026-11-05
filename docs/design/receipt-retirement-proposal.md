# Proposed native save-receipt lifecycle

This is a proposal, not an implemented retirement API. The current filesystem
profile retains at most 1024 terminal receipts within its 8 MiB bookkeeping bound,
and refuses new keyed saves when full. Missing evidence never proves no effect.

The smallest lifecycle that preserves the existing exact-input replay result is
**retain → archive → retain a durable lookup marker**. It needs a caller-owned,
durable archive and explicit admission for both archive bytes and lookup markers.
It cannot provide unlimited saves within finite storage.

1. The caller selects an exact store generation and terminal request identity,
   and supplies the expected retained receipt digest and archive destination.
   Only `committed` and `not_committed` receipts qualify. Refuse while any
   publication is unresolved, while a recovery hold references the object, or
   when either journal or archive admission cannot be established.
2. Under the same store mutation gate, copy the full receipt into the archive,
   including store identity, request identity, complete meaning digest, outcome
   and returned object metadata. Synchronize its file and directories, read it
   back and verify it. An archive receipt retains the original result, not a
   historical object body: receipts do not promise historical reads.
3. Publish and synchronize a live lookup marker binding those identities and
   digests to the archive. Only then remove the full live receipt and release its
   charge. Interrupted copying leaves the live receipt authoritative. Interrupted
   cleanup may leave both copies; recovery validates them and completes cleanup,
   never applies the object body again. Retirement replay is idempotent only for
   the same receipt and archive identity.
4. Resolution and exact-input replay follow the marker and verify the archive
   before returning the original result. A different meaning still conflicts.
   Missing, changed or inaccessible archives return an unknown/unavailable
   outcome and refuse mutation; they never reopen the request key for a new save.
   Lookup markers are not silently evicted. Exhausting their declared bound
   requires a new store generation, retaining the original generation for lookup.

Whole-generation rotation is available operationally today: retain the original
directory at its original identity, route old requests to it, and seed a successor
with independently verified current objects through the object API. Do not copy
identity documents or journals into the successor or treat an unresolved request
as fresh. Rotation needs storage for both generations and does not reclaim the
retained evidence. Use a read-only authority for old-generation inspection, and
stop its writers before copying. Copying receipts by hand is not this proposed API.

## Exact decision needed before implementation

Approve **archive-backed indefinite result availability**, and specify who owns
the archive, its access/durability guarantees, its byte budget and the live marker
count/byte budget. This preserves the existing replay contract and is the
recommended option. An unavailable archive must remain a refusal, not a fallback
to executing a save.

Alternatively, explicitly approve a caller-declared replay horizon followed by
permanent refusal of retired request keys. That requires a public retired-result
contract and durable meaning/identity tombstones; it reduces result availability
and still requires bounded storage and generation rotation. No horizon, implicit
expiry or outcome override is chosen by this patch.

## Uncertain publication and recovery constraints

Keep the original object record and all journal evidence, stop managed writers,
and resolve the original request. Existing recovery acknowledges a publication
only when the recorded request identity, meaning and object fingerprint verify.
An absent object after a `publishing` entry is not proof that it never committed:
later effects or damaged evidence can produce the same observation.

If verification remains impossible, retain the unknown outcome and mutation gate.
Do not retire that receipt, manually label it committed/not-committed, delete its
pending journal or resubmit its body with a new key. Preserve an independent copy
before repairs. Recovering from destroyed evidence or advancing a held generation
requires an explicit repair protocol and operator decision beyond this proposal;
current code provides no administrative override. Physical power-loss behavior,
hostile host edits, cross-host archive fencing and automatic migration remain
unqualified.
