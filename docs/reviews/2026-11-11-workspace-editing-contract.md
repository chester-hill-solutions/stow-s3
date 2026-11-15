# Native workspace file-save contract review

**Date:** 2026-11-11. **Disposition:** W26.B design accepted after source audit
and independent review. No editing implementation or runtime qualification.

The [contract](../contracts/workspace-editing-contract.md) and
[dependent batches](../design/workspace-resource-plan.md#w26-prerequisite-audit-and-execution-boundary)
extend the existing canonical queue. W26.A established that the workspace store
does not implement W02 guarded writes or durable request receipts. This review
settles the initial backend and recovery boundary before code is assigned.

Subsequent [C.1 implementation qualification](2026-11-11-workspace-editor-observations.md)
covers the owner/read foundation. This record retains the earlier design
disposition; save publication and recovery remain future work.

## Accepted decisions

| Decision | Reason and implementation consequence |
| --- | --- |
| One editor on the existing owning Workspace | Keeps one Session, Store, object index and Runtime accounting owner. A registry-only detached writer or second Runtime would split authority/accounting. |
| Primary natural paths only | Store-global managed-save capability would also advertise secondary buckets and escaped forms. Use a scoped internal Runtime admission path. |
| Shared quota and lock order | Reuse Runtime admission under lifecycle → registry mutation → capture → Instance.mu → Store.mu. Do not call public capture/runtime methods recursively under those gates. |
| Explicit host quiescence | Session liveness cannot fence direct filesystem, S3, Runtime or other-handle writers. The first guarantee covers managed clients sharing one editor. |
| Index is part of readiness | Capture walks files; natural object listings use index metadata. New bytes with an old entry cannot be reported as a coherent ready save. |
| Separate bounded workspace receipts | Reuse outcome semantics and atomic primitives; do not attach the FS object backend's journal format to natural files. |
| Conservative publishing ambiguity | A crash after rename but before durable published proof stays unknown. Matching bytes cannot prove which request published them. Pause admission and retain dependencies. |
| Recovery-aware owner bootstrap | Current Store initialization can rewrite index state before ownership/recovery checks. A qualified resume must preserve evidence and still expose resolution while ordinary writes are paused. |
| Coarse authority only; configured Policy unsupported | Workspace path read/write policy operations are not yet qualified. Do not treat object-key prefix policy as path authorization. |

The independent reviewer found no blocking contract contradiction. The main
implementation check is a single nonrecursive lock-acquisition path, including
Runtime quota/accounting and existing capture code. The final text explicitly
requires internal admitted helpers and contention tests for that boundary.

A focused consistency recheck confirmed that read-only owners may observe and
resolve receipts with retained read authority; saving requires current read and
write authority. This matches W02 ResolveSave and never resubmits payload. The
contract also distinguishes logical journal reservation from physical disk
headroom and permits index repair only from durable published evidence.

## Open qualification

The proposed native API and journal are not present in code. C.1 starts with
owner-bound read observations; C.2 publication stays internal until D.1 supplies
durable request semantics. D.2 must prove restart, initialization, capture and
cleanup barriers before MCP bindings qualify.

The initial design deliberately requires operator reconciliation for an ambiguous
publication window. It does not provide transactional multi-file writes, an
automatic abandon operation, physical disk-headroom guarantees, external-writer
fencing, protected multi-tenant policy, portable receipts or Linux/second-host
qualification. These limits cannot be closed by compilation or same-machine
fixtures.

This turn changed planning/contracts and recorded a separate read-only model
attempt. The previously qualified native artifact is unchanged. Documentation
links and whitespace are checked; no new native tests or conformance runs are
claimed for a design-only change. Implementation will run nearby fault/race
tests, independent review and the existing integrated gates on frozen sources.
