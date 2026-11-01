# Fence unchanged reconciled ledgers without replacing them

**Unchanged reconciliation no longer creates and publishes an identical replacement ledger.** It still reloads and validates state under the host/capture gates, checks holds, encoding limits and capacity, then syncs the existing compatible file, its immediate parent and every ancestor. Failed existing-file fencing falls back to the former replacement path. Pending settlement and changed usage still require replacement. The production diff is 12 insertions and three deletions across two files: a net nine lines, with no dependency, format, public API, retained cache, index, checkpoint or storage abstraction.

## Baseline and deterministic proof

The checkout begins clean at `3a41b9dd824313bdfac6ba9e5e1ab611f488fb75`; fetching main returns unchanged `219d1dcfff9807b04660a40313b32127e5be2933`, which is an ancestor of the checkout. The previous bounded-reader, checksum, scan-reuse and admission/recovery fixes remain complete. The [preceding isolation](2026-10-18-ledger-fences.md) measures a small-ledger durable replacement at about 3.56 ms, compared with 0.24 ms for the existing durable-file fence plus all ancestors. Its diagnostic ancestor omission was not adopted. This change targets the measured unchanged reconciliation write while retaining the fences.

Before writing candidate implementation code, native tests run against the exact original source on Railway. A real namespace with two resources is reconciled, and an open descriptor keeps the original ledger inode alive. Reconciliation with a separately cloned but value-identical resource slice replaces that inode on the original implementation. Bytes and logical usage are identical; the replacement is unnecessary physical I/O. A second native test makes the existing ledger directory non-writable while keeping a valid mode-0600 ledger and all required read/traverse permissions. The original unchanged reconciliation fails because it attempts a temporary replacement. Both failures reproduce normally and under race instrumentation, with no compile error, data race or root-permission skip.

Five safety-control groups already pass on the original source: changed resource fields/order/nil representation require replacement; incompatible file permissions are repaired by replacement or refused when replacement cannot occur; pending reservations cannot be cleared without a write; unreadable ancestors still cause the native fence error; an integrity-valid but over-quota state is refused even when supplied usage matches. [Original normal proof](2026-10-18-reconcile-reuse/raw/red/red-normal.log), [original race proof](2026-10-18-reconcile-reuse/raw/red/red-race.log), [verified original receipt](2026-10-18-reconcile-reuse/raw/red/validation-receipt.json). Root failure names are checked exactly, not inferred from exit 1.

## Implementation and preserved contracts

`Reconcile` computes an unchanged flag only after the existing fresh state read, store binding and held-projection checks. It requires no pending admission, equal nil-versus-empty representation, and value equality of the complete ordered resource slices. The comparison includes `StoreID`, ID, version, byte charge and class. A new usage slice with the same values qualifies; a pointer identity or byte-count-only comparison does not. Nil versus empty and ordering remain distinct so their prior encoded representation changes are still persisted.

A small private `Host.persist` method contains the former writer's encoding, maximum-size and aggregate quota checks. Existing `Host.write` callers delegate with unchanged=false. Only the checked reconciliation call can request reuse. The existing conservative staging charge of twice the encoded length remains even when the temporary file is avoided; this does not expand quota admission. All other writes, including startup, namespace/store binding, operation state, pending reservations and settlement, retain full replacement.

For an unchanged ledger, `atomicfile.SyncExisting` verifies a regular file with mode 0600, opens it for syncing and checks its identity, syncs the file, closes it and syncs its parent. This is the same existing primitive already used by outbox provenance persistence. On any probe/fence failure, the original `atomicfile.Write` path performs a complete replacement, preserving permission repair and its `ErrUnknown` plus native-error behavior. A successful replacement is allowed to supersede a failed reuse attempt, as with the existing outbox pattern. The complete `syncAncestors` chain remains after either successful path; its native ancestor errors remain visible.

The host mutex and capture lock remain held throughout comparison and persistence, excluding managed concurrent writers. No durable state, checksum bytes or descriptors are cached between operations. Every reconciliation still rereads and verifies the ledger, encodes it and recomputes capacity. This retains the existing owned-storage coordination qualification rather than adding protection against unsupported concurrent raw host edits. Canonical managed ledgers keep identical bytes when reused; changes continue using the same version-one writer.

