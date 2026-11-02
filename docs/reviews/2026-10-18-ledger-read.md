# Bound capacity ledger input before allocation

**The 8 MiB ledger limit was checked after reading the entire file.** Native startup therefore allocated in proportion to an oversized rejected ledger. Check the size of the opened descriptor before allocation, read into one fixed buffer with an EOF probe, and retain the existing format, integrity validation and error behavior. No dependency, public API, format, cache, index, checkpointing or storage abstraction is added.

## Baseline and proof before implementation

The checkout begins clean at `b847164373cbf982d891af82929e73791e851d8f`; fetching main returns unchanged `219d1dcfff9807b04660a40313b32127e5be2933`. The resource-slot, bucket, multipart, uncertain-publication and scan-reuse changes are complete and retained. `Host.read` called `os.ReadFile` before comparing `len(data)` with the existing 8 MiB `maxStateBytes` limit. The related outbox reader is private and specific to its own format, so no shared abstraction or new exported interface is introduced.

The hypotheses were that rejection materialized the whole file, that an existing read behavior bounded it, or that a replacement could change valid-input/error contracts. Tests ran on Railway before implementation edits. They create real sparse 16 MiB + 1 and 64 MiB + 1 files without allocating their contents in fixture setup. After GC, runtime allocation counters cover a native public `Open`, including its existing host gate and ledger reader. Both return `ErrUnknown`, but the original normal run allocates **16,786,648** and **67,118,328 bytes**, respectively; the race run reproduces the same scaling. This demonstrates the guard's placement defect without deliberately exhausting runner memory.

The regression budget is 9 MiB allocated bytes per oversized attempt: the existing 8 MiB encoded-input bound plus bookkeeping headroom. It is an internal regression budget, not a new public peak-memory guarantee. Refusal must leave the ledger inode, size and modification time unchanged. [Original normal proof](2026-10-18-ledger-read/raw/red/red-normal.log), [original race proof](2026-10-18-ledger-read/raw/red/red-race.log).

Controls already pass on the parent: a canonical integrity-protected file at exactly 8 MiB remains readable; one byte beyond the limit is refused without rewriting it; missing state initializes while empty state remains unknown; directory and permission failures preserve native operation/path/error identity. The exact-size codec fixture pads the existing root-string schema field and does not claim filesystem support for such a long path. Existing tamper/reopen tests remain part of the full suite.

The initial source upload created terminal failed deployment `3467f33d-bc38-472e-8eb0-96c97ca98d18` with no associated build. Only after confirming that state was unchanged source uploaded again. The [upload error](2026-10-18-ledger-read/raw/failed-attempt-upload.log), [terminal deployment state](2026-10-18-ledger-read/raw/failed-attempt-deployments.json), and [no-build evidence](2026-10-18-ledger-read/raw/failed-attempt-build.log) are retained. No local Go execution substituted for the failed upload.

## Change and contracts

`readBoundedStateFile` opens the existing path and stats that descriptor, avoiding a separate path-stat/open race. Negative sizes or sizes greater than 8 MiB return the existing `ErrUnknown` before a content buffer is allocated. Otherwise it allocates the captured size plus one probe byte and uses `io.ReadFull`. Native read errors other than the expected EOF conditions are returned unchanged. A stat failure now refuses rather than attempting an unbounded read with unknown size. A byte count different from the captured size is refused; a stable file returns only its actual bytes, excluding the probe.

Input allocation is therefore bounded by the captured allowed size plus one byte. Growth after stat cannot grow the buffer or make a valid-looking truncated prefix acceptable. Size drift is rejected instead of followed to EOF. The existing host/capture gate continues coordinating managed atomic writers; this adds no guarantee for arbitrary in-place host edits. Decoding, canonical re-encoding, integrity, option-binding and structural checks remain in `Host.read`. Missing-file handling and writer durability are unchanged. Close errors remain ignored as with the former read helper.

The 8 MiB limit bounds encoded input, not total decoded memory or process RSS. Valid JSON decoding, canonical integrity work and existing namespace accounting still allocate. The fix targets unbounded input materialization on rejection and introduces no global GC policy, allocation pool or retained state. Already oversized files remain refused with `ErrUnknown`; no migration, truncation or recovery discard is added. A concurrent unsupported raw size change can now be refused sooner.

The first implementation passes all runtime tests but fails the existing comment-count ratchet because of two added production comment lines. Those comments are removed and the private helper is named descriptively; no baseline is raised. An independent comparison verifies that initial/final production code differs only by that documented comment/name adjustment. All initial results remain, including its rejected quality status.

## Railway measurements

All formatting, builds, tests, race/conformance, vet, quality, site/documentation checks and benchmarks run exclusively on the authorized scratch runner, deployment `9ab6875d-76da-40be-ae20-d7c14e5887a0`. Go 1.25.6, Linux amd64, UID 10001, GOMAXPROCS 4, 4 CPU/4 GiB. Timed ledgers use verified ext4 `/results` (statfs 61267); the target service remains unchanged.

