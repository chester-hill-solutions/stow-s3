# Validate outbox framing before whole-file allocation

Baseline `7a248a0d425defb9f679f968f3cf8c04018090c7` already bounds the temporary protobuf record graph during duplicate rejection. Its file loader still called `os.ReadFile` before validating the native frame length. An invalid 8 MiB snapshot allocated about 8.4 MB before returning the expected length error, even when the header declared just two payload bytes.

This change checks the fixed header and size of the same opened file before allocating its contents. The v4 encoding, decoder, normalization, state/cache ownership, locks and durability protocol stay unchanged. No dependencies, formats or exported APIs are added.

## Reproduction and recovery

Two sparse 8 MiB files start with `STOWOUT4`, declaring payload lengths 2 and 4,294,967,295 respectively. Both disagree with the actual file size. The regression measures `PendingSnapshot` allocation after fixture creation and GC, checks the length error, unchanged last-known state/cached bytes and unchanged corrupt file size, then restores the original valid snapshot and recovers its intent.

Each large-file case failed the 1 MiB allocation budget twice normally and twice under race detection before modifying implementation. Truncated-header and malformed-wire controls passed. These are allocated-byte measurements from `runtime.MemStats.TotalAlloc`, not RSS or maximum live heap.

| Invalid 8 MiB frame | Original allocated bytes | Adopted candidate allocated bytes |
| --- | ---: | ---: |
| short-declaration | 8,398,224–8,403,712 | 1,184–1,440 |
| long-declaration | 8,398,208–8,398,336 | 1,184–1,424 |

[Original reproduction](2026-10-18-outbox-frame-read/raw/before/red-receipt.json) and [candidate proof](2026-10-18-outbox-frame-read/raw/candidate/candidate-receipt.json) retain all samples. This is a corrupt-journal resource defect, without a claim of a deployed incident or remote exploit.

## Reader and contracts

`reloadLocked` changes one call from `os.ReadFile` to a private reader with the same result interface. It opens and stats the file once, reads the fixed header through `File.ReadAt`, and refuses a native length mismatch before allocating a payload buffer. The remainder uses the same opened descriptor, retaining a captured version across atomic path replacement.

The reader reserves one byte beyond the captured file size to detect native growth and rejects that growth before reading an arbitrarily large tail. EOF yields the bytes actually read, so the existing decoder still rejects truncation. Legacy JSON retains whole-file reading and handles growth through the same descriptor. A platform-size guard prevents overflow before allocation. This applies to the existing seekable-file journal profile.

Forty new cases run twice normally and twice with race detection: four resource/error controls, 33 native/legacy byte/state/error-equivalence cases, a growth allocation guard, opened-version replacement, and legacy growth. The tests compare successful bytes/state and error classification with the existing decoder. Failed reloads preserve state and cached bytes, and valid bytes can be restored. Existing duplicate-flood, ownership, fencing, disappearance, concurrency, publication and recovery tests remain in the full focused suite.

Valid-journal memory still scales with file size and decoded state. This adds no absolute journal quota. Legacy JSON keeps its existing proportional read/parse costs. Direct in-place edits are not made into atomic snapshots; managed writers continue coordinating through the existing lock and atomic publication.

## Fresh profiles and scope decision

The baseline was refreshed from current source before this change. Latest fetched `origin/main` remained `219d1dcfff9807b04660a40313b32127e5be2933`. The preceding goal turn made concrete progress in commit `7a248a0`; this iteration starts from that committed decoder rather than repeating earlier work.

All Go formatting, builds, tests, profiles, benchmarks, quality gates, conformance and documentation checks ran on the existing Railway scratch runner. Local operations were source editing, Git, packaging, hashing and evidence analysis. Deployment `b394995c-6112-4041-a3f8-d2d47444aec4`; Linux amd64, Go 1.25.6, UID 10001, 4-CPU/4-GiB cgroup. Performance fixtures use the mounted ext4 `/results` volume, verified by statfs 61267.

