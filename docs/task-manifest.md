# Agent task manifest

`stow-s3 workspace prepare` creates a new, Stow-owned directory, copies the
declared local inputs into it, and prints a JSON launch descriptor. A task runner
starts its agent with the returned `working_directory` as the process cwd.

```json
{
  "version": 1,
  "root": "../tasks/fix-parser",
  "working_directory": "repo",
  "inputs": [
    { "source": "../../projects/parser", "destination": "repo" },
    { "source": "./task-notes.md", "destination": "repo/TASK.md" },
    { "source": "./fixtures", "destination": "repo/testdata" }
  ],
  "max_bytes": 67108864,
  "max_objects": 10000,
  "ttl_seconds": 604800,
  "registry_dir": "../stow-registry",
  "include_sensitive_inputs": false
}
```

Save this as `task.json`, then run:

```sh
stow-s3 workspace prepare --manifest task.json
```

Relative `root`, `registry_dir`, and input `source` paths are resolved from the
manifest's directory. Input destinations are relative to the new workspace;
directories copy their contents beneath the destination. The root's parent must
already exist and the root itself must not. Duplicate file destinations,
absolute or parent-traversing destination
paths, `.stow` destinations, symlink inputs, and non-regular input files are
refused. Common credential-looking filenames and paths (such as `.env`, `.npmrc`,
`.netrc`, SSH private keys, certificate/key files, and cloud credentials) are refused by default; an explicit
`include_sensitive_inputs: true` is required to copy them. This filename check
is a guard against accidental inclusion, not secret scanning. Sources are copied;
they are not moved or modified.

Preparation checks the seed's size and file count against the workspace limits
and returns a `base_identity` fingerprint, workspace ID, bucket, root, actual
working directory, effective capabilities, and authority. The prepared task
defaults to local `ReadWrite` authority and has no upstream access. It returns no
S3 credentials. A same-machine handoff reference can be printed with
`stow-s3 workspace handoff --id <workspace-id> --output handoff.json` and
resumed with `stow-s3 workspace resume --handoff handoff.json`.

Create an immutable snapshot with `stow-s3 workspace checkpoint --id <id>`.
Compare two snapshots with
`stow-s3 workspace diff --from <checkpoint-id> --to <checkpoint-id>`. Restore
one to a new workspace with
`stow-s3 workspace restore --checkpoint-id <checkpoint-id> --root <new-root>`.
The handoff reference may include `--checkpoint-id` to identify the snapshot
alongside its mutable workspace. Checkpoints live on the same machine, outside
the workspace, and are not portable archives. Capture scans the file tree before
and after copying and refuses detected changes; callers should stop writers
before requesting a checkpoint. `.stow` and `.git` are excluded. Common
credential-looking paths are recorded as excluded unless explicitly included.

Export a checkpoint for transfer with
`stow-s3 workspace export --checkpoint-id <id> --output checkpoint.tar.gz`;
import it on another machine with
`stow-s3 workspace import --archive checkpoint.tar.gz`. The archive is a
gzip-compressed tar containing checkpoint files and manifest metadata only. It
does not contain workspace credentials or registry state. Export refuses
sensitive-looking paths unless `--include-sensitive` is explicitly supplied;
import requires the same explicit opt-in for an archive that contains them.
Both operations default to limits of 1 GiB and 100,000 files, which can be
lowered or raised with `--max-bytes` and `--max-files`. Import validates paths,
types, sizes, and digests in a private staging directory before publishing the
checkpoint. It refuses traversal, symlinks, special files, corruption, and an
existing checkpoint ID. This is integrity validation, not encryption or secret
scanning.

Checkpoint directories are removed when their owning workspace is explicitly
destroyed or successfully reclaimed by TTL collection. A workspace kept alive
because it is adopted, locked, or otherwise not eligible for collection keeps
its checkpoints too. There is not yet a separate checkpoint retention or total
disk quota; hosts should manage workspace TTL and archive size deliberately.

TypeScript and Python expose thin wrappers over the same installed
`stow-s3 workspace` CLI contract. TypeScript imports them from
`@chester-hill-solutions/stow-s3/workspace`; Python exports snake_case helpers
from `stow_s3`. The wrappers return the CLI's JSON object and do not duplicate
workspace or archive semantics.

The current manifest accepts local file and directory inputs. It does not clone
Git repositories; callers that need a repo copy should provide an explicit local
source directory. Symlink inputs are rejected so preparation cannot copy a path
that escapes the declared source tree. Git-ref worktrees remain a later stage
of the workspace plan.

The workspace is an isolated copy for safe task editing. It is not an OS sandbox:
an agent running as the same user can still access other paths and network
resources allowed to that process. Limits are checked during preparation and
through Stow's object API, but ordinary file writes made directly by the agent
are not hard-enforced by the runtime.