The regression suite has seven top-level tests. It checks inode/mtime/byte preservation, all resource fields and order, nil-to-empty persistence, read-only directory behavior, successful and failed permission fallback, pending-reservation retention and later settlement, exact native ancestor-error identity, and quota refusal without changing the ledger. Existing concurrency, holds, slot limits, tampering, exact input-size boundaries, uncertain publication, reopen/recovery and S3 tests remain in the full validation scope. The new tests use native files and permissions rather than injected success stubs.

## Comparative measurements before production edits

The temporary candidate overlay passes all seven groups normally and under race instrumentation before production source is changed. The identical old-source replay still produces exactly the two expected failures. Four newly built binaries cover capacity and filesystem packages, original and candidate; stale binaries are deleted first. All **15 prototype steps** and **240 observations** pass. Only afterward are the exact Railway-formatted and hashed candidate bytes copied into production files.

Each cell runs in a separate child process. ABBA/BAAB ordering supplies four original/fixed observations per cell; C/D labels run the identical original binary as controls. Original and candidate receive equivalent native fixtures. All commands, binary identities, samples and min/max ranges are preserved. Positive timing percentages mean slower. Linux child peak RSS includes startup, calibration and fixtures; it is not per-operation memory or a cgroup/page-cache measurement. Allocation counters exclude fixture setup, later outcome/accounting checks and cleanup.

Ledger cells use 16 or 4,096 declared one-byte resources and separately measure native unchanged reconciliation, direct full writing, and admission with a counted no-op effect. Final ledger bytes, payload charges and pending charges must match expectations. Full-write and admission cells remain controls that do not request reuse. Filesystem cells measure a 64 KiB PUT, request-keyed save and empty-bucket deletion over zero or eight existing 1 MiB objects, plus standalone scan/reconciliation and unbound creation. Native scans/reconciliation verify exact final accounting. Initial fixture ancestors are explicitly fenced outside timing in both variants.

The prototype uses ten timed operations per process:

| Cell | Time, original → fixed | Time change | Bytes/op, original → fixed | Objects/op, original → fixed | Child peak RSS, original → fixed | Same-binary time change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| ledger/resources=16/stage=admission | 8.6768 → 8.4426 ms | -2.70% | 33,895.5 → 34,212.5 | 242.0 → 242.5 | 10.15 → 10.12 MiB | +18.88% |
| ledger/resources=16/stage=reconcile | 3.7940 → 0.2181 ms | -94.25% | 21,658.5 → 20,896.0 | 186.0 → 180.0 | 10.13 → 10.14 MiB | -6.15% |
| ledger/resources=16/stage=write | 3.9902 → 3.7409 ms | -6.25% | 9,588.5 → 9,368.0 | 55.5 → 55.0 | 10.10 → 10.14 MiB | -10.81% |
| ledger/resources=4096/stage=admission | 22.3438 → 22.0549 ms | -1.29% | 6,690,755.5 → 6,952,716.5 | 16,722.5 → 16,728.5 | 17.04 → 15.06 MiB | -0.26% |
| ledger/resources=4096/stage=reconcile | 12.8485 → 7.7440 ms | -39.73% | 5,390,704.0 → 5,180,124.5 | 16,668.0 → 16,657.0 | 14.92 → 14.68 MiB | -0.86% |
| ledger/resources=4096/stage=write | 7.9575 → 7.1038 ms | -10.73% | 1,200,134.5 → 1,148,198.0 | 61.5 → 61.0 | 14.62 → 14.46 MiB | -23.50% |
| filesystem/objects=0/op=delete | 15.8839 → 12.8097 ms | -19.35% | 59,064.0 → 58,584.0 | 523.0 → 517.0 | 12.11 → 12.01 MiB | -9.06% |
| filesystem/objects=0/op=put | 15.4910 → 14.3769 ms | -7.19% | 282,342.0 → 282,518.5 | 589.0 → 584.0 | 14.09 → 14.63 MiB | +5.79% |
| filesystem/objects=0/op=save | 36.0716 → 38.5236 ms | +6.80% | 449,083.0 → 449,730.0 | 1,224.5 → 1,225.5 | 14.82 → 14.70 MiB | +7.81% |
| filesystem/objects=8/op=delete | 21.8824 → 18.2675 ms | -16.52% | 8,605,053.0 → 8,604,852.5 | 813.0 → 807.0 | 16.38 → 16.45 MiB | +7.36% |
| filesystem/objects=8/op=put | 26.1383 → 21.4748 ms | -17.84% | 8,819,392.5 → 8,819,142.5 | 871.0 → 866.0 | 16.70 → 16.33 MiB | -5.23% |
| filesystem/objects=8/op=save | 44.0662 → 44.9696 ms | +2.05% | 8,994,196.5 → 8,995,547.0 | 1,501.5 → 1,503.0 | 17.50 → 17.21 MiB | +3.53% |
| filesystem/stage=reconcile | 10.2115 → 6.0117 ms | -41.13% | 8,529,132.5 → 8,528,172.0 | 475.0 → 469.0 | 16.74 → 16.49 MiB | +3.58% |
| filesystem/stage=scan | 5.3185 → 5.6275 ms | +5.81% | 8,491,091.5 → 8,491,079.5 | 309.0 → 309.0 | 16.41 → 16.31 MiB | +10.83% |
| filesystem/unbound-create | 3.9389 → 3.4334 ms | -12.83% | 4,132.0 → 4,129.0 | 40.0 → 40.0 | 10.09 → 10.06 MiB | -1.40% |

