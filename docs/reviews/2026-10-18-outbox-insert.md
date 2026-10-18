# Keep lookup-before-insert duplicate checks

**Reject the single-lookup decoder candidate. Production remains byte-identical to baseline `46eb7f9`.** The retained change adds nine regression cases: six interleaved duplicate/recovery cases and three allocation guards. No dependency, public API, format, retained cache or storage abstraction changes.

The candidate saves a map lookup on valid records, but writing before detecting a duplicate can grow a full eight-entry map. In the focused short-provenance benchmark, rejection allocated **1,288 → 2,032 bytes (+57.76%)**, **27 → 30 objects**, and the median time was **21.66% slower**. Both entry collections also allocated three additional objects. This outweighs the pilot's faster decode observations under the product's correctness/resource-first objective.

## Baseline and experiment

`origin/main` was fetched and remains `219d1dcfff9807b04660a40313b32127e5be2933`. The previous goal turn made concrete progress in `46eb7f9`, bounding file allocation before native framing rejection. Current source is clean at that baseline before adding these tests. Existing decoder, frame-read, ownership, fencing, identity, snapshot and copy remediations are preserved.

The [latest profile](2026-10-18-outbox-frame-read.md) showed domain maps/stored entries dominating minimal decode bytes. Its decoder source is unchanged here. A representation change would be broad; this experiment instead changes only `addProvenance` and `addEntry` in a temporary overlay.

The candidate records map length, assigns the decoded value, and checks whether length increased. An unchanged length means duplicate; it deletes that slot and returns the existing error. Deletion avoids retaining the replacement value while later wire records are validated. Valid insertions save the separate membership lookup. The generated protobuf decoder, root framing, validation/error priority, normalization and all storage interfaces remain the same. The candidate never enters the production worktree.

Three predictions were tested: saving a lookup might reduce valid-decode CPU, especially for longer identities; allocation/GC might leave little durable-write value; and assignment before rejection might hurt invalid-input handling. Initial functional/race checks pass for both variants. The third prediction becomes decisive at the small-map boundary.

## Pinned runtime and allocation reproduction

The actual Go 1.25.6 map implementation grows a full small map before finding whether the assigned key already exists. Its `PutSlot` source explicitly marks the unnecessary-growth-on-update case. The string fast path and generic implementation are retained with full-file hashes and Go version in the [runtime evidence](2026-10-18-outbox-insert/raw/pilot/runtime-map.json). This is evidence for the pinned runtime, not a claim about every future Go release or all map updates.

The new serialized-frame guard interleaves eight provenance, eight active and eight prepared records. It compares allocations for that valid prefix with a frame containing a conflicting duplicate and a later distinct record. The later record must still receive wire validation, and the duplicate must still return no partial state. Allocation difference includes legitimate extra string decoding/error formatting; it should not rebuild the prefix merely to reject it.

The first proof averaged 64 decodes and used a 512-byte incremental allowance. Production passes; the candidate fails all three collections twice normally and twice under race detection. The final guard averages 256 decodes with a 640-byte allowance to give runtime/error-formatting variation headroom. The candidate still fails all three collections twice per mode. These are `runtime.MemStats.TotalAlloc` allocated-byte differences, not peak live heap or RSS, and they are a fixture-specific regression bound rather than a new product quota.

| Field (2 provenance, 3 active, 4 prepared) | Production extra bytes/decode | Candidate extra bytes/decode |
| --- | ---: | ---: |
| 2 | 162–285 | 906–1,019 |
| 3 | 291–415 | 747–879 |
| 4 | 290–411 | 747–878 |

[Initial boundary proof](2026-10-18-outbox-insert/raw/boundary/boundary-receipt.json), [final higher-sample proof](2026-10-18-outbox-insert/raw/proof/boundary-receipt.json) and the exact source snapshots retain both methods. Candidate test exit 1 is expected and verified to come only from this allocation guard; a green harness step does not mean the candidate test passed.

The other six new cases interleave all three record collections at depths 129 and 1,025 per collection, insert a nonadjacent conflicting duplicate followed by a distinct record, compare decoding with the original batch-decoder oracle, and exercise real `FileOutbox` reload. Failed loads must preserve known state and corrupt bytes; restoring valid bytes must recover the active/prepared queues and provenance. Both implementations pass these functional contracts. The experiment exposes a resource regression, not an existing production data-loss defect.

## Railway method and complete measurements

All formatting, builds, tests, race/conformance checks, quality/documentation checks and benchmarks ran on the existing authorized Railway scratch runner. Local operations were source editing, Git, packaging, hashing and result analysis. Deployment `c47e63ce-668d-4c8b-ba95-3505188ff7f3`, Go 1.25.6, Linux amd64, UID 10001, 4 CPU/4 GiB. File benchmarks use the mounted ext4 `/results` volume (statfs 61267). The target service remains untouched.

Temporary Go overlays select the candidate and common probes; ordinary production source stays on baseline. Commands, source hashes, binary identities, filesystem checks and SHA-256-verified exports are retained. ABBA/BAAB ordering provides four observations per label/cell, with a 400-ms minimum. Component benchmarks use GOMAXPROCS 1; durable cycles use 4. These are limited synthetic measurements, without a traffic-distribution or statistical-significance claim. No identical-binary timing control was needed to reject a deterministic allocation regression; timing differences alone are not used as proof of code causation.

The pilot contains **168 observations across 21 cells**. Valid decodes hold 4,096 records of one collection. Rejection cases have one or 129 valid prefix records followed by a conflicting duplicate. Keys are 16 or 1,024 bytes; provenance identities include the bucket separator. Long custom entry IDs are supported, but do not represent the usual generated ID length. State/error equivalence is checked outside timing.

