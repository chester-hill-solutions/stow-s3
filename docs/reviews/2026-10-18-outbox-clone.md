# Keep compacting outbox state copies — 2026-10-18

## Decision

**Reject replacing the three state-copy loops with maps.Clone. Production source
remains byte-identical to baseline `1e650fc`.** The accepted change adds nine
copy-independence contracts, one capacity-retention regression and the isolated
benchmark, with the experiment and all measurements retained.

Dense isolated copies improve, but no clone allocation reduction is measured.
A churned snapshot with 128 live values per collection allocates **59 times as many
bytes** and takes **10.7–11.5 times as long** with the candidate. This resource cost
outweighs the mixed durable-write timings. The prior faster write-cache candidate
also remains rejected for its separate caller-string retention problem.

`origin/main` was fetched and remains `219d1dcfff9807b04660a40313b32127e5be2933`.
[Baseline refs](2026-10-18-outbox-clone/raw/baseline.json), [prior phase measurements](2026-10-18-outbox-write-phases.md)
and [write-cache rejection](2026-10-18-outbox-write-cache.md) establish completed
work. No new format, dependency, public API, cache or storage abstraction ships.

## Experiment and hypothesis

The phase profile attributed about 9% of large-backlog write time to cloning all
three maps. The temporary candidate changes just that function and adds the
standard-library maps import: use maps.Clone and preserve the existing mutable
empty-map result for nil inputs. It does not bypass reads, normalize differently,
retain writes in the byte cache, weaken locks or remove durability fences.

Three predictions were tested: bulk copying could reduce dense clone time;
explicit nil handling should preserve independence/rollback contracts; and
copying map tables could preserve deleted capacity and increase allocation cost.
The first two hold; the third makes a general replacement unacceptable here.
The pinned Go runtime copies the source map directory and tables. Its
[exact Clone source and full-file hash](2026-10-18-outbox-clone/raw/measure/runtime-clone.json) are retained.
This is evidence for Go 1.25.6, not a claim about every future runtime.

## Method and scope

All formatting, compilation, tests and measurements run on the authorized Railway
scratch runner, Go 1.25.6, Node 25.9.0, 4 CPU/4 GiB limit, UID 10001. File fixtures
use TMPDIR under the results volume, verified ext4 (type 61267). No local Go tool,
test, benchmark or commit hook runs. Production remains the baseline on disk;
Go overlays select the temporary candidate and the common diagnostic file-cycle
benchmark. Exact sources, overlays, commands, binary hashes and exports are retained.

An ABBA BAAB sequence gives four observations per variant/cell, at GOMAXPROCS 1
and 4 with a 400-ms minimum: **288 observations across 36 cells**. No adverse
observations are omitted. These are medians and ranges from limited samples,
not a statistical-significance claim or a production traffic distribution.

The isolated benchmark copies active, prepared and provenance maps. Dense cases
hold 0, 128 or 4,096 values per collection. Churned nonempty cases insert 32,768
values into each map and delete down to the stated live count before timing.
The two zero-depth labels both start empty; no deletion history is seeded at zero.
State/counter equivalence is checked outside timing. This private-helper fixture
exposes representation cost; it is not evidence that ordinary current journal
writes generate equally sparse maps. Current journal decoding generally rebuilds
maps from live values. No existing product data-loss or retention defect is claimed.

Durable cycles retain 0, 128 or 4,096 entries per active/prepared queue. Enqueue
and prepare each include admission plus completion/discard: two calls and two
durable replacements per operation. Provenance alternates a changed value; renew
updates a valid held claim: one call and one replacement. Setup, snapshots, GC
sampling and reopened-state checks are outside timing. Retained fields and final
provenance/lease values must survive reopening. Cached-buffer length stays zero
on both write paths, as before; the candidate adds no cache.

## Isolated clone results

