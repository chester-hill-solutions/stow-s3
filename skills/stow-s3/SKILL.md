---
name: stow-s3
description: Local S3-compatible object store and isolated ready-to-work filesystem workspaces for tests, CI, development, and agents. Use when a coding agent needs declared files staged into its own working directory, a persistent task with checkpoint/handoff, or disposable local S3 for tests. Also use when asked to mock, fake, stub, or localise S3. Provides task-manifest and workspace CLI workflows, TypeScript/Python CLI wrappers, scoped S3 sessions, the Go runtime, and the stow-s3 CLI. Triggers on "start the agent with files in place", "isolated agent workspace", "handoff an agent task", "checkpoint agent changes", "needs an S3 bucket in a test", "don't want to hit real S3 in CI", "S3 mock", "localstack alternative", "put an artifact somewhere in a build".
---

# Stow S3

An S3-compatible object store and filesystem workspace that runs on the local
machine. Real S3 over HTTP with SigV4, so existing SDK code works unchanged. A
prepared workspace is a persistent isolated copy for a coding task; a scoped
S3 session is disposable and deletes itself when it closes.

Repository: https://github.com/chester-hill-solutions/stow-s3
Machine-readable instructions: https://stow.chesterhillsolutions.ca/agent.md

## Decide first

For a coding task, prepare a workspace when the agent needs declared project
files in its own cwd and its work must survive a process restart. Start with a
version 1 task manifest and `stow-s3 workspace prepare --manifest task.json`;
use the returned `working_directory` as the agent process cwd. Manifests accept
local files/directories and explicit refs from local Git repositories. Git
inputs stage only the selected commit into a fresh shallow repository; dirty
files, older history, submodules, and Git LFS payloads are excluded. The
workspace CLI also supports resume, handoff, checkpoint, diff, restore, portable
checkpoint export/import, `list` to find a workspace again without keeping a note of
its id, `collect` for TTL reclamation, and `prune` to forget entries whose directory is
gone. See [`docs/task-manifest.md`](../../docs/task-manifest.md).

Reach for a disposable scoped session only when **all** of these hold:

- the bytes do not have to survive the process
- the bucket does not have to be shared with another machine
- the data does not have to be there after the test run

If any one of them does, use a prepared workspace or real S3 as appropriate.
Scoped sessions are disposable; prepared workspaces persist until explicit
destruction or safe TTL collection. Workspace isolation is not an OS sandbox,
and direct filesystem writes are not hard-limited by Stow's object API quotas.

If the code only calls two or three S3 operations and never asserts on
behaviour you care about, a plain in-memory fake is less machinery. Stow is
worth it when the code exercises the SDK itself — signing, multipart, range
reads, error shapes.

## Install

**The npm and PyPI install surfaces are not yet usable.** The `v0.2.0` release
partially published the main npm package to GitHub Packages, but failed before
publishing its platform packages. PyPI was not published. Do not try those
install commands first. The Go module is published and is the shortest working
path.

The npm package is distributed through **GitHub Packages**, not npmjs.org. Once
a complete release is announced, add this to `.npmrc` so npm resolves the scope
against the intended registry:

```ini
@chester-hill-solutions:registry=https://npm.pkg.github.com
```

GitHub Packages requires authentication for npm packages, including public
ones. Authenticate with a GitHub personal access token (classic) that has the
`read:packages` scope:

```bash
npm login --scope=@chester-hill-solutions --auth-type=legacy --registry=https://npm.pkg.github.com
```

Use the token as the password and keep it in your user-level npm config, not in
the project `.npmrc`. See [GitHub's npm registry authentication
guide](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-npm-registry).

```bash
go get github.com/chester-hill-solutions/stow-s3/pkg/stow   # Go, works now
```

For TypeScript or Python, work from a source checkout:

```bash
git clone https://github.com/chester-hill-solutions/stow-s3
cd stow-s3
make build                        # bin/stow-s3 for your platform
export STOW_BIN="$PWD/bin/stow-s3"

# TypeScript
cd packages/stow-s3 && npm install && npm run build && cd ../..

# Python
pip install -e "packages/stow-s3-py[boto3]"
```

A published package ships its own binary and needs no `STOW_BIN`. If the binary
is missing or unrunnable, `stow-s3 doctor` reports which of the two sides is
broken rather than failing opaquely.

## Starting and probing a server

Reach for this when you are writing a launcher, a CI step, or anything that starts
`stow-s3` itself rather than calling `withStow`.

- **Health is `/_stow/health`, not `/health`.** Every path outside `/_stow/` is an
  S3 path and requires SigV4, so an unsigned `GET /health` returns
  `403 AccessDenied`. That is a wrong-path probe, not an unhealthy server.
  `/_stow/health`, `/_stow/status`, and `/_stow/metrics` answer unsigned from
  loopback and need `STOW_ADMIN_TOKEN` (header `X-Stow-Admin`) from any other
  address. `/_stow/status` also reports `mode`, `write_policy`, `version`, and
  `uptime_sec`, so it is the one to assert on.
- **`--port` is a request.** `0` means ephemeral, and any port can be taken, so read
  the bound address from the server rather than assuming one. Credentials are
  generated unless you pass `--access-key`/`--secret-key`.
