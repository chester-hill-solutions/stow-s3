# Store outbox snapshots as protobuf

The filesystem outbox now writes a framed protobuf snapshot, using the protobuf
dependency already present for filesystem object records. It preserves the same
locks, fresh reload, state cloning, file/directory durability fence and acknowledgement
boundary. Existing JSON schemas 0–3 remain readable and convert on the next
successful write. The journal path, Go interfaces and S3 behavior remain unchanged.

Base `39f61ba9bfdb2be9bd7ddb469b5b1579f51f3509` includes the directory-fence fix.
A fresh fetch reports `origin/main` at
`219d1dcfff9807b04660a40313b32127e5be2933`, an ancestor.
[Baseline](2026-10-17-outbox-protobuf/raw/baseline.json).
This is one measured encoding change; no indexing, checkpointing, new storage
abstraction or unchanged-save shortcut is implemented.

## Evidence before changing the writer

The existing file outbox decodes a complete JSON journal under its filesystem lock
on reads and writes. Writes clone the complete state and encode it with indentation
before replacement. The previous [journal-depth report](2026-10-17-outbox-durability.md)
establishes the scaling cost. This comparison changes the representation while
holding the transaction and durability work constant.

Three ranked predictions were tested: protobuf would reduce large-journal codec
costs; generated-message conversion might offset some savings; small durable saves
would remain dominated by filesystem work. Compact JSON was included to separate
binary encoding benefits from removal of indentation.

The 160-observation pilot includes both generated-message conversions, framing,
decoding, normalization and complete saves. It executes native locks, fresh reads,
cloning and `atomicfile.Write` through the same benchmark transaction for all three
codecs. Indented JSON and compact JSON use schema 3. The prototype protobuf uses
schema 4. Each comparison uses forward/reverse order and a 100-ms minimum at
GOMAXPROCS 1. The two JSON-baseline labels are byte-identical copies of one test
binary; their separately timed samples are retained without pooling them into one
baseline. [Plan](2026-10-17-outbox-protobuf/raw/pilot/plan.json), [binary/source hashes](2026-10-17-outbox-protobuf/raw/pilot/build.json), [all pilot samples](2026-10-17-outbox-protobuf/raw/pilot/samples.json), [ranges](2026-10-17-outbox-protobuf/raw/pilot/summary.json).

The pilot shows a material whole-save benefit beyond compact JSON at large depth.
It also reveals a real adverse result: isolated mixed-state protobuf decoding
allocates more bytes than either JSON representation. The final measurements below
retain that result and add the complete native lookup path.

Before production wiring, three native tests fail twice normally and twice under
race: the existing writer still emits JSON, JSON migration does not produce protobuf,
and native opening of a future protobuf file does not expose the format-version
error. These are test-first checks for the requested representation; the baseline
durability contract was already corrected in the previous change.
[Original native failures](2026-10-17-outbox-protobuf/raw/native-regression/reproduce-normal.log), [original race failures](2026-10-17-outbox-protobuf/raw/native-regression/reproduce-race.log).

## Implementation and storage contracts

[file_outbox_proto.go](../../internal/runthrough/file_outbox_proto.go) converts the
three maps, sequence and fencing-token counters to generated messages. All 23
`OutboxEntry` fields and all upstream-state flags are preserved. Entry map identity
is distinct from the embedded entry ID. Sorting before encoding keeps output
deterministic and preserves JSON's handling of normalized invalid UTF-8 key
collisions. Timestamp text uses the existing RFC3339 nanosecond/offset semantics
and retains timestamp-range errors. This avoids changing lease or ordering behavior.

The frame has eight magic bytes (`STOWOUT4`), a four-byte little-endian payload
length and a protobuf payload declaring schema 4. The decoder checks the exact
length, schema version and unknown fields at the snapshot, provenance and entry
levels before returning a state. Unknown fencing fields cannot be silently dropped
on a later rewrite. The payload length field is bounded by `uint32`.

[file_outbox.go](../../internal/runthrough/file_outbox.go) selects the binary decoder
by its prefix and retains the previous JSON reader for migration. It still reloads
under the same process/file locks on every operation. Common state normalization
preserves prepared-entry migration, active/prepared overlap rejection and attempted-
write reconciliation. Persistence still uses `atomicfile.Write(..., 0600)` and
assigns the new in-memory state only after successful persistence.

