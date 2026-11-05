# Benchmark records

## Baselines and reproduction

- [Performance testing guide](../development/performance-testing.md): workload configuration and interpretation.
- [Benchmark harness](../../benchmark/README.md): build and remote measurement tools.
- [Session baseline](session-baseline.md) and [measurement data](session-baseline.json).
- [Local profile](performance-local.json) and [qualification profile](performance-qualification.json).
- [Railway environment](railway-environment.json).

## Published comparisons

The [October 19 marketing comparison](2026-10-19-marketing-comparison/README.md)
supports the current site dataset. It measured published **v0.3.3**, not v0.3.5.
The full result set stays in this tree. The earlier
[October 13 comparison](2026-10-13-competitor-performance/README.md) also retains
the dataset required by benchmark verification helpers.

Other dated reports remain below. Bulk historical outputs are recoverable from
the [evidence archive](../evidence-archive.md); they are not current performance guarantees.

## Investigation reports

| Date | Report |
| --- | --- |
| 2026-10-19 | [Refreshed marketing comparisons — October 19, 2026](2026-10-19-marketing-comparison/README.md) |
| 2026-10-16 | [Reject premature EOF in runtime streams](2026-10-16-stream-completeness/README.md) |
| 2026-10-16 | [Pair refreshed bodies with their captured metadata and upstream state](2026-10-16-runthrough-version/README.md) |
| 2026-10-16 | [Close failed run-through reads without losing the opening error](2026-10-16-runthrough-open-cleanup/README.md) |
| 2026-10-16 | [Remove copies of the buffered read result](2026-10-16-read-result-pointer/README.md) |
| 2026-10-16 | [Reject premature EOF in buffered reads](2026-10-16-read-completeness/README.md) |
| 2026-10-16 | [Close refused propagation source readers](2026-10-16-propagation-open/README.md) |
| 2026-10-16 | [Preserve prepared intents until local absence is reliable](2026-10-16-prepared-recovery/README.md) |
| 2026-10-16 | [Close provider readers when runtime opening fails](2026-10-16-open-error-cleanup/README.md) |
| 2026-10-16 | [Reject excess provider bytes in runtime reads](2026-10-16-excess-body/README.md) |
| 2026-10-15 | [Preserve marked read uncertainty in ReadForSave](2026-10-15-save-read-uncertainty/README.md) |
| 2026-10-15 | [Read timing investigation — no optimization retained](2026-10-15-save-read-call-boundary/README.md) |
| 2026-10-15 | [Distinguish opening absence from body-read failure](2026-10-15-save-body-failure/README.md) |
| 2026-10-15 | [S3 validation reconciles confirmed missing uploads](2026-10-15-s3-abort-recovery/README.md) |
| 2026-10-15 | [Reopen enforces the combined object-slot quota](2026-10-15-reopen-object-quota/README.md) |
| 2026-10-15 | [Reopen enforces the combined byte quota](2026-10-15-reopen-byte-quota/README.md) |
| 2026-10-15 | [Committed part uploads retain their identity and byte reservation](2026-10-15-part-publication/README.md) |
| 2026-10-15 | [Refuse ambiguous object HEAD before mutation](2026-10-15-object-head-uncertainty/README.md) |
| 2026-10-15 | [Missing custom-store uploads retain the S3 error contract](2026-10-15-multipart-validation/README.md) |
| 2026-10-15 | [Multipart completion preserves a committed publication](2026-10-15-multipart-publication/README.md) |
| 2026-10-15 | [Committed multipart initiation reserves its session and object capacity](2026-10-15-initiation-publication/README.md) |
| 2026-10-15 | [Filesystem listing: derive keys from record paths](2026-10-15-fs-listing-path-keys/README.md) |
| 2026-10-15 | [Custom multipart completion preserves committed results and cleanup accounting](2026-10-15-custom-multipart-publication/README.md) |
| 2026-10-15 | [Custom-store COPY preserves a committed result](2026-10-15-custom-copy-publication/README.md) |
| 2026-10-15 | [Core COPY correctness review — October 15, 2026](2026-10-15-core-copy-quality/README.md) |
| 2026-10-15 | [Conditional COPY source capture — October 15, 2026](2026-10-15-copy-source-snapshot/README.md) |
| 2026-10-15 | [Withdraw the generic COPY snapshot fallback — October 15, 2026](2026-10-15-copy-cost-restoration/README.md) |
| 2026-10-15 | [Runtime COPY adapters preserve committed identity](2026-10-15-copy-adapter-publication/README.md) |
| 2026-10-15 | [Retain multipart accounting after ambiguous completion lookup](2026-10-15-completion-lookup/README.md) |
| 2026-10-15 | [Checkpoint payload reuse](2026-10-15-checkpoint-payload-reuse/README.md) |
| 2026-10-15 | [Checkpoint capture characterization](2026-10-15-checkpoint-capture-characterization/README.md) |
| 2026-10-15 | [Buffered custom-store PUT preserves committed identity](2026-10-15-buffered-put-publication/README.md) |
| 2026-10-15 | [Custom-store batch deletion confirms missing keys](2026-10-15-batch-delete-missing/README.md) |
| 2026-10-15 | [Direct abort retry reconciles confirmed upload absence](2026-10-15-abort-retry/README.md) |
| 2026-10-15 | [Committed abort releases multipart quota exactly once](2026-10-15-abort-publication/README.md) |
| 2026-10-14 | [Further WASM size opportunities — Railway exploration](2026-10-14-wasm-size-exploration/README.md) |
| 2026-10-14 | [Owned HTTP body handoff — Railway evidence](2026-10-14-owned-http-body/README.md) |
| 2026-10-14 | [Memory write preparation outside the runtime lock](2026-10-14-memory-write-staging/README.md) |
| 2026-10-14 | [Memory HTTP PUT generator diagnosis](2026-10-14-memory-put-diagnosis/README.md) |
| 2026-10-14 | [Refreshed marketing comparisons — October 14, 2026](2026-10-14-marketing-comparison/README.md) |
| 2026-10-14 | [Read, quarantine, workspace and browser hot paths — 2026-10-14](2026-10-14-hot-paths/README.md) |
| 2026-10-14 | [Canonical embedded API and smaller WASM — 2026-10-14](2026-10-14-embedded-api/README.md) |
| 2026-10-13 | [Object-write preparation — October 13, 2026](2026-10-13-write-preparation/README.md) |
| 2026-10-13 | [WASM size regression — October 13, 2026](2026-10-13-wasm-size/README.md) |
| 2026-10-13 | [Usage and Recovery State consolidation — October 13, 2026](2026-10-13-usage-recovery/README.md) |
| 2026-10-13 | [Streaming and quarantine optimization — October 13, 2026](2026-10-13-streaming-performance/README.md) |
| 2026-10-13 | [Storage optimization — October 13, 2026](2026-10-13-storage-optimization/README.md) |
| 2026-10-13 | [Railway storage comparison — October 13, 2026](2026-10-13-railway-performance/README.md) |
| 2026-10-13 | [Memory performance rerun — October 13, 2026](2026-10-13-memory-performance/README.md) |
| 2026-10-13 | [Paired write-preparation comparison — October 13, 2026](2026-10-13-controlled-write-preparation/README.md) |
| 2026-10-13 | [Competitor performance — October 13, 2026](2026-10-13-competitor-performance/README.md) |
| 2026-10-12 | [Local storage performance evidence](2026-10-12-performance/README.md) |
