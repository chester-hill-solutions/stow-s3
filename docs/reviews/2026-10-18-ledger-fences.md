# Measure ledger replacement and ancestor fences before optimizing

**Keep the production durability path unchanged.** On this Railway fixture, a small ledger's ancestor chain costs about **0.14 ms**, a durable replacement **3.56 ms**, and fencing an existing valid ledger **0.24 ms**. Replacing unchanged reconciliation state is the stronger next investigation. This report records diagnosis and comparative evidence; it introduces no runtime optimization, dependency, public API, format, index, checkpoint or storage abstraction.

## Baseline and hypotheses

The checkout starts clean at `871e5f57171375d652ae8165773f994378eccaba`; fetching `origin/main` returns unchanged `219d1dcfff9807b04660a40313b32127e5be2933`. The previous checksum-copy reduction and all correctness repairs remain in force. The [prior change](2026-10-18-ledger-digest.md) has already passed its 22-step Railway validation, including broad race coverage and all four conformance profiles. No completed repair is repeated or bypassed in production here.

Three ranked, falsifiable hypotheses guide the comparison: omitting ledger ancestor syncs should materially reduce native mutation latency if those dominate; actual ledger replacements should dominate if the write-ahead/settlement sequence is more expensive; scanning/hashing should remain a substantial additional cost as existing payload grows. Inspection confirms a typical bound mutation performs **three ledger replacements**: authoritative reconciliation, a pending reservation before the effect, and settlement afterward. `Host.write` encodes and checks capacity, uses `atomicfile.Write`, then syncs ancestors from the host directory to root. `atomicfile.Write` already syncs the file and its immediate parent.

The current publication contract in [ADR 0020](../adr/0020-binary-object-records.md) requires file and immediate-parent sync for every publication and fresh ancestor fencing for relevant lifetime/reopen/failure cases. The pending reservation and verified reconciliation contracts from prior repairs must also remain. An expensive write or fence is not, by itself, evidence that its guarantee can be discarded.

## Diagnostic design

The original binary uses exact production source. A temporary Go overlay changes **only** the final `return syncAncestors(h.dir)` in `Host.write` to `return nil`. All encoding, integrity, admission checks, file syncs and immediate-parent syncs remain. Restoring that one call reconstructs the original source byte for byte. This deliberately relaxed build is an attribution experiment, **not a production candidate or a claim of equivalent crash guarantees**. It is never copied into an implementation file or committed as runtime code.

Four binaries cover capacity and filesystem packages, original and diagnostic. Each observation runs one isolated benchmark child process with ten timed operations. ABBA/BAAB ordering provides four observations for each original/diagnostic cell; C/D labels run the identical original binary as controls. There are **23 cells and 368 observations**, all successful and retained. Exact compiled benchmark source is saved after Railway formatting and all four builds, before cleanup, with a separate hash receipt. [Plan](2026-10-18-ledger-fences/raw/probe/plan.json), [all commands and observations](2026-10-18-ledger-fences/raw/probe/measurements.json), [ranges](2026-10-18-ledger-fences/raw/probe/comparison-analysis.json), [compiled source receipt](2026-10-18-ledger-fences/raw/probe/compiled-source-receipt.json).

Capacity fixtures use real canonical ledgers with 16 or 4,096 declared one-byte resources across two bound stores. They are ledger-scale fixtures, not a claim that every declared resource is a physical object file. Stages separately measure encoding, `atomicfile.Write`, the complete ancestor chain, `atomicfile.SyncExisting` plus ancestors, native `Host.write`, native unchanged `Reconcile`, and native `WithAdmission` with a counted no-op effect. Final ledger bytes must equal the starting bytes, payload charges must match resource counts, pending charges must be zero, and admission effect counts must match timed operations. Primitive stages isolate component costs; they do not substitute for complete namespace validation or recovery semantics.

Filesystem fixtures reuse the existing scan benchmark: ordinary 64 KiB PUT, request-keyed save, and empty-bucket deletion over zero or eight existing 1 MiB objects. Additional cells measure standalone scan/reconciliation and unbound bucket creation. Fixtures, binding, empty-bucket preparation, accounting verification and cleanup are outside timing/allocation counters. All fixtures explicitly fence initial host directory ancestors before timing in both builds. Each original/diagnostic case therefore starts from a fully fenced directory tree, despite the diagnostic build's omitted setup calls. Native post-operation reconciliation must confirm exact namespace accounting.

All formatting, compilation and benchmark execution occurs only on authorized Railway scratch deployment `a4b41a42-fdde-49d3-ba75-08b7d97cd7d9`, job `34ba9c01-d1ec-408d-98bb-0389586f61bc`. Go 1.25.6, Linux amd64, UID 10001, GOMAXPROCS 4, 4 CPU/4 GiB. Benchmark ledgers and payloads reside on verified ext4 `/results` (statfs 61267). Linux child maximum RSS includes startup, calibration and fixtures; it is neither per-operation RSS nor cgroup/page-cache usage. Fixtures are recently created; no cold-cache or production-throughput claim is made. Positive time changes mean slower. Independent stage medians must not be summed as an exact decomposition of a native operation.