[Schemas and generated code](../../internal/runthrough/outboxpb/outbox.proto) are
internal. The entry schema is separate to meet the existing generated-file size
limit. No dependency or public API is added. The protobuf runtime was already pinned
at v1.36.12. Generation uses protoc 3.21.12 and protoc-gen-go v1.36.12, entirely on
Railway. The compiler/plugin are scratch build tools; release builds consume the
checked-in generated Go. The initial generic helper was removed after the `any`
ratchet rejected it. Splitting the generated schema resolved the 510-line generated
file violation. The quality baseline is tightened from 4,081 to 4,080 comment lines;
no baseline is raised. Earlier failed gate logs are retained.

The differential tests compare protobuf round trips with the existing JSON decoder
for complete/empty state, invalid UTF-8 collisions and legacy attempted writes.
They cover maximum 64-bit counters, all entry fields, timestamp precision/offsets,
known-empty provenance, unknown-state flags, deterministic output, every truncation
of a fixture, trailing data, future versions and nested unknown fields. Native tests
verify persistence/reopen, JSON migration, monotonic IDs and fencing tokens, and
refusal of a future file without replacement. Existing claim-expiry fixtures now
modify the protobuf message while preserving all unrelated journal fields.

The directory-fence tests continue to require failed acknowledgements, unchanged
acknowledged memory, readable visible replacements, recovery IDs, `0600` permissions,
no temporary litter and two successive failed provenance-dependent reads while the
fence remains unavailable. They run as UID 10001 on Railway rather than skipping
as root. The shared writer's existing Windows directory-sync policy is unchanged;
the permission-fault tests intentionally skip Windows/root. Windows execution is
not claimed by these Linux measurements.

## Final Railway measurements

All generation, Go execution, formatting, tests, race/conformance, benchmarks, vet,
quality, site and documentation checks execute only on the authorized Railway
scratch runner. Local work is editing, packaging, orchestration, exported-data
analysis and Git. Deployment `5d3249fc-74b1-40d0-80c7-69e2b2505937` reached SUCCESS.
It uses pinned Go 1.25.6, linux/amd64, the 4-CPU / 4-GiB cgroup and persistent ext4
results volume. CPU labels mean GOMAXPROCS, not affinity or client concurrency.

Depth counts additional provenance rows plus one target row. Mixed state has 1,024
additional provenance rows and 1,024 pending intents. Timestamps are fixed; changed
saves alternate equal-length ETags. Setup is excluded and saved values are checked.
`B/op` measures cumulative allocation, not retained heap or peak RSS. Process
`wchar` telemetry counts payload writes during the measured loop, including any
other process writes, and is not a physical-disk amplification counter.

### Codec comparison, including conversion

The final codec cohort retains 640 observations: two comparison families, ABBA and
BAAB order, two repetitions per block, a 200-ms minimum and CPU 1/4. Each row has
eight observations per version. The compact-JSON and protobuf families each retain
their own JSON baseline observations. The same final source and fixtures are used
by every binary; selector overlays choose only the codec. [Plan](2026-10-17-outbox-protobuf/raw/codec-final/plan.json), [binary/source hashes](2026-10-17-outbox-protobuf/raw/codec-final/build.json), [all samples](2026-10-17-outbox-protobuf/raw/codec-final/samples.json), [ranges](2026-10-17-outbox-protobuf/raw/codec-final/summary.json), [resources/load](2026-10-17-outbox-protobuf/raw/codec-final/block-resources.json).

