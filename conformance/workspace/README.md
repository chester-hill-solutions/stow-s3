# The workspace contract

`cases.json` is one file describing what the fifteen `stow-s3 workspace` verbs do,
and three drivers read it:

| Driver | Runs the contract against |
|---|---|
| `conformance/workspace_contract_test.go` | the `stow-s3` binary, in a child process |
| `packages/stow-s3/test/workspace-contract.test.ts` | the TypeScript wrapper |
| `packages/stow-s3-py/tests/test_workspace_contract.py` | the Python wrapper |

All three assert the same declarations. A client cannot disagree with the engine
about what a verb returns without one of them failing.

## Why it exists

The two client wrappers had never been run against the binary they wrap. They were
wrong in the same way, in both languages: `workspace handoff --output` writes the
handoff document to the named path and prints nothing, and both wrappers decoded
stdout unconditionally, so asking for a path produced a JSON parse error and the
caller never received the document it had just asked the CLI to write.

The two wrappers were green at the time. Each had a test double that answered with
a contract the production code did not have — a bare string where the real
resolver returns a `ResolvedBinary`, and a fake binary that printed for every
command including the one that prints nothing. The wrappers were out of CI
entirely, which is the only reason it took a divergence between two lines of
history to surface.

A surface that was wrong on its first automated run should be assumed to be wrong
in ways nobody has looked at yet. That assumption is what this file exists to make
cheap to check.

## What the Go driver found that the wrappers had not

Writing a third driver from the CLI's observable behaviour reproduced the same
bug a third time: it decoded stdout for `handoff --output` and got an empty
object. Three independent implementations making one mistake is what makes this a
contract rather than a convention.

It also settled four properties of the engine that no test stated, each of which
had to be read out of the code before it could be written down:

- **A team's partition is a directory boundary, not a label.** A checkpoint filed
  under `teams/<name>/` is invisible from the registry root, so a delta across two
  teams cannot be attempted at all — the checkpoint is simply not found. The
  `crosses workspaces` refusal is reachable only between two workspaces inside one
  partition. Both boundaries are real; only one of them is a refusal with a
  message, and the contract says so rather than implying the other is a softer
  version of the same thing.
- **A handoff reference names a workspace; it does not transfer its identity.**
  `resume --handoff` returns the workspace the document names, in the registry the
  document recorded. `adopt` builds a *different* workspace from the archive. Both
  are asserted, because a caller assuming they returned the same id would resume
  the sender's workspace instead of the receiver's.
- **`handoff` has two document versions, and the archive decides which.** Without
  `--archive` the reference is local (version 1); with it, portable (version 2).
  Only the portable form is meant to cross a machine boundary.
- **`resume` refuses `--handoff` alongside `--registry-dir`.** A caller that could
  point a handoff at a different registry would resume a workspace somewhere the
  document never named, so the refusal is a safety property and is asserted rather
  than worked around.

## How a step is shaped

```json
{
  "id": "a sentence a reviewer can check against the code",
  "verb": "checkpoint",
  "registry": "a",
  "team": "platform",
  "args": { "id": "{{ws}}", "parent": "{{cp1}}" },
  "capture": { "cp2": "checkpoint_id" },
  "expect": {
    "parent_checkpoint_id": { "equals": "{{cp1}}" },
    "files": { "equals": 3 }
  }
}
```

- `id` is prose on purpose. A step that reads as a claim is one a reviewer can
  check against the code; the subtest name is derived from it.
- `{{...}}` in `args` and in expected values resolves against the captures and the
  work directory. A reference to a capture no earlier step produced is an error, not
  an empty string: a silently empty argument is a scenario that quietly stopped
  testing what it says it tests.
- `registry: "a"` is the driver's own directory. `default` is whatever the binary
  resolves with no `--registry-dir`, isolated under a temporary home — `adopt`
  takes no `--registry-dir`, so without that the run would write into a
  developer's real `~/.config`.
- `fail` asserts a refusal. It asserts both that the verb failed and that it said
  why, because a step expecting a refusal that gets a success means the guard the
  product relies on is not there.
- `write` changes the workspace between checkpoints, the way an agent editing a
  file would. The checkpoint after a write is what proves the write happened, so a
  `noop` step asserts nothing itself.
- `alsoWritten` asserts a file the verb was told to write. `returns: "file"` then
  asserts the file and the returned value are the same document; omitting it
  asserts they are *different*, which is what `delta` does — it writes the
  transported document to the path and prints its own result describing it.

### Matchers

`equals`, `notEquals`, `matches` (regex), `length`, `minLength`, `min`, `absent`.

Exact where the contract states an exact fact, loose only where an exact value
would encode the toolchain: the archive's byte count is a gzip file's size and
moves with the Go version, so it is bounded rather than pinned.

`read` steps are the one place a key is not a field path. Their keys are file paths
and are taken literally, because a workspace is full of names containing dots —
`seed.txt` is a file, not a field called `seed` inside a field called `txt`.

## Running it

```sh
go test ./conformance/ -run TestWorkspaceContractHoldsInTheCLI -v
```

The scenario is one linear pass: the steps share a registry and a work directory,
so a step cannot be run alone. A failing step stops the run rather than reporting
the same missing workspace as a dozen later failures.

## The substitution the document digest exists to stop

The digests inside a delta cover its **content bytes and nothing else**. Nothing in
the document binds the change list — the path a change names, its kind, its from/to
metadata — to the sender's intent.

So take a document that adds `notes.txt`, and rename the addition to
`planted.sh` in the change list and the content map together. The result is
entirely self-consistent: the content digest still matches the bytes, and the
precondition passes because `planted.sh` is absent from the base. Applied, it
publishes a valid checkpoint holding a file the sender never named, and reports
success. Verified against the binary: two checkpoints from one apply, same
timestamp, one holding `notes.txt` and one holding `planted.sh`.

This is the same class of check the handoff archive path already had. `delta`
reports the document's `sha256`; the receiver hands it back as `--expect-sha256`
and `apply` refuses a document that does not hash to it, before the base is loaded
and before anything is staged.

Two contract steps pin it, and the pair matters:

- **A substituted destination still applies when the receiver is not told the
  digest.** This asserts the substitution *succeeds*, deliberately. It is what
  makes the next step mean something — a refusal test that has never seen the
  vulnerable path cannot tell a working check from a check that fires for an
  unrelated reason.
- **A substituted destination is refused when the digest is supplied.**

That first step is also why the check is opt-in. A receiver with no trusted copy of
the digest cannot invent one, so failing closed would make `apply` unusable rather
than safe. The honest position is that the digest is the only thing covering the
change list, a receiver that can compare should, and the contract now says so
rather than leaving it implied.

## The limitation worth stating

This covers the `workspace` verbs, which is where the wrappers were wrong. It does
**not** cover the ephemeral session surface, and it cannot yet: the TypeScript and
Python clients select *different* stores for one session (`memory` and
`filesystem` respectively), so a shared session contract would be asserting two
different systems agree. That is a real finding about the clients, recorded rather
than papered over, and it is a prerequisite for extending this file to sessions.
