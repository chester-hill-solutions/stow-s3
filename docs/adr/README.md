# Architecture decision records

Sixteen decisions, in the order they were taken. This file is the index: it is where
to look to find out **what is in force**, and each ADR's own frontmatter is where to
look for what it decided.

Implementation ordering is owned by [the consolidated plan](../storage-foundation-plan.md), under ADR
0014. [The planning index](../planning-index.md) lists retired work orders; older
ADR references to a plan of record retain their historical context only.

An ADR is a sprint artifact. It records a decision taken at a time, against the
evidence available then, and decisions get revised. What is forbidden is a revision
that leaves no trace — an amended decision has to say so, here and in the ADR, and
`scripts/check-adr-index.mjs` fails the build if it does not.

## Status vocabulary

| Status | Meaning |
|---|---|
| `accepted` | In force. |
| `narrowed` | In force, with a later decision carving out part of it. |
| `superseded` | Replaced. Retained on disk for the reasoning, which is often the most useful part. |
| `proposed` | Written, not yet in force. |
| `rejected` | Considered and declined. Retained so the reasoning is not re-derived. |

A retired ADR is **not deleted.** The reasoning behind a decision nobody would make
today is still worth reading, and it is usually the reason the replacement is shaped
the way it is.

## Index

| ADR | Status | Supersedes | Superseded or narrowed by |
|---|---|---|---|
| [0001](0001-auto-detect-run-through.md) | `superseded` | — | 0011 |
| [0002](0002-sdk-compatibility-and-mirror-writes.md) | `narrowed` | 0001 (as an extension) | 0005 (section 5 only) |
| [0003](0003-embedded-runtime.md) | `accepted` | — | — |
| [0004](0004-agent-session-contract.md) | `accepted` | — | — |
| [0005](0005-live-write-requires-explicit-consent.md) | `accepted` | 0002 §5 | — |
| [0006](0006-owned-data-directory-reset.md) | `accepted` | — | — |
| [0007](0007-workspace-is-the-default.md) | `accepted` | — | — |
| [0008](0008-workspace-backend-real-files.md) | `accepted` | — | — |
| [0009](0009-workspace-outlives-process.md) | `accepted` | — | — |
| [0010](0010-an-enforcement-site-or-not-a-permission.md) | `accepted` (amended 2026-09-29: qualified Store retained) | — | — |
| [0011](0011-local-is-the-default-mode.md) | `accepted` | 0001 | — |
| [0012](0012-run-through-serves-only-its-own-buckets.md) | `accepted` | — | — |
| [0013](0013-task-session-attempt-artifact.md) | `narrowed` | — | 0014 (execution ownership and prerequisites) |
| [0014](0014-storage-product-caller-owned-execution.md) | `accepted` | — | — |
| [0015](0015-durable-workspace-notifications.md) | `accepted` | — | — |
| [0016](0016-scoped-access-encrypted-cache.md) | `accepted` | — | — |

## The drift this index exists to fix

ADR 0001 was superseded **in full** by 0011, and 0001's own frontmatter continued to
read `status: accepted` with no mention of it. The supersession was recorded only in
0011, so a reader who opened 0001 — the natural thing to do, since it is first — had
no way to know the decision was no longer in force.

That is the whole argument for two rules:

1. **Supersession is declared on both sides.** An ADR that has been replaced names its
   replacement, and the replacement names what it replaced. The gate fails if only one
   side does.
2. **The index is the entry point.** It is a generated-shaped file that a gate keeps in
   agreement with the frontmatter, so it cannot quietly fall behind the set.

## Amending an ADR

An ADR may be edited, and editing one is the normal case rather than an exception. What
the gate requires is that the edit be visible:

- add a dated entry under the ADR's `## Amendments` section saying **what changed and
  why** — a reader who disagrees with the change needs the reason more than the diff;
- bump `amended:` to the date;
- re-run `node scripts/check-adr-index.mjs --write-digests` to re-record
  `decision_digest:`, which is a hash of the decision prose;
- update the row in the table above if the status changed.

`decision_digest` is what makes the friction mechanical. Change an ADR's decision
without re-recording it and the gate fails with both hashes in the message. It is a
digest rather than a comparison against git history because a history comparison
cannot be validated in the commit that changes what it measures — every ADR would
differ from a parent that predates the digest, so the check would pass everything
exactly once and start working afterwards. A digest is self-contained and survives a
shallow clone.

The cost is four lines. The guarantee is that no reader can find a decision that
quietly became something else.

## Adding an ADR

Number it sequentially, give it `status:` and an `## Amendments` section, and add a
row here. The gate checks that every ADR is listed, that the listed status matches the
frontmatter, and that supersession agrees in both directions.