| CPU | Comparison | Operation | Median μs before → after | Median B/op before → after | Allocations/op before → after |
|---|---|---|---:|---:|---:|
| 1 | compact | mixed=1024/decode | 3703.43 → 3107.27 | 858,136.5 → 858,136.0 | 12,347.0 → 12,347.0 |
| 1 | compact | mixed=1024/encode | 3199.24 → 1806.91 | 1,876,332.0 → 1,097,954.0 | 8,202.0 → 8,199.0 |
| 1 | compact | provenance=0/decode | 1.72 → 1.58 | 960.0 → 960.0 | 17.0 → 17.0 |
| 1 | compact | provenance=0/encode | 0.99 → 0.43 | 504.0 → 264.0 | 6.0 → 5.0 |
| 1 | compact | provenance=128/decode | 83.73 → 76.75 | 35,496.0 → 35,496.0 | 540.0 → 540.0 |
| 1 | compact | provenance=128/encode | 69.73 → 36.07 | 38,491.0 → 20,056.0 | 262.0 → 261.0 |
| 1 | compact | provenance=16384/decode | 12020.32 → 10844.52 | 4,847,001.0 → 4,847,003.0 | 65,697.0 → 65,697.0 |
| 1 | compact | provenance=16384/encode | 10957.49 → 7965.12 | 4,776,251.0 → 2,474,140.0 | 32,777.0 → 32,774.0 |
| 1 | compact | provenance=2048/decode | 1418.20 → 1263.76 | 601,304.0 → 601,304.0 | 8,238.0 → 8,238.0 |
| 1 | compact | provenance=2048/encode | 1339.04 → 745.41 | 614,555.0 → 319,593.0 | 4,102.0 → 4,101.0 |
| 4 | compact | mixed=1024/decode | 3415.83 → 3036.68 | 858,146.5 → 858,146.0 | 12,347.0 → 12,347.0 |
| 4 | compact | mixed=1024/encode | 3455.84 → 2021.42 | 2,205,159.0 → 1,106,578.5 | 8,212.0 → 8,200.0 |
| 4 | compact | provenance=0/decode | 1.69 → 1.48 | 960.0 → 960.0 | 17.0 → 17.0 |
| 4 | compact | provenance=0/encode | 0.98 → 0.41 | 504.0 → 264.0 | 6.0 → 5.0 |
| 4 | compact | provenance=128/decode | 84.29 → 73.86 | 35,496.0 → 35,496.0 | 540.0 → 540.0 |
| 4 | compact | provenance=128/encode | 66.91 → 30.05 | 38,673.5 → 20,067.0 | 262.0 → 261.0 |
| 4 | compact | provenance=16384/decode | 11561.60 → 10162.68 | 4,847,005.5 → 4,847,007.0 | 65,697.0 → 65,697.0 |
| 4 | compact | provenance=16384/encode | 11714.92 → 6889.40 | 6,299,166.5 → 2,474,322.5 | 32,790.5 → 32,773.5 |
| 4 | compact | provenance=2048/decode | 1404.59 → 1154.33 | 601,306.0 → 601,306.0 | 8,238.0 → 8,238.0 |
| 4 | compact | provenance=2048/encode | 1198.21 → 656.68 | 634,680.5 → 321,015.5 | 4,104.0 → 4,101.0 |
| 1 | protobuf | mixed=1024/decode | 3588.36 → 719.88 | 858,136.0 → 1,204,487.0 | 12,347.0 → 12,329.0 |
| 1 | protobuf | mixed=1024/encode | 3057.36 → 802.04 | 1,876,330.0 → 719,600.0 | 8,202.0 → 3,080.0 |
| 1 | protobuf | provenance=0/decode | 1.70 → 0.40 | 960.0 → 776.0 | 17.0 → 9.0 |
| 1 | protobuf | provenance=0/encode | 1.01 → 0.32 | 504.0 → 328.0 | 6.0 → 5.0 |
| 1 | protobuf | provenance=128/decode | 85.52 → 16.95 | 35,496.0 → 30,912.0 | 540.0 → 402.0 |
| 1 | protobuf | provenance=128/encode | 71.56 → 22.49 | 38,491.0 → 22,128.0 | 262.0 → 134.0 |
| 1 | protobuf | provenance=16384/decode | 11896.53 → 2715.97 | 4,847,001.0 → 4,375,320.0 | 65,697.0 → 49,244.0 |
| 1 | protobuf | provenance=16384/encode | 10848.74 → 4667.21 | 4,776,247.0 → 2,646,256.0 | 32,777.0 → 16,390.0 |
| 1 | protobuf | provenance=2048/decode | 1441.52 → 280.91 | 601,304.0 → 523,960.0 | 8,238.0 → 6,173.0 |
| 1 | protobuf | provenance=2048/encode | 1297.57 → 454.51 | 614,555.5 → 346,352.0 | 4,102.0 → 2,054.0 |
| 4 | protobuf | mixed=1024/decode | 3656.24 → 1180.90 | 858,145.0 → 1,204,498.5 | 12,347.0 → 12,329.0 |
| 4 | protobuf | mixed=1024/encode | 3955.92 → 894.11 | 2,200,225.5 → 719,601.0 | 8,212.5 → 3,080.0 |
| 4 | protobuf | provenance=0/decode | 1.68 → 0.46 | 960.0 → 776.0 | 17.0 → 9.0 |
| 4 | protobuf | provenance=0/encode | 0.93 → 0.32 | 504.0 → 328.0 | 6.0 → 5.0 |
| 4 | protobuf | provenance=128/decode | 84.03 → 19.79 | 35,496.0 → 30,912.0 | 540.0 → 402.0 |
| 4 | protobuf | provenance=128/encode | 62.75 → 21.41 | 38,648.5 → 22,128.0 | 262.0 → 134.0 |
| 4 | protobuf | provenance=16384/decode | 12304.42 → 4082.10 | 4,847,005.0 → 4,375,324.5 | 65,697.0 → 49,244.0 |
| 4 | protobuf | provenance=16384/encode | 10854.42 → 4724.26 | 6,073,546.5 → 2,646,258.5 | 32,789.0 → 16,390.0 |
| 4 | protobuf | provenance=2048/decode | 1317.68 → 350.65 | 601,305.0 → 523,961.0 | 8,238.0 → 6,173.0 |
| 4 | protobuf | provenance=2048/encode | 1202.50 → 481.47 | 633,359.0 → 346,352.0 | 4,103.5 → 2,054.0 |