Direct unchanged reconciliation improves **3.794 → 0.218 ms (-94.25%)** at 16 resources and **12.849 → 7.744 ms (-39.73%)** at 4,096. The populated-store reconciliation stage improves **10.212 → 6.012 ms**. Native deletion and PUT show benefits, but request-keyed saves are adverse: **+6.80%** for the empty fixture and **+2.05%** for the populated fixture, with same-binary controls **+7.81%** and **+3.53%**. These results are retained, not dismissed or omitted. The large admission control also allocates **+3.92%** despite not using reuse. Aggregate JSON-pool/GC and runtime variation remain part of the measured paths; no exact causal attribution is made for these differences.

## Final Railway comparison

All formatting, builds, tests, race/conformance, quality, vet, site/docs checks and benchmarks run only on authorized Railway scratch deployment `cd70e535-3260-4baa-9e04-3767804a10d9`. Go 1.25.6, Linux amd64, UID 10001, GOMAXPROCS 4, 4 CPU/4 GiB. Timed ledgers/payloads use verified ext4 `/results` (statfs 61267). The target service is not started or changed.

The final comparison uses the actual selected production files rather than a candidate overlay, with exact original-source overlays for before binaries. It increases each process to **20 timed operations**, yielding a further **240 observations**, 480 including the prototype. Request-keyed saves accumulate more receipts at 20 operations than at ten, so absolute prototype/final timings are not identical workloads; original/fixed fixtures match within each comparison. The same fixed-operation design remains bounded below existing resource limits.

| Cell | Time, original → fixed | Time change | Bytes/op, original → fixed | Objects/op, original → fixed | Child peak RSS, original → fixed | Same-binary time change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| ledger/resources=16/stage=admission | 7.0171 → 8.1054 ms | +15.51% | 33,914.5 → 33,783.0 | 242.0 → 242.0 | 10.19 → 10.18 MiB | +2.86% |
| ledger/resources=16/stage=reconcile | 4.1004 → 0.1758 ms | -95.71% | 21,656.5 → 20,896.0 | 186.0 → 180.0 | 10.10 → 10.12 MiB | -9.30% |
| ledger/resources=16/stage=write | 3.3060 → 3.1639 ms | -4.30% | 9,368.0 → 9,368.0 | 55.0 → 55.0 | 10.15 → 10.10 MiB | -7.36% |
| ledger/resources=4096/stage=admission | 21.2399 → 21.3100 ms | +0.33% | 6,611,756.5 → 6,795,537.0 | 16,720.5 → 16,724.0 | 16.14 → 16.74 MiB | +16.19% |
| ledger/resources=4096/stage=reconcile | 12.2473 → 7.6151 ms | -37.82% | 5,180,666.5 → 5,442,208.0 | 16,662.5 → 16,662.5 | 14.62 → 14.59 MiB | -8.95% |
| ledger/resources=4096/stage=write | 7.0561 → 7.8067 ms | +10.64% | 1,147,638.0 → 1,147,662.5 | 60.0 → 60.0 | 14.65 → 14.43 MiB | -5.77% |
| filesystem/objects=0/op=delete | 14.1276 → 13.2328 ms | -6.33% | 59,789.5 → 59,205.5 | 523.0 → 517.0 | 11.84 → 11.77 MiB | +6.22% |
| filesystem/objects=0/op=put | 15.8488 → 16.7022 ms | +5.38% | 286,820.0 → 286,637.0 | 591.0 → 586.0 | 14.59 → 14.74 MiB | +5.46% |
| filesystem/objects=0/op=save | 35.0204 → 36.8047 ms | +5.09% | 515,866.5 → 514,239.5 | 1,525.0 → 1,524.0 | 14.60 → 14.99 MiB | +0.89% |
| filesystem/objects=8/op=delete | 22.7199 → 19.2482 ms | -15.28% | 8,605,054.5 → 8,604,603.5 | 813.0 → 807.0 | 16.62 → 18.07 MiB | +1.06% |
| filesystem/objects=8/op=put | 25.1438 → 21.5910 ms | -14.13% | 8,823,239.0 → 8,822,651.5 | 874.0 → 868.0 | 16.38 → 18.00 MiB | +3.13% |
| filesystem/objects=8/op=save | 42.6826 → 42.5690 ms | -0.27% | 9,062,070.0 → 9,060,914.5 | 1,796.5 → 1,794.5 | 17.10 → 17.27 MiB | -2.76% |
| filesystem/stage=reconcile | 10.8445 → 6.2017 ms | -42.81% | 8,528,603.5 → 8,528,275.5 | 475.0 → 469.0 | 16.48 → 16.52 MiB | -3.10% |
| filesystem/stage=scan | 5.2173 → 5.2692 ms | +0.99% | 8,491,076.5 → 8,491,216.0 | 309.0 → 309.0 | 16.07 → 16.40 MiB | -0.05% |
| filesystem/unbound-create | 3.9761 → 3.6633 ms | -7.87% | 4,103.0 → 4,102.0 | 40.0 → 40.0 | 10.14 → 10.11 MiB | -3.03% |

