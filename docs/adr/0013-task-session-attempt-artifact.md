---
---
status: narrowed
narrowed_by: 0014
amended: 2026-09-29
decision_digest: 6ea536364b26dfcd
relates_to: 0004, 0007, 0009, 0010
---

# Task specification, session, attempt, and artifact are four objects

**Scope narrowed, 2026-09-29:** [ADR 0014](0014-storage-product-caller-owned-execution.md) keeps the conceptual distinctions below but defers Stow-owned execution schemas, attempt identities/state machines, supervisors and mandatory attempt history. They are not prerequisites for storage or caller-triggered checkpoints. The original execution design is retained below for reference; implementing it requires a new product decision. The current input-preparation manifest remains distinct from this proposed execution TaskSpec.

This ADR fixes the vocabulary the execution layer in
[`work-session-plan.md`](../work-session-plan.md) E1 is built on. It exists because
the plan's own first exit condition is that the schema and state machine are settled
before a runner is written, and because the plan's most expensive mistake to avoid is
inventing a third identity alongside the two that already exist.

## Context

Stow already mints two kinds of identity, and they are not the same kind of thing:

- a **workspace** ID is `ws_` plus 16 hex characters — 8 random bytes
  (`internal/storage/workspace/workspace.go:153`, `var id [8]byte`);
- a **checkpoint** ID is `cp_` plus 24 hex characters — 12 random bytes
  (`pkg/stow/checkpoint.go:206`, `var id [12]byte`, and length-checked at
  `pkg/stow/checkpoint.go:427`).

Both are opaque, unguessable, and generated at creation. A workspace is a durable
directory plus a registry record; a checkpoint is a point in that directory's history.
A scoped S3 session is a third thing again, with a lifetime tied to the process that
created it and no durable identity at all.

An execution attempt wants to be a fourth. The temptation is to mint `at_` and be done,
and that would produce two problems: an attempt would have no defined relationship to
the workspace it runs against, so "resume" and "retry" would each need their own
special case; and the identity would carry no information about which contract version
produced it, so a record written by a future version would be indistinguishable from
one written now.

The repository has already been bitten by a document that was well-formed and
unversioned: the delta document grew a `version` field, and the task manifest is
version 1 with a strict decoder that refuses unknown fields. Those are the right
precedents and this ADR follows them.

## Decision

### Four objects, and the relationship between them

```
TaskSpec  ──►  Session  ──►  Attempt  ──►  Artifact
 (immutable)  (durable)    (bounded)    (declared outputs)
                 ▲             │
                 └─────────────┘  an attempt may checkpoint; a checkpoint may be an
                                  attempt's recorded state, but is not the attempt
```

**TaskSpec** is an immutable, versioned description of one unit of work: declared
inputs, working directory, argv, environment allowlist, secret *references*, timeout,
output paths, requested capabilities. It contains no secret values and no implicit host
environment. It is content-addressed, so the same specification run twice has the same
digest and can be recognised as the same work.

**Session** owns durable state. It is the workspace, plus the attempt history recorded
against it. A session does not imply a process and a process does not imply a session;
this is ADR 0009's rule ("a workspace outlives the process that created it") applied
one level up.

The name collides with ADR 0004's *agent session*, and deliberately so: that is the
scoped S3 session a caller receives, which has a lifetime tied to the process that
created it and no durable identity. Keeping one word for both is a hazard, and the
distinction is the whole point — a session here outlives its process, a session in ADR
0004 does not. If that is too sharp a distinction to carry in one word, the durable one
should be renamed before code is written, not after.

**Attempt** is one invocation against a session. It has its own opaque ID — `at_` plus
24 hex characters, matching the *checkpoint* generator, which is the longer of the two
— and its own state machine:

```
created ──► running ──► succeeded
                │  ├──► failed
                │  ├──► cancelled      (asked for, and stopped)
                │  ├──► timed_out
                │  └──► interrupted    (the evidence stopped making sense)
```

`interrupted` is not a failure state and not a success state. It means the record says
`running` and the process is gone, or the host restarted, or a write was cut. An
attempt in `interrupted` is **never** reported as `succeeded`, and never silently
reconciled into one. This is the rule the plan states and it is the one most likely to
be eroded by convenience.

