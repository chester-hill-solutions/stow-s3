# Refuse occupied outbox IDs at admission — 2026-10-18

## Baseline and consequences

Baseline `49eaa80` already rejects inconsistent journal identities. `origin/main`
was refreshed before this investigation; [baseline.json](2026-10-18-outbox-admission/raw/baseline.json)
records exact refs. This change addresses an existing API creating lost intents
or a journal that those recovery checks then refuse.

Enqueue, Prepare and PrepareOwned accept caller-supplied IDs. Their shared state
methods assigned directly into the destination map without checking whether an
ID already belonged to a live intent in either collection. Same-queue reuse could
replace the original operation, key/version and ownership fields. Cross-queue
reuse could persist an ID in both active and prepared maps, return success and
then fail to reopen. A held claim or prepared lease did not prevent the overwrite.
Two serialized concurrent calls with one ID could both report success.

The deterministic fixture admits `outbox-41` for `original`/`original-version`,
then tries that ID for a different delete of `replacement`/`replacement-version`.
Variants hold an active claim or prepared lease, with captured owner/token fields.
Identical input is also tested. These are temporary outboxes; no upstream requests
are sent. The normal run-through adapter requests automatic IDs. No deployed
incident or upstream data loss is asserted.

## Original reproduction

Before changing production code, the tests run twice normally and twice with
race detection against memory and filesystem outboxes:

- 32 serial cases: occupied active/prepared IDs, claimed/owned file records,
  Enqueue/Prepare/PrepareOwned and changed/identical input.
- 22 simultaneous-admission cases: all supported method pairs through a memory
  outbox, one file handle, or separate handles sharing a journal.
- Five available-ID controls: fresh Unicode/NUL custom IDs, automatic IDs,
  existing idempotent Commit behavior, completion, available-ID reuse and restart.

All **52 distinct regressions failed twice in each mode**, 104 failures per
normal/race run. Seven controls passed twice per mode: the five available-ID
controls plus two owned-prepare same-key exclusions that already rejected.
Each mode reported **24 journals that could not reopen after an accepted
collision**. The [red receipt](2026-10-18-outbox-admission/raw/before/red-receipt.json)
verifies the exact failing cases, repetitions and passing controls. The harness
expects regression exit code 1; successful harness status records that expected
failure, not a passing regression suite.

The first full validation found four existing snapshot subtests relying on
same-ID overwrite to alter a fixture. Their failed normal/race/full-suite stages
are retained in [the first attempt](2026-10-18-outbox-admission/raw/after/checks.json).
Revised fixtures use fresh intents for peer writes, check monotonic bounded counts
and every new record's paired fields, and retain held-slice/caller-mutation checks.
Additional coverage mutates existing records through MarkFailure or owned lease
renewal and checks that held snapshots and unrelated fields remain unchanged.
The six snapshot cases pass twice normally and twice under race detection against
original production sources via a Go overlay; they are not assertions deleted to
make admission refusal pass.

A measured first allocator used two lookups for every ID and returned a large
entry plus error. Its memory-cycle medians increased up to 34.2%. Those 256
[initial observations](2026-10-18-outbox-admission/raw/after/comparison-analysis.json) remain retained. Before removing redundant automatic-ID lookups,
two additional original-source counter tests demonstrated maximum-sequence wrap
replacing an existing `outbox-0`, twice normally and twice under race detection.
The first guarded candidate already refused that collision. The final allocator
preserves refusal through explicit exhaustion handling. The
[preflight receipt](2026-10-18-outbox-admission/raw/preflight/preflight-receipt.json)
records exact baseline failures, candidate passes and original snapshot passes.

## Contract and focused change

Admission creates a new intent. An ID already present in either live collection
returns an error, even for identical input. Existing Commit behavior remains
idempotent. A caller cannot use admission as a replacement or lease-takeover path.
There was no documented admission-upsert or identical-request idempotency contract.

