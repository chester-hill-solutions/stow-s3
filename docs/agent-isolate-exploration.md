# Agent Isolate: Exploration

**Planning status: retired product exploration — 2026-09-29.**
[The canonical storage plan](plan.md) is the only active work order.
[The disposition register](planning-index.md) maps this document's old items
to current work or explicit deferral. Storage continuity is now evaluated through S3/S4. Runner and sandbox ownership require a new explicit product decision.

The entire original body below is retained as history. Its statuses, unchecked
boxes, release gates, API sketches, priorities and instructions to start work are
not current instructions. Accepted ADRs and implemented contracts remain in force.

---

## Historical document (frozen)

> **Product decision, 2026-09-29:** [The canonical storage plan](plan.md) now governs. Stow owns portable working storage; callers own execution, agent turns and sandboxing. The experiments and earlier recommendations below retain their dated context. Runner/executor requirements are deferred; agent continuity will be evaluated through a thin storage integration, not a separate Stow execution product.

**Status:** exploratory; no implementation or product commitment  
**Date:** 2026-09-28  
**Scope:** documentation only

## Question

What would Stow need to become if it offered an *agent isolate*: a place to
prepare a task, run an agent with a restricted view of the host, and recover its
work as reviewable artifacts?

This is a step beyond the current prepared workspace. A workspace gives an
agent a real directory and a durable identity. It does not constrain what the
agent process can read, execute, or contact. An isolate would combine that
workspace with an enforced execution boundary and a supervisor that owns the
run lifecycle.

This exploration uses **agent isolate** as a working name for that combined
capability. It does not mean Stow becomes an agent model, planner, or coding
agent. Stow would prepare and contain a run supplied by a caller; the caller
would still choose the agent and its instructions.

## Starting point: what Stow already has

Stow already supplies several pieces an isolate could build on:

- A task manifest can copy declared files and a selected Git ref into a
  Stow-owned workspace.
- A workspace has a durable ID, quota settings, resume and handoff support,
  checkpoints, diff, restore, and portable checkpoint archives.
- The workspace is a real working directory. Files written by ordinary tools
  and objects written through its optional S3 facade are the same files.
- Local authority is the default. Upstream reads and writes are separate
  authority decisions; live writes require explicit consent.
- Preparation filters common credential-looking paths by default and reports
  sensitive paths when exporting archives.

These are useful *data and lifecycle* boundaries. They do not make the agent
process isolated. As the current manifest contract states, an agent running as
the same user can still access other paths and whatever network resources that
process can reach. Stow's byte and object limits do not hard-limit ordinary
filesystem writes.

## What “isolate” would have to guarantee

The word should be reserved for controls enforced outside the agent process.
A prompt, manifest field, or cooperative agent tool policy is not enforcement.
At minimum, a named isolation profile would need to state and verify:

1. **Filesystem view:** the task workspace is writable; only explicitly
   declared inputs and runtime files are readable; host credentials, sockets,
   home directories, and unrelated projects are not implicitly visible.
2. **Network view:** outbound access is denied by default. Any allowed hosts or
   services are explicit, scoped to the run, and distinguish local S3 access
   from access to real upstream providers.
3. **Process boundary:** child processes stay inside the run's lifecycle and
   cannot escape the configured filesystem or network restrictions.
4. **Resource bounds:** wall time, CPU, memory, process count, open files, and
   writable storage have explicit limits, with observable termination reasons.
5. **Secrets:** host environment variables are not inherited wholesale. Any
   injected secret has an explicit source, audience, lifetime, and redaction
   behavior. A workspace handoff reference remains distinct from credentials.
6. **Lifecycle and recovery:** interruption does not corrupt the durable
   workspace; the supervisor can report whether a run completed, failed, or was
   stopped; artifacts can be checkpointed and reviewed before promotion or
   transfer.
7. **Evidence:** the caller can inspect the effective profile and know which
   controls the selected host actually enforced. Unsupported controls fail
   closed or are clearly reported as unavailable.

