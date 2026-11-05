# Architecture decisions

Accepted and narrowed records govern design. Proposed records do not.
An accepted design can still have unfinished implementation.
Use the [usage guides](../README.md) for supported APIs and the
[storage plan](../storage-foundation-plan.md) for remaining work.

## Current records

| ADR | Decision | Status | Supersedes | Narrowed by |
| --- | --- | --- | --- | --- |
| [0002](0002-sdk-compatibility-and-mirror-writes.md) | SDK compatibility profile and mirror-writes | `narrowed` | — | 0005 (section 5 only) |
| [0003](0003-embedded-runtime.md) | Embedded runtime and capability profiles | `accepted` | — | — |
| [0004](0004-agent-session-contract.md) | Agent session contract | `accepted` | — | — |
| [0005](0005-live-write-requires-explicit-consent.md) | A policy is not consent to write upstream | `accepted` | 0002 (section 5, only as to what constitutes live-write consent) | — |
| [0006](0006-owned-data-directory-reset.md) | A reset deletes only a data directory stow created | `accepted` | — | — |
| [0007](0007-workspace-is-the-default.md) | The workspace is the default; S3 is an opt-in facade | `accepted` | — | — |
| [0008](0008-workspace-backend-real-files.md) | A workspace stores objects as real files, with metadata in one manifest | `accepted` | — | — |
| [0009](0009-workspace-outlives-process.md) | A workspace outlives the process that created it | `accepted` | — | — |
| [0010](0010-an-enforcement-site-or-not-a-permission.md) | A permission that is not checked is not a permission | `accepted` | — | — |
| [0011](0011-local-is-the-default-mode.md) | Local-only is the default; ambient credentials do not select an upstream | `accepted` | 0001 | — |
| [0012](0012-run-through-serves-only-its-own-buckets.md) | A run-through proxy serves the buckets it was given, not the ones upstream has | `accepted` | — | — |
| [0013](0013-task-session-attempt-artifact.md) | Task specification, session, attempt, and artifact are four objects | `narrowed` | — | 0014 |
| [0014](0014-storage-product-caller-owned-execution.md) | Stow owns portable working storage; execution remains caller-owned | `accepted` | — | — |
| [0015](0015-durable-workspace-notifications.md) | Stow owns durable workspace notifications | `accepted` | — | — |
| [0016](0016-scoped-access-encrypted-cache.md) | Scoped access and encrypted storage support protected cache deployments | `accepted` | — | — |
| [0017](0017-workers-profile-qualifies-platform-objects.md) | Workers qualifies platform objects as Stow's workspace substrate; execution stays caller-owned | `proposed` | — | — |
| [0018](0018-snapshot-isolation-enforced-egress.md) | Isolation by snapshot and enforced egress, not by asking agents to cooperate | `accepted` | — | — |
| [0019](0019-browser-project-content.md) | Browser projects use durable content references and the shared embedded runtime | `accepted` | — | — |
| [0020](0020-binary-object-records.md) | Filesystem objects use protobuf metadata and raw payloads | `accepted` | — | — |
| [0021](0021-remote-exposure-requires-consent.md) | A bind beyond loopback needs consent, and the service does not terminate TLS | `accepted` | — | — |

## Status vocabulary

- `accepted`: the decision is in force.
- `narrowed`: the remaining decision is in force; a later record changes part of it.
- `proposed`: the decision has not been accepted.
- `superseded` or `rejected`: the decision is no longer active.

## Retired decisions

Fully replaced records can be removed from the working tree once their replacement
is in force. Keep the ID, reason, replacement and immutable Git source in
[retired.json](retired.json); never reuse a retired number. This removes obsolete
instructions without losing the history of the decision.

| ADR | Retired decision | Replacement | Original record |
| --- | --- | --- | --- |
| 0001 | Automatic run-through selection from ambient credentials | [0011](0011-local-is-the-default-mode.md) | [Git history](https://github.com/chester-hill-solutions/stow-s3/blob/302900cf422439f4d4082d633dce2a22e79837c4/docs/adr/0001-auto-detect-run-through.md) |

## Maintaining records

1. Number new ADRs sequentially, without reusing retired IDs. Add a status,
   `decision_digest:` and an `## Amendments` section, then list the record here.
2. For a decision amendment, add a dated reason under Amendments and update
   `amended:`. Record changed decision prose with
   `node scripts/check-adr-index.mjs --write-digests`.
3. Keep supersession reciprocal while both records are present. For a removed
   record, the retirement register and its replacement carry that relationship.
4. Run `make check-adr-index check-doc-links`. The checks verify decision digests,
   retirement references, index status and local documentation links.
