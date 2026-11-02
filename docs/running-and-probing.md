# Running and probing a Stow server

Use this guide to start a server, read its endpoint and credentials, and check readiness.
<!-- repo-only:start -->
Tests in `internal/s3api/probe_contract_test.go` and `cmd/stow-s3/probe_contract_test.go` verify the protocol behavior.
<!-- repo-only:end -->

## Start a server

Choose how to manage the server:

| Shape | Use when | Credentials |
|---|---|---|
| `withStow()` / `with_session()` (TypeScript, Python) | One test, one bucket, throwaway | generated for you |
| `stow-s3 serve --port 0` | A server you start and stop yourself | generated unless you pass them |
| `stow-s3 serve --port 9000` | A fixed port you need | generated unless you pass them |

`--port 0` asks the OS for an ephemeral port.
Read the bound endpoint from readiness output. A requested fixed port can be unavailable.

Credentials are generated unless you supply `--access-key` and `--secret-key`,
or `STOW_LOCAL_ACCESS_KEY_ID` and `STOW_LOCAL_SECRET_ACCESS_KEY`.
Use the reported credentials in your client.

## Learn the endpoint and the credentials

Use the versioned readiness channel for launchers. The legacy stdout line is available for manual use.

**The versioned channel (use this).** Pass `--ready-fd <n>` and the server writes
exactly one JSON object to that inherited descriptor instead of printing
credentials to stdout:

```json
{"protocolVersion":1,"binaryVersion":"0.3.3","endpoint":"http://127.0.0.1:39115",
 "region":"us-east-1","accessKeyId":"...","secretAccessKey":"...","mode":"local",
 "backend":"memory","capabilities":{"persistent":false,"multipart":true,
 "upstream":false,"conditionalWrites":true,"presignedUrls":true,"maxBytes":0,
 "maxObjects":0,"maxRequestBytes":8388608}}
```

`endpoint` is the bound address. `capabilities` reports enforced limits; zero means no configured limit.
Reject unknown `protocolVersion` values.

**The legacy line.** With no `--ready-fd`, the server prints one line to stdout:

```
STOW_READY endpoint=http://127.0.0.1:39115 access_key=... secret_key=... mode=local
```

Parse the legacy line only if your launcher explicitly supports it.
Resolve the binary by absolute path to avoid the unrelated GNU Stow package manager.
Validate `binaryVersion` on the versioned channel or `mode=` on the legacy line.

Read a fresh readiness record for each process. Do not reuse files from earlier runs.
A per-run pipe avoids stale files and closes when the process exits.

## Confirm it is up

Diagnostics live under `/_stow/`, outside S3 authentication:

```bash
curl -sf "$endpoint/_stow/health"     # {"status":"ok"}
curl -sf "$endpoint/_stow/status"     # mode, policies, version, uptime, bucket and object counts
curl -sf "$endpoint/_stow/metrics"    # Prometheus text
```

Use `/_stow/status` to check `mode`, `write_policy`, `version` and `uptime_sec`.

Read-only diagnostics, including `/_stow/inspect`, need no token on loopback.
From other addresses, send the configured `STOW_ADMIN_TOKEN` as `X-Stow-Admin`.
The two outbox mutation routes require that token on every address, including loopback.
Unauthorized requests return `404`.

`/health` is an S3 path, not a health probe.
An unsigned request returns `403 AccessDenied`; this does not indicate server health.

## What the defaults will not do

`stow-s3 serve` binds `127.0.0.1` by default.
Pass `--host` to accept connections through another interface.

Local mode is the default. Upstream credentials alone do not enable run-through mode.
Use `--mode run-through` or `STOW_MODE=run-through` to select an upstream.
Enable writes separately with `--allow-live-writes` or `STOW_ALLOW_LIVE_WRITES=true`.
The startup banner reports the effective settings:

```
stow mode: local
  cache policy: none
  write policy: local-only
  upstream: not in use; run-through requires --mode run-through or STOW_MODE=run-through
```

In local mode, the banner says `upstream: not in use`.
Run-through mode reports an `override:` note.
A `mirrorWrites` policy without live-write consent keeps writes local and reports this in the banner.
Set `--mode local` explicitly in launchers that require local-only storage.

## Platform support

Native package targets are Linux and macOS on amd64 and arm64.
Workspaces require a supported advisory file lock and refuse unsupported hosts.
The S3 server has no workspace-lock requirement. Use WSL on Windows for the workspace workflow.

## A launcher that does all of this correctly

```bash
#!/usr/bin/env bash
set -euo pipefail
STOW_BIN="${STOW_BIN:?set STOW_BIN to the stow-s3 binary}"

# Use a new temporary file for this run; the server writes through descriptor 3.
umask 077
ready=$(mktemp); exec 3>"$ready"
"$STOW_BIN" serve --port 0 --mode local --data-dir "$PWD/.stow" --ready-fd 3 &
server=$!
trap 'kill "$server" 2>/dev/null || true; exec 3>&-; rm -f "$ready"' EXIT

# One record, one line, on the descriptor.
for _ in $(seq 1 100); do [ -s "$ready" ] && break; sleep 0.05; done
[ -s "$ready" ] || { echo "no readiness record" >&2; exit 1; }

endpoint=$(python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); print(m["endpoint"])' "$ready")
export AWS_ACCESS_KEY_ID=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["accessKeyId"])' "$ready")
export AWS_SECRET_ACCESS_KEY=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["secretAccessKey"])' "$ready")
export AWS_REGION=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["region"])' "$ready")

# Confirm the server answers before continuing.
curl -sf "$endpoint/_stow/status" >/dev/null || { echo "not serving" >&2; exit 1; }
```

For S3 itself, use an SDK and path-style addressing, as every snippet here does.
`stow-s3 doctor` performs the equivalent checks for a human: binary path, binary
version, platform, temp directory, backends, S3 configuration, and a live bind plus
a `/_stow/health` round trip.

## See also

- [`docs/task-manifest.md`](task-manifest.md) — the agent workspace lifecycle.
<!-- repo-only:start -->
- [`docs/workspace-contract.md`](workspace-contract.md) — what a workspace is and
  which host can provide one.
<!-- repo-only:end -->
- [`docs/compat-contract.md`](compat-contract.md) — the S3 surface, including what
  is deliberately not implemented.
- [README](../README.md) — the full reference.
