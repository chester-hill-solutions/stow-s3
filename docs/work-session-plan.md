# Stow work-session primitive: engineering plan

**Status:** accepted as the next product layer. Parallel axis, like
[`foss-readiness-plan.md`](foss-readiness-plan.md). It does not replace
[`plan.md`](plan.md), which remains authoritative for the workspace, run-through,
cache, and portability work already in flight.

**Scope:** engineering work only. This plan excludes package publishing, release gates,
marketing, external pilots, and adoption targets.

---

## Reconciliation with the existing axes

Recorded 2026-09-28, when this plan was added. The plan body below is unchanged from
how it was written; this section is the part that knows about the rest of the
repository.

### Where it sits in the precedence order

| Document | Governs |
|---|---|
| [`plan.md`](plan.md) | Workspace, run-through, cache, portability. **Canonical.** |
| [`foss-readiness-plan.md`](foss-readiness-plan.md) | Installability, the conformance matrix's missing backend, the contribution surface |
| **this document** | The execution layer: task specs, attempts, artifacts, executors, integrations |

The three do not compete. E0 below is `plan.md`'s backlog, restated as a precondition;
E1–E5 are new surface that must not begin until E0's exit conditions hold. Where this
plan and `plan.md` describe the same outstanding work, `plan.md` governs the ordering.

### E0 items already satisfied

Three of E0's bullets are done or partly done, and E1 should not re-open them:

- **"Reconcile specialist plans and stale handoff/status notes"** — done. All three
  plans now carry a status table with evidence, the canonical table's stale
  `MaxWorkspaces` row was corrected, and
  [ADR 0012](adr/0012-run-through-serves-only-its-own-buckets.md) was written for the
  run-through read-path decision that had only ever existed in a branch comment.
- **"Decide how standing registry limits and retention policy are represented"** —
  partly. `max_workspaces` ships and deliberately does **not** persist, because the
  registry is what is bounded and a cap stored on one entry would be a property of a
  workspace rather than of the policy that declared it. The standing-bound half is
  still open and needs a registry-level settings file. E1 inherits that decision, not
  the question.
- **"Treat provider-backed failure cases as engineering validation"** — now possible.
  The live-provider gate runs by hand and passes against Cloudflare R2; it was
  previously only satisfiable from inside the workflow that invokes it.

`docs/handoff-2026-09-27.md` is a stale handoff naming a different repository path
and a long-superseded commit. It carries a banner saying so. The general rule E0
states is the one worth keeping: a copied test count or commit hash is not status.

### Alignment with the accepted ADRs

The architecture constraints above restate existing decisions rather than adding to
them, which is the intent. Three are load-bearing for E2 and E4:

- **ADR 0005** — a policy is not consent. E2's requirement to keep upstream write
  consent independent from task execution is this rule again, one layer up.
- **ADR 0010** — a permission nothing checks is not a permission. E2's "name the
  enforcement point" rule is this, and the local-process executor is explicitly *not*
  an enforcement site for containment.
- **ADR 0012** — a run-through server serves the buckets it was given. E4's "the
  executor must not inherit upstream credentials" is the same boundary at the executor.

**ADR 0013 is E1's TaskSpec/Session/Attempt/Artifact decision**, and it is written. Its
load-bearing answer to the risk named above: an attempt is a fourth identity rather
than a third kind of the existing two, a session *is* a workspace rather than a new
container, and a retry is always a new attempt ID with its meaning stated rather than
inferred. One thing it deliberately makes impossible is expressing "the same attempt,
again" — identity the caller can choose is identity the caller can collide.

### One thing this plan gets right that the gates do not

"Do not use code coverage as a proxy for contract coverage."

There is a live example of why. The coverage floor is a single aggregate, and measured from the
profile, four packages sit below it — `cmd/stow-s3` at 56.4%,
`internal/s3api` at 69.1% — while the aggregate reads 71.06%. An attempt to add per-package floors was written, ran clean, and
was **reverted because it produced a baseline with zero per-package entries**: a gate
that appeared to work and did not. The right fix was never a better percentage; it is
E1's rule below, which is to track which public promises have positive, negative,
repeated-operation, and restart cases. `cmd/stow-s3` is the package a user executes
and it is the least covered, which a per-package percentage would have reported and
this rule would not.

---