Have the existing shared allocator check both live maps for a supplied ID before
changing its sequence. Errors propagate through private admission helpers and
existing public error returns. Keep candidate sequence
advancement local until availability is confirmed. Owned prepares check their
existing per-key exclusion before allocation, preserving its error precedence
and avoiding counter/token consumption on refusal. No extra full-state scan is
introduced. Explicit IDs require two map lookups independent of queue depth.
Automatic IDs use the existing monotonic sequence, whose maximum is maintained
when explicit numeric IDs are admitted and when journals reload. Refuse sequence
exhaustion instead of wrapping. The allocator mutates its caller-local entry
rather than returning another large struct, avoiding an extra value transfer.

Rejected operations return a zero entry and preserve the original queue fields,
claims/leases, file bytes, sequence and token allocation. Existing mutexes and the
shared filesystem lock cover the availability check and insertion, so peer handles
observe the winner before deciding whether another admission is allowed. No new
lock, map, index, tombstone, buffer, dependency, storage format or exported API
is added. Existing per-key owned-prepare exclusion remains in place.

Tests verify complete snapshots before/after refusal, byte preservation and reopen,
renewal by the original owner, subsequent automatic IDs and prepared tokens. The
race test requires exactly one winner and verifies its returned fields against
the reopened record. Existing valid-ID, migration, recovery, S3, ownership and
cross-process tests remain in the full verification.

## Validation

All **54 originally failing cases and seven controls pass twice normally and
twice under race detection**, 122 passing cases per mode in the new matrix, using
byte-identical original admission regression/benchmark source from the first red run;
the separate exhaustion tests were reproduced against original code before the
allocator refinement. The
[green receipt](2026-10-18-outbox-admission/raw/final/green-receipt.json) verifies exact cases and source hashes.

All **17 final Railway stages pass**: formatting; focused outbox tests twice normally
and twice with race detection; full Go suite; broad storage/runtime/embedded/
run-through/S3/public-package race tests; four conformance profiles (memory,
filesystem, runtime-memory, runtime-filesystem); vet; site build/tests;
documentation links; quality/file-size/format gates; original/final benchmark builds; and
interleaved comparison. [Final commands/results](2026-10-18-outbox-admission/raw/final/checks.json)
and [baseline commands/results](2026-10-18-outbox-admission/raw/before/checks.json)
retain every stage. No baseline or tolerance was raised.

All formatting, compilation, code checks, tests and measurements run on the same
authorized Railway scratch deployment, with Go 1.25.6, Node 25.9.0, a 4-CPU/4-GiB
cgroup and ext4 `/results`. Validation uses UID 10001. Local work is reading,
editing, Git, packaging, hashing and exported-data analysis. Exact source copies,
commands, upload/sync receipts and byte/hash-verified exports are retained. Final
report/documentation checks use the same validated source bytes. No baseline is raised.

## Comparative measurements

Separate before/final binaries execute bounded admission-and-completion cycles.
The initial and refined cohorts remain separate; each has its own original-code
baseline. The final baseline is built through a Go source overlay referencing
the verified original source copies, with the same benchmark source.
Each timed cycle admits one entry and acknowledges it, or prepares one entry and
discards it. It includes both public API calls, locking, allocation and, for files,
both atomic replacements with their file and parent-directory syncs. It is not isolated admission latency.

Memory and file outboxes retain either zero or 128 entries in each queue; the latter
has 256 retained intents with fixed fields and timestamps. Seeding is outside timing.
Untimed verification checks every retained field and both queue counts. Neither
queue grows during the measured cycle. Automatic IDs advance the existing counter;
explicit cycles use one available ID after completing its prior entry. Thus auto
ID/header widths and benchmark iteration counts can differ between observations.
No completed-ID history is introduced or measured.