| Values per map | Churned | CPU | Before → candidate µs | Time delta | Before → candidate B/copy | Before → candidate allocs/copy |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 0 | false | 1 | 0.105 → 0.077 | -26.30% | 144 → 144 | 3 → 3 |
| 0 | false | 4 | 0.110 → 0.081 | -25.95% | 144 → 144 | 3 → 3 |
| 0 | true | 1 | 0.104 → 0.081 | -21.96% | 144 → 144 | 3 → 3 |
| 0 | true | 4 | 0.110 → 0.086 | -21.49% | 144 → 144 | 3 → 3 |
| 128 | false | 1 | 37.683 → 29.880 | -20.71% | 114,312 → 114,312 | 268 → 268 |
| 128 | false | 4 | 47.620 → 37.305 | -21.66% | 114,312 → 114,312 | 268 → 268 |
| 128 | true | 1 | 149.755 → 1602.160 | +969.85% | 114,312 → 6,733,456 | 268 → 646 |
| 128 | true | 4 | 103.240 → 1190.394 | +1053.03% | 114,312 → 6,733,458 | 268 → 646 |
| 4096 | false | 1 | 2315.903 → 2000.784 | -13.61% | 3,714,128 → 3,714,128 | 8,246 → 8,246 |
| 4096 | false | 4 | 2264.079 → 1746.538 | -22.86% | 3,714,132 → 3,714,130 | 8,246 → 8,246 |
| 4096 | true | 1 | 2870.874 → 4405.256 | +53.45% | 3,714,128 → 9,526,928 | 8,246 → 8,582 |
| 4096 | true | 4 | 2373.838 → 3079.839 | +29.74% | 3,714,130 → 9,526,932 | 8,246 → 8,582 |

Dense clone allocation counts and bytes are unchanged. At 128 values after churn,
the copy grows from 114,312 B and 268 allocations to about 6,733,456 B and 646
allocations. At 4,096 churned values, bytes grow about 157% and time worsens
about 30–53%. The existing loops rebuild maps sized from live entries, while the
candidate duplicates backing capacity. Equal values do not establish equal cost.

## Durable-cycle results

| Per queue | Operation | CPU | Before → candidate ms | Time delta | Before → candidate allocs/cycle | Allocated-byte delta |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 0 | enqueue | 1 | 8.557 → 7.341 | -14.21% | 85.0 → 85.0 | +0.01% |
| 0 | enqueue | 4 | 7.684 → 7.092 | -7.70% | 85.0 → 85.0 | +0.00% |
| 0 | prepare | 1 | 8.097 → 6.794 | -16.09% | 85.0 → 85.0 | +0.01% |
| 0 | prepare | 4 | 8.104 → 6.470 | -20.16% | 85.0 → 85.0 | +0.01% |
| 0 | provenance | 1 | 3.685 → 3.228 | -12.41% | 41.0 → 41.0 | +0.00% |
| 0 | provenance | 4 | 3.260 → 3.631 | +11.41% | 41.0 → 41.0 | -0.02% |
| 0 | renew | 1 | 3.171 → 4.598 | +45.00% | 52.0 → 52.0 | -0.08% |
| 0 | renew | 4 | 3.985 → 4.144 | +3.99% | 52.0 → 52.0 | -0.21% |
| 128 | enqueue | 1 | 8.689 → 8.829 | +1.61% | 5,998.5 → 5,996.0 | -0.02% |
| 128 | enqueue | 4 | 8.434 → 8.041 | -4.66% | 6,001.5 → 5,993.5 | -0.08% |
| 128 | prepare | 1 | 8.885 → 8.012 | -9.82% | 5,997.0 → 5,997.5 | +0.00% |
| 128 | prepare | 4 | 8.180 → 8.416 | +2.89% | 5,995.5 → 5,996.5 | +0.01% |
| 128 | provenance | 1 | 4.555 → 4.111 | -9.75% | 2,996.0 → 2,998.5 | +0.05% |
| 128 | provenance | 4 | 4.195 → 4.180 | -0.37% | 2,996.0 → 2,996.0 | +0.01% |
| 128 | renew | 1 | 5.401 → 4.953 | -8.29% | 3,004.0 → 3,002.0 | -0.04% |
| 128 | renew | 4 | 5.054 → 4.388 | -13.18% | 3,002.0 → 3,002.0 | -0.00% |
| 4096 | enqueue | 1 | 47.503 → 44.174 | -7.01% | 181,734.0 → 182,896.0 | +0.40% |
| 4096 | enqueue | 4 | 46.434 → 42.488 | -8.50% | 182,315.0 → 182,896.5 | +0.20% |
| 4096 | prepare | 1 | 46.366 → 43.659 | -5.84% | 181,734.0 → 183,119.5 | +0.48% |
| 4096 | prepare | 4 | 44.017 → 43.474 | -1.23% | 182,896.0 → 183,119.5 | +0.08% |
| 4096 | provenance | 1 | 24.382 → 24.000 | -1.57% | 91,903.0 → 92,069.5 | +0.11% |
| 4096 | provenance | 4 | 22.996 → 23.893 | +3.90% | 91,718.5 → 92,071.5 | +0.24% |
| 4096 | renew | 1 | 23.480 → 23.374 | -0.45% | 91,514.0 → 91,980.5 | +0.32% |
| 4096 | renew | 4 | 21.865 → 21.434 | -1.97% | 91,775.0 → 91,572.0 | -0.14% |