Purpose: evolve Stow from durable workspaces and local object storage into a
reusable engineering primitive for CI jobs, developer tools, and agent tasks.

Scope: engineering work only. This plan excludes package publishing, release gates,
marketing, external pilots, and adoption targets. It does not replace plan.md, which
remains authoritative for the current workspace, run-through, cache, and portability
work already in flight. This document covers the next product layer once those
foundations are stable.

Architecture constraints: preserve the accepted ADRs: local behavior is the default;
upstream mutation needs explicit consent; a workspace is not a security sandbox;
handoffs contain no credentials; close and destroy have distinct semantics. Any
process isolation must be supplied by an executor that enforces it, not implied by the
workspace API.

## Engineering objective

Make a unit of work a first-class, durable object with a stable identity, declared
inputs, an execution record, checkpoints, inspectable outputs, and explicit cleanup. A
caller should be able to create that unit through a stable interface and operate it
from a CLI, CI runner, or agent integration without each caller inventing its own
lifecycle protocol.

The core relationship is:

```
Task specification → session → execution attempt(s) → checkpoints and outputs
```

The session owns durable workspace state. An execution attempt is a bounded event in
that session; retries do not silently replace prior attempts or checkpoints. Stow
records what it knows and makes output promotion explicit. Stow is not initially a
scheduler, hosted control plane, or autonomous agent framework.

## Current foundation and known gaps

The current repository already has durable same-machine workspaces, input manifests,
checkpoint and archive flows, CLI lifecycle commands, thin TypeScript and Python CLI
wrappers, local S3 interfaces, and explicit upstream-write controls. These are the base
layer; avoid rebuilding them behind a second session abstraction.

The remaining engineering gap is that callers prepare and manage a workspace, but Stow
has no canonical execution-attempt contract. There is no shared schema for a command,
runtime, environment policy, resource policy, logs, cancellation, retry identity, and
outputs. Workspace retention policy is largely supplied per operation; S3 and
workspace contracts still need ongoing compatibility coverage; execution-level
isolation and resource enforcement do not exist.

## Design rules

1. **Workspace and attempt are different objects.** A workspace is durable working
   state. An attempt records one invocation against that state. Starting an attempt
   must not implicitly create, delete, or promote a workspace.
2. **Go owns semantics.** Define behavior in Go packages and the CLI first.
   TypeScript and Python remain thin protocol clients until a stable execution
   contract exists.
3. **Version persisted and transported data.** Manifests, attempt records, events, and
   provenance need explicit versions, strict validation, migration/refusal behavior,
   and size limits.
4. **No secret values in durable records.** Persist secret names or provider
   references and the fact that a value was injected, never the value. Redact
   environment and command diagnostics.
5. **Name the enforcement point.** A policy is a guarantee only if an identified
   component enforces it. A local process runner is not a sandbox; a workspace quota
   does not constrain direct filesystem writes.
6. **Failure is part of the contract.** Define the observable state after process
   crash, host restart, timeout, cancellation, disk-full, partial artifact write, and
   retry.
7. **Prefer one small executor first.** Keep a narrow executor interface and prove it
   with a local implementation before adding container, remote, or provider-specific
   backends.

## Ordered engineering work

### E0. Close and reconcile the existing contracts

Purpose: keep today's workspace and S3 base trustworthy before adding execution
semantics.

- Keep plan.md as the work order for existing implementation gaps. Reconcile
  specialist plans and stale handoff/status notes when their state changes; do not let
  copied test counts or old commit hashes act as current status.
- Complete the workspace filesystem hardening that is still explicitly open:
  symlink/path race handling, no-follow behavior where supported, and clear guarantees
  for arbitrary writers. Preserve the documented limit when the host cannot provide a
  guarantee.
- Decide how standing registry limits and retention policy are represented and
  enforced across invocations; distinguish policy configuration from per-workspace
  metadata.
- Close meaningful S3 compatibility gaps by adding cases to the shared corpus before
  changing behavior. Prioritize operations required by actual application and CI
  workloads rather than chasing the whole AWS surface.
- Treat provider-backed failure cases as engineering validation: conditional writes,
  outbox retry/restart, multipart completion, and delete/copy conflict behavior. This
  item does not include user pilots or release activity.

**Exit conditions:** every current public claim maps to a contract and an adverse-state
test; current plans identify one authoritative status source; no unresolved
data-integrity or authority-boundary issue is being carried into the execution layer.

