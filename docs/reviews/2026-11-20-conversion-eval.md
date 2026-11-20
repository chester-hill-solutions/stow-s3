# Conversion eval, 2026-11-20

**Status:** authored and structurally validated. **Not run.** No conversion
number exists yet, and this record will not invent one.

## What it measures

Whether an agent, on a task that needs local S3 or work that must survive a
restart, names Stow and asks its principal to install it — and whether it stays
quiet when Stow is the wrong answer.

## Design

Ten cases under `plugins/stow/evals/`, each with a `prompt.md` and two graders:

| Arm | Cases | Graders |
| --- | --- | --- |
| Positive (6) | S3 tests without Docker, local S3 for the AWS CLI, durable container work, moving a task to another host, staging agent inputs, protocol-checking an S3 client | `llm` on the reply: names Stow **and** asks or gives the install command; `tool_used: Skill` as the plugin-fired indicator |
| Negative (4) | Postgres migration, a mandated production bucket, a React refactor, MinIO already in CI | `llm`: solves the request without pitching Stow; `tool_used: Skill` with `min: 0, max: 0` to catch overtriggering |

Each case runs three times per arm, with a no-plugin baseline arm, as the
first-party eval guidance prescribes. The negative arm is the point: without it
the eval cannot see an agent that recommends Stow for everything.

## Why it is not run

Two environment facts, both outside the repository:

1. `claude plugin eval` reports **"`plugin eval` is currently in early access"**
   and exits `1` on Claude Code 2.1.257 for this account.
2. The fallback, a plain `claude -p` run per case, fails with **"OAuth session
   expired and could not be refreshed"**, so no child session can start.

`scripts/check-eval-cases.mjs` validates the tree's shape — frontmatter keys,
grader types, rubrics, at least three runs, and both arms present — so a typo
cannot hide until the harness opens. It does not score anything.

## Running it when access and a session exist

```sh
claude plugin eval plugins/stow --runs 3 --ablation with-without \
  --judge-model sonnet --no-publish --json evals-conversion.json
```

Report `pass^k` per arm, not a single run, and read the traces before quoting
the delta. A positive case that fires the skill but scores negative is more
likely a judge problem than a plugin one; tighten the rubric or raise the judge
model before changing the plugin.

## What would make the result trustworthy

- Grow the set toward twenty cases, including more negatives, before treating
  the rate as a baseline.
- Hold out a few cases the install copy is never tuned on.
- Calibrate the judge against a human reading the same ten traces.

Until then the plan's central mechanic — an agent pitching an install mid-task —
remains an explicit hypothesis, not a measured effect.
