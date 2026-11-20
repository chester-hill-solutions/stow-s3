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

## Why it is not run

One environment fact: **the Claude Code session is not authenticated.** The run
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