Final adverse native filesystem timing cells: **objects=0/op=put +5.38%, objects=0/op=save +5.09%**. All observations and controls remain visible. Timing results are fixture- and runner-specific; no statistical-significance, universal speedup, constant-memory or general S3 throughput claim is made. Direct tests establish avoided replacement and retained safety behavior. [Final observations](2026-10-18-reconcile-reuse/raw/final/measurements.json), [final ranges](2026-10-18-reconcile-reuse/raw/final/comparison-analysis.json), [prototype observations](2026-10-18-reconcile-reuse/raw/prototype/measurements.json), [prototype ranges](2026-10-18-reconcile-reuse/raw/prototype/comparison-analysis.json).

## Adverse timing follow-up

A separate nine-step diagnostic rebuilds each package once and toggles reuse on/off in that same binary. All **80 additional observations** pass, with before/after binary paths and SHA-256 verified identical. C/D controls both force replacement. The temporary overlay adds environment checks in both modes; it is not a replacement for the uninstrumented original/fixed comparison and is never copied into production.

For this table, original means reuse disabled and fixed means reuse enabled within the identical diagnostic binary:

| Cell | Time, original → fixed | Time change | Bytes/op, original → fixed | Objects/op, original → fixed | Child peak RSS, original → fixed | Same-binary time change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| ledger/resources=16/stage=admission | 7.3794 → 6.4653 ms | -12.39% | 33,780.5 → 33,697.0 | 242.0 → 242.0 | 10.12 → 10.12 MiB | -4.75% |
| ledger/resources=16/stage=reconcile | 3.4932 → 0.1826 ms | -94.77% | 21,765.5 → 20,896.0 | 186.0 → 180.0 | 10.14 → 10.12 MiB | +8.20% |
| ledger/resources=4096/stage=write | 7.6053 → 6.9783 ms | -8.25% | 1,147,793.5 → 1,121,418.5 | 60.0 → 59.5 | 14.28 → 14.41 MiB | -2.31% |
| filesystem/objects=0/op=put | 16.1574 → 15.7866 ms | -2.29% | 286,217.5 → 286,775.0 | 590.5 → 586.0 | 14.69 → 14.27 MiB | -10.97% |
| filesystem/objects=0/op=save | 37.5437 → 40.1115 ms | +6.84% | 514,240.5 → 516,294.0 | 1,524.5 → 1,525.5 | 15.03 → 14.65 MiB | +3.69% |

The direct unchanged-reconciliation gain reproduces. Yet the direct admission control also shifts about **-12.39%** even though it does not call the reuse decision, and empty request-keyed save is adverse at about **+6.84%**. These observations limit attribution; they do not erase the original/fixed adverse samples or establish a general save gain. [Same-binary measurements](2026-10-18-reconcile-reuse/raw/diagnostic/measurements.json), [ranges](2026-10-18-reconcile-reuse/raw/diagnostic/comparison-analysis.json).

