# Reserve pending namespace resource slots before effects

**Pending admission omitted projected resources from the namespace-wide count.** A request could publish before its final ledger write returned `ErrFull`; a later store reopen then failed because the actual resources exceeded the existing 4,096-resource ceiling. Uncertain growth could also let a competitor successfully exceed that ceiling. Charge the larger settled or projected resource count, matching the existing maximum policy for bytes.

The production change initializes one integer count, takes its maximum with the pending count, and returns it. The existing 4,096 limit, per-store validation, byte accounting, persistence, public APIs, formats, dependencies and fences remain unchanged. No cache, index, checkpointing or storage abstraction is added.

## Baseline and deterministic native proof

The checkout begins clean at `2013409cea2b547749ac738c1788fbcf6804315d`; fetching main returns unchanged `219d1dcfff9807b04660a40313b32127e5be2933`. Recent bucket, multipart, uncertain-publication and scan-reuse fixes are already complete and retained. The outstanding lead is the counter returned by `storeCharge`: it charges pending bytes but returns only `len(store.Resources)`. `validateResources` checks each store independently, so two individually valid projections can exceed the namespace aggregate. The final settled write notices this only after the effect.

Three hypotheses were tested: the pending global count omitted growth; another validator caught it before effects; or a conservative maximum could incorrectly prevent deletion/replacement. A failing suite ran normally and under race detection on Railway before implementation edits. Five root cases fail on the parent; the two same-count replacement/deletion controls already pass:

- At the namespace limit, a growing admission returns `ErrFull` after invoking its callback, publishing a real marker and changing the durable ledger. It leaves a pending projection; one-byte resource accounting then reports 4,097 bytes for 4,096 expected entries.
- Two concurrent admissions for the last slot return one success and one refusal, but **both effects run**. Checking only accepted/refused totals would miss the defect.
- An uncertain growth retains its projected bytes across host reopen, but omitting its pending slot lets another store's growth **return success** and publish beyond the resource ceiling.
- An uncertain deletion must retain its old slot until verification. The parent still runs a competitor's effect before rejecting its final write; a follow-up attempt then remains blocked by that competitor's stray pending projection.
- Two real filesystem stores contain exactly **4,096 managed resources**, built through native unbound bucket creation and then bound/reconciled. A new native `CreateBucket` returns `ErrFull` while `HeadBucket` sees its metadata and its directory exists. The ledger changes, and `NewFilesystemStoreWithNamespace` subsequently fails to reopen the affected store with exhausted capacity. No production hook, fabricated committed marker, synthetic filesystem or write-error injector is used.

[Initial normal proof](2026-10-18-resource-count/raw/red/red-normal.log), [initial race proof](2026-10-18-resource-count/raw/red/red-race.log). The native fixture logs its actual scanned resource count before the attempt. The primitive cases exercise the real durable `Namespace` admission API with declared one-byte resources; the separate native case proves the storage producer and recovery failure. The callback in the first primitive case writes a marker through `os.WriteFile`.

The final suite replays the exact parent `accounting.go` through a standard Go overlay using the final regression sources. [Verified parent normal](2026-10-18-resource-count/raw/final/verified-red-normal.log), [verified parent race](2026-10-18-resource-count/raw/final/verified-red-race.log). Each fails for the same five specific reasons, without a build failure, native skip or race report. The filesystem fixture's setup was split into small helpers to meet the existing complexity limit; no quality baseline was raised.

## Change and contracts

`storeCharge` now contributes `max(len(settled), len(projected))` while a pending admission exists. Each store's maximum participates in the existing namespace sum before the effect. An admitted final count cannot exceed that pending count. Growth reserves its slot before publication; deletion retains its old slots until successful settlement or verified reconciliation; same-count replacement consumes no extra slot. Summing old and projected counts would incorrectly block legitimate operations at the limit, so that policy is explicitly covered by the controls.

The fixed native case verifies no bucket, no directory and a byte-identical ledger after refusal. It closes and successfully reopens the store, deletes a bucket from the other store, admits the formerly refused creation and verifies exact accounting against native scans. The primitive cases verify unknown growth/deletion across host reopen, subsequent verified release, shared concurrent admission and full-limit replacement/deletion both within one store and split across two stores.

The qualified coordination, held dependencies, request receipts, full-content resource versions, staging/recovery reservations and uncertain-publication behavior are unchanged. The error remains the existing `ErrFull`; S3 keeps its existing mapping. No old data is automatically rewritten or discarded. Formerly over-limit pending records can now fail capacity checking under the corrected calculation; this change adds no migration or reclamation path. Valid full-limit states and their cleanup/reopen paths remain supported and tested. This is enforcement of the existing ceiling rather than a new configurable limit.

## Railway cost measurements

