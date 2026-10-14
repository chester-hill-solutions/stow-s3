# Object-write preparation — October 13, 2026

The buffered object-write module now owns body materialization, MD5 ETags,
checksum resolution and retained metadata ownership.
Memory, managed filesystem and buffered workspace writes use the same implementation.
Caller buffers and returned metadata cannot change saved objects.

Keyed filesystem writes prepare the body once before checking retained request identity.
Previously, they buffered it twice and compared unresolved checksum options against resolved saved options.
Equivalent computed-checksum retries now return the original receipt after overwrite and reopening.
Changed payloads still conflict. Replay does not replace the current object.

Checksum verification remains mandatory.
Preparation failures preserve the previous object.
A nil reader represents an empty object, preserving the workspace write behavior.
Runtime admission, HTTP authentication, content-length checks and stream-size verification retain their existing interfaces.
Reader size hints affect allocation only.
Workspace staging continues to stream directly into its temporary file.
Public interface declarations remain unchanged.

## Validation

All builds and tests ran on Railway.
The final run passed `make standards`, all conformance lanes, Node/WASM checks,
protobuf drift checks and browser validation.
There were 33 passing browser tests, two optional skips and no unexpected or flaky results.
Private chart validation checked 13 combinations and six desktop/mobile previews.
Coverage was 71.30% (10,896 of 15,281 statements).
The production comment baseline decreased from 4,113 to 4,111.
Other quality limits were unchanged.

| Test | Observable behavior |
|---|---|
| `TestComputedChecksumSaveReplay` | PUT and receipt lookup return the original computed-checksum save after overwrite and reopening. |
| `TestStorePreparesOwnedChecksumBody` | All three adapters preserve owned payloads, metadata and supported checksums. |
| `TestStorePreparationFailurePreservesObject` | Reader failures and checksum mismatches cannot replace an existing object. |
| `TestStorePreparesEmptyBody` | A nil reader produces an empty object with a valid checksum. |
| Existing stream-size and HTTP-length tests | Invalid declared lengths cannot publish an object or retain a reservation. |

The rebuilt SDK WASM is 6,490,152 bytes, an increase of 1,328 bytes (0.0205%).
Its SHA-256 is `c81ac1e311e74fc9312ee6e88d86c2e661ef0bc70e949e495e7c309f695f2d0c`.
The cloud-built artifact was copied back after hash verification.
The earlier validation refreshed formatting in two generated Go reference declarations;
their fields and public interfaces did not change.

## Evidence

- [Final verification receipt](verification/receipt.json) and [complete log](verification/standards.log).
- [Generated SDK hashes](verification/generated-change.json), [artifact export](verification/artifact-export.json) and [quality measurement](verification/go-quality-report.json).
- [Retry regression reproduction](red-regressions.json) and [supplemental validation history](supplemental-validation.json).
- [Preparation module](../../../internal/storage/put_body.go) and [store-interface tests](../../../internal/storage/put_body_contract_test.go).
- [Railway environment readback](../railway-environment.json).

Reproduction deployment: `d4f7a9e1-34c6-456e-b86a-3bddb4ce6771`, observed `SUCCESS` with both targeted tests failing as expected.
Its sanitized snapshot restored the pre-fix filesystem PUT and replay implementations.
Other preparation changes remained in that reproduction snapshot.
The first reproduction image failed setup before tests ran and is excluded from regression evidence.

Final validation deployment: `6b36d5a2-38e0-4fe8-83d6-8852ef4ece07`, observed `SUCCESS`.
Final validation input hash:
`ad2dc4dc47bbaf19b274b689ef3f847a162dc0d7b4c32b9d4892f08da4965a0f`.

Temporary Railway compute was stopped after export.
No throughput benchmark ran in this pass.
The prior memory HTTP PUT regression still needs a controlled comparison for attribution.
The website charts retain their historical measurements and remain unpublished.
Record-publication consolidation remains separate work.
