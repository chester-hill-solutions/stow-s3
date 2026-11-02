# Bounded protobuf outbox decoding

The outbox decoder rejected duplicate identities only after constructing the entire generated protobuf graph and maps sized from repeated-message counts. A 65,550-byte snapshot containing 32,768 empty repeated records allocated 7–15.7 MB before returning an error. The new decoder reuses one generated message per record type and grows domain maps only as valid records are accepted.

This is one recovery/resource correctness change. It does not change the v4 format, encoder, dependencies, exported interfaces, storage operations, S3 behavior, or journal durability protocol. No deployed incident is inferred from the synthetic corrupt input.

## Baseline and reproduction

Review baseline: `5f3598f81bfed1b179521da2406fd251594fbff9`; latest fetched `origin/main`: `219d1dcfff9807b04660a40313b32127e5be2933`. The baseline already includes duplicate detection, ID binding, overlap checks, ownership fencing, token/sequence exhaustion guards, and the earlier COPY corrections. Those changes are preserved.

The regression builds a length-correct `STOWOUT4` frame with version 4 and 32,768 empty provenance, active, or prepared messages. Empty messages are legal protobuf; the second identical domain identity is the error. The file-recovery test replaces a valid journal with this frame, checks the failed reload leaves known state and corrupt bytes intact, then restores the valid bytes and recovers the original entry.

`runtime.MemStats.TotalAlloc` measures allocations during decode after fixture construction and GC. These are allocated-byte measurements, not RSS or peak live heap. Each test has a 1 MiB budget, leaving substantial headroom over the corrected measurements.

| Repeated collection | Original allocated bytes | Final candidate allocated bytes |
| --- | ---: | ---: |
| provenance | 7,072,384–7,072,384 | 1,400–1,656 |
| entries | 15,633,472–15,638,976 | 1,912–2,216 |
| prepared | 15,633,520–15,633,616 | 1,912–2,216 |

The original implementation failed all four resource/recovery cases twice normally and twice under race detection. Thirty equivalence controls passed in each run. The corrected implementation passed all 34 cases twice per mode. The [red/green receipt](2026-10-18-outbox-bounded-decode/raw/proof/proof-receipt.json) records exact cases and allocations; the [final-candidate logs](2026-10-18-outbox-bounded-decode/raw/confirmation/candidate-normal.log) retain the refined measurements.

## Implementation and preserved contracts

`decodeOutboxProto` retains the frame checks and delegates payload decoding to a private record decoder. `protowire` from the existing protobuf dependency only splits root fields. The generated protobuf decoder still handles root scalars, child records, unknown fields, UTF-8 validation and syntax errors. One lazily allocated entry and one provenance message are reused; domain values contain owned strings, not references to mutable scratch messages.

The decoder records the first domain failure for provenance, active entries and prepared entries, then continues validating subsequent wire records. Once a collection fails, it stops converting/inserting that collection's domain values. Final validation keeps the original priority: wire/UTF-8 errors, root format/version errors, provenance errors, active errors, prepared errors, then normalization. Root scalars retain merge/last-value behavior. The child recursion limit accounts for the removed outer generated-message decode.

The encoder, entry/timestamp conversion and normalization are byte-identical to baseline. Tests compare the result against a test-only copy of the original batch decoder: 14 explicit wire/error-order vectors, 14 group-depth boundary cases, 6,000 seeded byte mutations and 2,000 seeded field-order/competing-error cases per run. Successful states must match deeply; failures must preserve error classification and return no partial state. This is differential coverage, not a proof for every byte sequence.

This bounds the temporary repeated-message graph and avoids map preallocation from bogus counts. It does not impose an absolute journal-size quota: valid state remains proportional to valid entries; a large individual record or unknown-field payload can still require proportional memory, and all remaining wire bytes are still parsed after a domain failure. Corruption rejection is not a durability or atomicity redesign.

## Railway measurements

All formatting, Go builds/tests, race/conformance checks, quality/documentation checks and benchmarks ran on the existing Railway scratch runner. Local work was limited to source editing, Git, packaging, hashing and result analysis. Runner deployment: `0c2710b0-fe71-4fa7-b98d-bdb44ed287de`; Linux amd64, Go 1.25.6, 4-CPU/4-GiB cgroup, UID 10001. Durable benchmark fixtures used the mounted ext4 `/results` volume (statfs 61267). Source hashes, commands, environment and binary hashes are retained in each job directory.

Before changing the implementation, 12 uninstrumented observations established the decode baseline. Separate 3-second CPU profiles and allocation profiles at rate 1/10 iterations identified generated string copies and temporary message allocations. In the minimal allocation-object profile, string decoding contributed 60.44% of total profile objects and reflected message construction 11.70%; the focused decoder accounted for 83.94% of the full profile. Instrumented timings are excluded from latency comparisons. [Profiles and commands](2026-10-18-outbox-bounded-decode/raw/before/profile-runs.json).