These are candidate requirements, not a security certification. Even if all
are implemented, the threat model must say whether a hostile task is in scope,
whether kernel/runtime escape is in scope, and what host compromise means.

## Possible product boundary

The narrowest coherent product is a **supervised run around a prepared
workspace**:

```text
task manifest → prepare workspace → launch caller-supplied command in isolate
                                      ↓
                         status / logs / resource use
                                      ↓
                    checkpoint → inspect diff → export or handoff
```

Stow would own task inputs, workspace identity, local S3 bytes, quotas,
checkpoints, and artifact movement. A caller would supply the command, agent
configuration, and policy for any model credentials. Stow would not need to
choose a model, interpret task success, or own the agent's conversation history.

The optional S3 facade would need careful placement. The simplest safe local
shape is for the isolated process to reach a loopback facade bound to the same
workspace runtime. It must not receive a second store or a route to an upstream
provider by accident. A stronger isolation profile may not permit host loopback
at all; in that case Stow needs a deliberately exposed channel or the task must
use files only. Whether S3 belongs inside the first isolate is an open product
question.

## Runtime approaches to compare

No one mechanism gives the same guarantees on every host. A first design review
should compare these as backends behind one profile contract, not promise
cross-platform parity before measuring it.

| Approach | Potential fit | Main questions |
|---|---|---|
| Host OS sandbox | No separate container daemon; can be close to native tools | Which operating systems can enforce the required filesystem and network rules? What setup and permissions are needed? Can the agent and all descendants be reliably supervised? |
| Container runtime | Familiar packaging and process boundary; useful for reproducible toolchains | Is a runtime installed and usable? What host mounts, sockets, capabilities, and network defaults must be removed? What remains platform-specific? |
| WASM or another embedded runtime | Narrow capability surface for workloads that fit the runtime | Can the actual coding-agent toolchain run there? How are subprocesses, language toolchains, networking, and large repositories handled? What is the memory and startup cost? |

These are investigation paths, not commitments to a particular sandbox or
container product. The existing WASM object runtime is not itself an agent
execution environment; it stores bytes and exposes a runtime bridge.

## Work implied by the idea

Before implementation, Stow would need decisions and evidence in several
areas:

- **Threat model and profiles.** Define protected assets, attacker capability,
  host assumptions, and a small set of named profiles. “Isolated” cannot be a
  single boolean if host capabilities differ.
- **Launcher and supervisor.** Add a run lifecycle with validated command and
  arguments, controlled environment, cwd, signal handling, timeouts, process
  tree cleanup, exit status, logs, and resource accounting. This is a new
  responsibility beyond workspace preparation.
- **Mount/input model.** Decide whether inputs are copied as today, read-only
  mounted, or content-addressed. Keep output writable and make the boundary
  between task inputs, runtime, and host explicit.
- **Network policy.** Decide whether the default is no network; how package
  installation or model APIs are allowed; and whether local Stow S3 can be
  reached without enabling arbitrary host or upstream access.
- **Resource enforcement.** Enforce limits at the execution boundary, not only
  during preparation or S3 operations. Report actual enforced limits and
  distinguish requested from effective values.
- **Secrets and trust.** Avoid ambient credential inheritance. Decide if model
  credentials are passed directly, brokered, or remain the caller's
  responsibility. Add tests and documentation for redaction and task output
  that may contain secrets.
- **Workspace integrity.** Define checkpoint behavior while a run is active,
  recovery after forced termination, and artifact review before export or any
  live promotion. Existing checkpoint capture already asks callers to stop
  writers; an execution supervisor could make that lifecycle safer.
- **Support matrix and distribution.** Identify host OS, architecture,
  required privileges, install footprint, and clean-install story. A profile
  that silently degrades on one host is not the same profile.
- **Operational evidence.** Measure preparation, launch, idle overhead,
  repository-scale I/O, concurrent runs, peak resource use, cancellation, and
  cleanup on representative coding tasks. Existing tiny workspace pilots do
  not measure isolated execution.

## Suggested exploration sequence