## Capacity measurements

| Cell | Original time | Diagnostic time | Time change | Bytes/op, original → diagnostic | Objects/op, original → diagnostic | Child peak RSS, original → diagnostic | Same-binary time change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| resources=16/stage=admission | 7.1297 ms | 6.9772 ms | -2.14% | 33,588.5 → 31,322.0 | 242.0 → 200.0 | 10.14 → 10.10 MiB | +0.95% |
| resources=16/stage=ancestors | 0.1414 ms | 0.1679 ms | +18.77% | 1,240.0 → 1,240.0 | 21.0 → 21.0 | 10.12 → 10.11 MiB | +29.70% |
| resources=16/stage=atomic | 3.5574 ms | 2.8462 ms | -19.99% | 1,744.0 → 1,744.0 | 15.0 → 15.0 | 10.13 → 10.14 MiB | -0.04% |
| resources=16/stage=encode | 0.0085 ms | 0.0075 ms | -12.32% | 4,320.0 → 4,320.0 | 14.0 → 14.0 | 10.12 → 10.08 MiB | -0.82% |
| resources=16/stage=reconcile | 3.4381 ms | 3.4867 ms | +1.41% | 21,536.0 → 20,296.0 | 186.0 → 165.0 | 10.10 → 10.10 MiB | -26.58% |
| resources=16/stage=sync-existing | 0.2389 ms | 0.1708 ms | -28.51% | 2,312.0 → 2,312.0 | 30.0 → 30.0 | 10.14 → 10.11 MiB | -8.74% |
| resources=16/stage=write | 3.5809 ms | 3.3036 ms | -7.74% | 9,336.0 → 8,112.0 | 55.0 → 34.0 | 10.13 → 10.10 MiB | -2.27% |
| resources=4096/stage=admission | 21.6249 ms | 20.2119 ms | -6.53% | 6,848,069.0 → 6,949,856.5 | 16,727.0 → 16,685.5 | 14.62 → 14.85 MiB | +7.60% |
| resources=4096/stage=ancestors | 0.1655 ms | 0.2294 ms | +38.63% | 1,240.0 → 1,240.0 | 21.0 → 21.0 | 12.32 → 12.36 MiB | +11.68% |
| resources=4096/stage=atomic | 4.6393 ms | 4.4706 ms | -3.64% | 1,760.0 → 1,760.0 | 15.0 → 15.0 | 12.63 → 12.05 MiB | +3.65% |
| resources=4096/stage=encode | 1.2866 ms | 1.0461 ms | -18.70% | 728,689.0 → 623,285.5 | 17.0 → 14.5 | 13.90 → 13.26 MiB | +5.11% |
| resources=4096/stage=reconcile | 12.8538 ms | 11.7395 ms | -8.67% | 5,023,282.0 → 5,022,047.0 | 16,659.5 → 16,639.0 | 14.48 → 14.51 MiB | +5.33% |
| resources=4096/stage=sync-existing | 0.1978 ms | 0.2420 ms | +22.34% | 2,328.0 → 2,328.0 | 30.0 → 30.0 | 12.44 → 12.55 MiB | +43.30% |
| resources=4096/stage=write | 7.2607 ms | 6.3141 ms | -13.04% | 1,042,702.0 → 1,146,687.0 | 57.0 → 39.5 | 13.57 → 13.58 MiB | -2.91% |

The original 16-resource unchanged reconciliation takes **3.44 ms**, and its admission with two ledger transitions takes **7.13 ms**. At 4,096 resources those become **12.85 ms** and **21.62 ms**. Encoding alone grows from roughly **0.0085 ms** to **1.29 ms**. Direct full ancestor-chain measurements are only **0.14–0.17 ms** in the original build, while `atomicfile.Write` takes **3.56–4.64 ms**. `atomicfile.SyncExisting` plus the full ancestor chain takes **0.20–0.24 ms**. These observations identify durable file replacement as a substantial component and an existing primitive worth investigating for unchanged state; they do not establish a safe complete reconciliation shortcut.

The omitted ancestor call saves about 1.2 KiB of direct per-write bookkeeping in small-ledger cells. Large aggregate allocation samples still reflect JSON pool/GC variation: diagnostic 4,096-resource admission allocates more, and diagnostic large `Host.write` also allocates more despite omitting the call. No general memory or RSS improvement is inferred from this diagnostic build. Every sample and range is preserved.

## Native filesystem measurements

