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
provider/model and the provider's documented environment credential to the caller
process. Never put credentials in a prompt, fixture, notes, checkpoint or report.
Ask for missing model access without printing existing credential values.

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

Supply model access through the caller's environment. Credentials are not included
in the configuration, progress file or checkpoint. OpenCode gets its own home,
configuration and data directories beneath `stateDir`; the example does not copy
credentials from another OpenCode installation. The 30-second save deadline above
is an example choice, not a measured universal default.

```sh
node examples/opencode/run.mjs caller.json prompt.txt
```

The caller requests `STOW_NOTES.md` from the agent and writes `STOW_PROGRESS.json`
with the prompt and terminal outcome. These ordinary files travel with the
checkpoint. Full conversation restoration is not promised. On another host,
adopt the handoff, create a new caller configuration for that workspace, and ask
its agent to read those files before continuing.

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
make an explicit recovery decision. A failed agent execution can have a successfully
saved checkpoint; further work still requires an explicit caller decision. To
start a deliberately new session, retain the old state directory for diagnosis
and use a new state directory in a new configuration. After a crash, remove a
stale `admission.lock` only after confirming its owner and OpenCode process stopped.

The current example does not offer interactive approval handling or arbitrary
background writers. MCP tool calls by an agent are a separate best-effort mode.

## Evidence and tests

`make test-agent` runs controller/adapter tests plus a native transfer test. The
native test simulates a lost reply, relocates an unedited bundle, removes the
sender's test data, adopts into a fresh registry and continues a deterministic
fixture. It runs on one host and uses no model.

A separate local probe exercised the installed OpenCode 2.0.16 admission, wait,
failed terminal outcome and file-only quiescence path with a nonexistent provider.
Successful real-model continuation and a second physical host still require a
pilot with model access. The example remains experimental until those pass.

## Coordinating agent instructions

The [pasteable pilot instructions](../../docs/opencode-pilot-instructions.md)
cover installation, real-model work, recovery and cross-host evidence. Run that
coordinating agent in the Stow checkout; the caller launches the separate file-only
agent inside the prepared workspace.
