# Stow: agent instructions

Stow exposes a native S3-compatible HTTP endpoint and an embedded object engine
in Go and WebAssembly. Use prepared workspaces to checkpoint and transfer an
agent's files, browser file history to save edits offline, or scoped S3 sessions
for disposable test data. Native run-through mode caches reads from an upstream
bucket. The host owns execution and sandbox enforcement.

## When to reach for it

For a coding task whose files should persist across process restarts, use a
prepared workspace. Define a version 1 task manifest and run
`stow-s3 workspace prepare --manifest task.json`; start the agent with the
returned `working_directory` as its cwd. The manifest supports local files,
directories, and explicit refs from local Git repositories. Git preparation
uses a fresh shallow checkout of the selected commit; dirty files, older
history, submodules, and Git LFS payloads are excluded. See
[`docs/task-manifest.md`](../docs/task-manifest.md) for resume, checkpoint,
diff, restore, same-machine handoff, and portable archive commands. This is
filesystem isolation for task editing, not an OS security sandbox; direct file
writes by an agent are not hard-limited by Stow's object API quotas.

Use a scoped session when you need only disposable S3 bytes and none of these
are true:

- the bytes have to survive the process
- the bucket has to be shared with something outside this machine
- it has to be reachable after the test run

If any of those hold and a prepared workspace does not fit, use real S3. Scoped
sessions are disposable; prepared workspaces persist until explicitly destroyed
or safely collected.

Typical uses: a test that uploads and downloads, a build step that passes an
artifact between stages, an agent that needs scratch space, or a fixture that
would otherwise need a live bucket.

## Browser and cache integrations

Browser file history uses the WASM object engine and IndexedDB, with retained
stages, undo/redo, and explicit current-file sync. Host applications supply
credentials and networking; this profile does not start an HTTP S3 server in
the browser. Start with the [browser contract](/docs/browser-file-history/)
and the [runnable editor](/docs/browser-editor/).
The website walkthrough is a simulation; the editor runs real WASM and S3.

The native run-through adapter caches upstream reads, supports prewarming exact
keys, and serves local/cached bytes without upstream calls in explicit offline
mode. WASM makes the object engine embeddable in compatible hosts; do not infer
qualified edge-platform support from that portability alone.

