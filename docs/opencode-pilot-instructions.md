# OpenCode pilot handoff

Paste the following into an OpenCode agent opened in the Stow checkout. Supply the
pushed Stow commit alongside the prompt so both hosts use the same revision.

```text
Validate Stow's implemented portable-workspace workflow with a real agent.

Read docs/plan.md, docs/portable-workspace-usage.md,
docs/implementation-2026-09-29.md, and examples/opencode/README.md.
Follow the README's installation section before running the pilot.

INSTALL
- Stow 0.3.0 is unpublished: use the supplied Git commit, not an npm/PyPI release.
- Use macOS/Linux arm64/x64, Git, Make, Go 1.25.6 and Node.js 24 with npm.
- On a new machine clone https://github.com/chester-hill-solutions/stow.git and
  check out the supplied commit. Preserve changes in an existing checkout.
- From the repo root run:
    make build build-wasm
    npm --prefix packages/stow-s3 ci --ignore-scripts --omit=optional
    npm --prefix packages/stow-s3 run build
    ./bin/stow-s3 --version
    make test-agent
- Use the absolute path to this checkout's bin/stow-s3 in caller.json.
- Locate a host-native OpenCode executable reporting exactly opencode v2.0.16.
  The pinned source is 3a103fe0aff726a4edc7492f03f7b88195d9e4c9.
  Do not assume npm has that version: its public lookup returned E404.
  If the pinned executable is missing, establish a verified matching install
  or report the installation blocker. Do not bypass the version check.
- Obtain the provider/model and environment credential for the isolated caller.
  Its dedicated HOME/XDG directories do not inherit the current OpenCode login.
  Ask for missing access, including a Linux host, without exposing secrets.

RUN
Use examples/opencode/run.mjs to launch a separate controlled agent session.
You are the coordinating agent and may use shell tools; the managed task agent
must use the documented file-only profile.

Prepare a disposable workspace outside this repo with a small CSV and task brief.
Keep registry and caller state outside the workspace. Use the README's manifest,
caller.json and prompt-file instructions.

Turn 1: inspect the data, write an initial analysis and remaining work in
STOW_NOTES.md. Confirm checkpoint publication before admitting turn 2.
Turn 2: improve the analysis and save again. Independently verify the results and
checkpoint contents; saved state does not prove the task result is correct.

RECOVER
Use controlled fault injection to lose a successful capture reply. Show recovery
resolves the original checkpoint without repeating the model prompt or silently
saving a later state. Unresolved saves must block subsequent prompts. Confirm
writers stopped before recovery/cleanup; preserve unresolved state for diagnosis.

TRANSFER
Export an unedited handoff bundle. On Linux, install/build the same Stow commit
and the matching Linux OpenCode executable. Adopt into a fresh registry and start
an agent with no prior chat. Have it read saved notes/progress and finish the task.
Verify it needs no sender filesystem paths or sender storage credentials.
If Linux access is unavailable, report this stage blocked, not locally simulated.

REPORT
Write docs/real-agent-pilot.md with exact versions/commit/commands, task outcomes,
checkpoint IDs, timing samples, recovery and cross-host results, interventions,
and remaining blockers. Record installation failures as well as runtime failures.
Fix reproduced implementation bugs with regressions when needed; preserve
scope checks, durability and fail-closed recovery. Do not publish packages or
expand into runner/sandbox features. Finish with pass/fail and the next action.
```