Eight processes alternate ABBA then BAAB. Each executes all backend/depth/method/
ID-mode combinations at GOMAXPROCS 1 and 4, with a 300-ms minimum per case: four
observations per variant/cell, **256 observations per cohort, 512 total**. Allocation bytes/counts
are cumulative per two-call cycle; they are not retained heap or RSS.
File fixtures use Go b.TempDir under `/tmp` on the container overlay filesystem;
results/logs are on the separate ext4 `/results` volume. These timings do not
qualify persistent-volume latency or durability. A separate ext4 cohort below
provides persistent-volume timing observations, without a durability/SLA claim. The [validation-user filesystem
receipt](2026-10-18-outbox-admission/raw/final/fixture-filesystem.json) records the
actual paths and filesystem labels under UID 10001. These are
serial synthetic cycles, not throughput or latency guarantees for concurrent S3
requests. Shapes and runtime limits are explicit rather than workload-capacity claims.

A third, file-only cohort places TMPDIR under its UID-10001-owned ext4 result
folder and verifies the filesystem type before timing. It uses the same ABBA/BAAB
order, CPU settings and 300-ms minimum: **128 additional observations**, bringing
the retained total to **640 observations** across the three distinct cohorts.
The [ext4 fixture receipt](2026-10-18-outbox-admission/raw/volume/fixture-filesystem.json)
records the actual path; [volume observations and ranges](2026-10-18-outbox-admission/raw/volume/comparison-analysis.json)
remain separate from overlay data. Rebuilt original/refined binary hashes must
match the final overlay cohort before the report is finalized. No production
source or benchmark Go source changes between those two cohorts.

A targeted identical-binary control adds 16 ext4 observations for the two CPU-4
explicit-prepare cells. Both labels execute the same refined binary; hashes are
checked against the final comparison. Its [raw samples](2026-10-18-outbox-admission/raw/aa/comparison-analysis.json)
remain separate. **656 observations total** are retained: 640 before/after and
16 control observations. The control does not cancel the adverse ext4 results.

All values are per-cell medians. Times cover the two-call cycle; CPU means GOMAXPROCS.

### Memory outbox

| Depth per queue | Admission | ID | CPU | Final µs before → after | Initial Δ | Final Δ | Allocs/cycle before → after |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: |
| 0 | enqueue | auto | 1 | 0.322 → 0.296 | +15.19% | -7.97% | 3.0 → 3.0 |
| 0 | enqueue | auto | 4 | 0.330 → 0.305 | +11.89% | -7.54% | 3.0 → 3.0 |
| 0 | enqueue | explicit | 1 | 0.211 → 0.233 | +5.16% | +10.32% | 1.0 → 1.0 |
| 0 | enqueue | explicit | 4 | 0.250 → 0.245 | +30.07% | -2.26% | 1.0 → 1.0 |
| 0 | prepare | auto | 1 | 0.280 → 0.318 | +18.57% | +13.47% | 3.0 → 3.0 |
| 0 | prepare | auto | 4 | 0.324 → 0.319 | +12.27% | -1.44% | 3.0 → 3.0 |
| 0 | prepare | explicit | 1 | 0.199 → 0.218 | +18.47% | +9.54% | 1.0 → 1.0 |
| 0 | prepare | explicit | 4 | 0.228 → 0.234 | +19.63% | +2.78% | 1.0 → 1.0 |
| 128 | enqueue | auto | 1 | 0.310 → 0.301 | +10.67% | -3.11% | 3.0 → 3.0 |
| 128 | enqueue | auto | 4 | 0.309 → 0.319 | +7.10% | +3.27% | 3.0 → 3.0 |
| 128 | enqueue | explicit | 1 | 0.236 → 0.254 | +20.35% | +7.68% | 1.0 → 1.0 |
| 128 | enqueue | explicit | 4 | 0.230 → 0.260 | +30.44% | +13.08% | 1.0 → 1.0 |
| 128 | prepare | auto | 1 | 0.323 → 0.315 | +9.12% | -2.74% | 3.0 → 3.0 |
| 128 | prepare | auto | 4 | 0.325 → 0.310 | +11.96% | -4.66% | 3.0 → 3.0 |
| 128 | prepare | explicit | 1 | 0.198 → 0.226 | +31.82% | +13.86% | 1.0 → 1.0 |
| 128 | prepare | explicit | 4 | 0.237 → 0.264 | +34.19% | +11.13% | 1.0 → 1.0 |

