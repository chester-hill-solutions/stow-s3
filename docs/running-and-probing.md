# Running and probing a Stow server

This is the contract for driving `stow-s3` from a script, a launcher, or a test
harness: how to start a server, how to learn the endpoint and credentials it
actually got, how to confirm it is up, and what its defaults will not do to you.

It exists because each of those has an answer that is not the obvious one. Every
*behaviour* described here is asserted by a test — the route table in
`internal/s3api/probe_contract_test.go`, the process-level claims in
`cmd/stow-s3/probe_contract_test.go` — so if the behaviour changes, a test fails
rather than this document quietly becoming wrong. The advice is not asserted,
because advice is not behaviour.

## Start a server

Three shapes, in the order you probably want them:

| Shape | Use when | Credentials |
|---|---|---|
| `withStow()` / `with_session()` (TypeScript, Python) | One test, one bucket, throwaway | generated for you |
| `stow-s3 serve --port 0` | A server you start and stop yourself | generated unless you pass them |
| `stow-s3 serve --port 9000` | A fixed port you need | generated unless you pass them |

`--port` is a **request, not a promise**. `0` asks the operating system for an
ephemeral port, and any port can be taken, so the bound address is whatever the
server reports after it listens. Credentials are likewise generated when you do
not supply `--access-key` / `--secret-key` (or `STOW_LOCAL_ACCESS_KEY_ID` /
`STOW_LOCAL_SECRET_ACCESS_KEY`). Never hardcode either value: hardcoding the port
reports a server ready while it listens somewhere else, and hardcoding keys
reports a server that will reject every request.

## Learn the endpoint and the credentials

There are two channels, and they are not interchangeable.

**The versioned channel (use this).** Pass `--ready-fd <n>` and the server writes
exactly one JSON object to that inherited descriptor instead of printing
credentials to stdout:

```json
{"protocolVersion":1,"binaryVersion":"0.2.0","endpoint":"http://127.0.0.1:39115",
 "region":"us-east-1","accessKeyId":"...","secretAccessKey":"...","mode":"local",
 "backend":"memory","capabilities":{"persistent":false,"multipart":true,
 "upstream":false,"conditionalWrites":true,"presignedUrls":true,"maxBytes":0,
 "maxObjects":0,"maxRequestBytes":8388608}}
```

`endpoint` is the address the server actually bound. `capabilities` is what the
server is enforcing, not what the client assumed. A limit of `0` means no limit,
never "unlimited" in the sense of unknown. Reject a `protocolVersion` you do not
know rather than parsing it anyway.

**The legacy line.** With no `--ready-fd`, the server prints one line to stdout:

```
STOW_READY endpoint=http://127.0.0.1:39115 access_key=... secret_key=... mode=local
```

It is a human-readable fallback, and reading it is a parsing decision you own. It
is also easy to satisfy by accident: the tokens `stow` and `STOW_READY` appear in
unrelated output, and Debian ships an unrelated `stow` package manager that prints
to stdout. Resolve the binary by absolute path and check `binaryVersion` (the
versioned channel) or the `mode=` field before you trust a grep.

**Do not cache readiness across processes.** Anything you write to disk can
outlive the process that wrote it, and the next run will cheerfully reuse a
"ready" record for a server that is gone. The descriptor is anonymous and is
closed when the process exits, so a per-run pipe cannot have this problem — which
is the reason to prefer it over a file.

## Confirm it is up

Diagnostics live under `/_stow/`, outside S3 authentication:

```bash
curl -sf "$endpoint/_stow/health"     # {"status":"ok"}
curl -sf "$endpoint/_stow/status"     # mode, policies, version, uptime, bucket and object counts
curl -sf "$endpoint/_stow/metrics"    # Prometheus text
```

`/_stow/status` is the better probe of the two, because it reports `mode`,
`write_policy`, `version`, and `uptime_sec` — the fields worth asserting on, and a
low uptime on a port you expected to be long-lived means the process was reaped
and replaced.

**Which routes need a credential.** The four read-only routes above answer with no
token from loopback, and require `STOW_ADMIN_TOKEN` (sent as `X-Stow-Admin`) from
any other address — which includes another container and another host. The two
`/_stow/outbox/*` routes change what is propagated to a live provider and always
require the token, including on loopback. A request without a valid token gets
`404`, not `403`, so the route is not advertised.

**`/health` is not the health endpoint.** Every path outside `/_stow/` is an S3
path, and an unsigned request to one is refused with `403 AccessDenied` before the
route is even considered. A `403` on `/health` means the request was unsigned,
not that the server is unhealthy. This is the same answer real S3 gives, and it is
correct.

## What the defaults will not do

`stow-s3 serve` binds `127.0.0.1` unless you pass `--host`. A server that is not on
loopback is not reachable from another machine or another container.

The server runs in **local mode by default**, and no combination of ambient
environment variables changes that. `STOW_ENDPOINT`, `STOW_ACCESS_KEY_ID`, and
`STOW_SECRET_ACCESS_KEY` describe an upstream; they do not select one. Run-through
mode requires naming it, with `--mode run-through` or `STOW_MODE=run-through`, and
propagating writes upstream requires a second, separate opt-in
(`--allow-live-writes` or `STOW_ALLOW_LIVE_WRITES=true`). The startup banner states
which of these you got:

```
stow mode: local
  cache policy: none
  write policy: local-only
  upstream: not in use; run-through requires --mode run-through or STOW_MODE=run-through
```

In run-through mode that last line becomes an `override:` note, which is how you
tell the two situations apart: `upstream: not in use` means local, and an
`override:` line means an upstream *is* in use. A `mirrorWrites` policy without the
live-writes opt-in keeps every write local and says so in the banner.

Pin `--mode local` if you want the server to say so explicitly rather than by
default. It is worth doing in CI, where the environment is not yours.

## Platform support

Release artifacts are published for `linux` and `darwin`, on `amd64` and `arm64`.
There is no Windows build, and adding one is more than a row in the release matrix:
the workspace backend requires an advisory file lock to establish that nobody else
is using a workspace, and Go's standard library only offers one on Unix, so on
Windows `stow.OpenWorkspace` refuses rather than guessing. That follows from the
lock being required rather than optional, and no test here can observe it, because
every CI platform this runs on has the lock. The S3 server itself has no such
requirement. On Windows, use WSL.

## A launcher that does all of this correctly

```bash
#!/usr/bin/env bash
set -euo pipefail
STOW_BIN="${STOW_BIN:?set STOW_BIN to the stow-s3 binary}"

# Descriptor 3 is a pipe this script reads; the server never sees a path.
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

# Confirm before declaring the server up, and assert the mode you expect.
curl -sf "$endpoint/_stow/status" >/dev/null || { echo "not serving" >&2; exit 1; }
```

For S3 itself, use an SDK and path-style addressing, as every snippet here does.
`stow-s3 doctor` performs the equivalent checks for a human: binary path, binary
version, platform, temp directory, backends, S3 configuration, and a live bind plus
a `/_stow/health` round trip.

## See also

- [`docs/task-manifest.md`](task-manifest.md) — the agent workspace lifecycle.
- [`docs/workspace-contract.md`](workspace-contract.md) — what a workspace is and
  which host can provide one.
- [`docs/compat-contract.md`](compat-contract.md) — the S3 surface, including what
  is deliberately not implemented.
- `README.md` — the full reference.