| Cell | Original time | Diagnostic time | Time change | Bytes/op, original → diagnostic | Objects/op, original → diagnostic | Child peak RSS, original → diagnostic | Same-binary time change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| objects=0/op=delete | 14.5263 ms | 14.0577 ms | -3.23% | 60,105.5 → 57,158.0 | 523.0 → 469.5 | 11.94 → 11.83 MiB | -7.52% |
| objects=0/op=put | 15.9885 ms | 15.2943 ms | -4.34% | 282,725.0 → 279,583.0 | 589.0 → 535.0 | 14.17 → 14.37 MiB | +10.31% |
| objects=0/op=save | 38.5955 ms | 34.6987 ms | -10.10% | 449,805.0 → 446,619.0 | 1,225.5 → 1,171.5 | 14.29 → 13.99 MiB | -2.19% |
| objects=8/op=delete | 24.0078 ms | 19.6461 ms | -18.17% | 8,605,759.0 → 8,602,335.5 | 813.5 → 759.0 | 16.72 → 17.33 MiB | +6.04% |
| objects=8/op=put | 26.4336 ms | 23.2692 ms | -11.97% | 8,819,720.0 → 8,816,947.0 | 872.0 → 817.5 | 16.39 → 16.65 MiB | +1.80% |
| objects=8/op=save | 51.0058 ms | 39.3861 ms | -22.78% | 8,993,778.0 → 8,992,242.0 | 1,501.0 → 1,448.5 | 17.02 → 16.76 MiB | +10.03% |
| stage=reconcile | 9.8930 ms | 10.4987 ms | +6.12% | 8,528,604.5 → 8,527,692.0 | 475.0 → 457.0 | 16.56 → 16.37 MiB | -1.85% |
| stage=scan | 5.6488 ms | 5.3738 ms | -4.87% | 8,491,364.5 → 8,491,078.0 | 309.0 → 309.0 | 17.25 → 16.29 MiB | -0.35% |
| unbound-create | 3.6192 ms | 3.8928 ms | +7.56% | 4,131.5 → 4,141.0 | 40.0 → 40.0 | 10.12 → 10.12 MiB | +15.79% |

The diagnostic build measures some large native improvements, including **51.01 → 39.39 ms** for the populated-store save. However, unchanged paths also move: the small primitive `atomicfile.Write` cell is **19.99% faster** despite using exactly the same atomic writer; the scan-only cell is **4.87% faster**; unbound creation is **7.56% slower**. Same-binary control changes include **+10.03%** for populated saves and **+15.79%** for unbound creation. The populated standalone reconciliation is adverse at **+6.12%**. These are retained observations, not discarded samples or proven consequences of ancestor omission.

The direct ancestor-chain cost is much smaller than some macro deltas. This experiment therefore does **not** establish that removing ledger ancestor syncs causes the observed 11–23% populated-mutation gains. It supplies no basis to relax durability guarantees or promise those speedups. The 8 MiB scan remains about **5.65 ms** and allocates **8.49 MB**, consistent with the already documented remaining full verified payload pass; no index or incremental accounting is introduced.

## Decision, validation and next finding

Production source remains byte-identical to the baseline. All nine diagnostic steps pass: install both temporary benchmark fixtures, format, compile all four binaries, measure every cell, and remove temporary Go benchmark files. Each sampled command exits successfully and checks its final outcomes. [Checks](2026-10-18-ledger-fences/raw/probe/checks.json), [receipt](2026-10-18-ledger-fences/raw/probe/validation-receipt.json). This is a measurement-only change, so the earlier baseline's conformance/race evidence is referenced rather than represented as a new test run for the deliberately weakened diagnostic. Additional Railway site build/tests, documentation links and source/evidence hashes validate this saved report before teardown. No local project formatting, builds, tests or benchmarks run.

The source and authorized historical reports/evidence are transported in one compressed archive; [transport receipt](2026-10-18-ledger-fences/raw/transport-receipt.json) records bytes, file count and SHA-256. Both the [actual transport Dockerfile](2026-10-18-ledger-fences/raw/transport-Dockerfile.txt) and [original validation recipe](2026-10-18-ledger-fences/raw/Dockerfile.txt) are retained. No upload retry or failed diagnostic run occurs in this investigation. Runtime/source identities, every observation, compiled-source hashes and scratch-runner teardown are preserved in the [artifact manifest](2026-10-18-ledger-fences/raw/artifact-manifest.json). The target service remains unchanged.

**The next independently reviewable change should investigate avoiding replacement when reconciliation finds unchanged settled usage, using the existing durable-file fencing primitive if the full contract can be preserved.** It must still verify capacity and holds, clear pending reservations only after authoritative reconciliation, fence the correct current file, handle permissions/replacement errors, and preserve failure/reopen behavior. A measured candidate and a deterministic regression must precede any implementation. The primitive **3.56 ms versus 0.24 ms** contrast motivates that investigation; it is not an end-to-end speedup claim. No such shortcut, ancestor-cache change or other optimization is implemented here. The broader product-quality goal remains active.