Large-cycle median time changes range from an 8.50% reduction to a 3.90% increase.
Small empty renew shows a 45.00% increase. Favorable dense cloning does not erase
those adverse timings or establish a stable request-latency improvement. Full
cycle allocation counts/bytes move by less than 1%; the isolated dense clone
allocations are identical. No allocation benefit is claimed. Further timing
confirmation is not pursued because the resource regression already rejects the
candidate. [All observations, ranges, iterations, process heap/HWM and frame sizes](2026-10-18-outbox-clone/raw/measure/comparison-analysis.json)
and [order/commands/binary hashes](2026-10-18-outbox-clone/raw/measure/comparison-runs.json) remain available.
Heap/HWM metrics are process observations, not a per-operation memory SLA.

## Deterministic resource regression

After the matrix, add a generous 2 MiB live-growth ceiling for the same private
128-value churned snapshot. Create the source in a helper, run GC and read
runtime.MemStats, clone once, run GC again and read memory with both source and
clone alive. Value/counter equivalence is required. This captures retained copy
capacity rather than only transient cumulative allocation.

The original implementation passes twice normally and twice with race detection,
adding **112,912–113,568 B**. The candidate fails twice per mode, adding
**6,732,072–6,732,952 B**. Expected candidate exit 1 means a reproduced regression,
not a passing test suite. [Sample/count receipt](2026-10-18-outbox-clone/raw/proof/capacity-receipt.json),
[normal candidate failure](2026-10-18-outbox-clone/raw/proof/candidate-normal.log) and
[race candidate failure](2026-10-18-outbox-clone/raw/proof/candidate-race.log) are retained.

The guard is a test budget for this fixture, not a new general quota. Source setup
creates tens of MiB of temporary objects and forces GC; that adds bounded test
CPU and memory work. It adds no production allocation. The committed copy contracts
also preserve mutable nonnil maps for nil inputs, unchanged values/counters, and
independence on insertion, replacement and deletion in all three collections.

## Verification and disposition

The initial experiment passes 27 contract/caller-buffer cases twice for each
variant in normal and race modes (54 passes per variant/mode). They include
canonicalization, counter fences, failed directory-sync handling and changed peer
bytes; the permission-fault case runs unprivileged and is not skipped. The new
capacity regression then fails only against the rejected candidate as above.

The accepted baseline plus all new tests passes the full Go suite, broad race
(storage, runtime, embedded, run-through, S3 API and public package), four
conformance profiles, vet, site build/tests and documentation links. An initial
quality check flags aggregate complexity in the new independence table test.
Its existing assertions move into a named helper; no case or assertion changes.
The failure and original tested source remain retained. Final formatting, focused
normal/race and unchanged quality/file-size/format gates pass on the corrected
source. The final focused contract/resource subset has **28 cases passing twice
per mode: 56 normal passes and 56 race passes, with no failures or skips**.

[Initial verification checks](2026-10-18-outbox-clone/raw/initial-final/checks.json),
[quality failure](2026-10-18-outbox-clone/raw/initial-final/quality.log),
[final checks](2026-10-18-outbox-clone/raw/final/checks.json), [green receipt](2026-10-18-outbox-clone/raw/final/green-receipt.json)
and [exact final source hashes](2026-10-18-outbox-clone/raw/final/environment.json) record the scope.
Only the test layout changes after full verification; production, benchmark and
capacity-regression sources remain byte-identical to their tested versions.
All modified Go bytes come from Railway formatting, with hashes checked locally.
The report/site/link checks run again after this report and evidence are saved.

Production source is verified against `1e650fc` before commit. The scratch runner
is stopped after verified exports; the separate target remains removed and
unchanged. [Baseline refs](2026-10-18-outbox-clone/raw/baseline.json) and the
[artifact manifest](2026-10-18-outbox-clone/raw/artifact-manifest.json), including the final teardown receipt,
retain the audit trail. No discarded overlay enters
the product build.

The next performance priority is avoidable work in protobuf decode/encode, which
has a larger measured share at backlog. Any state-reuse experiment must establish
bounded ownership for caller strings and pass both retention guards, peer-byte
validation, canonicalization, fencing, rollback and durability contracts. No next
optimization, format change, indexing or incremental checkpointing is implemented
in this change.
