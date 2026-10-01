---
status: accepted
amended: 2026-10-01
decision_digest: f563828e5c06a8d6
---

# A permission that is not checked is not a permission

This ADR records that milestone M1.3 was closed on incomplete acceptance
criteria, reopens it, and decides two questions it left open: what to do about
the four `authority.Operation` values that are defined, exported, and consulted
nowhere, and whether `pkg/stow` keeps a pluggable-store seam that has no
implementor at the time of acceptance. Sections 4–5 were revised on
2026-09-29 against the retained API and conditional-write evidence; the enforcement
decisions remain in force.

It does not change ADR 0002, ADR 0003, or ADR 0005. It makes ADR 0005's consent
boundary enforceable, which is the thing ADR 0005 assumed and the code did not
deliver.

## Context

Commit `25e958c` is titled *"M1.3: authority, enforced below every interface."*
M1.3's own acceptance criteria were four, and three do not hold:

| Criterion | State |
|---|---|
| `grep -riE "authoriz" internal/runtime/*.go` returns matches | Holds weakly — the only hit is a comment |
| An operation refused for a given `Authority` is refused identically through S3, native, and WASM | **Fails** — four of twelve operations are refused by no path |
| `DevBypass` cannot widen authority | **Vacuous** — no principal→authority mapping exists for it to widen |
| A test asserts the native path refuses what the S3 path refuses | Partial — two hand-kept copies of the matrix, agreeing by discipline |

`internal/authority` is good work and is not the problem: a closed twelve-value
operation set, a `uint32` mask chosen so authorization is not the most expensive
line in the hot path, fail-closed behaviour on an unknown operation, `All()`
derived from the list so adding an operation cannot silently narrow it, and the
attenuation rule expressed once. The problem is that four of the twelve values it
defines are never passed to `Allows`.

`EnvironmentDestroy` is the one that matters most. `internal/runtime/instance.go`
carries a comment stating that destruction "*is gated*".
`pkg/stow/workspace.go` calls `w.store.Destroy()` with no check. `ReadWrite()`
withholds `environment.destroy` — asserted by its own test — and the directory is
removed anyway.

`UpstreamWrite` matters second. `pkg/stow/authority.go` documents `ReadWrite()`
as "*an Authority for local work with no reach outside the environment: no
upstream*." The only gate on upstream propagation is
`internal/runthrough/adapter.go`'s `decideUpstreamWrite`, which collapses
Policy × AllowLiveWrites and cannot see the caller's grant. A caller who passes
`ReadWrite()` still gets local writes mirrored to a real bucket.

Two things make this a recurrence rather than an oversight. The commit before
`25e958c` is `1982104`, *"a multipart capability that was decorative."* And the
reason the parallel mechanism exists is structural, not careless:
`cmd/stow-s3/runtime_store.go` wraps the run-through adapter *inside* the runtime
instance, so the adapter sits below the chokepoint and cannot consult the
authority without a dependency cycle. The real gate is unreachable from where the
decision is made.

Nothing caught it because the gate that reports on this code has no test.
`tools/quality` produces every complexity, parameter, and `any` finding and
therefore decides what lands in `scripts/baselines/go-quality.json`; it has no
test file. `node --test scripts/*.test.mjs` matches one file against eight gates.
A milestone whose criteria are prose is not a gate.

## Decisions

### 1. A permission with no enforcement site is deleted, not documented as pending

Three of the four are enforced immediately: `EnvironmentDestroy`, `UpstreamRead`,
`UpstreamWrite`. `EnvironmentPromote` is **removed** from the operation set, from
`pkg/stow`'s re-exports, and from the `ReadOnly` and `ReadWrite` presets, and is
re-added by M4.3 with the operation it guards.

This is a deliberate departure from enforcing all four. Gating
`EnvironmentPromote` before `Promote` exists means asserting that a bit is
consulted by an operation with no call site — which is the exact shape of the
defect this ADR exists to close. ADR 0009 section 4 already records that
promotion's contract is decided and its implementation is blocked; the operation
set should match that state rather than anticipate it.

### 2. The authority is carried to where the decision is made

The adapter gains the `Authority` as a value at construction, so it can be
consulted without a cycle. `UpstreamRead` and `UpstreamWrite` become the single
upstream gate, and `Policy × AllowLiveWrites` becomes attenuation applied when the
instance's authority is built rather than a second, independent mechanism.
`RetryPending` is inside the gate: it currently bypasses even
`decideUpstreamWrite` and runs from the per-second retry worker and from the
admin route, both outside the runtime instance.

