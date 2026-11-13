# Code review records

Dated reports retain their original findings and validation scope. Start with
the [correctness closure register](2026-10-18-correctness-closure.md) for its bounded
lead list, or the [architecture overview](2026-10-22-architecture-overview.md) for
the later structural assessment. These records do not replace the
[current work plan](../development/storage-foundation-plan.md).

Supporting logs, probes and source snapshots are available through the
[evidence archive](../evidence-archive.md).

## Reports

| Date | Report |
| --- | --- |
| 2026-11-12 | [Aboiteau cached-image consumer retry](2026-11-12-aboiteau-consumer-retry.md) — retry failed before the lifecycle stream after a main-container OOM event; retry and prior containers were later confirmed removed |
| 2026-11-12 | [Aboiteau cached-image consumer recheck](2026-11-12-aboiteau-consumer-recheck.md) — 21 checks passed, but the QEMU smoke failed during final observation and container cleanup remains unconfirmed |
| 2026-11-12 | [Direct Luna checkpoint continuation](2026-11-12-checkpoint-luna-continuation.md) — fresh same-host model read saved state and inputs through resource-only MCP after sender removal; broader W25 gates remain open |
| 2026-11-12 | [Workspace editing proxy diagnostics repair](2026-11-12-workspace-editing-proxy-repair.md) — bounded guard/upstream telemetry and corrected paid-fallback classification; no live-model qualification |
| 2026-11-12 | [Scope-aligned Free Router workspace editing attempt](2026-11-12-workspace-editing-scope-aligned.md) — stage one saved correctly, but a local proxy 502/retry caused strict trace rejection; no stage two ran |
| 2026-11-12 | [Free Router commit-gated workspace editing attempt](2026-11-12-workspace-editing-free-commit-gated.md) — both saves committed, but an extra stage-two read outside the stage inputs failed strict trace verification |
| 2026-11-12 | [Direct Luna workspace editing and continuation](2026-11-12-workspace-editing-luna-direct.md) — two fresh agents completed native MCP edits across an adopted checkpoint on one host; broader caller/platform gates remain open |
| 2026-11-12 | [Workspace editing required-read pilot](2026-11-12-workspace-editing-required-read.md) — second live attempt failed after one verified state read; no batch read/save |
| 2026-11-12 | [Workspace editing model pilot](2026-11-12-workspace-editing-model.md) — live model gate failed; strict MCP evidence rejected a schema-only response |
| 2026-11-12 | [Workspace editing CLI/MCP and SDK verification](2026-11-12-workspace-editing-mcp.md) |
| 2026-11-11 | [Native workspace save recovery verification](2026-11-11-workspace-editor-recovery.md) |
| 2026-11-11 | [Internal workspace durable save-request verification](2026-11-11-workspace-editor-durable-requests.md) |
| 2026-11-11 | [Internal workspace file publication verification](2026-11-11-workspace-editor-publication.md) |
| 2026-11-11 | [Native workspace editor observation verification](2026-11-11-workspace-editor-observations.md) |
| 2026-11-11 | [Native workspace file-save contract review](2026-11-11-workspace-editing-contract.md) — design only |
| 2026-11-11 | [Local checkpoint file resource qualification](2026-11-11-checkpoint-resources.md) |
| 2026-11-11 | [Workspace resource paging and host qualification](2026-11-11-workspace-resource-hosts.md) |
| 2026-11-11 | [Native workspace resource qualification](2026-11-11-workspace-resources.md) |
| 2026-10-22 | [Architecture review: historical observations and dispositions](2026-10-22-architecture-overview.md) |
| 2026-10-19 | [Workspace uncertainty and create-only overwrite — 2026-10-19](2026-10-19-workspace-uncertainty.md) |
| 2026-10-19 | [SigV4 repeated signed headers — 2026-10-19](2026-10-19-sigv4-repeated-header.md) |
| 2026-10-19 | [SigV4 query order follows the encoded names — 2026-10-19](2026-10-19-sigv4-query-order.md) |
| 2026-10-19 | [Opaque entity tags are not folded by case — 2026-10-19](2026-10-19-opaque-etag.md) |
| 2026-10-18 | [Admit durable saves before creating their intent](2026-10-18-save-admission.md) |
| 2026-10-18 | [Reserve pending namespace resource slots before effects](2026-10-18-resource-count.md) |
| 2026-10-18 | [Fence unchanged reconciled ledgers without replacing them](2026-10-18-reconcile-reuse.md) |
| 2026-10-18 | [Multipart body ownership — 2026-10-18](2026-10-18-part-ownership.md) |
| 2026-10-18 | [Outbox write phases on the Railway persistent volume — 2026-10-18](2026-10-18-outbox-write-phases.md) |
| 2026-10-18 | [Reject native outbox state reuse after writes — 2026-10-18](2026-10-18-outbox-write-cache.md) |
| 2026-10-18 | [Fence unchanged provenance without replacing the journal](2026-10-18-outbox-unchanged.md) |
| 2026-10-18 | [Refuse exhausted outbox fencing tokens — 2026-10-18](2026-10-18-outbox-token-exhaustion.md) |
| 2026-10-18 | [Copy only the requested outbox snapshot — 2026-10-18](2026-10-18-outbox-snapshot.md) |
| 2026-10-18 | [Reuse byte-identical validated outbox state — 2026-10-18](2026-10-18-outbox-reuse.md) |
| 2026-10-18 | [Outbox full-journal read and decode cost — 2026-10-18](2026-10-18-outbox-read-cost.md) |
| 2026-10-18 | [Allocate temporary protobuf provenance messages together — 2026-10-18](2026-10-18-outbox-provenance-messages.md) |
| 2026-10-18 | [Reject invalid outbox ownership credentials — 2026-10-18](2026-10-18-outbox-ownership.md) |
| 2026-10-18 | [Reject null legacy journal roots](2026-10-18-outbox-null.md) |
| 2026-10-18 | [Reject overlapping prepared-record migration — 2026-10-18](2026-10-18-outbox-migration-collision.md) |
| 2026-10-18 | [Keep lookup-before-insert duplicate checks](2026-10-18-outbox-insert.md) |
| 2026-10-18 | [Preserve protobuf outbox identities — 2026-10-18](2026-10-18-outbox-identities.md) |
| 2026-10-18 | [Validate outbox framing before whole-file allocation](2026-10-18-outbox-frame-read.md) |
| 2026-10-18 | [Keep compacting outbox state copies — 2026-10-18](2026-10-18-outbox-clone.md) |
| 2026-10-18 | [Bounded protobuf outbox decoding](2026-10-18-outbox-bounded-decode.md) |
| 2026-10-18 | [Bind outbox identities to entry IDs — 2026-10-18](2026-10-18-outbox-binding.md) |
| 2026-10-18 | [Refuse occupied outbox IDs at admission — 2026-10-18](2026-10-18-outbox-admission.md) |
| 2026-10-18 | [Native capacity fixture qualification — 2026-10-18](2026-10-18-native-fixtures.md) |
| 2026-10-18 | [Requalify multipart namespace admission](2026-10-18-multipart-admission.md) |
| 2026-10-18 | [Bound capacity ledger input before allocation](2026-10-18-ledger-read.md) |
| 2026-10-18 | [Measure ledger replacement and ancestor fences before optimizing](2026-10-18-ledger-fences.md) |
| 2026-10-18 | [Remove the capacity ledger checksum's full-input copy](2026-10-18-ledger-digest.md) |
| 2026-10-18 | [Incoming-write memory assessment — 2026-10-18](2026-10-18-incoming-memory.md) |
| 2026-10-18 | [Documentation inventory capture — 2026-10-18](2026-10-18-doc-inventory.md) |
| 2026-10-18 | [Bounded correctness closure register — 2026-10-18](2026-10-18-correctness-closure.md) |
| 2026-10-18 | [Remove duplicate payload copies from filesystem multipart completion](2026-10-18-completion-buffers.md) |
| 2026-10-18 | [Stream capacity-scan hashing instead of materializing payloads](2026-10-18-capacity-scan-stream.md) |
| 2026-10-18 | [Retain capacity until failed writes reconcile](2026-10-18-capacity-fence.md) |
| 2026-10-18 | [Admit filesystem bucket metadata before creating directories](2026-10-18-bucket-admission.md) |
| 2026-10-18 | [Stable API signature rendering — 2026-10-18](2026-10-18-api-reference-positions.md) |
| 2026-10-18 | [Reuse verified capacity usage within one mutation](2026-10-18-admission-scan.md) |
| 2026-10-17 | [Preserve unknown workspace multipart owners during recovery](2026-10-17-workspace-recovery.md) |
| 2026-10-17 | [Preserve committed workspace DELETE outcomes](2026-10-17-workspace-delete.md) |
| 2026-10-17 | [Avoid cache work for objects that cannot fit](2026-10-17-refresh-eviction-performance.md) |
| 2026-10-17 | [Reduce multipart listing allocations without weakening recovery](2026-10-17-part-listing-performance.md) |
| 2026-10-17 | [Store outbox snapshots as protobuf](2026-10-17-outbox-protobuf.md) |
| 2026-10-17 | [Refuse loss of an observed outbox journal](2026-10-17-outbox-missing.md) |
| 2026-10-17 | [Single-key outbox lookup allocation review](2026-10-17-outbox-lookup.md) |
| 2026-10-17 | [Reject zero-byte outbox journals](2026-10-17-outbox-empty.md) |
| 2026-10-17 | [Return outbox durability failures and measure journal save cost](2026-10-17-outbox-durability.md) |
| 2026-10-17 | [Preserve published filesystem completion outcomes after cleanup failure](2026-10-17-filesystem-completion.md) |
| 2026-10-16 | [Atomic workspace multipart part publication](2026-10-16-workspace-parts.md) |
| 2026-10-16 | [Bounded storage contract audit — 2026-10-16](2026-10-16-storage-contract-audit.md) |
| 2026-10-16 | [Correct filesystem multipart recovery accounting](2026-10-16-multipart-recovery.md) |
| 2026-10-15 | [Documentation routes validate without generated website output](2026-10-15-doc-links.md) |
| 2026-10-15 | [COPY quota review — 2026-10-15](2026-10-15-copy-quota.md) |
| 2026-10-15 | [Stow codebase map and immediate correctness review](2026-10-15-codebase-map.md) |
| 2026-10-15 | [Stow v0.3.2](2026-10-15-cleanup-release-proposal.md) |
