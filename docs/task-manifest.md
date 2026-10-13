# Agent task manifest

`stow-s3 workspace prepare` creates a directory, copies declared inputs, and returns a JSON launch descriptor.
Run the agent with the returned `working_directory` as its current directory.

```json
{
  "version": 1,
  "root": "../tasks/fix-parser",
  "working_directory": "repo",
  "inputs": [
    { "source": "./task-notes.md", "destination": "repo/TASK.md" },
    { "source": "./fixtures", "destination": "repo/testdata" }
  ],
  "repositories": [
    {
      "source": "../../projects/parser",
      "destination": "repo",
      "ref": "refs/heads/main"
    }
  ],
  "max_bytes": 67108864,
  "max_objects": 10000,
  "max_checkpoint_bytes": 536870912,
  "max_checkpoints": 20,
  "max_workspaces": 100,
  "ttl_seconds": 604800,
  "registry_dir": "../stow-registry",
  "team": "platform",
  "include_sensitive_inputs": false
}
```

Save the manifest as `task.json` and prepare it:

```sh
stow-s3 workspace prepare --manifest task.json
```

The [workspace guide](portable-workspace-usage.md) covers S3 access, portable object checkpoints,
capture recovery, registry policy and MCP tools. Existing v1 file checkpoints remain supported.

## Paths and inputs

Relative `root`, `registry_dir` and source paths resolve from the manifest's directory.
Destinations are relative to the new workspace. Directory inputs copy their contents beneath the destination.
The root's parent must exist; the root itself must not exist.
Sources are copied without modification.

Preparation rejects duplicate file destinations, absolute destinations, parent traversal, `.stow` destinations,
symlinks and non-regular input files. Destination names must satisfy portable filesystem naming rules,
including restrictions on Windows device names and unsupported characters.

Preparation rejects common credential paths such as `.env`, `.npmrc`, `.netrc`, SSH keys and cloud credentials by default.
Set `include_sensitive_inputs: true` to include them.
This checks filenames, not file contents; it is not secret scanning.

## Git inputs

Each repository needs an explicit `ref` resolving to a local commit.
Preparation creates a detached shallow checkout of that commit inside the workspace.
It does not modify the source repository or create a linked worktree.
The task copy has no source remote.

Uncommitted files, untracked files, older history, submodules and Git LFS payloads are not included.
Repository checkouts accept regular files only. Symbolic links are rejected.
Sensitive paths in the selected tree also require `include_sensitive_inputs: true`.

## Preparation result and teams

Preparation checks seeded bytes and file count, including shallow Git metadata, against workspace limits.
The result includes:

- `base_identity`, workspace ID, bucket, root and working directory;
- effective capabilities and authority;
- resolved commits in `repositories`;
- checkpoint limits and registry headroom.

Prepared tasks default to local `ReadWrite` authority, with no upstream access or S3 credentials.

`team` selects `<registry_dir>/teams/<team>`. Commands list, resume and collect within that partition.
It is a directory partition, not an authorization identity.
Without `team`, records use the registry root.
Use the same team value on later commands.

Create and resume a same-machine handoff:

```sh
stow-s3 workspace handoff --id <workspace-id> --output handoff.json
stow-s3 workspace resume --handoff handoff.json
```

## Capture, compare and restore

Pause writers before capture. A live owner can keep its workspace claim during capture or handoff.
`resume` takes ownership; capture and handoff read the registered directory.

```sh
stow-s3 workspace checkpoint --id <id>
stow-s3 workspace diff --from <checkpoint-id> --to <checkpoint-id>
stow-s3 workspace restore --checkpoint-id <checkpoint-id> --root <new-root>
```

Capture scans before and after copying and rejects detected changes.
This does not provide a snapshot of an actively changing filesystem.
It excludes `.stow`, `.git` and sensitive-looking paths unless explicitly included.
Checkpoints are stored outside the workspace, on the same machine.
A handoff can include `--checkpoint-id` to name a checkpoint alongside the mutable workspace.

## Transfer archives

```sh
stow-s3 workspace export --checkpoint-id <id> --output checkpoint.tar.gz
stow-s3 workspace preview --archive checkpoint.tar.gz
stow-s3 workspace import --archive checkpoint.tar.gz
```

The gzip tar archive contains checkpoint files and manifest metadata, without credentials or registry state.
Export and import require `--include-sensitive` for sensitive-looking paths.
Preview validates paths, types, sizes, digests and trailer without extracting files.
It reports included and sensitive-looking paths for review.

Preview and import default to 1 GiB and 100,000 files.
Set `--max-bytes` and `--max-files` to change those limits.
Import validates in private staging before publication.
It rejects traversal, symlinks, special files, corruption and an existing checkpoint ID.
Integrity checks do not encrypt data or scan for secrets.

For a portable handoff, use `handoff --archive <path>`.
The document records archive SHA-256, size and file count.
Move it with the archive; relative archive paths resolve from the document's directory.
On the destination, `adopt --handoff <reference> --root <new-root>` verifies, imports and restores the checkpoint.
A digest mismatch fails before directory creation.

A version-1 handoff contains an ID and registry directory, without an archive.
`resume` accepts it for local use. `adopt` requires a portable archive.

## Deltas

`delta --from <base> --to <target> --output <path>` records changed paths and payloads.
`apply --delta <path> --base <checkpoint-id>` verifies the named base and creates a new checkpoint.
The base remains unchanged.

A base mismatch returns `ErrDeltaConflict`, without modifying the target.
Payload digests are verified before writing. Documents over `MaxDeltaBytes` fail at decode.
Delta bounds match archive bounds. Deltas between different workspaces are rejected.
Deltas do not merge independent edits.

## Retention limits

Destroying a workspace or successful TTL collection removes its checkpoint directories.
Adopted, locked and otherwise ineligible workspaces retain their checkpoints.

`max_checkpoint_bytes` and `max_checkpoints` limit retained payload bytes and checkpoint count per workspace.
Bytes count captured file sizes, without manifest or filesystem overhead.
Zero or omitted values mean unlimited.
Capture fails if either limit would be exceeded; older checkpoints are not automatically evicted.
These limits are separate from per-capture `--max-bytes` and `--max-files`.
The preparation result reports them in `capabilities.checkpoint_limits`.

`max_workspaces` limits registry record count, within the selected team partition.
It is checked before preparation creates anything.
Zero or omitted values mean unlimited. At capacity, use `workspace prune` for missing directories
or `workspace collect` for eligible expired workspaces.
Stow does not remove another workspace automatically to make room.

This manifest limit applies to one preparation request; it is not stored as registry policy.
Supply it on every request that needs the bound.
`capabilities.registry_bound` reports the limit, current count and remaining capacity.
For standing registry policy, see the [workspace guide](portable-workspace-usage.md#retention).
Retention limits exclude filesystem overhead and files written directly into the registry.

## Client wrappers and isolation

TypeScript exports workspace helpers from `@chester-hill-solutions/stow-s3/workspace`.
Python exports snake_case helpers from `stow_s3`.
Both wrap the native CLI and return its JSON results.

A workspace is a separate copy for task editing. It is not an OS sandbox.
An agent can access other paths and network resources permitted to its process.
Limits apply during preparation and Stow API operations; they do not prevent direct filesystem writes.
See the [Agent Isolate proposal](history/plans/agent-isolate-exploration.md) for a possible execution boundary.