1. Write a threat model and define one deliberately narrow profile, including
   explicit non-goals and host assumptions.
2. Pick one host and one real agent/toolchain workload. Prototype the launch
   boundary outside Stow first, and verify the filesystem, network, process,
   and resource claims with adversarial probes.
3. Compare the measured prototype with the current baseline: ordinary process
   in a prepared workspace. Record setup friction and recovery behavior as
   well as performance.
4. Only if the boundary holds, specify the public run descriptor, status
   protocol, cancellation semantics, and how it composes with workspace IDs,
   checkpoints, and optional S3.
5. Decide whether one host-specific profile is valuable enough to ship, or
   whether cross-platform parity is a prerequisite for the product promise.

This sequence keeps the first question empirical: can Stow enforce a useful
boundary around a real agent task without taking on the agent itself?

## Open decisions

- What exact CPU, memory, wall-time, process-count, and disk limits can every
  supported backend enforce? Requested hard limits should not silently degrade.
- Which artifact formats are in the first portable task bundle: WASI component,
  target-specific native executable, or both?
- How can a destination reconstruct a Git worktree when its source was a local
  repository on the originating machine? Include a source snapshot, fetch a
  pinned remote commit through a broker, or support both?
- Which execution backend and placement should be the first pilot target?
- Does the observed handoff pain justify this product? No external adoption
  evidence is available yet.

The following are working directions from product exploration, not accepted
API or security contracts:

- Stow remains **“buckets anywhere”**: the task owns one durable Stow workspace
  that appears as a directory and has the same bytes through its optional S3
  facade.
- The portable handoff is the task brief plus repository identity/base commit
  and an immutable checkpoint. A fresh run materializes that state into a new
  isolated Git worktree. Agent conversation state is an optional add-on.
- The agent and its child tools execute inside the isolate. Stow supervises
  their process lifecycle while the caller chooses the agent command.
- Network is denied by default. A run receives explicit service capabilities;
  model-provider calls are brokered so raw provider keys do not enter the
  isolate.
- The intended boundary protects the host and other workspaces from ordinary
  task processes. It does not claim to contain a kernel or sandbox-runtime
  escape.
- The product may be described as a small task operating environment, but its
  implementation should compose established host/WASM isolation mechanisms
  rather than become a new kernel.

## Market read and PMF hypothesis

Research checked current primary product documentation on 2026-09-28. A generic
agent sandbox is already a busy category:

| Product | Documented overlap |
|---|---|
| [E2B](https://e2b.dev/) | Agent-focused microVMs, snapshots, pause/resume/fork, network policy, and secrets handling. |
| [Daytona](https://www.daytona.io/docs/en/persistence/) | Persistent agent sandboxes, filesystem and memory snapshots, resume, and independent forks. |
| [Modal](https://modal.com/docs/guide/sandbox-snapshots) | Command execution plus filesystem/directory snapshots that can seed new and parallel sandboxes. |
| [Vercel Sandbox](https://vercel.com/sandbox) | On-demand Linux microVMs for running an agent or its generated code. |
| [Cloudflare Sandbox SDK](https://developers.cloudflare.com/sandbox/) | Edge code execution in isolated containers, S3-compatible object storage mounted as files, and outbound-traffic controls. |

The opening hypothesis for Stow is **portable continuity for agent work**, not
another general-purpose remote machine: take a task checkpoint, recreate its
Git worktree on another runner, and continue with the same Stow directory/S3
semantics. This could matter to teams whose agent tasks are interrupted, handed
between people or runners, or moved between local, CI, and edge placements.

This is a hypothesis, not PMF evidence. The current [workspace pilot](agent-workspace-pilot.md)
is a tiny synthetic smoke check, and the [project assessment](assessment-2026-09-27.md)
records no external adoption pilot. The highest-value validation is to ask teams
already running coding agents to walk through a recent interrupted or handed-off
task, measure the state they had to reconstruct, and try a portable handoff on
that task. Repeat use across two execution placements and willingness to adopt
or pay would be meaningful signals. Interest in faster remote machines alone
would put Stow head-to-head with the established sandbox providers.

## Current readiness and test evidence

The data and task-lifecycle foundation is real; the execution boundary is not
implemented:

- `workspace prepare` creates a fresh shallow Git repository at an explicit
  commit and returns its repository metadata in the launch JSON. That metadata
  is not part of `CheckpointManifest`.
- Checkpoint capture and restore operate on verified files. Capture excludes
  `.git`; restore recreates files but cannot currently reconstruct the original
  Git worktree or its base commit. The portable archive has integrity and path
  validation but is not an encrypted secret store.
- The workspace directory and S3 facade can expose the same file bytes. The
  workspace backend enforces Stow API limits, but direct filesystem writes by
  the agent are not hard-limited by those quotas.
- No Stow component starts an agent inside an OS/WASM sandbox, restricts that
  process's network, or brokers model-provider calls. The current agent process
  is launched externally with the prepared directory as its working directory.
- Stow's existing WASM artifact runs the object runtime through a JavaScript
  bridge. It is not a WASI command runner and does not execute arbitrary agent
  binaries. The [deploy-anywhere measurements](deploy-anywhere-plan.md) record
  an 8 MiB post-boot memory floor for that storage runtime.

Relevant existing tests passed on 2026-09-28:

- `go test ./pkg/stow ./internal/storage/workspace`
- `go test ./internal/storage/... ./internal/runtime/...`
- `go test ./cmd/stow-s3` (required loopback access for its local HTTP tests)
- `make test-wasm` (one WASM storage-runtime integration test passed)

The workspace, checkpoint/archive, runtime, storage, and CLI foundations are
therefore green in the tested host environment. These tests do not prove any
agent-process isolation property; that property has no implementation to test.

## Recommended next steps

1. **Validate the wedge before building an executor.** Run external discovery
   with teams that already operate coding agents. Establish the frequency and
   cost of task recovery and cross-runner handoff, then test whether they prefer
   a portable checkpoint to their current provider-specific snapshots.
2. **Specify the portable handoff envelope.** Include the task brief, repository
   identity and exact base commit, a verified file delta including deletions,
   working-directory layout, and an artifact/runtime requirement. Resolve how
   the destination obtains the base tree. Exclude provider credentials and keep
   conversation history optional.
3. **Prove one execution backend against the security profile.** Test that an
   agent and its child processes cannot read host sentinel files, access a
   second task workspace, or reach unapproved network endpoints; test enforced
   limits, cancellation, cleanup, and checkpoint recovery after interruption.
   Report the effective profile and refuse unsupported hard-limit requests.
4. **Exercise the handoff across placements.** Resume the same portable task on
   a laptop and one remote execution target. Compare agent-ready time, recovery
   success, state-transfer size, and setup steps with a provider-native
   snapshot/fork workflow.
5. **Choose whether to expand.** Add more runtimes only if the portable task
   contract works and pilot users repeatedly use the handoff. Keep local, CI,
   and edge support as separately verified profiles; the same API alone does
   not establish identical isolation.

No external teams were contacted as part of this desk research. The user-facing
pilot needs named participants and their explicit outreach path before it can
run.

## Related contracts

- [Agent continuity pilot, 2026-09-29](agent-continuity-pilot-2026-09-29.md) — measured offline input capture and two-process continuation, including the archive-path workaround; fresh-model and cross-host continuation remain untested.
- [Agent task manifest](task-manifest.md) — current workspace preparation,
  lifecycle, and explicit non-sandbox limitation.
- [Workspace contract](workspace-contract.md) — object/file mapping and
  workspace storage behavior.
- [ADR 0007](adr/0007-workspace-is-the-default.md) — workspace-first surface
  and optional S3 facade.
- [ADR 0009](adr/0009-workspace-outlives-process.md) — durable identity,
  collection, and handoff semantics.
- [ADR 0010](adr/0010-an-enforcement-site-or-not-a-permission.md) — principle
  that a permission without an enforcement site is not a permission.