The initial embedded-scratch prototype produced 224 valid-workload observations: 28 cells, four samples per variant, ABBA/BAAB ordering, 400 ms minimum. It reduced allocations but retained adverse timings: rich decode on one CPU was 17.66% slower; populated enqueue/prepare cells were up to 8.10% slower. Empty file operations used 3–10.54% more bytes. These results remain in the [initial analysis](2026-10-18-outbox-bounded-decode/raw/comparison/comparison-analysis.json).

The adopted variant allocates scratch messages lazily. A longer confirmation used 1-second minimum runs, the same alternating order, four observations per variant per cell, followed by a separate identical-candidate-binary control with both labels: 56 paired observations and 56 same-binary observations. No earlier adverse observation was discarded. The following are median deltas for the final candidate; positive latency means slower.

| Workload (`-4` = GOMAXPROCS 4; otherwise 1) | Allocated bytes/op, before → after | Allocation count delta | Time delta |
| --- | ---: | ---: | ---: |
| file depth=0/enqueue-4 | 9,716 → 9,918 | -1.18% | -5.17% |
| file depth=128/enqueue-4 | 1,008,298 → 831,778 | -8.39% | +1.86% |
| file depth=4096/enqueue-4 | 32,000,806 → 26,053,195 | -8.63% | +2.38% |
| decode rich=false | 7,682,520 → 4,664,151 | -11.71% | +0.72% |
| decode rich=false-4 | 7,682,526 → 4,664,160 | -11.71% | -1.98% |
| decode rich=true | 10,229,384 → 7,116,376 | -7.29% | -1.30% |
| decode rich=true-4 | 10,229,388 → 7,116,378 | -7.29% | -2.29% |

The decode fixtures contain 4,096 active and 4,096 prepared entries, with a minimal and an all-fields/provenance shape (908,400 and 2,698,344 frame bytes). File depth is the count **per queue**. The enqueue cycle enqueues and removes a probe, so depth stays fixed; other initial cells measure prepare/discard, changing provenance and renewal. Durable cells include locking, cloning, serialization, fsync and reload, and verify reopened state. HWM/live-heap metrics are process-wide diagnostics including setup; they are not isolated per-operation peak measurements.

The final populated enqueue cycles allocated 17.51–18.59% fewer bytes, with measured median latency +1.86–2.38%. Empty enqueue used 201 more bytes/op (+2.07%) and one fewer allocation. Rich/minimal decode allocated 30.43–39.29% fewer bytes and 7.29–11.71% fewer objects. Same-binary control medians varied from −6.38% to +9.81%; this is observed runner variation, not a statistical confidence interval. These results support the memory benefit without establishing a general speedup or proving the absence of a CPU cost.

| Same candidate binary, two labels | Label median time delta |
| --- | ---: |
| file depth=0/enqueue-4 | +7.27% |
| file depth=128/enqueue-4 | +9.81% |
| file depth=4096/enqueue-4 | +6.77% |
| decode rich=false | -4.95% |
| decode rich=false-4 | -4.10% |
| decode rich=true | +3.01% |
| decode rich=true-4 | -6.38% |

[All final samples and summaries](2026-10-18-outbox-bounded-decode/raw/confirmation/confirmation-analysis.json); [exact commands and binary identities](2026-10-18-outbox-bounded-decode/raw/confirmation/confirmation-runs.json).

## Validation and limits

The final adopted production source hashes match the measured lazy-scratch candidate. Required checks: focused outbox tests twice normally and under race detection; full `go test ./...`; broader storage/runtime/embedded/runthrough/S3/public-package race tests; memory, filesystem, runtime-memory and runtime-filesystem conformance; `go vet ./...`; quality, size and formatting gates; site build/tests and documentation links. [Final checks](2026-10-18-outbox-bounded-decode/raw/final/checks.json), [regression receipt](2026-10-18-outbox-bounded-decode/raw/final/green-receipt.json).

A harness mistake initially launched the previous six-stage confirmation plan after source adoption. That extra run is retained under `raw/extra` and excluded from the baseline/paired/control measurements and final validation claims. The active plan was corrected; final validation requires the separate 14-stage job and its plan/receipt. The report and evidence also undergo Railway documentation checks after publication.

New costs: a small root-field scanner, per-record generated unmarshal calls, retained per-collection errors and test-only original-decoder oracle. The implementation has more private orchestration code; the oracle must remain fixed when reviewing future decoder changes. Empty workloads retain a small byte cost. A dependency upgrade could change recursion/error behavior, so the wire-order/depth differential tests matter. Generated string copies are retained to preserve ownership; no unsafe aliasing or pooling is introduced.

The next performance investigation is generated string-copy and domain-map allocation cost, which remains visible in the profiles. Establish a new post-change profile and a separately reviewable Railway comparison before any further optimization. No indexing, incremental checkpointing, format migration or broader storage machinery is part of this change.

[Evidence manifest](2026-10-18-outbox-bounded-decode/raw/artifact-manifest.json) covers the retained raw jobs, profiles, source snapshots, analysis scripts, validation receipts and isolated runner teardown.
