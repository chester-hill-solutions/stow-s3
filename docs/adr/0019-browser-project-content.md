---
status: accepted
decision_digest: 7b9729ef35512f6d
relates_to: 0003, 0014
---

# Browser projects use durable content references and the shared embedded runtime

## Context

The original browser profile replays all payloads into a memory runtime and stores complete file revisions. That profile remains useful for small working sets, but full replay, base64 transport and duplicated asset history make large file tools expensive.

## Decision

Add host-backed asynchronous object storage below the existing Go runtime. Native and WASM callers share lifecycle, quota, authorization and conditional-write behavior. Browser hosts supply immutable content and transactional metadata, and unsupported host operations remain explicit. Binary and streaming transport are versioned additions to the existing bridge.

Add a project facade over those content primitives. Immutable project revisions name files and assets by content references. Related file changes and the history cursor commit atomically. Project sync persists an attempt, publishes missing verified content and conditionally publishes the selected manifest. The caller owns rendering, edit semantics, credentials and collaboration.

The existing memory/browser APIs remain compatible. The new persistence profile uses a separate versioned database, namespace ownership and explicit migration. Migration retains the source and refuses uncertain legacy remote saves until they are reconciled through their original contract.

## Consequences

Large projects can retain unchanged assets without duplicating their payloads. On-demand reads and bounded streaming replace full replay; complete-object convenience APIs still materialize bytes. Quotas distinguish durable content, descriptors, bookkeeping and host-managed physical overhead. Garbage collection protects retained revisions and unfinished work, and a new exclusive opener releases only dead-handle read protections.

This extends the later-profile direction in ADR 0003. It does not turn object storage into an editing or collaboration engine, and it does not add filesystem workspaces to hosts without a filesystem.

## Amendments

None. Recorded at acceptance.
