# Retain capacity until failed writes reconcile

**A failed post-publication directory fence discarded capacity for bytes already visible.** Mark filesystem writer/fence I/O errors with the existing `capacity.ErrUnknown`, so the existing admission mechanism retains its durable reservation until a verified native scan reconciles it. Production changes only the two error returns and success return in `writeCapacityFile`. No dependency, format, public API, cache, index or storage abstraction is added.

## Baseline and native proof

The branch starts clean at `c26db1ce32826f80fcce939bddebf32e98fdac73`. Fetching main returns unchanged `219d1dcfff9807b04660a40313b32127e5be2933`. Guarded-save admission, integrated multipart admission and their measured costs remain intact. Recent source, existing tests and the sibling branch/history were checked; this shared writer error classification is not repaired by the existing fixes.

`atomicfile.Write` explicitly permits visible replacement after a later fence fails. The namespace admission primitive withdraws an ordinary callback-error reservation, while retaining one marked `ErrUnknown`. The shared filesystem writer returned generic atomic-write/ancestor-fence errors, so it incorrectly selected withdrawal.

The regression uses actual native object/part methods and a real permission error, with no production hook, test overlay fault injector, synthetic backend or invented committed marker. The store lives under a separate ancestor directory. Mode 0300 keeps traversal/writes possible but prevents opening that ancestor for its required fence. The owned store remains readable, allowing preflight scans and atomic publication. The capacity host is outside the protected ancestor. Tests assert the exact native `PathError` (`open` on that ancestor), then consume the new bytes through object/part readers and reload the persisted host ledger.

A roughly **1.5 MB** visible record is charged as **zero** for a new write or only its old small size for replacement; staging is zero. Two other writes then succeed, bringing visible payload to roughly **3.5 MB against a 3 MiB allowance**. This reproduces for new/replaced objects and multipart parts, normally and under race detection. The tests restore permissions and verify both same-store retry and constructor/reopen recovery. No durability success is inferred from visible bytes.

Eight post-fence cases exercise those four publication shapes and two recovery paths. Two additional pre-rename permission cases verify the conservative policy: unchanged old bytes stay readable, the original permission error remains inspectable, and a verified old-state scan permits retry. Those two cases are policy/recovery coverage, not evidence that withdrawing a known-no-effect reservation was itself corrupting data.

[Original normal proof](2026-10-18-capacity-fence/raw/final/verified-red-normal.log), [original race proof](2026-10-18-capacity-fence/raw/final/verified-red-race.log). Exit 1 is expected and actual native failure messages are checked; the exact parent `capacity.go` is selected by an ordinary Go source overlay. The fault itself needs no overlay. [Fixed normal results](2026-10-18-capacity-fence/raw/final/focused-normal.log) and [race results](2026-10-18-capacity-fence/raw/final/focused-race.log) pass twice each, including all ten leaf cases without root/permission skips.

## Change and contracts

Both `writeBytesAtomic` errors and subsequent `syncSaveAncestors` errors are joined with the existing unknown-outcome sentinel. The existing durable pending admission remains in the host ledger. Same-namespace competitors must respect the projected payload and staging charge, including after reopening the host ledger. A native retry/reopen scans actual owned files and clears staging while charging exactly the observed data.

The generic atomic-writer interface does not certify absence of effect on error. It is therefore classified conservatively rather than parsing error strings or introducing a new phase API. A pre-rename I/O failure can retain an allowance even when no new bytes were published; a retry that reaches capacity admission, or reopening the store, reconciles actual files. A request rejected earlier by its preconditions does not perform that reconciliation. This can temporarily refuse other writers. A caller that abandons a failed store must reopen/reconcile it to release the reservation. The original error remains discoverable through `errors.Is`, and no `ErrMutationCommitted` is fabricated. Quota/precondition refusals before the effect and the admission primitive's known ordinary-error rollback are unchanged. Existing storage and S3 behavior remains otherwise intact.