The fresh baseline has 42 uninstrumented observations: full decode, isolated domain conversion, and durable enqueue/remove cycles at CPU 1/4. Decode fixtures contain 4,096 active and 4,096 prepared entries, minimal or all-fields/provenance shapes. Separately, four CPU/allocation profile groups cover minimal decode, rich decode, rich domain conversion and populated durable enqueue. Instrumented timings are excluded from latency comparisons. [Profile summary](2026-10-18-outbox-frame-read/raw/before/profile-analysis.json), [commands/binary identity](2026-10-18-outbox-frame-read/raw/before/profile-runs.json).

Minimal decoding allocates 4,664,150 bytes/op and 61,539 objects; isolated domain conversion with already-owned protobuf strings allocates 3,757,200 bytes/op and 8,290 objects. Rich decoding allocates 7,116,376 bytes/op versus 4,538,440 for isolated domain conversion. The isolated component is diagnostic: its setup/string ownership differs, so its timings cannot simply be subtracted from full decoding.

In the minimal allocation-object profile, string decoding is 68.41% of the whole profile and the focused decoder is 81.82%. In allocation-space, entry insertion is 51.49% of the whole profile, string decoding 12.43%, and the focused decoder 63.93%. Setup contributes outside that focus. Allocation count and byte cost have different leaders; cumulative percentages are not additive. The durable-write CPU profile also includes cloning, serialization, sorting and GC; it does not isolate filesystem wait time. These observations do not justify a new format, filesystem index or storage abstraction.

## Comparative measurements and costs

The first valid reader used a consumed prefix followed by `io.ReadFull`. It fixed the corrupt-frame allocation but added one allocation per valid read, with cached native reload medians +8.02–9.70%. Its empty CPU-4 enqueue cycle was +7.10%; other write deltas ranged −15.87% to +4.01%. All 192 paired/control observations are retained under [partial reader analysis](2026-10-18-outbox-frame-read/raw/partial/comparison-analysis.json).

The adopted comparison uses `File.ReadAt`, matching the existing object-record I/O style, and reads the captured file without assembling a consumed prefix. It keeps the same framing/recovery guards. An earlier prototype build lacked an explicit integer conversion; that failed job is retained under `raw/initial` and excluded from timing claims. Its incomplete comparison is not treated as a successful measurement.

Each reader comparison uses 1-second minimum benchmark runs, ABBA/BAAB ordering, four samples per label/cell, 96 paired observations and 96 identical-candidate-binary control observations. Both variants use identical probe bodies and fixture setup. Binary identities are recorded per command. Cached reload isolates the private read/cache comparison without locking or snapshot copying; durable write cycles include locks, cloning, decode, encode, fsync and removal, then verify reopened state. Depth counts entries per queue. Process HWM/live-heap diagnostics include setup and are not isolated per-operation peaks.

The final paired medians below retain every favorable and adverse cell. Positive time deltas mean slower. `-4` denotes GOMAXPROCS 4; an unsuffixed write row uses 1.

| Workload | Time µs/op, before → after | Time delta | Bytes/op, before → after | Objects/op, before → after |
| --- | ---: | ---: | ---: | ---: |
| write depth=0/enqueue | 7,337.29 → 7,787.66 | +6.14% | 9,916 → 9,021 | 84.0 → 84.0 |
| write depth=0/enqueue-4 | 7,581.01 → 7,660.79 | +1.05% | 9,900 → 9,021 | 84.0 → 84.0 |
| write depth=128/enqueue | 8,303.51 → 8,605.38 | +3.64% | 831,704 → 831,764 | 5,511.5 → 5,512.5 |
| write depth=128/enqueue-4 | 8,482.98 → 8,204.05 | -3.29% | 831,762 → 831,698 | 5,512.5 → 5,511.0 |
| write depth=4096/enqueue | 51,297.55 → 48,282.88 | -5.88% | 26,026,136 → 26,041,428 | 170,121.0 → 170,326.0 |
| write depth=4096/enqueue-4 | 47,912.59 → 46,503.60 | -2.94% | 26,041,564 → 26,040,783 | 170,326.5 → 170,317.5 |
| cached reload format=legacy/depth=0-4 | 3.94 → 3.74 | -5.02% | 952 → 488 | 5.0 → 5.0 |
| cached reload format=legacy/depth=128-4 | 20.06 → 20.92 | +4.30% | 82,360 → 82,360 | 5.0 → 5.0 |
| cached reload format=legacy/depth=4096-4 | 533.43 → 556.35 | +4.30% | 2,482,632 → 2,482,632 | 5.0 → 5.0 |
| cached reload format=native/depth=0-4 | 3.54 → 3.65 | +2.99% | 952 → 456 | 5.0 → 5.0 |
| cached reload format=native/depth=128-4 | 8.46 → 8.25 | -2.39% | 29,112 → 29,112 | 5.0 → 5.0 |
| cached reload format=native/depth=4096-4 | 222.08 → 204.78 | -7.79% | 909,760 → 909,768 | 5.0 → 5.0 |

