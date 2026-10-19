# Stream capacity-scan hashing instead of materializing payloads

**Capacity scans now hash through open files using reusable scratch capped at 32 KiB, rather than first allocating each complete file.** Native binding of a 64 MiB binary object allocates about 146 KB in the initial fixed proof, compared with about 134 MB originally. Multipart raw parts show the same improvement. Unknown paths are refused before their contents are read. Existing resource IDs, full-file SHA-256 versions, physical byte charges and validation remain. No index, checkpoint, retained cache, format, dependency, public API or storage abstraction is introduced.

## Baseline and proof before implementation

The checkout starts clean at `fd92db7c3dfa470144e4e4294cb0e046b4ae5add`. Fetching main returns unchanged `219d1dcfff9807b04660a40313b32127e5be2933`. The preceding reconciliation, reader, checksum and admission fixes are complete and retained. The prior report identifies remaining whole-file capacity reads; this change does not repeat the scan-reuse repair or alter ledger persistence/fences.

Three falsifiable hypotheses guide the measurement: payload-sized allocation should grow with an existing object or part; header parsing might add another payload-sized read; streaming the same bytes should retain resource identity while reducing scratch. Source inspection finds `os.ReadFile` for every walked file before classification, followed by `sha256.Sum256(data)`. Object validation then opens the path separately to read its header.

Before writing candidate implementation code, the new native regressions run on exact original source. Ordinary binary objects are created through the unbound streaming store from a limited repeating reader, so fixture setup does not hold a payload-sized client slice. Multipart manifests are created through the native API; raw part bytes are written through the existing durable streaming writer. That fixture matches the existing on-disk part format and isolates scanning from `UploadPart`'s separate buffering behavior. It does not claim that multipart ingestion is streamed.

The measured operation is public `BindNamespace`, including its real scans and ledger/binding work. Fixtures contain one 8 MiB or 64 MiB object or raw part under a namespace with ample existing supported quota. After GC, `runtime.MemStats.TotalAlloc` covers only binding. A 4 MiB internal regression ceiling allows bookkeeping and parsed metadata but rejects payload materialization; it is not a new public total-memory guarantee. Payload inode, mtime, byte charge and independent full-file SHA-256 identity must remain correct.

Original normal allocations are **16,909,960 / 134,348,000 bytes** for 8/64 MiB objects and **16,904,728 / 134,354,960 bytes** for parts. A sparse unknown 64 MiB file is refused with `ErrUnknown`, but allocates **67,137,632 bytes** before refusal. All five allocation leaves fail normally and under race instrumentation. Independent identity controls, malformed-record refusal and native permission/symlink refusal already pass. Root failure names, actual allocation messages, no compile errors/data races, and absence of permission skips are verified. [Original normal proof](2026-10-18-capacity-scan-stream/raw/red/red-normal.log), [original race proof](2026-10-18-capacity-scan-stream/raw/red/red-race.log), [receipt](2026-10-18-capacity-scan-stream/raw/red/validation-receipt.json).

The unchanged-code baseline also measures 30 isolated observations. A 64 MiB object scan allocates **67.14 MB/op**, and binding **134.35 MB/op**. Header reads allocate **1,360 bytes/op** for both 1 MiB and 64 MiB objects. This falsifies payload materialization by the ordinary binary header reader and identifies the full-file scan buffer as the unnecessary cost. [Baseline observations](2026-10-18-capacity-scan-stream/raw/red/measurements.json), [baseline analysis](2026-10-18-capacity-scan-stream/raw/red/baseline-analysis.json).

## Implementation and storage contracts

The existing walk retains its error, symlink, directory and lock-file handling. It keeps a local scratch slice for the scan and delegates each file to a private reader. The reader opens and stats the file, then validates its class before allocating hash scratch. An unknown path returns the existing refusal without reading its potentially large contents. Invalid negative sizes are refused.

Ordinary object headers are parsed with the existing `readRecordHeader` through that same descriptor. Header parsing uses positional reads, so the later hash starts at byte zero. Bucket JSON is validated through that descriptor and rewound afterward. Save-entry and multipart-manifest validators retain their existing validation helpers. No payload-format checks are dropped; version, header length, declared payload length, malformed record, manifest, JSON and native read errors remain in the relevant paths.