Configuration alone never manufactures authority. That is ADR 0005 and ADR 0002
invariant 5; this ADR only makes it reachable.

**How the value reaches the adapter, decided 2026-09-26.** Three shapes were
available and the one chosen is *one resolved value, constructed once, passed to
both*. The alternatives were a `func() Authority` the adapter reads live, and
inverting the construction so the adapter sits above the instance.

The live closure was rejected because a retry worker runs once a second: a gate
whose answer can change between the check and the use is harder to reason about
than a value, and a closure is not something a refusal-matrix table can assert on.
Inverting the construction was rejected as disproportionate — it inverts the
layering and restructures the CLI, and it is the shape that per-caller authority
(M3.1) will need, not the shape that environment-wide authority needs today.

What was implemented: the caller resolves the grant once, and
`Config.effectiveAuthority` folds `AllowLiveWrites` into it *at construction*.
The adapter stores the result and every upstream decision consults only that.
`AllowLiveWrites` is therefore read exactly once, in one place, instead of at every
decision — which is the substance of this decision. The two fields were previously
both consulted at the decision, which is what made them two mechanisms kept in
step by hand.

The authority is a field on `Config` rather than a sixth argument to
`NewWithOutbox`: the adapter is built from a Config, and a parameter past the
ceiling buys no behaviour. Its location is not the point; its being read in exactly
one place is.

One consequence worth recording. Because the grant is resolved at construction, the
enqueue and the retry can no longer disagree — the shape of R-201's original bug
is no longer reachable through configuration. The gate in the propagation funnel is
therefore defence in depth, and the test for it seeds an outbox entry directly
rather than producing one with a write.

### 3. The anti-recurrence test is part of the definition

A test asserts that **every** operation in `authority.Defined()` is enforced at a
chokepoint, documented as deliberately ungated, or recorded with the site that
enforces it elsewhere — with the justification in the definition for the ungated
case and in `authority.EnforcedElsewhere` for the third. Adding a decorative
permission fails the suite.

**Amended 2026-10-01.** The original decision was binary, and it had a third answer
it could not express. `workspace.capture` is enforced in `pkg/stow`, by
`Workspace.CreateCheckpoint`, because a workspace is a resource
`internal/runtime` has no verb for — the reach test derives its table from
`authority.Defined()` and lives in `internal/runtime`, so it has nothing to invoke.
The two available answers were both wrong: an `Ungated` entry would have asserted
the operation was unenforced while it was enforced, which is the exact false claim
this ADR exists to make impossible, and inventing a runtime verb for the sake of a
test would have put a permission where the enforcement is not.

So there are now three categories, and the third is machine-checked rather than
asserted: `authority.EnforcedElsewhere` names the enforcing site, and
`TestEnforcedElsewhereNamesASiteThatConsultsTheOperation` requires the file to exist
and to contain a call of the shape the enforcement scan already recognises. A named
site is a claim about a file the checking package cannot otherwise see, so an
unchecked claim would be a hole with a comment over it. The policy reach test
separately requires every recorded operation to appear in its own table, so the two
lists cannot drift.

The set is now thirteen operations, having been twelve when this ADR was written.
Of the four that were ungated at acceptance, three are enforced; `Ungated` retains
only `environment.promote`, which is not implemented. The historical findings above
are left as written: they record what was true when the decision was taken, and
rewriting them would falsify the reason for the decision.

One thing this ADR's anti-recurrence test does not do, and should not be claimed to:
it proves an operation has a gate, not that the gate is reached. That is the
separate refusal matrix, and the deliberate breaks that check each enforcement site
fails its own case when the check is removed or pointed at a different operation are
what make a gate's presence evidence rather than an absence of complaints.

This is the actual fix. The four missing enforcement sites are a symptom; a
permission set that can grow without anyone noticing is the disease. The
refusal matrix becomes one table driving both the runtime and S3 test suites,
which are today two hand-maintained copies.

### 4. `pkg/stow` retains a qualified pluggable store

**Amended 2026-09-29.** Keep `Options.Store`, `stow.Store` and `storeAdapter`.
The current API already exposes the seam. A public `Runtime` implements it, and
[conditional-write tests](../../pkg/stow/conditional_put_test.go) exercise a
supplied runtime through the adapter, including stale-write refusal, expected
absence and competing writers. Removal is no longer the best response to the
original fidelity concern.