All formatting, builds, tests, race/conformance, vet, quality, site/documentation checks and benchmarks run exclusively on the authorized scratch runner, deployment `43591a64-7c02-472f-8c6e-2efd25f76d10`. Go 1.25.6, Linux amd64, UID 10001, GOMAXPROCS 4, 4 CPU/4 GiB. The target service is unchanged.

Two benchmark families isolate the added counter check. `Admission` measures the real durable namespace API with 16 or 4,096 declared resources and an equal-count replacement, using a minimal successful callback. Its host ledgers live on ext4 `/results` (statfs 61267), explicitly verified after fixture construction; setup and binding are outside timing. This isolates admission bookkeeping and is not a native filesystem or S3 throughput benchmark. `Charge` measures the in-memory accounting fold with 0, 16 or 2,048 settled resources and one additional projected resource, checking the appropriate original/fixed result. It isolates the integer check rather than disk I/O.

Each API observation has ten timed operations; accounting observations use adaptive 200 ms calibration. Four original/fixed observations per cell use ABBA/BAAB order, with four observations per identical-original-binary control label C/D: **80 observations and 32 commands**. All samples, calibration counts, binary identities and ranges are retained. Go allocated bytes/objects are measured; peak RSS is not. Positive percentages mean more/slower.

| Entry point | Median time, original → fixed | Time change | Bytes/op, original → fixed | Objects/op, original → fixed | Same-binary control time change |
| --- | ---: | ---: | ---: | ---: | ---: |
| Admission/resources=16 | 8.425 → 8.168 ms | -3.05% | 40,224.5 → 40,225.5 | 245.5 → 245.5 | +2.55% |
| Admission/resources=4096 | 25.732 → 22.369 ms | -13.07% | 8,777,155.5 → 8,462,189.5 | 16,745.0 → 16,739.0 | +3.68% |
| Charge/settled=0 | 5.18 → 5.44 ns | +4.94% | 0.0 → 0.0 | 0.0 → 0.0 | +9.27% |
| Charge/settled=16 | 43.29 → 42.59 ns | -1.62% | 0.0 → 0.0 | 0.0 → 0.0 | -3.04% |
| Charge/settled=2048 | 4931.50 → 4866.50 ns | -1.32% | 0.0 → 0.0 | 0.0 → 0.0 | +2.16% |

[All measurements](2026-10-18-resource-count/raw/final/measurements.json), [analysis and ranges](2026-10-18-resource-count/raw/final/comparison-analysis.json), [fixture](2026-10-18-resource-count/raw/count-bench.go.txt). Every adverse sample is retained; noisy timing or tiny accounting deltas do not establish statistical significance. The admission-level timing and allocated-byte differences are not attributed to the integer check without further evidence. The direct accounting fold reports zero allocated bytes and objects in both binaries. No broad speed claim follows from this correctness change. Refused growth now avoids unsafe publication and pending-ledger changes. Valid-path persistence and scan costs remain; the prior scan improvement and expensive ledger/fence operations are not reimplemented here.

The native regression creates roughly four thousand real bucket metadata files per invocation, adding filesystem work to validation; its initial parent case takes about 2 seconds normally and 5 seconds under race detection on this runner. Production adds only the maximum calculation and no heap allocation machinery, extra persistence or new state field.

## Validation and next finding

All 22 final steps pass: formatting; quality/file-size/format ratchets; exact-parent normal/race red proofs; focused filesystem/capacity normal/race tests twice; full Go suite; broad capacity/storage/runtime/embedded/runthrough/S3/public-package race tests; four conformance profiles; vet; site build/tests; documentation links; and temporary benchmark installation/formatting, before/after builds, measurement and cleanup. The initial three-step expected-failure job also passes its harness checks. [Checks](2026-10-18-resource-count/raw/final/checks.json), [validation receipt](2026-10-18-resource-count/raw/final/validation-receipt.json), [fixed normal suite](2026-10-18-resource-count/raw/final/focused-normal.log), [fixed race suite](2026-10-18-resource-count/raw/final/focused-race.log). The six new top-level tests comprise seven leaf cases including the two at-limit controls. Existing uncertain-fence/recovery, allocation, held-bucket, multipart and shared-save admission tests remain green. Native permission tests execute as the unprivileged runner.

The temporary Go benchmark is removed. Committed source is verified against the exact Railway-formatted/tested bytes. Report/evidence receive additional Railway source-hash and documentation checks before scratch teardown.

The next correctness lead is the capacity ledger read bound: `Host.read` reads the whole file before enforcing its 8 MiB limit. It requires a deterministic allocation/refusal reproduction before any change. The measured remaining ledger persistence/fence costs and the earlier fresh-store unbound timing difference also remain performance work. No next finding is implemented in this change.

The [artifact manifest](2026-10-18-resource-count/raw/artifact-manifest.json) records the parent implementation, native and primitive proofs, every measurement, final validation and runner teardown evidence.