- **Use `--ready-fd 3`.** The server writes one JSON record to that descriptor
  (`endpoint`, `region`, `accessKeyId`, `secretAccessKey`, `mode`, `backend`,
  `capabilities`) and keeps credentials off stdout. Without the flag it prints a
  `STOW_READY endpoint=… access_key=… secret_key=…` line instead, which is easy to
  satisfy by an unrelated program's output. Do not cache readiness on disk: it
  outlives the process that wrote it.
- **It is loopback-only by default** and local-only by default. `STOW_ENDPOINT` and
  ambient AWS credentials describe an upstream but do not select one; that needs
  `--mode run-through` or `STOW_MODE=run-through`, and propagating writes upstream
  needs `--allow-live-writes` or `STOW_ALLOW_LIVE_WRITES=true`. The banner says
  `upstream: not in use` when local.
- **No Windows build**, and the workspace backend needs an advisory file lock that
  Windows does not offer, so `workspace prepare` refuses there. Use WSL.

Full contract, with a copy-pasteable launcher:
[`docs/running-and-probing.md`](../../docs/running-and-probing.md).

## Disposable S3 fixture

Use a scoped session when a test or build step needs temporary S3 objects: one
call, a private server on an ephemeral port, a bucket already created, and
cleanup on close. For coding tasks whose files must persist or be handed off,
use the prepared-workspace flow described above.

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
// server and data are gone here, even if the callback threw
```

Python:

```python
from stow_s3 import with_session

with with_session() as session:
    s3 = session.s3_client()
    bucket = session.new_bucket_name()   # collision-resistant; you create it
    s3.create_bucket(Bucket=bucket)
    s3.put_object(Bucket=bucket, Key="input.json", Body=b'{"task":"summarize"}')
    body = s3.get_object(Bucket=bucket, Key="input.json")["Body"].read()
```

The two clients differ deliberately and the difference is not a bug: the
TypeScript session creates the bucket and exposes it as `session.bucket`, while
the Python session performs no S3 I/O and leaves `create_bucket` to you. That
is what keeps `boto3` an optional extra rather than a hard dependency.

## What the session actually guarantees

Server-enforced, so these are safe to rely on:

- `STOW_*`, `S3_*`, and `AWS_*` are stripped from the child environment before
  anything is set, so a stray cloud credential cannot turn a local session into
  a run-through one.
- The server exits when its parent process dies, including on SIGKILL.
- Defaults: 16 MiB and 1,000 objects per session, 8 MiB per request body. The
  server enforces them on every request; the client does not police them.
- Capabilities and limits come from the readiness message, so a client never
  reports a limit the server has not confirmed.

## When the network goes away

Use this when the bytes have to survive a network that might not come back, but the
work does not have to be shared. A run-through server keeps a local store in front
of a real bucket, and a separate cache in front of that:

```sh
# Warm the exact keys the task needs. A prefix or a glob is refused.
stow-s3 prewarm --bucket models --data-dir /srv/agent --keys bert.bin,bert.config.json

# Ask what you can read with the network gone.
curl -s http://127.0.0.1:PORT/_stow/inspect        # loopback needs no token
```

`stow-s3 prewarm --offline` runs the same verb as a check: it fetches nothing and
reports what the cache already holds. `cached: true` means the key is in the cache
**once the warm finished**, not that it was fetched — a warm larger than the cache
limit reports what survived it.

`readable: false` on a cached key means it is indexed but its lifetime has run. Read
`has_expiry` beside `expires_at`, because an entry with no lifetime has a zero
timestamp, and a zero that means both "never expires" and "expired long ago" is not a
value to branch on.

`--offline` (or `STOW_OFFLINE=true`) refuses every upstream call, so the guarantee is
checkable rather than a side effect of a timeout. `--offline=false` overrides an
exported `STOW_OFFLINE=true` for one command.

A dead upstream is reported as 503 naming it, not as a 500 blaming this server. A
read the cache can serve still succeeds.

## Other shapes

- `Stow.start()` (TypeScript) — a server that outlives one callback, for a
  suite that shares one endpoint.
- `stow-s3 serve --port 0` — a server you start yourself. A hand-run server
  prints its endpoint and credentials on stdout; a session receives them over
  an inherited file descriptor so they never reach a log.
- Go, in-process with no HTTP listener and no credentials:

```go
import stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"

rt, err := stow.Open(stow.Options{Backend: stow.BackendMemory})
```

Backends are `memory` (throwaway), `filesystem` (survives a restart), and
run-through (local in front of an upstream bucket).

## Not implemented

Versioning, ACLs, bucket policies, lifecycle rules, replication, event
notifications, object tagging, object lock, S3 Select, KMS-backed encryption.

Implemented: bucket CRUD, object put/get/head/copy/delete, prefix listing with
pagination, range reads, conditional reads and writes, MD5/CRC32/CRC32C/SHA-1/
SHA-256 checksums, multipart uploads, presigned requests, SigV4.

## Reporting problems

Include the output of `npx stow-doctor --json`, the language and version, and
the S3 operation that failed. That is enough to reproduce almost anything.