### Actual native saves

The native comparison retains 320 observations with ABBA/BAAB ordering, two
repetitions, a one-second minimum and CPU 1/4: eight samples per row/version. The
baseline overlays only `file_outbox.go` and `outbox.go` from the starting revision;
the candidate uses the exact validated production path. Both versions call
`FileOutbox.SaveUpstreamState`, including native locks, reload, clone, update,
encoding and file/parent fsync. Both still replace the full journal on unchanged
values. [Plan](2026-10-17-outbox-protobuf/raw/native-final/plan.json), [binary/source hashes](2026-10-17-outbox-protobuf/raw/native-final/build.json), [all samples](2026-10-17-outbox-protobuf/raw/native-final/samples.json), [ranges](2026-10-17-outbox-protobuf/raw/native-final/summary.json), [resources/load](2026-10-17-outbox-protobuf/raw/native-final/block-resources.json).

| CPU | Save | Median ms before → after | Change | Median B/op before → after | Allocations/op before → after |
|---|---|---:|---:|---:|---:|
| 1 | mixed=1024/changed | 14.144 → 6.777 | -52.08% | 3,896,846.5 → 2,603,719.0 | 21,618.0 → 16,472.0 |
| 1 | mixed=1024/unchanged | 14.384 → 6.790 | -52.80% | 3,902,972.0 → 2,603,735.0 | 21,618.5 → 16,472.0 |
| 1 | provenance=0/changed | 3.304 → 3.298 | -0.17% | 4,687.0 → 4,327.0 | 53.0 → 44.0 |
| 1 | provenance=0/unchanged | 4.122 → 3.236 | -21.49% | 4,687.0 → 4,327.0 | 53.0 → 44.0 |
| 1 | provenance=128/changed | 3.670 → 3.253 | -11.37% | 99,559.0 → 74,511.0 | 834.0 → 568.0 |
| 1 | provenance=128/unchanged | 3.608 → 3.347 | -7.23% | 99,632.0 → 74,583.5 | 834.0 → 568.0 |
| 1 | provenance=16384/changed | 36.575 → 17.615 | -51.84% | 13,146,499.5 → 9,523,849.0 | 98,576.5 → 65,728.0 |
| 1 | provenance=16384/unchanged | 36.421 → 17.264 | -52.60% | 13,446,951.0 → 9,523,870.0 | 98,579.0 → 65,728.0 |
| 1 | provenance=2048/changed | 8.068 → 5.280 | -34.56% | 1,603,563.5 → 1,192,336.0 | 12,380.0 → 8,265.0 |
| 1 | provenance=2048/unchanged | 7.986 → 5.497 | -31.16% | 1,603,631.5 → 1,192,402.0 | 12,380.0 → 8,265.0 |
| 4 | mixed=1024/changed | 14.455 → 6.879 | -52.41% | 4,395,088.0 → 2,603,733.0 | 21,637.0 → 16,472.0 |
| 4 | mixed=1024/unchanged | 14.638 → 7.821 | -46.57% | 4,409,849.5 → 2,603,751.5 | 21,637.0 → 16,472.0 |
| 4 | provenance=0/changed | 3.278 → 3.423 | +4.42% | 4,690.0 → 4,327.0 | 53.0 → 44.0 |
| 4 | provenance=0/unchanged | 3.273 → 3.407 | +4.12% | 4,690.0 → 4,327.5 | 53.0 → 44.0 |
| 4 | provenance=128/changed | 3.922 → 3.658 | -6.72% | 100,819.0 → 74,512.0 | 835.0 → 568.0 |
| 4 | provenance=128/unchanged | 3.372 → 3.481 | +3.26% | 101,078.5 → 74,584.0 | 835.0 → 568.0 |
| 4 | provenance=16384/changed | 37.711 → 18.409 | -51.18% | 15,662,990.5 → 9,523,855.0 | 98,599.0 → 65,728.0 |
| 4 | provenance=16384/unchanged | 35.994 → 18.309 | -49.13% | 15,692,053.5 → 9,523,874.5 | 98,599.0 → 65,728.0 |
| 4 | provenance=2048/changed | 7.833 → 5.699 | -27.25% | 1,732,215.0 → 1,192,337.0 | 12,389.0 → 8,265.0 |
| 4 | provenance=2048/unchanged | 8.363 → 5.267 | -37.03% | 1,735,592.5 → 1,192,413.0 | 12,389.0 → 8,265.0 |