### File outbox

| Depth per queue | Admission | ID | CPU | Final µs before → after | Initial Δ | Final Δ | Allocs/cycle before → after |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: |
| 0 | enqueue | auto | 1 | 1,203.291 → 1,116.662 | +6.56% | -7.20% | 87.0 → 87.0 |
| 0 | enqueue | auto | 4 | 1,208.633 → 1,112.496 | +10.66% | -7.95% | 87.0 → 87.0 |
| 0 | enqueue | explicit | 1 | 1,225.950 → 1,291.443 | +6.09% | +5.34% | 86.0 → 86.0 |
| 0 | enqueue | explicit | 4 | 1,269.466 → 1,193.117 | -0.89% | -6.01% | 86.0 → 86.0 |
| 0 | prepare | auto | 1 | 1,145.280 → 1,162.315 | +5.39% | +1.49% | 87.0 → 87.0 |
| 0 | prepare | auto | 4 | 1,092.155 → 1,148.566 | +7.46% | +5.17% | 87.0 → 87.0 |
| 0 | prepare | explicit | 1 | 1,267.619 → 1,228.501 | +0.41% | -3.09% | 86.0 → 86.0 |
| 0 | prepare | explicit | 4 | 1,100.212 → 1,173.015 | +3.83% | +6.62% | 86.0 → 86.0 |
| 128 | enqueue | auto | 1 | 1,979.275 → 1,954.271 | -3.75% | -1.26% | 6,034.0 → 6,034.0 |
| 128 | enqueue | auto | 4 | 1,974.887 → 2,030.677 | -17.00% | +2.82% | 6,035.0 → 6,035.0 |
| 128 | enqueue | explicit | 1 | 1,987.630 → 1,911.447 | -2.46% | -3.83% | 6,033.0 → 6,033.0 |
| 128 | enqueue | explicit | 4 | 1,956.627 → 2,065.952 | +1.10% | +5.59% | 6,033.0 → 6,033.0 |
| 128 | prepare | auto | 1 | 2,030.960 → 1,905.183 | +1.47% | -6.19% | 6,034.0 → 6,034.0 |
| 128 | prepare | auto | 4 | 1,933.682 → 2,028.390 | +8.35% | +4.90% | 6,035.0 → 6,035.0 |
| 128 | prepare | explicit | 1 | 1,936.898 → 1,999.254 | -0.80% | +3.22% | 6,033.0 → 6,033.0 |
| 128 | prepare | explicit | 4 | 2,103.644 → 1,960.121 | +7.75% | -6.82% | 6,033.0 → 6,033.0 |

### Ext4 volume outbox

This separate cohort uses the same byte-identical original/refined binaries as the final overlay cohort, with TMPDIR on the ext4 results volume. Cohorts are not pooled.