The scratch starts small for small files, grows geometrically when needed and never exceeds 32 KiB. It is reused across files only within the current scan. `io.CopyBuffer` hashes through a limited reader, so the file's optional `WriterTo` path cannot ignore the supplied buffer. The limit is the captured encoded file size plus one probe byte, saturating at MaxInt64 to avoid overflow. A read error is propagated; a byte count different from the captured size returns `ErrUnknown`. Growth cannot grow the scratch or be silently treated as a valid captured prefix; truncation is refused.

The shared resource builder preserves SHA-256 of the cleaned pathname as ID, lowercase hex SHA-256 of the **entire encoded file** as version, actual encoded bytes as charge, and the existing payload/recovery class. The stream includes object magic, header length, protobuf metadata and payload; it is not a payload-only ETag. The existing byte-slice `resourceFor` interface remains for projected admissions and uses the same builder. Independent tests calculate expected IDs and hashes without that shared helper.

The store's existing managed-operation mutex/ownership qualification still applies. The scan adds no cross-operation state, partial index or cached usage. Object header validation and hashing now use one descriptor, strengthening captured-file consistency without promising safety against unsupported concurrent raw in-place host edits. Namespace admission, pending reservations, resource slots, quota accounting, holds, journal behavior and all durability fences are unchanged.

Legacy JSON object decoding and metadata parsing retain their existing allocations. Bucket JSON, multipart manifests and save-entry validation are not replaced by new formats or decoders. Large metadata can still cost memory. The 32 KiB cap describes hash scratch, not decoded metadata, legacy payloads, resource arrays, all publication paths or total process memory. The initial unknown-file read now fails earlier; native permission/open identity and symlink refusal remain covered.

## Candidate measurement before production edits

The temporary two-file candidate overlays pass all five regression groups normally and under race instrumentation. Original-source replay still produces exactly the allocation failures. Native fixed binding allocates **156,600 / 145,968 bytes** for 8/64 MiB objects and **172,920 / 173,032 bytes** for parts; the unknown-file refusal allocates **19,736 bytes**. Race-instrumented binding remains below 240 KB and the unknown refusal below 45 KB. These results verify that allocation no longer scales with the ordinary payload sizes in these fixtures.

All 12 prototype steps and 240 paired observations pass before production source is changed. An empty Go stub exists only in original-source overlays to suppress the newly added private reader file, avoiding duplicate helper definitions. It is not a compatibility format or a second implementation. Only the exact Railway-formatted/tested candidate files are selected afterward. [Candidate normal](2026-10-18-capacity-scan-stream/raw/prototype/prototype-normal.log), [candidate race](2026-10-18-capacity-scan-stream/raw/prototype/prototype-race.log), [candidate checks](2026-10-18-capacity-scan-stream/raw/prototype/checks.json).

All Go formatting, builds, tests, race/conformance, quality, vet, site/docs checks and benchmarks run only on authorized Railway scratch deployment `3a2c61df-2837-418b-bae3-4bff53fc6359`. Go 1.25.6, Linux amd64, UID 10001, GOMAXPROCS 4, 4 CPU/4 GiB. Benchmark payloads and ledgers reside on verified ext4 `/results` (statfs 61267). The target service is not started or changed.

Each cell runs in a separate benchmark child. Initial creation, binding where applicable, independent reference hashing, empty-bucket preparation, final identity/charge checks and cleanup are outside Go timing/allocation counters. Cells cover scan, public binding and native empty-bucket deletion over a 1 MiB or 64 MiB object or part, plus header-only and empty-store controls. The deletion is a supported small mutation whose admission still verifies existing data. It does not change the large payload. A later scan confirms its exact charge/version and the deleted bucket's absence.