| Shape | Journal bytes before → after | Change |
|---|---:|---:|
| mixed=1024 | 508,920 → 157,598 | -69.03% |
| provenance=0 | 153 → 65 | -57.52% |
| provenance=128 | 11,545 → 7,233 | -37.35% |
| provenance=16384 | 1,458,329 → 917,569 | -37.08% |
| provenance=2048 | 182,425 → 114,753 | -37.10% |

Slower native medians retained: CPU 4 provenance=0/changed +4.42%; CPU 4 provenance=0/unchanged +4.12%; CPU 4 provenance=128/unchanged +3.26%.

### Actual single-key lookups

The separate lookup cohort retains 160 observations, ABBA/BAAB ordering, two
repetitions, a 200-ms minimum and CPU 1/4. It calls `FileOutbox.UpstreamState`,
including file reading, decoding, sequence/token updates, whole-state cloning and
the single-key lookup. It checks the returned value and reports process writes.
This captures the full read cost rather than interpreting isolated decoder
allocation as the whole operation's allocation. [Plan](2026-10-17-outbox-protobuf/raw/lookup-final/plan.json), [binary/source hashes](2026-10-17-outbox-protobuf/raw/lookup-final/build.json), [all samples](2026-10-17-outbox-protobuf/raw/lookup-final/samples.json), [ranges](2026-10-17-outbox-protobuf/raw/lookup-final/summary.json).

| CPU | Lookup | Median μs before → after | Change | Median B/op before → after | Allocations/op before → after |
|---|---|---:|---:|---:|---:|
| 1 | mixed=1024 | 3985.43 → 1026.59 | -74.24% | 1,888,449.0 → 1,882,548.5 | 13,393.0 → 13,375.0 |
| 1 | provenance=0 | 9.35 → 7.80 | -16.52% | 2,600.0 → 2,416.0 | 30.0 → 22.0 |
| 1 | provenance=128 | 101.80 → 29.43 | -71.09% | 59,480.0 → 50,800.0 | 555.0 → 417.0 |
| 1 | provenance=16384 | 13294.50 → 4006.60 | -69.86% | 7,888,312.0 → 6,875,952.0 | 65,774.0 → 49,321.0 |
| 1 | provenance=2048 | 1497.50 → 375.41 | -74.93% | 987,281.0 → 844,400.0 | 8,259.0 → 6,194.0 |
| 4 | mixed=1024 | 4417.06 → 1634.97 | -62.99% | 1,888,463.5 → 1,882,569.5 | 13,393.0 → 13,375.0 |
| 4 | provenance=0 | 9.40 → 7.46 | -20.63% | 2,600.0 → 2,416.0 | 30.0 → 22.0 |
| 4 | provenance=128 | 104.07 → 31.83 | -69.41% | 59,480.0 → 50,800.0 | 555.0 → 417.0 |
| 4 | provenance=16384 | 14460.34 → 5849.66 | -59.55% | 7,888,314.0 → 6,875,957.5 | 65,774.0 → 49,321.0 |
| 4 | provenance=2048 | 1484.90 → 503.87 | -66.07% | 987,275.5 → 844,402.5 | 8,259.0 → 6,194.0 |