With live writes enabled, native run-through commits locally first and records
the exact version in a durable outbox. Transient failures retry with bounded
backoff. A changed upstream object or an unknown upstream baseline stops
propagation; local bytes remain available, and the caller decides how to resolve
the conflict. Recovery acknowledges an attempted write only after the remote
ETag, content type, user metadata, and requested checksum match that version;
an already absent object confirms a delete. Recovery can still require another
conditional attempt, so do not assume exactly-once delivery. Outbox retry and
discard routes require the admin token, including on loopback. See the
[run-through contract](/docs/s3-compatibility/#62-read-through-cache-semantics).

## Checkpoints and handoff for agent work

Prepare a separate workspace and hold it with `workspace serve` while the agent
works. Before saving, stop known writers and write objective, progress and next
steps into ordinary workspace files. Use `workspace checkpoint --portable` with
a persisted random `--request-key` and a positive `--timeout`. Only report a save
when its outcome is `committed`; after an uncertain reply, reuse the same options
with `--resolve`. Retry only transient failures with bounded backoff, then report
failure and the last confirmed checkpoint. Do not rerun a prompt as a storage retry.

The local `mcp` command exposes the same storage operations within configured
workspace/transfer scope. Agent-requested saves are best effort. A host controller
must own prompt admission to guarantee its chosen turn boundary. Full chat state,
process memory, credentials and external side effects are outside a checkpoint.
Use the [workspace guide](/docs/workspaces/) for capture and recovery options.

## Install

Native v0.3.0 binaries for Linux and macOS are distributed through
[GitHub Releases](https://github.com/chester-hill-solutions/stow-s3/releases/tag/v0.3.0).
Verify the archive with the release checksum manifest and run `stow-s3 --version`.
npm and PyPI publication are deferred; use source checkouts for those clients.


The 0.3.0 TypeScript and Python clients are not yet published. Use a source checkout.
Native package targets are Linux and macOS on x64 and arm64. Use WSL on Windows.

Go, published and installable now:

```bash
go get github.com/chester-hill-solutions/stow-s3/pkg/stow
```

TypeScript and Python, from a source checkout:

```bash
git clone https://github.com/chester-hill-solutions/stow-s3
cd stow-s3
make build                      # produces bin/stow-s3 for your platform
export STOW_BIN="$PWD/bin/stow-s3"

# TypeScript
cd packages/stow-s3 && npm install && npm run build && cd ../..

# Python
pip install -e "packages/stow-s3-py[boto3]"
```

`STOW_BIN` tells the client which server binary to run. A published package
carries its own binary and does not need it.

If the binary cannot be found or run, `stow-s3 doctor` reports which of the
client and server sides is broken instead of failing opaquely.

## Starting a server yourself

A scoped session is the default, but a launcher, a CI step, or a test that wants
one endpoint for several cases starts the server directly. Two things about that
are not guessable:

- **The health endpoint is `/_stow/health`, not `/health`.** Every path outside
  `/_stow/` is an S3 path and needs SigV4 signing, so an unsigned `GET /health` is
  refused with `403 AccessDenied`. That 403 means the probe used the wrong path,
  not that the server is unhealthy. `/_stow/health`, `/_stow/status`, and
  `/_stow/metrics` answer with no credential from loopback; from any other address
  (another container, another host) they need `STOW_ADMIN_TOKEN` in the
  `X-Stow-Admin` header. `/_stow/status` is the better probe because it reports
  `mode`, `write_policy`, `version`, and `uptime_sec`.
- **Do not hardcode the port, and do not cache readiness.** `--port` is a request:
  `0` asks for an ephemeral port and any port can be taken, so the bound address is
  whatever the server reports. Credentials are generated unless you pass them. Pass
  `--ready-fd 3` and the server writes a JSON readiness record to that descriptor
  carrying the endpoint it actually bound, its region, its credentials, and its
  capabilities. The descriptor dies with the process, so nothing on disk can
  outlive a server the way a cached "ready" file does.

```bash
./bin/stow-s3 serve --port 0 --mode local --data-dir "$PWD/.stow" --ready-fd 3
```

Use the [server guide](/docs/server/) for readiness fields and probe authentication.

`STOW_ENDPOINT` and AWS credentials in the environment describe an upstream; they do
not select one. The server is local-only unless you pass `--mode run-through` or
`STOW_MODE=run-through`, and it propagates writes upstream only with
`--allow-live-writes` or `STOW_ALLOW_LIVE_WRITES=true`. The startup banner states
which you got: `upstream: not in use` means local.

## Disposable S3 fixture

When you only need temporary S3 objects for a test or build step, start a scoped
session, use a normal S3 client, and let the session clean up. This pattern is
for disposable data; use a prepared workspace when task files need to persist or
be handed off.

TypeScript:

```ts
import { GetObjectCommand, PutObjectCommand } from "@aws-sdk/client-s3";
import { withStow } from "@chester-hill-solutions/stow-s3";

const body = await withStow(async ({ s3, bucket }) => {
  await s3.send(new PutObjectCommand({
    Bucket: bucket,
    Key: "input.json",
    Body: '{"task":"summarize"}',
  }));

  const got = await s3.send(new GetObjectCommand({
    Bucket: bucket,
    Key: "input.json",
  }));
  return got.Body?.transformToString();
});
```

Python:

```python
from stow_s3 import with_session

with with_session() as session:
    s3 = session.s3_client()
    bucket = session.new_bucket_name()   # you create the bucket yourself
    s3.create_bucket(Bucket=bucket)
    s3.put_object(Bucket=bucket, Key="input.json", Body=b'{"task":"summarize"}')
    body = s3.get_object(Bucket=bucket, Key="input.json")["Body"].read()
```

The two clients differ deliberately. The TypeScript session creates the bucket
and exposes it as `session.bucket`. The Python session does no S3 I/O at all:
`new_bucket_name()` returns a collision-resistant name and creating the bucket
is yours, which is what keeps `boto3` an optional extra rather than a hard
dependency.

## What a session guarantees

These are enforced by the server, not promised by the client, so you can rely
on them:

- It cannot inherit your cloud configuration. `STOW_*`, `S3_*`, and `AWS_*` are
  stripped from the child environment before anything is set.
- It cannot outlive your process. The server exits when the parent dies,
  including on SIGKILL.
- It is bounded. A default session holds at most 16 MiB and 1,000 objects, and
  one request body is capped at 8 MiB.
- It reports its real capabilities from the readiness message rather than
  assuming them.

## Long-running instead of scoped

Use `Stow.start()` in TypeScript when the server must outlive a single
callback, and `stow-s3 serve --port 0` when you want a server you start
yourself. Both hand back an endpoint and credentials; the hand-run server
prints them on stdout, a session receives them over a file descriptor so they
never reach a log.

Go has no HTTP server in the embedded profile at all:

```go
import stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"

rt, err := stow.Open(stow.Options{Backend: stow.BackendMemory})
```

## What is not implemented

Versioning, ACLs, bucket policies, lifecycle rules, replication, event
notifications, object tagging, object lock, S3 Select, and KMS-backed
encryption. Check the README before assuming any of these exist; the gap is
narrow and deliberate, but guessing wastes a cycle.

## Links

- [API reference](/docs/api/): public methods, parameters, return types and errors.

- Repository and full README: https://github.com/chester-hill-solutions/stow-s3
- Diagnostics: `npx stow-doctor` or `stow-s3 doctor`
- Licence: Apache 2.0