Each comparison uses ABBA/BAAB order with four original/fixed observations per cell and four observations per identical-original-binary control label C/D. Peak RSS is Linux child maximum usage, including startup, calibration, fixtures and final checks, not isolated per-operation RSS or cgroup/page-cache memory. Setup streams payloads and reference hashing to avoid masking the improvement with a large client-side slice. Recently written files do not establish cold-cache storage throughput. Positive time changes mean slower.

Prototype measurements use three timed operations per process:

| Cell | Time, original → fixed | Time change | Bytes/op, original → fixed | Objects/op, original → fixed | Child peak RSS, original → fixed | Same-binary time change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| empty | 0.0558 → 0.0627 ms | +12.20% | 9,610.0 → 8,842.0 | 111.0 → 117.0 | 10.15 → 10.13 MiB | +0.80% |
| header=1048576 | 0.0166 → 0.0117 ms | -29.44% | 1,360.0 → 1,360.0 | 18.0 → 18.0 | 11.02 → 10.10 MiB | +3.18% |
| header=67108864 | 0.0214 → 0.0175 ms | -18.24% | 1,360.0 → 1,360.0 | 18.0 → 18.0 | 10.14 → 10.12 MiB | -2.73% |
| kind=object/size=1048576/stage=bind | 22.1711 → 17.8672 ms | -19.41% | 2,212,839.5 → 157,410.5 | 812.5 → 817.0 | 14.33 → 12.07 MiB | +10.15% |
| kind=object/size=1048576/stage=delete | 14.2458 → 11.5969 ms | -18.59% | 1,130,682.0 → 98,445.0 | 564.0 → 564.0 | 14.24 → 12.56 MiB | -5.13% |
| kind=object/size=1048576/stage=scan | 0.8025 → 0.6047 ms | -24.65% | 1,072,701.0 → 44,733.0 | 148.0 → 150.0 | 15.50 → 12.39 MiB | -8.39% |
| kind=object/size=67108864/stage=bind | 108.8476 → 101.0618 ms | -7.15% | 134,334,304.0 → 158,634.0 | 820.5 → 818.0 | 170.43 → 12.31 MiB | -0.24% |
| kind=object/size=67108864/stage=delete | 58.6151 → 53.8154 ms | -8.19% | 67,195,098.5 → 101,879.5 | 574.5 → 565.0 | 138.40 → 12.56 MiB | -0.23% |
| kind=object/size=67108864/stage=scan | 39.8233 → 37.1346 ms | -6.75% | 67,133,714.5 → 44,733.0 | 150.0 → 150.0 | 137.86 → 12.04 MiB | -0.11% |
| kind=part/size=1048576/stage=bind | 21.7374 → 20.5004 ms | -5.69% | 2,222,862.5 → 168,789.0 | 861.5 → 873.0 | 13.79 → 10.14 MiB | -19.33% |
| kind=part/size=1048576/stage=delete | 13.7826 → 12.5944 ms | -8.62% | 1,136,466.5 → 105,509.0 | 606.0 → 610.0 | 15.13 → 11.58 MiB | +3.35% |
| kind=part/size=1048576/stage=scan | 0.8454 → 0.6263 ms | -25.92% | 1,072,287.5 → 46,960.0 | 165.0 → 173.0 | 14.99 → 10.56 MiB | -0.84% |
| kind=part/size=67108864/stage=bind | 104.7846 → 94.1994 ms | -10.10% | 134,354,337.0 → 171,113.0 | 881.0 → 875.5 | 137.55 → 11.46 MiB | -7.11% |
| kind=part/size=67108864/stage=delete | 54.1592 → 49.0070 ms | -9.51% | 67,209,762.5 → 106,272.0 | 619.0 → 611.0 | 137.75 → 11.44 MiB | +1.49% |
| kind=part/size=67108864/stage=scan | 39.3460 → 36.4463 ms | -7.37% | 67,138,818.5 → 46,944.0 | 170.5 → 173.0 | 138.26 → 10.38 MiB | +1.76% |