The mixed CPU-1 lookup falls from 3.985 to 1.027 ms while complete allocation
falls slightly from 1,888,449 to 1,882,549 bytes/op. The larger isolated decoded
message allocation is offset by the smaller file buffer at this operation boundary.
Every native lookup sample records zero process writes.

### Costs and practical limits

The stripped CLI built with the repository's release flags grows from **15,048,996 to
15,081,764 bytes** (+0.218%). The baseline footprint build additionally stubs
the new codec file so an unused import does not initialize the outbox descriptor;
test binaries include both codecs and cannot establish this production footprint.
Both CLI builds use `-trimpath`, `-ldflags=-s -w` and `-buildvcs=false`. Temporary
CLI binaries are removed after hashes/sizes are recorded. [CLI footprint and hashes](2026-10-17-outbox-protobuf/raw/footprint/footprint.log).

All adverse timings and allocation rows remain in the samples and range summaries.
Generated messages add intermediate allocations; mixed-state decoding can allocate
more bytes than JSON in isolation, and some encoding rows can exceed compact JSON's
allocated bytes. Complete save/read costs above include the smaller input/output
buffers and identify where the tradeoff actually lands. No global claim of lower
allocation for every codec operation, peak-memory reduction, fixed storage latency,
or small-save neutrality is made.

The on-disk representation changes. Existing JSON files migrate on a successful
write; previous binaries cannot read the rewritten protobuf file. The existing
rule that all writers use the same revision remains necessary. Paths retain their
configured names, including `.json` suffixes, while the content becomes binary.
Ad hoc JSON-file readers must change; native inspection APIs remain unchanged.
The framed payload length is limited to `uint32`. The journal still grows with
state and is read, cloned and replaced in full. This is not bounded-resource or
incremental persistence machinery.

## Validation and retained evidence

The final implementation passes focused tests twice normally and twice under race,
the full Go suite, broad storage/runtime/embedded/run-through/S3/public-package race
tests, all four conformance profiles, vet, site build/tests and document checks.
The first full matrix's only failing step is the required stale-comment baseline
reduction; no production source changes between that pass and the successful
quality-only recheck. Source hashes make that boundary reviewable:
[Full commands/results](2026-10-17-outbox-protobuf/raw/source-checks/checks.json), [source hashes](2026-10-17-outbox-protobuf/raw/source-checks/environment.json), [final quality check](2026-10-17-outbox-protobuf/raw/quality-checks/checks.json), [final source/quality hashes](2026-10-17-outbox-protobuf/raw/quality-checks/environment.json).

| Full matrix check | Exit | Seconds |
|---|---:|---:|
| format | 0 | 0.03 |
| focused-normal | 0 | 7.70 |
| focused-race | 0 | 11.81 |
| full-go | 0 | 59.36 |
| broad-race | 0 | 65.36 |
| conformance-memory | 0 | 4.84 |
| conformance-filesystem | 0 | 9.18 |
| conformance-runtime-memory | 0 | 7.50 |
| conformance-runtime-filesystem | 0 | 4.09 |
| vet | 0 | 8.13 |
| quality | 2 | 0.23 |
| site-build | 0 | 0.59 |
| site-tests | 0 | 0.68 |
| doc-links | 0 | 0.08 |

The successful quality recheck tightens the count and runs quality/file-size/format
together. Final report/site/document checks run on Railway against the exact source:
[final report receipt](2026-10-17-outbox-protobuf/raw/quality-checks/report-receipt.json). The retained total is 1,280 observations: 160 pilot, 640 final codec,
320 actual native saves and 160 actual lookups. Plans, logs, source snapshots,
binary/source hashes, cgroup/filesystem/load metadata, cleanup records and verified
export receipts are saved. Expected red tests and failed setup gates are retained.
No test or CLI binaries are committed. The initial upload manifest predates the
staged edits; later jobs record their final source hashes. The scratch runner is
stopped after export and the separate benchmark target remains untouched:
[verified service status](2026-10-17-outbox-protobuf/raw/runner-stop-status.json).

## Next finding, not implemented

Unchanged saves still reload, clone and replace the entire journal. Single-key
provenance reads also clone unrelated queue maps through `readState`. The lookup
and save measurements establish the remaining work. A future change must retain
fresh reads under the file lock and the completed durability fence after an
uncertain earlier rename. Neither optimization is implemented here.

Follow-up: [single-key lookup measurements](2026-10-17-outbox-lookup.md) remove the
redundant lookup clone. Full-journal reads and unchanged-save rewrites remain.