Valid cells read real namespace snapshots with 16 or 4,096 declared resources. Oversized cells call native `Open` on sparse files and verify `ErrUnknown`. Setup, binding, file construction and disk checks are excluded from timing/allocation counters. Each cell runs in a separate benchmark child process with ten timed operations. Python's Linux child resource usage records that process's maximum RSS, including startup, calibration and fixture construction; RSS is not an isolated per-operation measurement or a cgroup/page-cache total.

Four original/fixed observations per cell use ABBA/BAAB order, plus four observations per identical-original-binary control label C/D: **64 final observations, 128 including the initial implementation**. All commands, binary identities, samples, calibration counts and ranges are retained. Positive time percentages mean slower.

| Entry point | Median time, original → fixed | Time change | Bytes/op, original → fixed | Objects/op, original → fixed | Child peak RSS, original → fixed | Same-binary control time change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| oversized=16777217 | 3.0987 → 0.0087 ms | -99.72% | 16,787,518.0 → 1,592.0 | 16.0 → 15.0 | 40.67 → 10.15 MiB | +12.82% |
| oversized=67108865 | 11.6583 → 0.0085 ms | -99.93% | 67,119,192.0 → 1,592.0 | 16.0 → 15.0 | 136.44 → 10.13 MiB | +1.56% |
| valid=16 | 0.0385 → 0.0398 ms | +3.37% | 15,232.0 → 15,232.0 | 136.0 → 136.0 | 10.13 → 10.14 MiB | +7.21% |
| valid=4096 | 5.0726 → 5.0450 ms | -0.54% | 3,704,790.0 → 3,914,049.5 | 16,549.5 → 16,554.0 | 12.59 → 12.78 MiB | -0.30% |

[Final measurements](2026-10-18-ledger-read/raw/final/measurements.json), [ranges and analysis](2026-10-18-ledger-read/raw/final/comparison-analysis.json), [initial measurements](2026-10-18-ledger-read/raw/initial/measurements.json), [benchmark fixture](2026-10-18-ledger-read/raw/ledger-bench.go.txt), [RSS wrapper](2026-10-18-ledger-read/raw/measure-child.py).

The initial 64 MiB rejection measures roughly **67.12 MB → 1.6 KB** allocated per operation, with child peak RSS **141.65 MB → 10.58 MB**. Small valid snapshot timing is adverse in that run: **+10.01%**, against a **-9.63%** same-binary control change; this is retained and not dismissed as noise. The quality correction motivates the final rerun rather than selecting a favorable timing sample. The final 4,096-resource valid case also allocates 3.7048 → 3.9140 MB/op (+5.65%); its timing is nearly unchanged. That allocation difference is retained as an unresolved cost rather than attributed to a particular mechanism without profiling. No statistical-significance or general S3 throughput claim is made. Valid-path costs and all adverse observations remain visible; the reader still opens, stats, reads and closes ordinary files.

## Validation and next work

All 22 final steps pass: formatting; quality/file-size/format ratchets; exact-parent normal/race red proofs; focused filesystem/capacity normal/race suites twice; full Go tests; broad capacity/storage/runtime/embedded/runthrough/S3/public-package race tests; four conformance profiles; vet; site build/tests; documentation links; benchmark installation/formatting, before/after builds, isolated measurement and cleanup. The initial expected-failure job passes its three harness checks. The first implementation's 22-step run has only the recorded quality failure. [Final checks](2026-10-18-ledger-read/raw/final/checks.json), [validation receipt](2026-10-18-ledger-read/raw/final/validation-receipt.json), [parent replay](2026-10-18-ledger-read/raw/final/verified-red-normal.log), [fixed normal](2026-10-18-ledger-read/raw/final/focused-normal.log), [fixed race](2026-10-18-ledger-read/raw/final/focused-race.log).

Four new top-level tests cover six leaf cases. Exact-limit acceptance, refusal without rewriting, missing/empty distinctions, native directory/permission errors, integrity tampering, pending-slot recovery, full-limit filesystem reopen and prior allocation/uncertain-publication cases remain green. Permission cases execute without a root skip. The temporary Go benchmark is removed. Report/evidence receive additional Railway documentation and source-hash checks before teardown; committed source must match the exact formatted/tested bytes.

The accepted-ledger allocation/timing deltas need isolation. The next measured work also includes ledger persistence/ancestor-fence amplification and the unresolved earlier fresh-store unbound timing difference. This change does not alter those paths or implement the next optimization. No further production change is included.

The [artifact manifest](2026-10-18-ledger-read/raw/artifact-manifest.json) records original source, both implementation runs, every observation, upload-failure provenance, validation and scratch teardown evidence.
