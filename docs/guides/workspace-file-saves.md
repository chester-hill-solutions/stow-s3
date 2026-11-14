# Managed workspace file saves

**Qualification:** installed native consumers on Linux and macOS, x64 and
ARM64, exercise observed-token saves, receipt resolution and subsequent saved
checkpoint reads. The [release notes](../releases/v0.4.0.md) link the exact
release checks. Host-quiesced coordination is required; native protocol checks
do not qualify every model provider or external host.

**Availability:** the editing flags and tools require a v0.4.0 native binary
from [GitHub Releases](https://github.com/chester-hill-solutions/stow-s3/releases/tag/v0.4.0)
or a source build. The npm 0.3.7 binaries do not include them. See the
[release notes](../releases/v0.4.0.md).

Use the managed file tools when a host needs comparison before replacement and
a retained result after a lost reply. The host prepares the workspace, supplies
its registry and identity, and keeps all writers outside this editor quiescent:
filesystem tools, S3 calls, direct Runtime calls and background processes.
The advisory writer Session does not fence those other writers.

## Connect

Use the v0.4.0 native executable or build from source. The workspace must already be
registered, with the selected directories present.

```sh
/absolute/path/to/stow-s3 mcp \
  --registry-dir /absolute/registry --workspace-id WORKSPACE_ID \
  --workspace-files --workspace-edit \
  --read-root docs --write-root docs --coordination host_quiesced \
  --timeout 30s
```

Repeat the read/write root flags for additional existing relative directories;
each write root must be within the read scope. A dot write root explicitly
selects the whole permitted workspace. Private paths, symlinks and aliases remain
excluded. This profile writes complete small regular files under existing real
parents; it adds no directory creation, rename, deletion, append or patch tool.

The server claims one registered owner and shares its editor across clients.
Another live owner prevents startup. Registered permissions and storage quotas
remain authoritative; capture byte/file settings do not replace them. A
read-only owner can observe and resolve but cannot save. Editing refuses object,
resource-only, selected-checkpoint and browser modes. Existing file resources
remain reads and do not mint comparison tokens.

## Observe and save

Call stow_workspace_file_read_for_save with a relative path:

```json
{"path":"docs/notes.md"}
```

The result contains complete base64 bytes, optional UTF-8 text, observed absence,
digest, mode, time, generation and an opaque token with an expiry. Optional text
is omitted when necessary to fit the response budget. Only a missing leaf under
an accessible valid parent establishes absence.

Persist a random request key and the intended path, bytes and options before
calling stow_workspace_file_save:

```json
{
  "path":"docs/notes.md",
  "request_key":"caller-generated-unique-key",
  "observation_token":"token-from-read-for-save",
  "text":"Updated notes\n"
}
```

Supply exactly one text or data_base64 field, including an empty string for an
empty file. Supply exactly one observation_token or replace=true. Replacement
deliberately bypasses comparison while retaining authorization and quotas.
Existing mode, content type and user metadata are preserved. Each publication
gets a new generation, including an identical-byte replacement.

Decoded files are bounded to 64 KiB. There are at most 64 live observations with
a five-minute lifetime. Encoded arguments and tool results each fit 256 KiB;
base64 supports binary bodies and text that would exceed the JSON argument
budget through escaping. Eight editing/resource calls share admission and a
deadline capped at ten seconds or the configured timeout, whichever is shorter.

Two clients observing the same basis cannot both commit a replacement. A stale
basis is a conflict. Release a no-longer-needed basis with
stow_workspace_file_release_observation; invalid or expired tokens refuse.
Releasing a token does not remove a retained save result.

Retention holds at most 1,024 terminal requests and one pending request within
an 8 MiB charged journal budget, with 64 KiB per record. Full retention refuses
new saves while retained lookup remains available. There is no automatic
receipt eviction; preserve the journal whenever a result is uncertain.

## Resolve an interrupted reply

Interpret outcome independently from MCP isError. Only committed confirms the
save; a known committed effect remains committed if later bookkeeping fails.
Results contain the original generation and metadata, not historical bytes.
After a known not_committed result, a deliberately new save uses a fresh
observation and new request key; the original key retains its original meaning.

After a lost reply, keep the original request key and stop new writes. On
restart add --recover-file-saves to the same editing configuration. Every root
containing save records requires this explicit recovery selection, including a
terminal-only journal. Ordinary startup refuses it. Then call
stow_workspace_file_resolve_save:

```json
{"path":"docs/notes.md","request_key":"caller-generated-unique-key"}
```

Resolve starts no payload write. It returns the original retained result even
when later bytes occupy the path. New live observations are unavailable while
publication is unresolved. Current-file resources may still return complete
bytes without establishing a save basis or ready version.

Missing evidence does not prove that no effect occurred. Publishing without
durable proof remains unknown even when bytes match. Preserve the journal and
dependent workspace; operator reconciliation is required for that ambiguity.
Do not delete records to retry or downgrade. Observation tokens disappear on
restart; receipt resolution needs only the original path and request key.

## Capture a continuation checkpoint

After a known committed file save, the caller may separately invoke
stow_checkpoint_create with its workspace_id and a distinct persisted
request_key. Wait for a committed checkpoint result and retain its checkpoint
identity. File saving does not implicitly capture, run an agent or assert task
success. Interrupted checkpoint capture uses the existing checkpoint resolve
workflow; it is a separate request from the file save.

See the [editing contract](../contracts/workspace-editing-contract.md) for
operation semantics and refusal rules.

<!-- repo-only:start -->
See the [editing contract](../contracts/workspace-editing-contract.md),
[execution plan](../design/workspace-resource-plan.md#w26-prerequisite-audit-and-execution-boundary)
and the [CLI/MCP qualification](../reviews/2026-11-12-workspace-editing-mcp.md).
The [native qualification](../reviews/2026-11-11-workspace-editor-recovery.md)
separately records backend process-kill boundaries.

<!-- repo-only:end -->