The first-operation trace finds no eligible reconciliation for either first PUT or first save. A further three-step trace follows the actual **20-operation pattern on the Railway volume**, with accounting checked afterward. It finds **19 eligible and one ineligible reconciliation for ordinary PUT**, but **zero eligible and 20 ineligible reconciliations for distinct request-keyed saves**. The first-call observation alone would have misrepresented PUT coverage. These are eligibility counts, not latency measurements. Both traces remain preserved. [Repeated trace](2026-10-18-reconcile-reuse/raw/eligibility/trace.log), [per-operation eligibility](2026-10-18-reconcile-reuse/raw/eligibility/eligibility.json), [initial trace](2026-10-18-reconcile-reuse/raw/diagnostic/trace.log).

The save fixture therefore has no demonstrated benefit from this shortcut. Its repeated original/fixed adverse timings remain unresolved and are not attributed conclusively to the change or dismissed as noise. The same-binary follow-up isolates shortcut activation from whole-binary differences without claiming to explain every timing shift. All diagnostic Go files and trace hooks are removed; production bytes remain the exact 26-step validated candidate.

Final costs also include **+5.05% allocated bytes** in large unchanged reconciliation and **+9.93% child peak RSS** in populated PUT, despite reduced replacement work. Those observations remain visible alongside the lower small-ledger allocation count and operation timings. This optimization targets replacement I/O; it does not establish a process-memory improvement.

The new comparison is linear in the resource count and adds value comparisons before changed-state replacement. It allocates no comparison map or persistent cache. Incompatible/failing existing-file probes add their metadata/open/sync attempt before the original replacement fallback; that error-path overhead is not separately benchmarked. Encoding and quota accounting remain, so the shortcut has diminishing benefit as those dominate. Whole-object capacity scans still allocate and hash the existing payload, and no reliable process peak-RSS reduction is claimed. Unchanged reconciliation can now succeed without directory write permission when its existing ledger and required fences are usable; actual changes and pending settlement still need replacement permission. The native tests cover this intended availability difference.

## Validation and provenance

All **26 final steps** pass: formatting; quality/file-size/format ratchets; original-source normal/race red replays; filesystem/capacity normal and race suites twice; full Go tests; broad capacity/storage/runtime/embedded/runthrough/S3/public-package race tests; four conformance profiles; vet; site build/tests; documentation links; two benchmark installs, formatting and exact source snapshots; all four binary builds; measurement; cleanup. [Final checks](2026-10-18-reconcile-reuse/raw/final/checks.json), [final receipt](2026-10-18-reconcile-reuse/raw/final/validation-receipt.json), [normal suites](2026-10-18-reconcile-reuse/raw/final/focused-normal.log), [race suites](2026-10-18-reconcile-reuse/raw/final/focused-race.log). All native permission controls execute without root skips. No quality baseline is raised. Both temporary Go benchmark files are removed. Additional Railway site/docs and exact-source/evidence checks validate the saved report before scratch teardown; committed Go files must match the formatted/tested bytes.

The first upload terminates before an associated build, deployment `39e23475-0cb7-4501-aa93-8c8eb6a1466a`. Its error, terminal state and no-build log remain preserved. Only after verifying that state is the same source retried with stronger gzip compression: **68,126,808 → 64,549,806 bytes**. Streaming hashes verify byte-identical uncompressed tar data; both transport receipts and source inventory remain. The [actual transport recipe](2026-10-18-reconcile-reuse/raw/transport-Dockerfile.txt) and [validation Dockerfile](2026-10-18-reconcile-reuse/raw/Dockerfile.txt) are saved. No local project execution substitutes for Railway validation.

The [artifact manifest](2026-10-18-reconcile-reuse/raw/artifact-manifest.json) records the baseline, original proof, complete prototype/final results, exact compiled benchmark sources, source hashes, failed upload and runner teardown evidence. The implementation uses existing interfaces and the existing file-fencing primitive; no journal migration or caller action is required.

## Next finding

Capacity scanning still uses whole-file reads while validating and hashing existing objects. The 8 MiB fixtures continue to allocate roughly 8.5 MB for a scan. Measure allocation and child peak RSS with larger supported individual objects, then assess streaming verification through existing interfaces before considering an index or new accounting machinery. The two required pending/settled ledger transitions and remaining payload/directory fences also retain real costs. No next optimization is implemented here. The broader product-quality goal remains active.