Qualification is per operation and coordination profile. A custom store must
positively declare `ConditionalWriteStore.SupportsConditionalWrites()` before
conditional puts are forwarded. An undeclared store refuses conditional writes
before consuming the body; ordinary unconditional calls remain compatible.
Multipart retains its separate complete optional interface. A declaration is an
implementor's obligation, not proof of durability, cross-process coordination,
resource ACLs or managed version guards.

The original objections still bound what this seam promises. JSON-decoded WASM
options cannot inject a Go interface. The public adapter does not expose the full
internal checksum/list vocabulary, and `HeadBucket` scans `ListBuckets`; there is
no O(1) claim. Content ETags do not guard metadata-only changes or replacement
history. New stronger operations must have enforced refusal and separate evidence;
they cannot infer qualification from a backend label or conditional-write support.
The [shared admission contract](../storage-admission-contract.md) defines those
boundaries before guarded/scoped work extends them.

At initial acceptance, removal was selected because the seam had no identified
implementor and silently lost conditional/checksum options. That historical
rationale is retained here; positive capability refusal and a tested implementor
now provide a compatible remedy for conditional writes. Other gaps remain explicit.

### 5. Compatibility follows actual changes to the seam

**Amended 2026-09-29.** The removal and corresponding major-version change from
the original decision are withdrawn. Existing `Store` implementations remain valid
for legacy operations; no mandatory method is added to qualify them for a stronger
operation. Additive optional interfaces and capability fields must refuse unsupported
requests rather than silently weaken them. Legacy unconditional S3/public puts keep
their existing meaning.

If a later change removes a public symbol or adds a required interface method,
record and version that actual breaking change at that time. Retaining this seam
neither repairs every adapter gap nor certifies all S3 semantics.

### 6. The verification substrate is a prerequisite, not a follow-up

Testing `tools/quality` and the seven untested gates lands before M1.3 reopens.
The suppression-detection regex in `check-lint-ratchet.mjs` is deleted rather
than repaired, because it cannot match a multi-line `eslint-disable` and has
therefore been reporting suppressed violations as compliant. The three gates that
maintain separate directory lists consume one shared list, so the code that
decides whether the repository passes is inside the repository's own gates.

## Consequences

- **The milestone record changes.** M1.3 is reopened. Anything citing it as
  complete — M2.4, M3.1, M4.1 all declare it a blocking edge — was blocked on a
  milestone that was not finished.
- **`ReadWrite()` stops lying.** Its documentation becomes true, which is the
  only way a caller can rely on a preset.
- **`EnvironmentPromote` disappears from the public surface** until M4.3. Any
  code naming `stow.EnvironmentPromote` stops compiling. There is none in this
  repository.
- **The outbox gains a parameter.** `runthrough.NewWithOutbox` takes an
  authority. Every construction site is in this repository.
- **A future promotion must arrive with its gate.** M4.3 cannot land while
  `EnvironmentPromote` is absent and ungated; re-adding the bit is part of
  shipping the operation, not a follow-up.
- **The gate suite becomes load-bearing.** Until M0 lands, no milestone's
  acceptance criteria are mechanical, which is the condition that let this
  happen.
- **The plan of record moves.** `docs/architecture/environment-plan.md` is
  superseded by `docs/architecture/environment-implementation.md`, which absorbs
  it and adds requirement identifiers, a traceability matrix, an assumptions
  register, and a binding decision rule.

## Amendments

- 2026-09-29: Revised sections 4–5 to retain the existing custom-store seam with
  per-operation qualification and explicit unsupported refusal. The public Runtime
  implementor and conditional adapter tests provide the evidence missing at original
  acceptance. Withdrawn the planned removal/major-version change; sections 1–3 and 6
  retain their enforcement decisions. Corrected the duplicate frontmatter delimiter.
  W01 in the consolidated plan owns the shared admission contract.
- 2026-10-01: Amended section 3. The enforcement decision was binary — enforced at a
  chokepoint or documented as ungated — and `workspace.capture` needed a third answer,
  because it is enforced by `pkg/stow` where the reach test in `internal/runtime` has
  no verb to invoke. Added `authority.EnforcedElsewhere` as a machine-checked record
  of the enforcing site, rather than an `Ungated` entry that would have claimed the
  operation unenforced while it is enforced. The operation set is thirteen, not the
  twelve recorded in the problem statement above, and three of the four ungated
  operations are now enforced; `environment.promote` remains and is unimplemented.
  Sections 1–2 and 4–6 retain their decisions. Checkpoint authorization is W04.
