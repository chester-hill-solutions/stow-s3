# Multiplayer lab — throwaway prototype

Can several agents work in one directory, explain their changes only to agents
interested in those files, and recover the shared work through Stow checkpoints?
This experiment explores a product above the storage primitive. It adds no runner,
model, messaging or collaboration API to Stow itself.

From the repository root, run:

```sh
make multiplayer
```

The default uses deterministic actors, opens a loopback dashboard, builds a Revenue
Observatory and verifies a checkpoint restore. The command prints the URL; open it
to watch ownership, participants, reports, recipients and the artifact. Ctrl-C closes
the finished view. Node 22+ and the repository's pinned Go toolchain are required;
the example imports the committed TypeScript distribution.

For actual agents using the local OpenCode installation and selected Zen model:

```sh
make multiplayer-real
```

Real mode makes provider calls. It requires OpenCode v2.0.16, its local cached model
catalog and an existing Zen API login or `OPENCODE_API_KEY`. It uses the most recent
locally selected `opencode` provider model. An explicit profile follows the
[existing caller profile](../opencode/README.md):

```sh
node examples/multiplayer-prototype/run.mjs --real --profile /absolute/profile.json
```

No key is printed or included in checkpoint data. Each participant receives a
separate OpenCode server, session, external HOME and caller state, with shared
workspace read access and file-tool edit permissions restricted to its assigned
outputs and notes. Process tools are denied. These permissions are a tool policy;
this experiment is for trusted local participants, without OS sandbox enforcement.

## What happens

Mira calculates revenue from `data/orders.csv` and owns `data/metrics.json`. Jules
builds `site/index.html` and subscribes to the metrics file. Ren writes
`docs/data-quality.md` and listens only to `docs/`.

All three begin concurrently in the same directory. After a turn settles, the
coordinator compares that participant's assigned outputs and publishes its notes
as an immutable JSON report under `collaboration/messages/`. Recipients are selected
from exact file or directory subscriptions. A filesystem watcher queues relevant
reports without model polling. The designer receives the analyst's contract change
at the next safe turn boundary; the auditor gets no extra turn.

The coordinator stops every writer, exports portable assignments/history and saves
a single Stow checkpoint. Only confirmed capture permits acknowledgment. It restores
the checkpoint to another local directory, compares the artifacts and opens a fresh
room to verify pending reports retain their recorded recipients. Fresh registration
and ownership are explicit; process identities, leases, credentials and local
acknowledgments are never restored.

This is a finite scripted scenario, not a general scheduling service. Claims are
cooperative reservations; OpenCode permissions enforce static output assignments.
Agents can read files while their peers are changing them. Reports trigger work
only after the relevant producer has settled. Checkpoints require a global pause.

## Other runs

```sh
# Offline normal and interrupted recovery checks, without a persistent view:
make multiplayer-check

# Interrupt an admitted real designer, then resume with a fresh session:
node examples/multiplayer-prototype/run.mjs --real --interrupt-designer

# Reopen a completed room without more model calls:
node examples/multiplayer-prototype/run.mjs --view /absolute/evidence.json
```

Each run leaves a scratch `stow-multiplayer-PROTOTYPE-*` directory containing the
workspace, restored workspace, local registry, external caller state and sanitized
`evidence.json`. The command prints its path. Interrupted work is recovered from
the current directory and report files; this does not yet implement recovering a
coordinator crash or moving an in-flight session across hosts.

See [room semantics](ROOM.md) and [observations](NOTES.md). Delete or absorb the
prototype after the product model is decided.

## Subsequent product direction

The user subsequently chose shared live editing without file claims: agents know who
is present, what they are doing and their last read/edit locations, and adapt to relevant
peer/dependency changes. See the [product plan](../../docs/realtime-multiplayer-plan.md)
and [engineering detail](../../docs/realtime-multiplayer-engineering.md). This prototype
remains dated evidence of its earlier ownership-based model; it does not implement
the new experience. The latest RT-0 starts with continuous native activity/file
observation and shadow decisions; RT-1 tests useful awareness and bounded steering
before conditional guarded-editor expansion. Native observation does not prevent stale
writes. Current RT work lives in the [standalone collaboration repository](https://github.com/chester-hill-solutions/gangcode/blob/main/docs/plan.md). This example remains historical evidence.