The adopted reader retains five allocations per cached reload. Empty native reload bytes fall from 952 to 456 (−52.10%); populated reload bytes are unchanged. Durable empty cycles use about 9% fewer allocated bytes. Native cached-reload median times range −7.79% to +2.99%, legacy −5.02% to +4.30%; durable cycles range −5.88% to +6.14%. Same-binary controls range −7.30% to +10.98%. The +6.14% write result remains an adverse observation rather than being dismissed by the control range.

| Identical candidate binary, two labels | Label median time delta |
| --- | ---: |
| write depth=0/enqueue | -7.30% |
| write depth=0/enqueue-4 | -2.18% |
| write depth=128/enqueue | +10.98% |
| write depth=128/enqueue-4 | -5.66% |
| write depth=4096/enqueue | +1.81% |
| write depth=4096/enqueue-4 | -0.71% |
| cached reload format=legacy/depth=0-4 | -0.53% |
| cached reload format=legacy/depth=128-4 | -1.90% |
| cached reload format=legacy/depth=4096-4 | -0.69% |
| cached reload format=native/depth=0-4 | +0.41% |
| cached reload format=native/depth=128-4 | +3.24% |
| cached reload format=native/depth=4096-4 | -7.00% |

These control deltas describe observed variation, not confidence intervals and not proof that a particular slowdown has no code contribution. The extra header probe has a real I/O cost; absolute times and adverse cells remain visible. This is a resource-correctness change with measured costs, without a general speedup claim. [All final observations](2026-10-18-outbox-frame-read/raw/candidate/comparison-analysis.json), [commands and hashes](2026-10-18-outbox-frame-read/raw/candidate/comparison-runs.json).

## Final verification and remaining work

Adopted production bytes match the measured candidate. The rest of `file_outbox.go` is byte-identical apart from the single read call. The protobuf encoder/decoder are unchanged. Final validation includes focused tests twice normally/race, full Go tests, broader storage/runtime/embedded/runthrough/S3/public-package race coverage, all four conformance configurations, vet, quality/size/format gates, site build/tests and documentation links. Report/evidence documentation checks run again on Railway after publication. [Final checks](2026-10-18-outbox-frame-read/raw/final/checks.json), [final regression proof](2026-10-18-outbox-frame-read/raw/final/green-receipt.json).

New costs and limits: a small private file reader, a fixed header probe per reload, and the valid-workload timing/allocation changes above. Seekable regular journals are the qualified profile. This does not replace full-file validation with metadata-only cache acceptance, change durability fences, add retained buffers, or impose a new payload quota.

The next measured performance target is domain-map and stored-entry allocation, which retains most minimal decode bytes even with already-owned strings. Any representation or checkpointing change needs its own comparative measurements and recovery evidence. No such change is included here.

The [evidence manifest](2026-10-18-outbox-frame-read/raw/artifact-manifest.json) covers profiles, all comparisons including the unused reader, source snapshots, regression/validation receipts and isolated Railway runner teardown.
