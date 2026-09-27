# Stow S3 — agent instructions

You are reading this because someone pointed you at Stow S3, an S3-compatible
object store and task workspace that runs on the local machine. It speaks real
S3 over HTTP with SigV4. For coding tasks, Stow can prepare an isolated working
directory from declared local inputs; for disposable S3 tests, a scoped session
cleans itself up on close.

## When to reach for it

For a coding task whose files should persist across process restarts, use a
prepared workspace. Define a version 1 task manifest and run
`stow-s3 workspace prepare --manifest task.json`; start the agent with the
returned `working_directory` as its cwd. The manifest supports local files,
directories, and explicit refs from local Git repositories. Git preparation
uses a fresh shallow checkout of the selected commit; dirty files, older
history, submodules, and Git LFS payloads are excluded. See
[`docs/task-manifest.md`](../docs/task-manifest.md) for resume, checkpoint,
diff, restore, same-machine handoff, and portable archive commands.

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

## Install

**The npm and PyPI install surfaces are not yet usable.** The `v0.2.0` release
partially published the main npm package to GitHub Packages, but failed before
publishing its platform packages. PyPI was not published. Do not spend a turn
trying those install commands until a complete release is announced. The Go
module *is* published and is the shortest path today.

The npm package is distributed through **GitHub Packages**, not npmjs.org. Once
a complete release is announced, add this to `.npmrc` so npm resolves the scope
against the intended registry:

```ini
@chester-hill-solutions:registry=https://npm.pkg.github.com
```

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

## The pattern you want

Start a scoped session, use a normal S3 client, let the session clean up. This
is the shortest correct path and it is what the library is built around.

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