The 64 MiB object scan reduces allocated bytes **67.13 MB → 44.7 KB**, with child peak RSS **137.86 → 12.04 MiB**. Binding peak RSS is **170.43 → 12.31 MiB**. Large object/part operation medians improve roughly 7–10% in this run. The empty-store scan is adverse at **+12.20%**, about **55.8 → 62.7 microseconds**, with a **+0.80%** same-binary control change. Its allocated object count rises from 111 to 117 despite fewer bytes. These costs are retained, not dismissed or hidden. Header-only control timings also move despite unchanged reader code, limiting attribution for small timing differences.

## Final comparison and new costs

The final run uses actual selected production files and exact original-source overlays, with five timed operations per process. It repeats 240 observations after all final validation gates: **480 paired observations plus 30 original baseline observations**. Binding's first versus subsequent calls have different setup work, so absolute 3x/5x results are not treated as identical workloads; original/fixed counts match within each comparison.

| Cell | Time, original → fixed | Time change | Bytes/op, original → fixed | Objects/op, original → fixed | Child peak RSS, original → fixed | Same-binary time change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| empty | 0.0505 → 0.0481 ms | -4.82% | 9,609.0 → 8,841.0 | 111.0 → 117.0 | 10.12 → 10.14 MiB | +4.32% |
| header=1048576 | 0.0126 → 0.0112 ms | -11.12% | 1,360.0 → 1,360.0 | 18.0 → 18.0 | 10.12 → 10.16 MiB | +9.48% |
| header=67108864 | 0.0163 → 0.0130 ms | -20.47% | 1,360.0 → 1,360.0 | 18.0 → 18.0 | 11.13 → 10.14 MiB | -14.44% |
| kind=object/size=1048576/stage=bind | 18.6167 → 16.1655 ms | -13.17% | 2,216,571.0 → 159,470.0 | 821.5 → 821.0 | 14.22 → 12.49 MiB | -17.91% |
| kind=object/size=1048576/stage=delete | 11.9689 → 11.7330 ms | -1.97% | 1,129,715.5 → 98,439.5 | 564.0 → 564.0 | 15.57 → 12.41 MiB | +16.84% |
| kind=object/size=1048576/stage=scan | 0.9105 → 0.6271 ms | -31.13% | 1,071,460.5 → 44,699.0 | 146.0 → 150.0 | 15.34 → 12.40 MiB | -1.94% |
| kind=object/size=67108864/stage=bind | 107.9052 → 97.2515 ms | -9.87% | 134,348,654.5 → 162,067.0 | 840.5 → 823.5 | 139.38 → 12.18 MiB | +0.65% |
| kind=object/size=67108864/stage=delete | 57.0610 → 51.8013 ms | -9.22% | 67,204,859.0 → 100,128.5 | 579.0 → 565.5 | 140.99 → 12.07 MiB | +3.14% |
| kind=object/size=67108864/stage=scan | 40.8653 → 37.8955 ms | -7.27% | 67,133,422.5 → 45,549.0 | 150.0 → 150.0 | 138.47 → 12.19 MiB | -0.84% |
| kind=part/size=1048576/stage=bind | 18.1699 → 17.7301 ms | -2.42% | 2,226,656.5 → 170,944.0 | 869.5 → 878.0 | 14.28 → 11.44 MiB | +9.71% |
| kind=part/size=1048576/stage=delete | 12.5282 → 11.6605 ms | -6.93% | 1,135,255.0 → 105,592.0 | 604.5 → 610.0 | 16.24 → 11.55 MiB | +2.74% |
| kind=part/size=1048576/stage=scan | 0.8647 → 0.6511 ms | -24.70% | 1,074,017.0 → 46,944.0 | 166.0 → 173.0 | 14.91 → 10.13 MiB | -5.62% |
| kind=part/size=67108864/stage=bind | 102.9871 → 91.4592 ms | -11.19% | 134,359,350.5 → 173,131.5 | 889.5 → 881.0 | 137.74 → 11.77 MiB | -1.43% |
| kind=part/size=67108864/stage=delete | 55.5232 → 51.0654 ms | -8.03% | 67,202,307.5 → 107,277.0 | 613.5 → 611.5 | 157.60 → 11.70 MiB | -2.16% |
| kind=part/size=67108864/stage=scan | 38.7781 → 38.0749 ms | -1.81% | 67,134,093.5 → 47,781.5 | 168.0 → 173.0 | 137.25 → 10.86 MiB | -1.04% |

