# Agent host setup

First-run configuration for the hosts agents use. The zero-configuration path is
the **object mode** server: one directory, one bucket, no workspace registry.
Workspace mode is the second step, once a workspace exists.

Verify the binary before wiring any host:

```sh
stow-s3 --version
stow-s3 doctor
```

## Claude Code

The plugin delivers the guidance (when to use Stow, the three verbs, the ask).
It does not auto-start a server, because a server needs a directory and bucket
that the plugin cannot choose for you.

```sh
claude plugin marketplace add chester-hill-solutions/stow-s3
claude plugin install stow@chester-hill-solutions
```

Add an object-mode server:

```sh
claude mcp add stow -- stow-s3 mcp \
  --object-dir "$HOME/.stow-objects" --bucket stow --create-bucket
```

The five object tools appear (`stow_object_read_for_save`, `stow_object_save`,
`stow_object_resolve_save`, `stow_object_release_observation`,
`stow_object_capabilities`). First prompt to prove it:

> Save "hello" to the key demo.txt with stow_object_save, then read it back with
> stow_object_read_for_save and show me the bytes.

## Codex CLI

Add the server to `~/.codex/config.toml`:

```toml
[mcp_servers.stow]
command = "stow-s3"
args = ["mcp", "--object-dir", "/absolute/path/.stow-objects", "--bucket", "stow", "--create-bucket"]
```

Restart the session and run the same first prompt. Codex sees the tools under
the `stow` server name.

## OpenCode

Add the server to `opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "stow": {
      "type": "local",
      "command": ["stow-s3", "mcp", "--object-dir", "/absolute/path/.stow-objects", "--bucket", "stow", "--create-bucket"],
      "enabled": true
    }
  }
}
```

The experimental controller in the repository is a narrower profile; ordinary
OpenCode sessions use the MCP entry above.

## Node and Python

Both clients start and stop a private server for you, so there is no host
configuration at all:

- TypeScript: [`withStow`](../../packages/stow-s3/README.md) starts a loopback endpoint, waits
  for readiness, and cleans up with the callback.
- Python: [`with_session`](../../packages/stow-s3-py/README.md) is the same shape as a context
  manager.

## Workspace mode

Once a workspace exists (`workspace prepare`), an editing server needs its
registry and write roots:

```sh
claude mcp add stow-workspace -- stow-s3 mcp \
  --registry-dir "$HOME/.stow-registry" --workspace-id "$WORKSPACE_ID" \
  --workspace-files --workspace-edit --read-root docs --write-root docs \
  --coordination host_quiesced
```

This exposes the four guarded file tools. A committed save survives the process
being killed; `workspace resume` reopens the work and `workspace checkpoint`
captures it.

## Version scope

These notes describe the **0.4.3 native build**. The published 0.4.2 binary and
npm 0.3.7 predate the SIGKILL-safe save, the configurable edit bound and
CRC64NVME multipart completion.

## Limits to know before promising anything

- Workspace saves: 64 KiB by default, `--max-file-bytes` up to 1 MiB.
- Multipart full-object checksums: CRC32, CRC32C, CRC64NVME, SHA-1, SHA-256;
  composite is refused.
- Streaming SigV4 (`STREAMING-AWS4-HMAC-SHA256-PAYLOAD`) is not supported.
- The supported S3 surface and its divergences are in the
  [compatibility contract](../contracts/compat-contract.md).
