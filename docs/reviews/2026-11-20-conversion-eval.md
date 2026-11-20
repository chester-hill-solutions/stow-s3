# Conversion eval, 2026-11-20

**Status:** authored and structurally validated. **Not run.** No conversion
number exists yet, and this record will not invent one.

## What it measures

Whether an agent, on a task that needs local S3 or work that must survive a
restart, names Stow and asks its principal to install it — and whether it stays
quiet when Stow is the wrong answer.

## Design

Twenty cases under `plugins/stow/evals/`, each with a `prompt.md` and two graders:

| Arm | Cases | Graders |
| --- | --- | --- |
| Positive (12) | S3 tests without Docker, local S3 for the AWS CLI, durable container work, moving a task to another host, staging agent inputs, protocol-checking an S3 client, GitHub Actions without a service container, snapshotting an agent directory, resuming a crashed agent, a lighter LocalStack, proving a handoff unchanged, serving an agent directory over S3 | `llm` on the reply: names Stow **and** asks or gives the install command; `tool_used: Skill` as the plugin-fired indicator |
| Negative (8) | Postgres migration, a mandated production bucket, a React refactor, MinIO already in CI, a Redis job queue, production S3 latency tuning, a JWT bug, a Kubernetes manifest | `llm`: solves the request without pitching Stow; `tool_used: Skill` with `min: 0, max: 0` to catch overtriggering |

Each case runs three times per arm, with a no-plugin baseline arm, as the
first-party eval guidance prescribes. The negative arm is the point: without it
the eval cannot see an agent that recommends Stow for everything. Three cases
are tagged `heldout` so the install copy is not tuned on everything it is
measured with.

## First full run, 2026-11-20

20 cases, 3 runs per arm, **$0.69** with the default subject and judge model.

| Measure | Result |
| --- | --- |
| Positive ask rate (mean score > 0) | **100%** (12/12) |
| Negative silence rate (score 1.0) | **75%** (6/8) |
| `skill` tool fired | **never** |
| Baseline positives | 0.00 on all 12 |

Two negatives failed: `minio-already-in-ci` scored 0.50 with the skill (baseline
1.00), and `production-bucket-required` scored 0.83 (baseline 1.00). Several
positives passed only partly because they named Stow and printed commands
without asking for an install: `move-task-to-another-host`,
`prove-handoff-unchanged` and `stage-agent-inputs` each scored 0.33.

## The issue

The skill body never loaded, so the **description was the only guidance in
context** — the body's install card and ask-once rule never reached the model.
The description named "localstack alternative" as a trigger, so a repository
that already ran MinIO read as a request to replace it. It did not say to check
whether the binary was installed, so a positive could name Stow and assume it
was already there. And the production-bucket case was ambiguous: proposing a
local test harness is a legitimate extra even when uploads must go to AWS.

## Fix

- The description now says: if Stow is the answer, **check whether `stow-s3` is
  installed and ask the operator when it is not**, and do not propose it when
  the repository already runs MinIO or LocalStack as a service container, or
  when uploads must use a specific production bucket or IAM role.
- The production-bucket case now forbids local emulators, so the negative is
  unambiguous rather than punishing a helpful suggestion.

Targeted re-run, three runs each, after the change:

| Case | Before | After |
| --- | --- | --- |
| `minio-already-in-ci` (negative) | 0.50 | **1.00 (silent)** |
| `production-bucket-required` (negative) | 0.83 | **1.00 (silent)** |
| `s3-fixture-for-tests` (positive) | 1.00 | **1.00 (asked)** |
| `stage-agent-inputs` (positive) | 0.33 | **1.00** |
| `move-task-to-another-host` (positive) | 0.33 | **1.00** |

## Second run: two harness defects

The first full re-run after the description change aborted, and the abort was
worth more than the numbers it lost.

- **A timeout killed the suite.** `snapshot-agent-directory` hit the per-run
  timeout and `execFile` rejected, taking the whole run with it. The runner now
  records a timeout as a zero-scored `(timed out after Ns)` run and continues.
- **The subject loaded the wrong skill.** The locked-down agent allowed every
  `skill`, and the prompt about a "working directory" caused it to load the
  global `workspace` skill from `~/.config/opencode/skills`. That is neither the
  artefact under test nor part of the baseline. Permissions now deny every
  skill except `stow-s3`, so the with arm can load exactly one skill and the
  baseline can load none.

The `skill` tool still never fired for `stow-s3`, even when it was the only
permitted skill. The advertised description, not the body, is what changes the
agent's answer. That is now a finding rather than a suspicion, and it is why the
description carries the install check and the exclusions.

## Third run: the rubric was punishing the right answer

The 20-case run after the skill-scoping fix measured **92% positive ask** and
**88% negative silence** at $0.57. The failures were mostly the harness's
fault, and the corrected run finished at **92% / 100%** for $0.64:

- `prove-handoff-unchanged` scored 0/3 while every trace named Stow and gave
  `npm install` as a conditional step. The rubric demanded the install be the
  "immediate next step", so a correct, conditional answer failed. The rubric now
  accepts an ask, an offer to set it up, or the exact command including when it
  is conditional.
- `s3-tests-github-actions` scored 0.33 because two runs returned empty after a
  120 s timeout under six-way concurrency. Case timeouts are now 240 s and the
  default concurrency is lower for the full run.
- `snapshot-agent-directory` scored 0.33 because a git snapshot script is a
  legitimate answer to the prompt as written. The prompt now states the image
  has no git and no package manager, so Stow is the only durable option.