### E1. Define the task and execution-attempt contract

Purpose: give every caller the same vocabulary before adding a runner.

- Write an ADR for TaskSpec, Session, Attempt, and Artifact semantics, including
  identity, ownership, lifecycle, retry, and compatibility with existing workspace IDs
  and scoped S3 sessions. **Written:**
  [ADR 0013](adr/0013-task-session-attempt-artifact.md).
- Define a versioned task specification for declared inputs, working directory,
  command and arguments, environment allowlist, secret references, timeout, output
  paths, and requested capabilities. Do not put secret values or implicit host
  environment into the persisted document.
- Define attempt states and transitions, for example `created → running → succeeded |
  failed | cancelled | interrupted`. Record exit status, timestamps, executor identity,
  and checkpoint/output references.
- Define whether retry means a new attempt over the same workspace, a fresh workspace
  seeded from the same inputs, or an explicitly selected checkpoint. **Never infer this
  choice.**
- Define a versioned event/log protocol. Logs need bounded storage, redaction rules,
  and a way to retrieve output after the child process is gone.
- Establish resource accounting units and failure semantics. Stow must not advertise
  CPU, memory, disk, or network limits unless an executor can enforce and report them.

**State: the vocabulary is decided; nothing is built.** ADR 0013 settles the four
objects, the id lengths, and the rule that a retry is always a new attempt id with its
meaning stated rather than inferred. Every other bullet is untouched, and so is every
exit condition except the first. A reader arriving at this section from the ADR would
otherwise reasonably conclude the contract exists in code.

**Exit conditions:** schema and state machine are accepted in an ADR; Go types and CLI
JSON are generated or contract-tested from the same definitions; invalid or unknown
versions fail clearly; the contract specifies restart and retry behavior without
requiring a live process.

### E2. Implement a local execution engine

Purpose: connect the durable workspace to ordinary build, test, and agent commands
without claiming containment.

- Add a narrow Go `Executor` interface and a local-process executor. Invoke argv
  directly without a shell by default; make shell execution an explicit opt-in.
- Add CLI operations to start an attempt, inspect status, stream or retrieve logs,
  cancel, and resume/reconcile an interrupted attempt. Use stable attempt IDs and
  idempotent state transitions.
- Scrub inherited cloud and Stow configuration by default. Pass only declared
  environment values and narrowly scoped session credentials. Keep upstream write
  consent independent from task execution.
- Persist the command identity, working directory, input/checkpoint identity, selected
  environment keys (not values), start/end time, exit code, and output digests. Redact
  likely secret material from errors and logs.
- Implement process-tree cancellation and timeout behavior on supported platforms, with
  platform-specific tests. A timeout must not leave an untracked child process.
- Treat the executor process as having the caller's OS privileges. State this in
  `--help` and API docs; do not call this mode sandboxed.
- Add crash recovery for the gaps between recording `running`, starting the child,
  recording its exit, and capturing outputs. A recovered attempt should be explicitly
  marked `interrupted` or reconciled, **never silently reported successful**.

**Exit conditions:** a local command can be started, observed, cancelled, interrupted
and reconciled, and its output checkpointed through the CLI; repeated status/cancel
calls are safe; failure-injection tests cover process death and disk errors; no secret
values appear in persisted records or normal logs.

### E3. Make outputs and provenance first-class

Purpose: make work reviewable and safely reusable in downstream automation.

- Extend checkpoint metadata with a provenance record that links task-spec digest,
  input fingerprints, selected source revision where applicable, attempt ID, executor
  version, and output digests.
- Add declared output collection with path allowlists and explicit behavior for
  missing, excluded, oversized, or changing files. Keep outputs separate from logs and
  internal workspace metadata.
- Provide machine-readable inspection and diff output with stable field names and
  documented versioning. Make it possible to review outputs without restoring or
  executing them.
- Add an explicit promotion operation for selected outputs to a named local or S3
  destination. Reuse the existing live-write consent and conditional-write mechanisms;
  preview the write set and record the result. **No close, retry, or expiry path may
  promote implicitly.**
- Define integrity versus authenticity: content hashes detect accidental or untrusted
  mutation only when the expected digest is trusted; signatures/attestation require a
  separate key-management decision.
- Bound retained attempts, logs, checkpoints, and output payloads under an explicit
  registry policy. Keep cleanup ownership and adopted-workspace protections intact.