Durable cycles retain 0, 128 or 4,096 entries per active/prepared queue, then enqueue and acknowledge one probe: two locked, durable replacements per iteration. Reopened state must match. Fixture setup, validation and process HWM/live-heap sampling are outside timing. Process memory metrics include setup and are not isolated operation peaks.

Positive deltas below mean more/slower. All adverse cells are retained, including the **+15.69% empty-write median**. Valid decode byte/object counts are unchanged; observed CPU improvements are insufficient to justify the invalid-input allocation cost.

| Pilot workload | Time delta | Allocated-byte delta | Object-count delta |
| --- | ---: | ---: | ---: |
| write depth=0/enqueue-4 | +15.69% | -0.01% | +0.00% |
| write depth=128/enqueue-4 | -8.20% | -0.01% | -0.05% |
| write depth=4096/enqueue-4 | -11.52% | +0.05% | +0.10% |
| decode field=entries/key=1024 | -3.51% | -0.00% | +0.00% |
| decode field=entries/key=16 | -2.59% | +0.00% | +0.00% |
| decode field=prepared/key=1024 | -8.73% | -0.00% | +0.00% |
| decode field=prepared/key=16 | -5.55% | +0.00% | +0.00% |
| decode field=provenance/key=1024 | -3.95% | +0.00% | +0.00% |
| decode field=provenance/key=16 | -11.78% | +0.00% | +0.00% |
| reject field=entries/key=1024/prefix=1 | -5.16% | +0.00% | +0.00% |
| reject field=entries/key=1024/prefix=129 | -5.79% | +0.00% | +0.00% |
| reject field=entries/key=16/prefix=1 | -0.15% | +0.00% | +0.00% |
| reject field=entries/key=16/prefix=129 | -13.49% | +0.00% | +0.00% |
| reject field=prepared/key=1024/prefix=1 | -6.28% | +0.00% | +0.00% |
| reject field=prepared/key=1024/prefix=129 | -2.27% | +0.00% | +0.00% |
| reject field=prepared/key=16/prefix=1 | -10.09% | +0.00% | +0.00% |
| reject field=prepared/key=16/prefix=129 | -4.48% | +0.00% | +0.00% |
| reject field=provenance/key=1024/prefix=1 | -0.48% | +0.00% | +0.00% |
| reject field=provenance/key=1024/prefix=129 | -8.87% | +0.00% | +0.00% |
| reject field=provenance/key=16/prefix=1 | -4.32% | +0.00% | +0.00% |
| reject field=provenance/key=16/prefix=129 | -14.37% | +0.00% | +0.00% |

[All pilot samples, medians and ranges](2026-10-18-outbox-insert/raw/pilot/comparison-analysis.json), [commands/binary identities](2026-10-18-outbox-insert/raw/pilot/comparison-runs.json), [functional/race receipt](2026-10-18-outbox-insert/raw/pilot/contract-receipt.json).

The focused follow-up adds **48 observations across six cells**, using eight valid records followed by a duplicate. It exercises the boundary omitted by the one/129-record pilot. No earlier observation is discarded or replaced.

| Eight-record rejection | Bytes/op, before → candidate | Objects/op, before → candidate | Time delta |
| --- | ---: | ---: | ---: |
| field=entries/key=1024/prefix=8 | 33,929 → 34,385 | 72 → 75 | +3.10% |
| field=entries/key=16/prefix=8 | 4,472 → 4,928 | 71 → 74 | +15.62% |
| field=prepared/key=1024/prefix=8 | 33,929 → 34,385 | 72 → 75 | +7.10% |
| field=prepared/key=16/prefix=8 | 4,472 → 4,928 | 71 → 74 | +5.94% |
| field=provenance/key=1024/prefix=8 | 15,200 → 15,944 | 29 → 32 | +7.20% |
| field=provenance/key=16/prefix=8 | 1,288 → 2,032 | 27 → 30 | +21.66% |

Every boundary cell allocates three more objects. Short provenance rejection allocates 57.76% more bytes; short active/prepared rejection allocates 10.20% more. The source and repeated measured byte/object changes support the growth attribution. Timing remains a measured observation with the limitations above. [All focused samples and ranges](2026-10-18-outbox-insert/raw/boundary/comparison-analysis.json), [commands/binary identities](2026-10-18-outbox-insert/raw/boundary/comparison-runs.json).

## Retained change, validation and next work

The two new test files preserve interleaved duplicate recovery and guard prefix allocation at the full-small-map boundary. The allocation test samples more calls to reduce measurement noise; it adds GC/allocation work to the test suite. Runtime/formatting changes may require remeasurement of its allowance. Existing mutation, field-order, recursion, malformed-input, ownership, identity and recovery tests remain in force.

Final validation checks the unchanged production decoder and exact formatted tests: focused tests twice normally/race, full Go suite, broader storage/runtime/embedded/runthrough/S3/public-package race tests, four conformance configurations, vet, quality/size/format gates, site build/tests and doc links. The report and evidence undergo another Railway documentation check after publication. [Final checks](2026-10-18-outbox-insert/raw/final/checks.json), [nine-case regression proof](2026-10-18-outbox-insert/raw/final/green-receipt.json).

The remaining measured decode-byte cost is stored map values, rather than duplicate-check allocations. This experiment gives no basis to introduce a new state representation, index or checkpointing scheme. The next correctness review should return to resource admission and recovery across the storage paths, before proposing broader allocation machinery. No next production change is implemented here.

The [evidence manifest](2026-10-18-outbox-insert/raw/artifact-manifest.json) covers all 216 timing observations, rejected source/overlays, runtime evidence, normal/race proofs, final checks, source verification and isolated runner teardown.
