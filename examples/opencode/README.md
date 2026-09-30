# OpenCode checkpoint caller (experimental)

This example controls a dedicated OpenCode 2.0.16 session and saves a portable
Stow checkpoint after each settled prompt. It starts OpenCode itself, holds a
Stow workspace server throughout the turn, and shuts OpenCode down before
releasing that storage claim. It never automatically reruns a prompt.

The initial profile allows file tools only. Shell commands, subagents and other
execution tools are denied and refused if observed in session context. This is a
caller convention, not an OS sandbox. Do not attach another UI/client to this
session, install plugins in its dedicated config, or write to its directory from
another process while it saves. A forcibly killed controller can leave an agent
process behind: stop that process before recovery or collection. Ordinary
OpenCode UI sessions are outside this verified admission path.

## Install and build the pilot

This Stow candidate is **unpublished**. Build the exact source revision on each
host; installing an older npm/PyPI release does not provide this workflow.

Prerequisites: macOS or Linux (arm64/x64), Git, Make, Go **1.25.6** (the version in
`.go-version`), Node.js **24** with npm, and an OpenCode **2.0.16** executable for
that host. Python, Docker and global Stow installation are not required for this
pilot. Install missing toolchains using the host's normal package/version manager.

For a fresh checkout:

```sh
git clone https://github.com/chester-hill-solutions/stow.git
cd stow
```

If the repository already exists, preserve local changes before updating it. On
the recipient host, check out the same commit the sender records with
`git rev-parse HEAD`; do not assume a later `main` has the same behavior.

From the repository root:

```sh
make build build-wasm
npm --prefix packages/stow-s3 ci --ignore-scripts --omit=optional
npm --prefix packages/stow-s3 run build
./bin/stow-s3 --version
make test-agent
```

The optional dependency omission is specific to this pilot build: it avoids
packaged native carriers taking precedence over the source-built binary. It is
not the recipe for the complete quality suite, which needs optional tooling;
`make test-all` and `make standards` manage their own dependency installation.
Record the absolute checkout path. `stowBinary` below must name its
`bin/stow-s3`, and the version check must report 0.3.0. The native binary is built
for the current host; rebuild on Linux rather than copying a macOS executable.

### OpenCode version and credentials