**Artifact** is a declared output collected from an attempt, with a digest, a size, and
the attempt that produced it. Artifacts are separate from logs and from internal
workspace metadata, because a reviewable output and an operational log have different
retention and different redaction rules.

### Identity

Attempt IDs follow the existing shape rather than inventing a scheme: `at_` plus 24 hex
characters from 12 CSPRNG bytes, generated at creation and never derived from anything
the caller controls. An attempt ID is not a function of its argv, its workspace, or its
retry index, because that would make it forgeable and would let two callers collide on
a name they both chose.

A **retry is a new Attempt ID**, always. This is the plan's "never infer this choice"
rule, resolved: there is no way to express "the same attempt, again". What a retry
*means* — the same workspace, a fresh workspace seeded from the same TaskSpec, or an
explicitly selected checkpoint — is a field on the new attempt, and it is required
rather than defaulted. Stow will not guess, because the three have different data
implications and a wrong guess is a silent overwrite.

### Versioning

Every persisted or transported document carries an explicit integer `version`, and the
reader refuses a version it does not implement. It does not guess, and it does not
migrate on read. The precedents are the task manifest (version 1, strict decoding,
unknown fields refused) and the delta document.

**An attempt record additionally carries the TaskSpec digest and the contract version
that wrote it**, so a record from a future version is recognisable as such rather than
being misread as a current one with missing fields. This is the difference between a
version field on the document and provenance on the record, and only the second makes
an old record safe to encounter.

### No secret values in durable records

A TaskSpec persists secret *names* or provider references and the fact that a value was
injected. It never persists the value. Attempt records persist the set of environment
keys that were passed, not their contents. Logs are redacted on the way in, because a
record written correctly and a log written correctly are different code paths and only
one of them is under the author's control at write time.

This is stated here rather than left to E2 because it is a property of the *schema*,
not of the runner, and a schema that can hold a secret will eventually be asked to.

### Retry identity and attempt history

Attempts are recorded, not replaced. A session's history is an append-only list of
attempt IDs with their terminal states. Re-running a session does not overwrite the
previous attempt's record, its logs, or its artifacts, and it does not reuse its ID.

This costs storage and buys the property that matters: an operator asking "what
happened to this task" gets every invocation, including the one that failed and the
one that was retried after it.

## Consequences

- The execution layer gets no new persistence format for workspaces or checkpoints.
  A session *is* a workspace; an attempt is a record beside it in the registry. The
  risk this plan names — "avoid rebuilding the base layer behind a second session
  abstraction" — is answered structurally rather than by discipline.
- E1 must generate or contract-test the Go types and the CLI JSON from one definition,
  so the two cannot disagree. The shared corpus is the existing mechanism.
- The executor is a separate concern and gets its own ADR when E4 starts. What is
  fixed here is the interface's *shape*: narrow, one method to start, one to observe,
  one to cancel, and no method that implies containment.
- A caller cannot express "retry this exact attempt". That is deliberate, and it is
  the one place where the design makes something impossible rather than discouraged.
- Anything that would let a caller choose an attempt ID is out of scope for the same
  reason: an identity the caller picks is an identity the caller can collide.

## Considered Options

1. **One object: extend the checkpoint with execution fields** (rejected). A checkpoint
   is a point in a workspace's history; an attempt is an invocation. Merging them makes
   "which of the five runs produced this output" unanswerable, which is the first
   question an operator has.
2. **Derive the attempt ID from argv and a counter** (rejected). Deterministic IDs are
   convenient for humans and are forgeable and collision-prone for everything else.
   The existing generators are random for the same reason.
3. **Let a retry continue the previous attempt's record** (rejected). This is the
   "silently replace prior attempts" the plan forbids, and it is also the reading that
   makes a failed-then-succeeded run look like one success.
4. **Attempt inherits the workspace ID as its identity** (rejected). Then there is no
   way to have two attempts against one workspace, which is the normal case for any
   task retried once.

## Amendments

- 2026-09-29: Narrowed by ADR 0014 after the user selected portable working storage with caller-owned execution. Retained design vocabulary; deferred Stow execution obligations and removed them as prerequisites for checkpoint integrations.
