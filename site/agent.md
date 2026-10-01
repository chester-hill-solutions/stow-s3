# Stow — agent instructions

You are reading this because someone pointed you at Stow, a local object store
and task workspace. Its centre of gravity is the workspace: a real directory with
a durable identity, verified checkpoints, and a handoff you can hand to another
host. It also speaks real S3 over HTTP with SigV4, for the part of a job that is
genuinely a disposable S3 test.

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

## Checkpoints and handoff for agent work (0.3.0 development tree)

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
See the portable workspace guide and experimental OpenCode example in the repository.

## Install

**The npm and PyPI install surfaces are not yet announced as usable.** The
`v0.2.0` release published the main package and all four platform carriers to
public npmjs, then failed before the GitHub Release, so there is no release page
and no `SHA256SUMS.txt` to verify a download against, and PyPI was not published.
A clean install of `0.2.0` from npm has not been verified end to end, which is
the reason to wait rather than a claim that it is broken. Do not spend a turn
trying those install commands until a complete release is announced. The Go
module *is* published and is the shortest path today.

**Platforms: Linux and macOS, on x64 and arm64.** There is no Windows build, and
the workspace backend needs an advisory file lock that Windows does not provide,
so it refuses there rather than guessing. On Windows, use WSL.

The next candidate, **0.3.0**, targets the same public npmjs distribution and has
**not yet been published**. Publisher setup, live-provider gates and anonymous
exact-version installation must pass before availability is announced. Published
npm versions are immutable. For the new candidate, remove any old scope override
to `npm.pkg.github.com`; it would send installation requests to the wrong
registry.

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
  capabilities — and the descriptor dies with the process, so nothing on disk can
  outlive a server the way a cached "ready" file does.

```bash
./bin/stow-s3 serve --port 0 --mode local --data-dir "$PWD/.stow" --ready-fd 3
```

The full contract — both readiness channels and their traps, the probe routes, what
the defaults will not do, and platform support — is in
[`docs/running-and-probing.md`](../docs/running-and-probing.md). Every claim in it
is asserted by a test.

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

- Repository and full README: https://github.com/chester-hill-solutions/stow-s3
- Diagnostics: `npx stow-doctor` or `stow-s3 doctor`
- Licence: Apache 2.0