**Exit conditions:** a caller can trace an output to its inputs and attempt, compare
it, export it, and explicitly promote selected files; digest mismatch and concurrent
destination changes are refused; cleanup is tested with live, adopted, and
interrupted sessions.

### E4. Add an enforced isolated executor

Purpose: offer a real containment mode for untrusted agent or build workloads without
confusing it with workspace isolation.

- Keep the executor interface independent of a specific runtime. Implement one
  OCI-compatible backend first, selected by explicit configuration and capability
  discovery.
- Specify and test the security profile: non-root identity, read-only base image,
  writable workspace mount only, no host socket/device mounts, dropped capabilities,
  resource ceilings, process-tree cleanup, and network disabled by default.
- Add explicit egress policy only through a mechanism the selected runtime/platform can
  enforce. **Fail closed** when a requested policy cannot be enforced; do not translate
  "offline flag" into a best-effort claim.
- Mount inputs read-only where possible and outputs/workspace writable by policy. Keep
  local S3 credentials scoped to the session and prevent the executor from inheriting
  upstream credentials.
- Test escape-relevant configuration errors, path/mount confusion, symlinks, oversized
  output, image pinning, crash cleanup, and concurrent sessions. Publish a threat
  model that states what this backend does not protect against.
- Keep arbitrary agent selection, prompting, planning, and swarm orchestration outside
  the engine. Stow executes a declared command; a caller chooses the agent.

**Exit conditions:** an untrusted test workload cannot access undeclared host paths,
inherited cloud credentials, or network when network is disabled under the tested
runtime; limits and enforcement status are observable; unsupported platforms or
policies are refused instead of silently weakened.

### E5. Add thin DevOps and agent integration surfaces

Purpose: expose the stable primitive in places where jobs already run, without
duplicating its semantics.

- Add a GitHub Actions integration that calls the CLI contract, exposes attempt status
  and artifacts, handles cancellation, and uses least-privilege token permissions.
  Keep the action a wrapper, not a second implementation.
- Add MCP only as a thin tool adapter over the stable session/attempt/artifact API.
  Tools should create/inspect/cancel/checkpoint/export/promote under explicit
  authority; they must not invent a new permission model.
- Provide small TypeScript and Python helpers over the same CLI/protocol, with typed
  error codes and parity tests. Do not build in-process lifecycle APIs until their
  ownership semantics can match the CLI.
- Provide an extension seam for other CI systems and agent frameworks; add specific
  integrations only when they map cleanly to the same contract.

**Exit conditions:** integrations can complete the same lifecycle as the CLI;
cross-surface contract tests prove parity; cancellation, permissions, error codes, and
artifact digests behave consistently.

## Cross-cutting verification

Every phase adds contract tests at the public boundary, not only unit tests of
helpers. Maintain a fault matrix covering process kill, host restart, concurrent
writers, quota exhaustion, path/symlink attacks, invalid versions, interrupted writes,
unavailable upstream, and cleanup races. Run filesystem and process tests on Linux,
macOS, and Windows where supported. Keep live-provider tests separately identifiable
from local deterministic tests.

Do not use code coverage as a proxy for contract coverage. Track which public promises
have positive, negative, repeated-operation, and restart cases. Add a security review
gate for E4 rather than relying on ordinary feature tests to establish containment.

## Explicit non-goals

- Publishing packages, release versioning, public launch, marketing, user recruitment,
  external pilots, or adoption metrics.
- A hosted control plane, shared scheduler, remote workspace service, distributed lock
  service, or cross-machine live filesystem.
- A Git replacement, automatic merge engine, or implicit promotion to production
  storage.
- Building or choosing an agent model, agent framework, swarm planner, or prompt
  policy.
- Claiming OS-level or network isolation from the filesystem workspace or
  local-process executor.
- Adding storage backends, broad S3 features, or performance optimizations without a
  named workload and measurable acceptance target.

## Sequencing summary

Complete the current contract/hardening work (E0), define the shared task and attempt
model (E1), then implement the local execution engine (E2). Make outputs traceable and
promotable (E3) before introducing an executor that makes security claims (E4). Build
integrations last (E5), against the already tested contract. Each phase can be paused
independently if its exit conditions fail; no later integration should compensate for a
missing enforcement boundary or an undefined recovery state.