The final 64 MiB object scan allocates **67,133,422.5 → 45,549 bytes/op**, with child peak RSS **138.47 → 12.19 MiB**. Object binding improves **107.91 → 97.25 ms** and deletion **57.06 → 51.80 ms**. The final empty scan is faster by **4.82%**, against a **+4.32%** same-binary control change, but still allocates **117 versus 111 objects/op**. The earlier **+12.20%** empty-scan result remains adverse evidence; the final timing does not establish that its cost has disappeared. No lower allocation-count claim is made for all small files.

[Final observations](2026-10-18-capacity-scan-stream/raw/final/measurements.json), [final ranges](2026-10-18-capacity-scan-stream/raw/final/comparison-analysis.json), [prototype observations](2026-10-18-capacity-scan-stream/raw/prototype/measurements.json), [prototype ranges](2026-10-18-capacity-scan-stream/raw/prototype/comparison-analysis.json). Every observation, binary hash, command and range is retained. The regression counters establish payload-independent allocation in the tested binary/part fixtures; child RSS establishes the measured process footprint for this harness. No universal latency, statistical-significance or global constant-memory claim is made.

Streaming adds bounded-buffer and incremental hash bookkeeping and more small reads in place of one large materialization. Metadata-only files now pass through streaming hash state; bucket JSON also rewinds its descriptor after validation. Those costs can increase small-scan time and allocation count. Resource collection remains linear in file count; all data is still read and hashed, so hashing/I/O time remains linear in payload bytes. No pool or retained buffer hides memory between operations. The implementation primarily removes unnecessary full-file input allocation and its GC pressure. Existing buffered incoming capacity-bound/guarded publication and multipart ingestion are separate remaining costs.

## Validation and provenance

All **23 final steps** pass: formatting; quality/file-size/format ratchets; exact-original normal/race red replays; filesystem/capacity normal and race suites twice; full Go tests; broad capacity/storage/runtime/embedded/runthrough/S3/public-package race tests; four conformance profiles; vet; site build/tests; documentation links; temporary benchmark installation/formatting and exact source snapshot; both binary builds; final comparison; cleanup. [Final checks](2026-10-18-capacity-scan-stream/raw/final/checks.json), [receipt](2026-10-18-capacity-scan-stream/raw/final/validation-receipt.json), [normal suites](2026-10-18-capacity-scan-stream/raw/final/focused-normal.log), [race suites](2026-10-18-capacity-scan-stream/raw/final/focused-race.log). Native permission cases execute without a root skip. No quality baseline is raised. The temporary Go benchmark is removed. The shared record reader remains unchanged. Additional Railway report/site/docs and exact-source/evidence checks run before scratch teardown; committed Go bytes must match those actually tested.

The first upload fails before an associated build, deployment `a49b8ef7-a60a-48c2-a950-1dbdeeef2b03`. Its error, terminal state and no-build evidence remain saved. Only after that confirmation is source retried with stronger gzip compression: **65,398,107 → 64,469,839 bytes**. Streaming hashes prove byte-identical uncompressed tar contents. Both transport receipts, the source inventory, actual transport Dockerfile and original validation recipe remain. No local project execution substitutes for Railway validation.

The [artifact manifest](2026-10-18-capacity-scan-stream/raw/artifact-manifest.json) records original/candidate/final sources, tests, every measurement, failure provenance and scratch-runner teardown. No stored data is rewritten by scanning and no migration or caller action is required.

## Next finding

Capacity-bound/guarded incoming publication and multipart ingestion still materialize their input; legacy and metadata decoding can also retain size-dependent memory. The buffered admission paths need separate native allocation/RSS measurements and a correctness proof for staging, reservations, failure/recovery and receipt semantics before changing publication. Do not infer that this scan fix solves those paths. No next optimization is implemented here, and no indexing or checkpoint machinery is introduced. The product-quality goal remains active.
