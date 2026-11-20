---
status: accepted
decision_digest: 7fb3f53b91b6b05a
amended: 2026-11-19
relates_to: 0008, 0009, 0014
---

# A file-save journal is a state, not a lockout

## Context

Workspace file editing writes a journal under `.stow/file-saves`. A save records
a pending record, publishes the payload, writes a terminal receipt, and clears
the pending record. Recovery resolves a pending record; the barrier in
`CheckWorkspaceFileSaveBarrier` already answers the safety question the journal
exists for: does the journal belong to this workspace, and is any record still in
doubt?

`workspace.New` did not ask that question. It refused whenever the journal
directory was present, so a workspace that had completed an editing session was
refused by every later `workspace resume`, `workspace serve` and
`workspace destroy`, with `workspace file observation unsupported`. The only way
back was to remove the journal by hand, which is exactly the intervention a
durable record is supposed to make unnecessary. The mutation gate had the same
shape: a store that had not loaded the journal reported the journal as
unsupported rather than as settled.

The effect was a seam failure between two surfaces that were each correct alone.
Editing worked; lifecycle worked; editing followed by lifecycle did not. Nothing
walked both surfaces in one test, so the composition was never exercised.

## Decision

Opening a workspace classifies its file-save journal instead of inferring from a
directory name. The classification is the barrier's: absent, intact, or blocked.

- **Absent**: open as before. No journal exists.
- **Intact**: open. The journal's identity matches the workspace, the directory
  holds only journal documents, and no pending record exists. The journal is
  durable evidence of saves that already settled, not a refusal.
- **Blocked**: refuse the open with `ErrWorkspaceFileBlocked`. Anything that
  cannot be shown intact is blocked, including a matching identity with a pending
  record, a foreign identity, an aliased or symlinked journal directory, a
  malformed document, and a journal with no manifest behind it.

Object mutations on an open workspace apply the same rule when the handle has not
loaded the journal: an intact journal permits ordinary object work, and anything
else refuses every mutation, read, listing and destroy. A handle that has loaded
the journal keeps its existing pending-record refusals.

An editing session that opens a workspace holding an intact journal joins it
instead of refusing the workspace it was asked to edit. The second editing
session of a workspace is an ordinary event.

Blocked state is resolved by the existing recovery surface: an MCP editing
session started with `--recover-file-saves` resolves a pending record and writes
its terminal receipt. No journal document is deleted to make a workspace openable,
and no new lifecycle verb is added for it.

The refusal names recovery rather than unsupported observation, because the
workspace is not incapable of workspace files: it is refusing to guess about one.

## Consequences

A workspace edited once is resumable, servable and destructible afterwards. A
workspace interrupted mid-save still refuses all of those until the pending
record is resolved, which is the guarantee the journal exists to give.

`workspace.New` does more work than a directory check: it opens the root, reads
the manifest and decodes the journal when one is present. The cost is bounded by
the journal's existing limits and is paid once per open.

The constructor and the barrier can no longer disagree about the same journal,
because there is one predicate instead of two.

A blocked journal is a state an operator can be told about but not one they can
silently clear: removing `.stow/file-saves` still destroys evidence of a save
whose effect was never established, and remains the wrong answer for it.

## Amendments

- 2026-11-19: Accepted. Replaces directory presence as the constructor's test
  with the barrier's absent/intact/blocked classification, and has an editing
  session adopt an intact journal. `workspace recover` was considered and not
  added: `mcp --workspace-edit --recover-file-saves` already resolves the blocked
  case, and a second entry point would be a second implementation of one
  decision.