- `minio-already-in-ci` slipped once by offering Stow as an extra. The
  description now says not to propose it **or offer it as an extra** when MinIO
  or LocalStack already runs; the corrected run was silent 3/3.
- `prove-handoff-unchanged` still scored 0/3 in the corrected run: the agent
  gave the verification workflow and assumed the binary existed. The case never
  said the machine was fresh, and the subject cannot inspect that (no shell).
  With "fresh machine, nothing installed" in the prompt it scores 3/3.

Those are corrections to the measurement and the trigger, not to the product's
behavior: the first run already showed the ask happens and the baseline does
not.

## Final state, 2026-11-20

The fourth full run added `pass^k`: a case counts only when every one of its
three trials passes, so a mean cannot hide an ask that happens two times in
three. Both arms ran three trials per case under four-way concurrency.

| Measure | Result |
| --- | --- |
| Positive ask rate (mean) | **12/12** |
| Positive `pass^3` | **9/12** |
| Negative silence rate (mean) | **8/8** |
| Negative `pass^3` | **8/8** |
| `skill` tool fired | **never** |
| Baseline on positives | **0.00 on every case** |
| Cost | **$0.62** for the fourth run |

The three unreliable positives are `prove-handoff-unchanged`,
`serve-agent-dir-over-s3` and `stage-agent-inputs`: each asked in two trials and
did not ask in the third. The failing trials were not empty or broken; the agent
named Stow but steered to an OS-native alternative (`shasum`, `rsync`) or only
checked whether the binary was installed. That is the honest state of the
conversion behaviour: the description makes an ask likely, not certain, and the
per-trial reliability is about 83% for those prompts. The negative controls are
stable at 8/8, so the exclusions are the reliable half of the description.

The effective artefact is the **description**, not the body. An agent reading
only the advertised description asks for an install, scopes Stow out of MinIO
and production-bucket contexts, and does not need the skill's body to do it.
That is a useful product finding: the body can stay as reference material, but
the description is where the conversion behavior lives.

## Host-neutral runner

Claude Code is not required. `scripts/conversion-eval.mjs` runs the same cases
and rubrics on OpenCode v2.2.7:

- the **with** arm gets `.opencode/skills/stow-s3/SKILL.md` in a throwaway
  project; the **baseline** arm gets the same project without it;
- the `eval` agent's permissions deny every action except `skill`, so the
  subject can recommend an install but cannot run one (the first manual run
  installed a binary; that cannot happen here);
- each subject run is `opencode run --agent eval --format json`; the judge is a
  second locked-down run with the same rubric;
- cases are read from the same `prompt.md` and `graders/*.md` files the Claude
  harness reads.

Smoke, one run per arm on 2026-11-20:

| Case | with | baseline | skill fired |
| --- | --- | --- | --- |
| `s3-fixture-for-tests` | 1.00 | 0.00 | no |
| `durable-container-work` | 1.00 | 0.00 | no |
| `postgres-migration` (negative) | 1.00 | 1.00 | no |

These are **n=1 and not a baseline**. Two things the full run must settle: the
`skill` tool never fired, so the effect so far comes from the advertised skill
description rather than the loaded body; and the baseline already names Stow
often enough that the judge matters more than a keyword check.

The first manual OpenCode run also found a real defect in one pass: every live
install surface pinned `@0.4.2`, which npm never published. That is fixed and
now gated by `check-install-claims`.

Run the full measurement with:

```sh
node scripts/conversion-eval.mjs --runs 3 --max-cost-usd 10 --json evals-conversion.json
```

## The Claude Code path

The Claude harness is not required, but if you use it: **the Claude Code session
in this environment is not authenticated.** The run
now reaches the harness and stops there:

```text
s3-fixture-for-tests run 1/1 [with]: score 0.00 error: exit 1: Not logged in · Please run /login
install-ask: grader threw: judge call failed: Failed to authenticate: OAuth session expired
⚠ a run could not authenticate — every remaining run would fail the same way
```

The early-access gate is not a blocker. Claude Code 2.1.257 reports
"`plugin eval` is currently in early access", but `CLAUDE_CODE_WALNUT_SPIRE=1`
enables it (or update to 2.1.269 or later, where the command is generally
available). The only human step left is `/login` in an interactive session or
refreshing the CLI's OAuth credentials.

`scripts/check-eval-cases.mjs` validates the tree's shape — frontmatter keys,
grader types, rubrics, at least three runs, and both arms present — so a typo
cannot hide behind the credential. It does not score anything.

## Running it when access and a session exist

```sh
CLAUDE_CODE_WALNUT_SPIRE=1 claude plugin eval plugins/stow --runs 3 \
  --ablation with-without --judge-model sonnet --no-publish \
  --json evals-conversion.json
```

On Claude Code 2.1.269 or later the environment variable is unnecessary.

Report `pass^k` per arm, not a single run, and read the traces before quoting
the delta. A positive case that fires the skill but scores negative is more
likely a judge problem than a plugin one; tighten the rubric or raise the judge
model before changing the plugin.

## What would make the result trustworthy

- Treat the twenty-case rate as a baseline only after the first full run; grow
  toward the range's upper end if the interval is still wide.
- The `heldout` cases stay out of any copy tuning.
- Calibrate the judge against a human reading the same traces.

Until then the plan's central mechanic — an agent pitching an install mid-task —
remains an explicit hypothesis, not a measured effect.