| Depth per queue | Admission | ID | CPU | µs before → after | Change | Allocs/cycle before → after |
| ---: | --- | --- | ---: | ---: | ---: | ---: |
| 0 | enqueue | auto | 1 | 10,276.698 → 8,380.993 | -18.45% | 87.0 → 87.0 |
| 0 | enqueue | auto | 4 | 8,345.797 → 9,085.639 | +8.86% | 87.0 → 87.0 |
| 0 | enqueue | explicit | 1 | 8,537.379 → 9,617.942 | +12.66% | 86.0 → 86.0 |
| 0 | enqueue | explicit | 4 | 8,017.399 → 7,108.405 | -11.34% | 86.0 → 86.0 |
| 0 | prepare | auto | 1 | 7,424.133 → 7,796.673 | +5.02% | 87.0 → 87.0 |
| 0 | prepare | auto | 4 | 9,345.101 → 8,533.496 | -8.68% | 87.0 → 87.0 |
| 0 | prepare | explicit | 1 | 8,289.547 → 8,403.470 | +1.37% | 86.0 → 86.0 |
| 0 | prepare | explicit | 4 | 7,674.039 → 9,684.326 | +26.20% | 86.0 → 86.0 |
| 128 | enqueue | auto | 1 | 9,677.833 → 9,253.072 | -4.39% | 6,034.0 → 6,034.0 |
| 128 | enqueue | auto | 4 | 8,323.645 → 9,462.302 | +13.68% | 6,035.0 → 6,035.0 |
| 128 | enqueue | explicit | 1 | 8,950.223 → 8,908.487 | -0.47% | 6,033.0 → 6,033.0 |
| 128 | enqueue | explicit | 4 | 11,000.995 → 9,009.555 | -18.10% | 6,033.0 → 6,033.0 |
| 128 | prepare | auto | 1 | 9,946.110 → 8,957.377 | -9.94% | 6,034.0 → 6,034.0 |
| 128 | prepare | auto | 4 | 9,150.793 → 8,208.444 | -10.30% | 6,035.0 → 6,035.0 |
| 128 | prepare | explicit | 1 | 8,624.943 → 8,076.288 | -6.36% | 6,033.0 → 6,033.0 |
| 128 | prepare | explicit | 4 | 7,952.444 → 10,145.281 | +27.57% | 6,033.0 → 6,033.0 |

### Identical-binary ext4 control

Both A/B labels execute the exact refined binary, CPU 4, explicit-ID prepare/discard cycles. These 16 observations remain separate from the before/after cohorts.

| Depth per queue | Label A median ms | Label B median ms | Label median change | Overall sample range ms |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 6.176 | 6.345 | +2.73% | 5.479–10.669 |
| 128 | 6.940 | 7.531 | +8.52% | 6.650–12.623 |

This demonstrates variability in the control; it does not rule out an implementation effect or replace the ext4 before/after results. The +27.6% ext4 cell remains an unresolved performance finding.

The largest absolute difference between median allocated bytes is 50.5 B/cycle.
Exact bytes, iteration counts and ranges for each cell remain in the analysis.

[All samples and ranges](2026-10-18-outbox-admission/raw/final/comparison-analysis.json),
[commands/order and binary hashes](2026-10-18-outbox-admission/raw/final/comparison-runs.json)
and the [harness hash](2026-10-18-outbox-admission/raw/comparison-harness-receipt.json)
are retained. Adverse samples and cells are not discarded. Small sample counts do
not establish statistical significance. Benchmark binaries are removed after hashes
are recorded; the scratch runner is stopped after verified exports and documentation checks.

## Compatibility and remaining limits

Callers relying on Enqueue/Prepare to replace an occupied ID now receive an error.
Use Commit to finish an existing prepared intent and a fresh ID for a new live
intent. Fresh matching custom IDs preserve their existing behavior. Automatic allocation
returns an error when its uint64 sequence is exhausted instead of wrapping.
The journal format remains schema 4, and supported legacy journals remain readable.
Already overwritten intents cannot be reconstructed here.

Availability is checked against live queues. Completed IDs are not retained as
an idempotency history, and supplied IDs must not be recycled while stale consumers
could still refer to their old intent. Available-ID reuse after completion remains
possible; the regression controls make this boundary explicit. This change does
not add exactly-once delivery, permanent deduplication or a new recovery protocol.

Next, return to the measured whole-journal write/fence costs. Establish comparative
benefit for a minimal change before adopting an optimization. No filesystem index,
incremental checkpoint or storage abstraction is introduced by this change.
