# Agent adoption plan, 2026-11-20

**Research basis:** [what the first-party sources say about agent tool adoption](2026-11-20-agent-tool-adoption-best-practices.md).

**Status:** active for the adoption workstream only. The
[consolidated plan](../development/storage-foundation-plan.md) remains the sole
scope and dispatch authority; this record orders adoption work and states the
conversion test it is judged by.

## Goal

Raise the rate at which an agent, on hitting a wall, names Stow and asks its
principal to install it.

The conversion event is one sentence:

> "I can unblock this in one command, no daemon and no credentials — install it?"

Nothing in this plan matters if that sentence does not become more likely.

## Wedge

- **S3 tests without Docker** — frequent, measurable, and the alternative
  (daemon, privileges, image pulls, or a mock that proves nothing) is visibly
  worse.
- **Work that survives the machine** — the emotional half. An agent that has lost
  a container's work once will remember a tool that kept it.

Both already exist. The plan is packaging truth where agents read it, plus five
small fixes so the first refusal does not contradict the packaging.

## Phase 0 — Protect the ask

| Item | Acceptance | Effort |
| --- | --- | --- |
| Pre-flight refusals report `not_committed`; `unknown` only for real uncertainty | Every refusal code's outcome is asserted | 1.5 d |
| `--max-file-bytes`, and the refusal names the bound and the flag | 100 KB file round-trips; over-bound error names the number | 1 d |
| Seeding skips sensitive paths and reports them | `.env` project prepares; `excluded` lists it; `refuse` still aborts | 1 d |
| Every refusal carries a machine-readable `next` | Table test enumerates refusal codes and requires non-empty `next` | 1.5 d |
| Streaming SigV4 refusal names the actual unsupported header | Error mentions `STREAMING-AWS4-HMAC-SHA256-PAYLOAD` | 0.5 d |

## Phase 1 — Make the proof visible

| Item | Acceptance | Effort |
| --- | --- | --- |
| Kill-and-resume demo: prepare → save → `SIGKILL` → resume → checkpoint byte-identical | Runs in CI under 30 s; linked from the front page | 1 d |
| `doctor --json` as evidence: version, endpoint, data dir, backend, ready, limits | One screen, no prose, sub-second | 0.5 d |
| Install card in `README.md`, `site/index.html`, `site/agent.md`, `skills/stow-s3/SKILL.md` and the MCP description | npm, binary and MCP commands each tested in a clean container; uninstall documented | 1 d |
| Plugin and marketplace artefact in this repository, bundling the skill with the MCP server; `server.json` for the registry | `claude plugin validate` passes; one install delivers guidance and server; registry entry carries name plus install pointer (its description is capped at 100 characters) | 2 d |

The skill file is the storefront most agents read. It should teach the trigger,
the three verbs and the ask, not only the API.

## Phase 2 — The capability they cannot improvise

| Item | Acceptance | Effort |
| --- | --- | --- |
| Handoff: export, then adopt on a second root, one command each | Second host reads the checkpoint; ids and digests verified | 2–3 d |
| Import and handoff idempotency, half-failure recovery | Interrupted import leaves no partial state; retry converges | included |
| A measured comparison an agent can cite: Docker/MinIO vs mock vs Stow | Short table with start time, image size, privileges — [Stow measured, Docker unmeasured here](2026-11-20-local-s3-comparison.md) | 1 d |

## Phase 3 — Be where agents look

| Item | Acceptance | Effort |
| --- | --- | --- |
| MCP tool descriptions at the published bar: 3–4 sentences covering what it does, when to use it **and when not to**, parameters, what is returned, and the next call | Review against the cited guidance plus the existing behaviour contract test; a one-clause description fails | 1.5 d |
| `outputSchema` where the result is structured, and `ToolAnnotations` (`readOnlyHint`, `destructiveHint`, `openWorldHint`) | Schema validation on every tool result; annotations never the only statement of safety | 1 d |
| Discovery: plugin marketplace listing and MCP Registry `server.json` | A cold agent can find Stow through a documented surface; the skill is treated as the closing surface, not the discovery surface | 1 d |
| First-run examples for OpenCode, Claude, Codex, plain Node and Python | [Agent host setup](../guides/agent-hosts.md) covers each host, the zero-config object server, and a first prompt that stores and reads | 2 d |
| "Check us in one command" on the front page | Points at the adapter contract and the importable corpus | 0.5 d |

## Phase 4 — Measure conversion, not usage

Twenty to fifty tasks, **balanced**: tasks that need S3 or durable work, and
negative controls where the right answer is to use nothing, a mock, or an
existing fixture. Each task has a reference trace and a grader; each trial runs
in a clean environment; every trace is read before a number is quoted.

Each trace is scored:

- worked around it (Docker, mock, live credentials)
- named Stow but did not ask
- **named Stow and asked the principal to install it**
- asked, was refused, and dropped it (a first-class outcome, not a failure)

Report `pass^k` over at least three trials, not a single run. Calibrate the
transcript judge against a human reading the same traces, and hold out a set the
copy is not tuned on. The eval and its traces live in the repository.

The cases and their design are in
[the conversion eval record](2026-11-20-conversion-eval.md); they are authored
and structurally validated, and the run reaches the harness. It stops only
because the local Claude Code session is not authenticated (`/login`), not
because of the early-access gate: `CLAUDE_CODE_WALNUT_SPIRE=1` opens it on this
build.

Targets are directional until the set has at least twenty tasks: half of the S3
tasks and a quarter of the durability tasks asking, **and** the negative
controls not pitching Stow. An eval without negatives cannot detect the
overtriggering failure the sources warn about.

Package and release downloads, registry listings and issue text are context, not
the measurement; there is no telemetry, so the eval is the only conversion
signal.

## Sequencing

1. Week 1: Phase 0, in parallel across the five items.
2. Week 2: Phase 1; first external test with the proof and the card.
3. Week 3: Phase 2, then Phase 3 including the plugin/marketplace artefact.
4. Weeks 4-5: Phase 4 with the balanced set and trials, then iterate on the weakest trigger.

Release 0.4.3 carries the proof and the card at the end of week 2. Handoff
follows in 0.4.4 if it slips.

## Risks

| Risk | Mitigation |
| --- | --- |
| The ask reads as self-serving | The skill frames it as unblocking the principal's task, states the fallback, and asks once per task; a refusal ends the ask |
| The skill-authored ask has no documented prior art | Treat it as the plan's hypothesis, not its assumption; the host's permission UI is what actually gates installation, and the eval records refusals |
| Overclaiming S3 parity | Link the compatibility contract; let the corpus be the proof |
| Install friction kills the ask | One command per ecosystem, no root, no daemon, uninstall is deleting one directory |
| A demo that is not reproducible | It runs in CI, not in a post |
| Feature creep | Each item is judged by the conversion eval, not by how good it feels to build |

## Not in this plan

No telemetry, no dashboards, no rewritten marketing voice, and no new surface
beyond what the four walls need. This workstream is packaging plus five small
correctness fixes.