Use the existing pinned executable if available. Verify its version; the caller
requires the exact output `opencode v2.0.16`. The inspected source is
[OpenCode v2.0.16](https://github.com/anomalyco/opencode/releases/tag/v2.0.16),
commit `3a103fe0aff726a4edc7492f03f7b88195d9e4c9`.

On 2026-09-29, the public npm lookup for `opencode-ai@2.0.16` returned E404.
The [general installation guide](https://opencode.ai/docs/) is not evidence that
this pinned version can be installed through npm. If a host lacks the pinned
binary, obtain a matching platform build through its verified distribution/source
build process and check the output before proceeding. Record a missing build as
an installation blocker; do not silently install another version or remove the
caller's version check. A fresh-host OpenCode installation has not been verified
by Stow's local pilot tests.

The caller isolates OpenCode's HOME/XDG directories. An existing interactive
OpenCode login is therefore not automatically available. Supply an explicit
provider/model and the provider's environment credential to the caller process.
For Zen, use provider ID `opencode`, an unprefixed model ID such as `gpt-5-nano`,
and `OPENCODE_API_KEY`. That named credential is forwarded; all other `OPENCODE_*`
variables are removed so ambient runtime/config overrides do not enter this
dedicated process. Other providers' environment credentials remain inherited.
Never put credentials in a prompt, fixture, notes, checkpoint or report.

HOME is `<stateDir>/opencode/home`. XDG data, config, cache and state are aligned
with that home's `.local/share`, `.config`, `.cache` and `.local/state` directories.
Provider configuration belongs at
`<stateDir>/opencode/home/.config/opencode/opencode.jsonc`. If using a dedicated
legacy `auth.json` instead of an environment credential, provision it at
`<stateDir>/opencode/home/.local/share/opencode/auth.json`, with mode 600.
Keep both outside the workspace. The caller does not import an interactive login.

The receiving-host pilot also needed a matching model catalog at
`<stateDir>/opencode/home/.cache/opencode/models.json` and an explicit provider
configuration with its endpoint. Provision these for that binary when needed;
an empty catalog or a double-prefixed model ID does not establish model access.
Use a fresh caller state directory after upgrading this example: earlier runs
used separate XDG directories. Retain failed state for diagnosis.

## Prepare and run

Keep the Stow checkout, disposable workspace, registry and caller state in separate
directories. Use a small fixture for the first run. The manifest directory is the
base for relative paths; the workspace parent must exist and its root must not.
Use the existing [task manifest format](../../docs/task-manifest.md), then run
from the Stow checkout:

```sh
./bin/stow-s3 workspace prepare --manifest /absolute/path/task.json
```

Keep the returned workspace ID and registry directory.

Create a caller configuration outside the workspace:

```json
{
  "workspaceID": "ws_REPLACE_WITH_PREPARED_ID",
  "registryDir": "/absolute/path/registry",
  "stateDir": "/absolute/path/caller-state",
  "stowBinary": "/absolute/path/stow/bin/stow-s3",
  "opencodeBinary": "/absolute/path/opencode",
  "model": { "providerID": "your-provider", "id": "your-model" },
  "deadlineMs": 30000,
  "maxBytes": 1073741824,
  "maxFiles": 100000
}
```

Supply model access using the environment or dedicated files described above.
Credentials are not included in the caller configuration, progress file or
checkpoint. The 30-second save deadline above is an example choice, not a measured
universal default.

```sh
node examples/opencode/run.mjs caller.json prompt.txt
```

The caller starts OpenCode and creates its session in Stow's returned absolute
`working_directory`. For a manifest with `working_directory: "repo"`, the agent
works in `<root>/repo`, where it reads and writes `STOW_NOTES.md`. The caller writes
`STOW_PROGRESS.json` at the workspace root and tells the agent its relative path
(`../STOW_PROGRESS.json` in this example). Both files travel with the checkpoint.
Full conversation restoration is not promised.

On another host, adopt into a root that **does not exist**; only its parent may
be created beforehand:

```sh
received_root="$HOME/stow-received"
mkdir -p "$(dirname "$received_root")"
./bin/stow-s3 workspace adopt --handoff /absolute/path/bundle/handoff.json \
  --root "$received_root" --registry-dir /absolute/path/receiver-registry
```

Create a new caller configuration using the returned workspace ID and local
registry, then ask its agent to continue from the saved notes and progress.

The caller compares task file content and modes before execution and after
quiescence, before writing its own progress. Results and progress include `changes`
with `added`, `modified` and `deleted` workspace-relative paths. The scan ignores
root `STOW_PROGRESS.json`, root `.stow` and Git internals, and uses the configured
file/byte bounds. A successful turn with no task file change saves its checkpoint
with `reviewRequired: true`, returns exit code **2**, and blocks further prompts
until an explicit caller decision. Failed/interrupted saved turns also exit 2.
File changes alone do not prove the requested task was completed correctly.

## Failure and recovery

A pending turn blocks further prompts. Transient save contention retries at most
three times with jitter over 250/500 ms windows, within the configured deadline.
Every retry resolves the same persisted request key first. Permanent failures
return immediately. A lost reply can resolve to the already saved checkpoint.

```sh
node examples/opencode/run.mjs caller.json --recover
```

Recovery only reconciles an existing capture; it cannot rerun an uncertain model
prompt or silently save a later tree as the original turn. If no capture was
started, or its retained receipt is absent, the caller must inspect the work and
make an explicit recovery decision. A failed agent execution or a successful no-op
can have a successfully saved checkpoint; further work still requires an explicit
caller decision. To
start a deliberately new session, retain the old state directory for diagnosis
and use a new state directory in a new configuration. After a crash, remove a
stale `admission.lock` only after confirming its owner and OpenCode process stopped.

The current example does not offer interactive approval handling or arbitrary
background writers. MCP tool calls by an agent are a separate best-effort mode.

## Evidence and tests

`make test-agent` runs controller/adapter tests plus native transfer tests. The
native test simulates a lost reply, relocates an unedited bundle, removes the
sender's test data, adopts into a fresh registry and continues a deterministic
fixture. Another test runs the full caller against an OpenCode protocol fixture,
adopts files under `repo/` after removing sender data, verifies both process/session
directories, and checks saved no-op review and closed admission. These tests run
on one host and use no model.

`make test-agent` also discovers the opt-in real-model test, skipping it explicitly
unless `STOW_REAL_MODEL_CONFIG` is set. To run the model gate, supply a profile
outside the workspace containing `opencodeBinary`, `model`, `deadlineMs`,
`maxBytes` and `maxFiles`, as in the caller configuration above. Optional
`providerConfigPath` and `modelCatalogPath` select dedicated setup files to copy
into the test's fresh isolated home; environment credentials are inherited using
the rules above. The test supplies its own disposable workspace, registry, caller
state and source-built Stow binary.

For example (the credential stays in the caller environment):

```json
{
  "opencodeBinary": "/absolute/path/opencode",
  "model": { "providerID": "opencode", "id": "gpt-5-nano" },
  "deadlineMs": 30000,
  "maxBytes": 1048576,
  "maxFiles": 100
}
```

```sh
STOW_REAL_MODEL_CONFIG=/absolute/path/model-profile.json make test-agent-real
```

This makes a provider call. It adopts a bundle containing a CSV and sender notes
with the sender tree/registry removed, asks a fresh agent to write a result and
append a marked handoff summary, independently checks the numbers and notes, and
restores the saved checkpoint to verify those exact artifacts. Failed test state
is retained outside the workspace for diagnosis. The required target fails on a
missing profile; a skipped optional test is not a real-model pass.

A separate local probe exercised the installed OpenCode 2.0.16 admission, wait,
failed terminal outcome and file-only quiescence path with a nonexistent provider.
The receiving-host pilot verified adoption/integrity but exposed a wrong-directory
successful no-op. After this fix, the local real-model gate passed with
`opencode/space-bunny-free`, independently verifying the task outputs and restored
checkpoint artifacts after removing sender data. Continuation on a second physical
host, Linux runtime and real interruption/cleanup still require a pilot. The
example remains experimental until those pass. See the
[remediation record](../../docs/opencode-receiver-remediation-2026-09-29.md).

## Coordinating agent instructions

The [pasteable pilot instructions](../../docs/opencode-pilot-instructions.md)
cover installation, real-model work, recovery and cross-host evidence. Run that
coordinating agent in the Stow checkout; the caller launches the separate file-only
agent inside the prepared workspace.
