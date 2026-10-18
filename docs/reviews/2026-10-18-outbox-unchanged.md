# Fence unchanged provenance without replacing the journal

Date: 2026-10-18 (Toronto; receipts use UTC).
Baseline: `a4eba1f4785323ece11ad50954c577fc31b8df83` on `codex/core-copy-quality`.
Freshly fetched `origin/main`: `219d1dcfff9807b04660a40313b32127e5be2933`. Worktree initially clean.
The preceding [null-root correction](2026-10-18-outbox-null.md) is included in both variants.

## Measurements before implementation

Every provenance save reloaded, cloned, encoded and replaced the entire journal,
even when the requested row and all its flags were already present unchanged.
Earlier [protobuf measurements](2026-10-17-outbox-protobuf.md#actual-native-saves)
established the full-rewrite cost. Before changing production code, an isolated
three-file prototype was compared with the current implementation on Railway.
All 40 pilot observations are retained. At 16,384 extra provenance rows, CPU 1,
unchanged saves measured 19.213 → 3.355 ms and written payload bytes fell to zero.
The smallest fixture measured 4.322 → 0.172 ms. Changed results were mixed,
including +3.08% latency for the largest provenance fixture. The pilot established
material value, with a longer paired comparison reserved for the final source.
See [pilot observations](2026-10-18-outbox-unchanged/raw/pilot/samples.json).

Before wiring the implementation, the final observable regression failed for six
unchanged values twice normally and twice under race detection: every save
replaced the journal inode. Known-empty creation, complete flag comparison, peer
freshness, equal-value JSON migration, permission repair and symlink replacement
controls all passed. No compile failure or race report is used as evidence.
The test/quality gates passed against this original production implementation.
See [normal contracts](2026-10-18-outbox-unchanged/raw/contracts-before/contracts-normal.log)
and [race contracts](2026-10-18-outbox-unchanged/raw/contracts-before/contracts-race.log).

## Implementation and durability

An unchanged save still acquires the process mutex and filesystem lock, reloads
and validates the current journal, reconstructs counters and compares the entire
`UpstreamState` plus row presence. It can skip cloning, encoding and replacement
only for an existing native protobuf journal.

The shared internal atomic-file helper opens a compatible existing regular file
read/write, verifies the opened inode, syncs its data, checks close, then completes
the existing platform-specific parent-directory fence. A visible file is not
assumed durable. This also fences a valid snapshot restored without a prior file
sync. If this operation fails, the caller attempts the existing full replacement;
success is returned only after that path completes its full fence. Errors cannot
be converted into an unfenced acknowledgement.

Legacy JSON still migrates on an equal-value save. Incompatible files, including
symlinks and unexpected POSIX permissions, use the full replacement path, preserving
`0600` and avoiding writes through the symlink. Changed rows use the existing
clone/encode/write transaction. Common locking/reload and committed-state assignment
are shared rather than duplicated. Existing directory-fence API tests require two
consecutive failures after a replacement becomes visible, stream closure, no cache
publication and successful repair; they pass with the new path.

There is no new dependency, public client API, journal format, index, checkpoint
or storage abstraction. `SyncExisting` is an internal-package helper sharing the
existing directory policy. Windows retains the existing directory-sync policy;
performance qualification here is Linux only. A private format marker preserves
legacy migration and is updated on successful reads and writes.

## Final Railway comparison

There are **320 observations**: ten fixture/mode combinations × two variants ×
two CPU settings × eight observations per cell. Each CPU uses ABBA and BAAB blocks
with two repetitions and a one-second minimum per observation. The actual public
`SaveUpstreamState` benchmark is unchanged. Both variants include identical final
regression/benchmark fixtures. Original binaries overlay exactly the three
production files from the baseline; final binaries use the validated source.
Hashes are in [build metadata](2026-10-18-outbox-unchanged/raw/comparison/build.json).

The pinned Go 1.25.6 runner has four CPUs and 4 GiB memory. CPU labels are
`GOMAXPROCS`, not client concurrency or affinity. Temporary journals use the ext4
results volume. Rows count extra provenance entries plus one target; mixed has
1,024 extra provenance entries and 1,024 pending operations. Timestamps and ETag
sizes are fixed. The changed mode alternates equal-length ETags.

All rows show before → after medians. B/op is cumulative allocated bytes, not RSS
or retained heap. Written-B/op is Linux process `wchar`, not physical disk traffic;
fsync can write kernel-managed data without appearing in this metric. Nine unchanged observations contain 8 or 16 process-level bytes in total, all
below one complete journal payload; their medians are zero. The pinned Go runtime
has 8-byte poller wakeup writes, but no descriptor-level attribution was performed.
These counter observations are retained in [counter notes](2026-10-18-outbox-unchanged/raw/comparison/counter-notes.json).
The healthy unchanged-save branch issues no payload write calls. Journal sizes and final
row values are verified. Raw observations and ranges are retained; medians are
not statistical-significance or cold-cache claims.

| CPU | Fixture/mode | ms/op | B/op | Allocations/op | Written-B/op |
| --- | --- | --- | --- | --- | --- |
| 1 | mixed=1024/changed | 8.022 → 8.151 (+1.61%) | 2,603,719 → 2,603,671 | 16,472 → 16,471 | 157,598 → 157,598 |
| 1 | mixed=1024/unchanged | 8.205 → 1.012 (-87.67%) | 2,603,735 → 1,369,936 | 16,472 → 12,348 | 157,598 → 0 |
| 1 | provenance=0/changed | 4.138 → 4.077 (-1.48%) | 4,327 → 4,279 | 44 → 43 | 65 → 65 |
| 1 | provenance=0/unchanged | 3.876 → 0.319 (-91.76%) | 4,327 → 2,896 | 44 → 28 | 65 → 0 |
| 1 | provenance=128/changed | 4.050 → 4.568 (+12.78%) | 74,511 → 74,463 | 568 → 567 | 7,233 → 7,233 |
| 1 | provenance=128/unchanged | 4.339 → 0.325 (-92.51%) | 74,583 → 40,744 | 568 → 421 | 7,233 → 0 |
| 1 | provenance=16384/changed | 20.085 → 18.635 (-7.22%) | 9,523,848 → 9,523,802 | 65,728 → 65,727 | 917,569 → 917,569 |
| 1 | provenance=16384/unchanged | 19.335 → 3.637 (-81.19%) | 9,523,870 → 5,302,656 | 65,728 → 49,263 | 917,569 → 0 |
| 1 | provenance=2048/changed | 6.152 → 6.202 (+0.81%) | 1,192,336 → 1,192,288 | 8,265 → 8,264 | 114,753 → 114,753 |
| 1 | provenance=2048/unchanged | 6.271 → 0.431 (-93.12%) | 1,192,412 → 648,480 | 8,265 → 6,192 | 114,753 → 0 |
| 4 | mixed=1024/changed | 8.322 → 7.956 (-4.40%) | 2,603,733 → 2,603,686 | 16,472 → 16,471 | 157,598 → 157,598 |
| 4 | mixed=1024/unchanged | 7.799 → 1.476 (-81.07%) | 2,603,749 → 1,369,948 | 16,472 → 12,348 | 157,598 → 0 |
| 4 | provenance=0/changed | 3.669 → 3.363 (-8.34%) | 4,327 → 4,279 | 44 → 43 | 65 → 65 |
| 4 | provenance=0/unchanged | 3.418 → 0.318 (-90.69%) | 4,327 → 2,896 | 44 → 28 | 65 → 0 |
| 4 | provenance=128/changed | 3.950 → 3.627 (-8.17%) | 74,512 → 74,464 | 568 → 567 | 7,233 → 7,233 |
| 4 | provenance=128/unchanged | 4.030 → 0.324 (-91.95%) | 74,584 → 40,744 | 568 → 421 | 7,233 → 0 |
| 4 | provenance=16384/changed | 19.085 → 18.299 (-4.11%) | 9,523,852 → 9,523,804 | 65,728 → 65,727 | 917,569 → 917,569 |
| 4 | provenance=16384/unchanged | 19.143 → 4.495 (-76.52%) | 9,523,874 → 5,302,659 | 65,728 → 49,263 | 917,569 → 0 |
| 4 | provenance=2048/changed | 5.907 → 6.151 (+4.13%) | 1,192,336 → 1,192,290 | 8,265 → 8,264 | 114,753 → 114,753 |
| 4 | provenance=2048/unchanged | 5.821 → 0.540 (-90.73%) | 1,192,414 → 648,482 | 8,265 → 6,192 | 114,753 → 0 |

Adverse latency medians: CPU 1 mixed=1024/changed: +1.61%; CPU 1 provenance=128/changed: +12.78%; CPU 1 provenance=2048/changed: +0.81%; CPU 4 provenance=2048/changed: +4.13%.

The adverse CPU-1 provenance=128 changed-save case was investigated with a longer, separate paired cohort: 32 observations, two-second minimums, four repetitions per block, ABBA/BAAB order, and byte-identical binaries. Its median was 3.579 → 3.774 ms (+5.43%). Both the original adverse result and this follow-up remain evidence; no sample is replaced or discarded. Timing ranges overlap and no statistical attribution to code versus filesystem variation is claimed. Across pilot, main and follow-up cohorts, **392 observations** are retained. See [follow-up observations](2026-10-18-outbox-unchanged/raw/changed-followup/samples.json) and [follow-up ranges](2026-10-18-outbox-unchanged/raw/changed-followup/analysis.json).

[All observations](2026-10-18-outbox-unchanged/raw/comparison/samples.json),
[medians and ranges](2026-10-18-outbox-unchanged/raw/comparison/analysis.json),
[resource counters](2026-10-18-outbox-unchanged/raw/comparison/block-resources.json).

## Validation, costs and compatibility

All 15 final stages passed on Railway: formatting; downward-only quality-baseline
maintenance; focused tests twice normally and twice under race detection; the full
Go suite; broad storage/runtime/embedded/run-through/S3/public-package race tests;
four conformance profiles; vet; quality/file-size/format gates; site build, site
tests and documentation links. See [final checks](2026-10-18-outbox-unchanged/raw/source-checks/checks.json).
The baseline change records reduced production comment volume; no floor is raised.
The final report is verified against exact source and document hashes in the
[report receipt](2026-10-18-outbox-unchanged/raw/source-checks/report-receipt.json).
All compilation, formatting, tests and benchmarks ran on Railway. Local operations
were source edits, Git, packaging and analysis of exported data.

Costs include file identity/mode inspection, a read/write open, file sync/close and
a directory fence for unchanged saves. A file that cannot use this path attempts
full replacement. Changed saves add row comparison and shared callback bookkeeping;
the comparative table retains their costs. A private format flag is added per
handle. No new per-key cache is introduced.

File inode and modification time are preserved on successful healthy no-change
saves; callers must not depend on every save publishing a new file. Content,
permissions, fresh cross-process state and durability remain protected. This does
not guarantee every unchanged save avoids replacement: JSON migration and fallback
cases intentionally replace it. Retained raw evidence includes every pilot, main-comparison and targeted-follow-up
observation. Scratch benchmark binaries are removed after their hashes are saved;
the Railway runner is stopped after verified export.

## Next measured priority

The full-journal read/parse cost remains on every operation. The next investigation
should measure its contribution and supported workload limits against the current
code before proposing a larger change. No indexing, incremental checkpointing or
new storage abstraction has begun. This optimization does not make journal memory
usage bounded or remove its linear relationship with retained state.
