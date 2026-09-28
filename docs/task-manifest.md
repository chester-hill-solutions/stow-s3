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
  "ttl_seconds": 604800,
  "registry_dir": "../stow-registry",
  "team": "platform",
  "include_sensitive_inputs": false
}
```

Save this as `task.json`, then run:

```sh
stow-s3 workspace prepare --manifest task.json
```

Relative `root`, `registry_dir`, input `source`, and repository `source` paths
are resolved from the manifest's directory. Input destinations are relative to
the new workspace; directories copy their contents beneath the destination.
Each repository needs an explicit `ref` that resolves to a commit in the local
repository. Preparation fetches only that commit into a new detached shallow
repository inside the task root. It does not register a linked worktree or
modify the source repository, add a source remote to the task copy, or include
source changes that are uncommitted or untracked. Older Git history is not
fetched. Submodules are not initialized; Git LFS content is not downloaded.
The root's parent must already exist and the root itself must not. Duplicate
file destinations, absolute or parent-traversing destination paths, `.stow`
destinations, symlink inputs, and non-regular input files are refused. Common
credential-looking filenames and paths (such as `.env`, `.npmrc`, `.netrc`, SSH
private keys, certificate/key files, and cloud credentials) are refused by
default; an explicit `include_sensitive_inputs: true` is required to copy them.
This filename check is a guard against accidental inclusion, not secret
scanning. Sources are copied; they are not moved or modified.

Destination paths and copied input path segments are checked against portable
filesystem naming rules, including Windows device names and characters that
cannot be represented consistently across supported platforms.

Preparation checks the seed's size and file count (including the shallow Git
metadata) against the workspace limits and returns a `base_identity`
fingerprint, workspace ID, bucket, root, actual working directory, effective
capabilities, authority, and a `repositories` array with the resolved commit
for each Git input. The prepared task defaults to local `ReadWrite` authority
and has no upstream access. It returns no S3 credentials. A same-machine
handoff reference can be printed with
`stow-s3 workspace handoff --id <workspace-id> --output handoff.json` and
resumed with `stow-s3 workspace resume --handoff handoff.json`.

`team` partitions the registry: the workspace and its checkpoints are filed under
`<registry_dir>/teams/<team>`, so two jobs sharing one machine cannot resume,
list, or collect each other's workspaces. It is a directory, not a label — a
sweep in one partition does not see the other — and it names an organization, not
an identity, so it carries no authorization of its own. Omit it and the
workspace is filed at the registry root, which is where every workspace that
predates the field already is.

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
inspect a transfer archive before importing it with
`stow-s3 workspace preview --archive checkpoint.tar.gz`; import it on another
machine with
`stow-s3 workspace import --archive checkpoint.tar.gz`. The archive is a
gzip-compressed tar containing checkpoint files and manifest metadata only. It
does not contain workspace credentials or registry state. Export refuses
sensitive-looking paths unless `--include-sensitive` is explicitly supplied;
import requires the same explicit opt-in for an archive that contains them.
Preview validates paths, types, sizes, file digests, and the archive trailer
without extracting or publishing files. It reports included paths and any
sensitive-looking paths, so the import opt-in decision can be made after review.
Both operations default to limits of 1 GiB and 100,000 files, which can be
lowered or raised with `--max-bytes` and `--max-files`. Import validates paths,
types, sizes, and digests in a private staging directory before publishing the
checkpoint. It refuses traversal, symlinks, special files, corruption, and an
existing checkpoint ID. This is integrity validation, not encryption or secret
scanning.

A handoff can carry the bytes as well as the names. `handoff --archive
<path>` writes the named checkpoint beside the reference and records the
archive's SHA-256, size, and file count in the document, so the two travel
together; `adopt --handoff <reference> --root <new-root>` on the receiving
machine verifies that digest, imports the archive, and restores a working
workspace in one step. A relative archive path is resolved against the
reference's own directory, so a copied pair stays valid. The digest is the
contract: a handoff that names bytes it cannot vouch for is refused before any
directory is created. A version 1 reference — an ID and a registry directory,
no archive — is still accepted by `resume` and still means the same thing;
`adopt` refuses it, and says which flag would make it portable.

Two machines that already share a point can exchange only what changed. `delta
--from <base> --to <target> --output <path>` writes a versioned document naming
both ends and carrying the content of every added or changed path;
`apply --delta <path> --base <checkpoint-id>` brings a third point to the
target and publishes a new checkpoint, leaving the base untouched. The base is
required rather than defaulted, because the conflict rule is stated against it:
a path the document says was A and is now B, applied to a target holding
neither, is one writer's intent applied to a state that does not exist. That is
`ErrDeltaConflict` and it is a refusal, never a merge, and the target is left
byte-identical. Content is verified against the digest each change declares
before it is written, and a document over `MaxDeltaBytes` is refused at decode.
Bounds match the archive path's, so a delta and an archive of the same work are
accepted or refused together. A delta crossing two workspaces is refused.

Checkpoint directories are removed when their owning workspace is explicitly
destroyed or successfully reclaimed by TTL collection. A workspace kept alive
because it is adopted, locked, or otherwise not eligible for collection keeps
its checkpoints too. `max_checkpoint_bytes` and `max_checkpoints` set separate
per-workspace caps on retained checkpoint payload. Payload bytes are the sum of
captured file sizes; manifest and filesystem overhead are not included. Zero or
an omitted field means unlimited. A checkpoint that would cross either cap is
refused; Stow never evicts an older checkpoint to make room. These caps are
separate from the per-capture `--max-bytes` and `--max-files` archive limits.
The `prepare` result reports both values in `capabilities.checkpoint_limits`.
Retention limits bound captured payload bytes and count, not manifest or
filesystem overhead or arbitrary files written directly into the registry.

TypeScript and Python expose thin wrappers over the same installed
`stow-s3 workspace` CLI contract. TypeScript imports them from
`@chester-hill-solutions/stow-s3/workspace`; Python exports snake_case helpers
from `stow_s3`. The wrappers return the CLI's JSON object and do not duplicate
workspace or archive semantics.

The manifest accepts local file and directory inputs plus explicit refs from
local Git repositories. Repository checkouts support regular files only;
checked out symbolic links are refused to avoid a staged path resolving outside
the task tree. A selected tree that contains a sensitive-looking path still
requires `include_sensitive_inputs: true`. The path check is not secret
scanning.

The workspace is an isolated copy for safe task editing. It is not an OS sandbox:
an agent running as the same user can still access other paths and network
resources allowed to that process. Limits are checked during preparation and
through Stow's object API, but ordinary file writes made directly by the agent
are not hard-enforced by the runtime.