The initial Railway source upload created a terminal failed deployment before any build. Its status/error/no-build evidence is retained; only after confirming it was terminal was unchanged source uploaded again. No Go verification ran locally.

## Railway measurements

All formatting, builds, tests, race/conformance, vet, quality/site/documentation checks and benchmarks run on the existing authorized Railway scratch runner, deployment `3ae259eb-72c0-43d9-9b8f-304f544305c1`. Go 1.25.6, Linux amd64, UID 10001, GOMAXPROCS 4, 4 CPU/4 GiB; writes use mounted ext4 `/results` (statfs 61267). The target service remains unchanged.

Benchmarks time fresh 64 KiB object/part publication, both successful and with the real ancestor-fence failure. Setup, permission changes, quota/outcome verification and cleanup are outside timing/allocation accounting. Each cell runs ten timed operations. Four original/fixed observations per cell use ABBA/BAAB order, plus four observations per label for identical-binary controls: **64 observations**. Binary hashes, commands, all samples and ranges are retained. These are synthetic operation measurements, not peak RSS, production throughput or statistical-significance claims. Positive changes mean more/slower.

| Publication | Median time, original → fixed | Time change | Bytes/op, original → fixed | Objects/op, original → fixed | Same-binary control time change |
| --- | ---: | ---: | ---: | ---: | ---: |
| part=false/fault=false | 20.604 → 17.793 ms | -13.64% | 228,978.0 → 228,239.0 | 679.5 → 678.5 | +16.23% |
| part=false/fault=true | 21.780 → 13.634 ms | -37.40% | 225,887.0 → 216,455.5 | 634.5 → 582.0 | -6.91% |
| part=true/fault=false | 19.163 → 17.201 ms | -10.24% | 159,681.0 → 159,920.0 | 655.5 → 656.0 | -5.66% |
| part=true/fault=true | 17.646 → 12.439 ms | -29.51% | 158,693.5 → 149,575.0 | 640.5 → 590.0 | -1.72% |

[All observations and binary identities](2026-10-18-capacity-fence/raw/final/measurements.json), [analysis and ranges](2026-10-18-capacity-fence/raw/final/comparison-analysis.json), [benchmark fixture](2026-10-18-capacity-fence/raw/fence-bench.go.txt).

Success performs the same writes and fences. Failure adds an error wrapper and deliberately omits the unsafe reservation-withdrawal ledger write; any observed failure speed/allocation reduction reflects this corrected state transition, not an optimization claim. Reserved capacity persists longer on ambiguous I/O failure, including conservative pre-rename failures. The previously measured multipart accounting/scanning costs remain; this change does not remove them or claim broader storage performance gains.

## Validation and next finding

All 22 final steps pass: the standard 14 gates (formatting; quality/file-size/format ratchets; focused normal/race tests twice; full Go suite; broad capacity/storage/runtime/embedded/runthrough/S3/public-package race tests; four conformance profiles; vet; site build/tests; documentation links), two original-source red proofs, and six benchmark setup/build/measurement/cleanup steps. The temporary benchmark Go file is removed. [Checks](2026-10-18-capacity-fence/raw/final/checks.json), [independent validation receipt](2026-10-18-capacity-fence/raw/final/validation-receipt.json). Report/evidence receive additional Railway documentation/source-hash checks before teardown; committed source is verified against the exact formatted/tested bytes.

The next correctness lead is **bucket metadata admission**: native bucket creation/removal still uses direct filesystem writes/removal instead of namespace admission. The earlier setup-ledger discrepancy points to this gap, but quota refusal, cleanup and S3 bucket visibility need native reproductions before implementation. The measured duplicate scans and multipart write amplification also remain performance work; no further production change is implemented here.

The [artifact manifest](2026-10-18-capacity-fence/raw/artifact-manifest.json) records source, native proofs, every benchmark observation, upload-failure provenance, final validation and teardown evidence.
