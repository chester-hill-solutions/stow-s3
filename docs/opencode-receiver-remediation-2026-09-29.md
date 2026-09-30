# OpenCode receiving-host remediation

Plan item: S3 caller integration. Date: 2026-09-29.

The receiving-host report supplied by the user, “Stow cross-host handoff writeup
2026-09-29.md”, tested source commit `f54927668a135146518f56efaf27eca2b7df1578`
on macOS arm64. It reports byte-identical adoption of four payloads, a new
workspace identity and a preserved checkpoint identity. Its model turn reported
`succeeded` while leaving the task files unchanged. These are receiving-host
observations, not a successful continuation claim.

## Reproduced caller defects and changes

The current caller passed `holder.ready.root` to both the OpenCode process and
session even though native readiness already exposes absolute `working_directory`.
It now uses that directory, validates it remains inside the workspace, and tells
the agent where root-level `STOW_PROGRESS.json` is relative to its cwd.

The caller previously saved the agent outcome without task-file evidence. It now
hashes task files before execution and after quiescence, before writing progress.
It records added, modified and deleted relative paths, including mode changes.
Root progress, root `.stow` and Git internals do not count as task work. Scans are
cancellable and bounded by the configured bytes/files. A successful no-op keeps
the original terminal outcome, saves its checkpoint, adds `reviewRequired: true`,
exits 2, and closes admission. Recovery preserves this evidence and only resolves
the original capture receipt. A changed file still does not prove task correctness.

The server previously removed `OPENCODE_API_KEY` along with runtime overrides.
It now forwards that named Zen credential while removing other `OPENCODE_*`
variables. HOME and XDG locations are aligned beneath one isolated home, allowing
dedicated config/catalog/auth provisioning without guessing which base a binary
uses. This changes the earlier XDG layout; operators must use fresh caller state
and preserve older failed directories for diagnosis.

The [runbook](../examples/opencode/README.md) and [pilot instructions](opencode-pilot-instructions.md)
now specify cwd, credential/config paths, successful no-op handling, a nonexistent
adoption root and the explicit real-model gate.

## Validation

On this macOS host with Node v25.9.0 and Go 1.25.6:

- `GOCACHE=/private/tmp/stow-go-build make test-agent`: 26 passed, 0 failed,
  1 explicitly skipped real-model test because no `STOW_REAL_MODEL_CONFIG` was
  supplied. The Go cache was relocated and loopback access allowed for the local
  test servers under the desktop execution sandbox.
- The full-caller regression exports a portable fixture, removes sender files and
  registry, adopts into a fresh registry, runs a protocol fixture in `repo/`,
  independently verifies the three-row total of 31.50 and changed notes, then
  confirms an unchanged successful turn saves with review and blocks another prompt.
- Regressions verify ordering, unchanged-byte rewrites, additions,
  modifications, deletions, modes, scan bounds/cancellation, directory containment,
  named credential forwarding and no-op evidence through uncertain-save recovery.
- `STOW_REQUIRE_REAL_MODEL=1 node --test examples/opencode/real-model.test.mjs`
  without a profile fails immediately with the required-profile error. The
  required model gate cannot silently pass by skipping.
- Documentation command-surface validation and `git diff --check` passed.

The opt-in real-model test uses a fresh adopted workspace containing a CSV and
sender notes, a fresh agent, and a unique summary marker. It checks the calculated
result, changed notes and exact artifacts restored from the saved checkpoint.
Run it with `STOW_REAL_MODEL_CONFIG=/absolute/path/model-profile.json make test-agent-real`.
Provider calls require that explicitly supplied profile. Optional dedicated
provider config/catalog paths are copied outside the captured workspace, and
failed caller state is retained for diagnosis.

### Authorized real-model follow-up

The user then requested the existing OpenCode model. The local model selection
record identified `opencode/space-bunny-free`, with an existing Zen API login.
That credential was read directly into the test process environment without
printing it or writing a credential copy. A dedicated provider configuration and
the existing matching catalog were staged into the disposable isolated home.

`make test-agent-real` passed: **1 passed, 0 failed, 0 skipped**, 20.009 seconds
for the test (20.088 seconds for the runner), with OpenCode v2.0.16 and the
source-built Stow 0.3.0 binary. The fresh agent read adopted files under `repo/`,
produced the independently verified total of **31.50 over 3 data rows**, modified
the seeded notes with the requested summary and unique marker, and saved a
checkpoint. Restoring that checkpoint reproduced the exact notes and verified
result. Sender files and registry had been removed before execution.

This is a successful real-model continuation from an adopted bundle on the same
host. It verifies actual task writes and checkpoint contents after the fix; it
does not substitute for continuation on a second physical host or Linux.

## Remaining acceptance

Successful real-model continuation from an adopted bundle now passes locally.
Real interruption/cleanup, continuation on a second physical host after this fix,
and Linux runtime remain acceptance gates. The reported adoption pass and local
model pass do not close those gates. Nothing was published.

The pinned upstream setup was checked against the OpenCode source at
`3a103fe0aff726a4edc7492f03f7b88195d9e4c9`: [Zen credential handling](https://github.com/anomalyco/opencode/blob/3a103fe0aff726a4edc7492f03f7b88195d9e4c9/packages/core/src/plugin/provider/opencode.ts),
[HOME/XDG roots](https://github.com/anomalyco/opencode/blob/3a103fe0aff726a4edc7492f03f7b88195d9e4c9/packages/util/src/global-roots.ts),
[config discovery](https://github.com/anomalyco/opencode/blob/3a103fe0aff726a4edc7492f03f7b88195d9e4c9/packages/core/src/config/discovery.ts),
and [legacy auth migration](https://github.com/anomalyco/opencode/blob/3a103fe0aff726a4edc7492f03f7b88195d9e4c9/packages/core/src/database/migration/20260805200742_import_legacy_credentials.ts).
