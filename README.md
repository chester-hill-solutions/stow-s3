# Stow

A detached working environment for coding agents: a workspace of files and a
cache of S3 data, both of which survive the machine, both of which you can verify,
and neither of which needs a live privileged connection to use.

It is also an S3-compatible object store, and a good one for local development and
tests. That part is not the interesting part, and this README leads with the other
thing because the interesting part is what an agent can be *given*.

## The two pillars

**A workspace** is an isolated directory that already contains the inputs you
declared, checkpointed immutably, diffable, and resumable on another machine.
Fan out sixteen agents, each with its own workspace, and each one hands back a
checkpoint you can compare.

**An S3 cache** (`stow-s3 serve --mode run-through`) keeps a local copy of object
data, scoped to one bucket, that stays readable when the upstream is unreachable.

Both are instances of one property: **the agent process never holds the upstream
AWS credential.** The keys live in the stow server's environment. The agent is
handed a local endpoint and a generated local key pair, and cannot reach anything
it was not given. Disconnect it from the network and the workspace is still there
and the cached data is still readable.

This matters more than it first appears. An agent that reads untrusted text — a
ticked issue, a fetched page, a file from a repository it does not control — is
being fed instructions by whoever wrote that text. Giving it a credential that
reaches a real bucket gives those instructions somewhere to go. Stow's answer is
that the thing the agent holds is worth nothing outside the process that made it.

## This is not version control

Worth saying plainly, because the vocabulary invites the mistake. A delta
describes what a path *was* and what it *is now*, and applying one verifies the
"was" against the checkpoint you name. A delta that crosses two workspaces is
refused outright. There is no merge, no conflict resolution, and no history you
can walk.

So: fan out, checkpoint, compare, and take what you want. Do not fork a workspace,
edit it for a week, and expect to bring the work back. That is not what this does,
and pretending otherwise is how you lose an afternoon to a refusal that was
designed to stop you.

## What Stow provides

- A workspace for an agent: declared inputs, immutable checkpoints, diff,
  restore, portable handoff, and adoption on another machine.
- An S3 HTTP server for local development and tests.
- A scoped session API for TypeScript and Python: one call starts a private
  server, hands back a ready S3 client, and cleans everything up on close.
- A run-through cache: local data with an upstream behind it, usable offline.
- A Go runtime for in-process, memory-backed storage.
- A WebAssembly runtime for Node.js and browser integrations.
- Filesystem persistence for data that must survive a process restart.
- SigV4 authentication for the S3 endpoint.
- `stow doctor`, a diagnostic that reports both the client and server sides of an
  installation.

Applications use Stow to create buckets and upload or download files through the
S3 APIs they already use. A **bucket** is a named container. An **object** is a
file stored in a bucket, together with metadata such as its content type, size,
and ETag.

Stow implements the common S3 operations needed by application and test workloads:

- create, inspect, list, and delete buckets;
- put, get, head, copy, and delete objects;
- list objects with prefixes and pagination;
- range reads;
- conditional writes and reads;
- MD5, CRC32, CRC32C, SHA-1, and SHA-256 checksums;
- multipart uploads;
- presigned requests.

Stow implements a subset of Amazon S3. Versioning, ACLs, bucket policies, lifecycle rules, replication, notifications, tagging, object lock, S3 Select, and KMS-backed encryption are not implemented.

If you are choosing between Stow and a fuller S3 emulator for a test suite: Stow
is one static binary with no runtime dependency, it is fast enough to run
per-test-case, and it implements the operations above and refuses the rest
explicitly rather than pretending. It is not a drop-in for a workload that needs
versioning or bucket policies.

## When to use it

Use Stow when a workload needs S3 behavior without a cloud account or network
service, or when you are giving an agent somewhere to work that will still be
there afterwards:

- giving a coding agent an isolated directory seeded with its declared inputs;
- checkpointing what an agent did, and comparing two agents' work;
- moving a workspace to another machine and picking it up there;
- keeping object data readable when the upstream is not;
- local application development;
- unit and integration tests that need a real S3-shaped API;
- CI jobs that need disposable object storage;
- tools and agents that need a private scratch bucket;
- Go programs that want an in-process object runtime;
- browser or Node.js integrations that need an embedded runtime;
- development against an existing upstream S3-compatible service.

## A workspace, end to end

Declare the inputs in a manifest. The manifest states where the workspace lives,
which files to seed it with, and which team's partition of the registry it
belongs to. The root's parent directory must already exist:

```json
{
  "version": 1,
  "root": "/srv/agents/task-42",
  "team": "platform",
  "inputs": [
    { "source": "/srv/inputs/spec.md", "destination": "spec.md" },
    { "source": "/srv/inputs/fixtures", "destination": "fixtures" }
  ]
}
```

Prepare it. The result names the workspace, the totals it seeded, and the
directory to use as the agent process's cwd:

```sh
stow-s3 workspace prepare --manifest task.json
```

```json
{
  "workspace_id": "ws_843fbd0cdf9b744b",
  "root": "/srv/agents/task-42",
  "seeded_objects": 2,
  "seeded_bytes": 16,
  "team": "platform"
}
```

The agent works in `root`. When you want what it did, checkpoint it. The
checkpoint is immutable and content-addressed. Every later verb needs `--team
platform` too, because a team's partition is a real directory boundary and a
checkpoint filed under it is invisible without it:

```sh
stow-s3 workspace checkpoint --id ws_843fbd0cdf9b744b --team platform
```

```json
{
  "checkpoint_id": "cp_a4424a293103532626d4a507",
  "workspace_id": "ws_843fbd0cdf9b744b",
  "files": 2,
  "bytes": 16
}
```

Two checkpoints of the same workspace diff into exactly what changed between
them, which is how you compare two agents working from the same inputs:

```sh
stow-s3 workspace checkpoint --id ws_843fbd0cdf9b744b \
  --parent cp_a4424a293103532626d4a507 --team platform
stow-s3 workspace diff --from cp_a4424a293103532626d4a507 \
  --to cp_77f5079ad25db3e0a66ae6cb --team platform
```

```json
{
  "changes": [{ "path": "report.md", "kind": "added" }]
}
```

To move the work to another machine, write a handoff. With `--archive` the
document is portable and carries a checkpoint you can adopt anywhere:

```sh
stow-s3 workspace handoff \
  --id ws_843fbd0cdf9b744b \
  --checkpoint-id cp_77f5079ad25db3e0a66ae6cb \
  --team platform \
  --archive /tmp/task-42.tar.gz \
  --output /tmp/task-42.handoff.json
```

`--output` writes the document to that path and prints nothing, which is the
point: the file is the artifact the receiving machine gets. Without `--archive`
the reference is local (version 1); with it, portable (version 2).

On the other machine, adopt it. The archive and its digest are verified before
anything is written, so a handoff that arrived over a channel is checked rather
than trusted:

```sh
stow-s3 workspace adopt --handoff /tmp/task-42.handoff.json --root /srv/agents/task-42
```

```json
{
  "workspace_id": "ws_460a0b6f67fb107b",
  "root": "/srv/agents/task-42",
  "checkpoint_id": "cp_77f5079ad25db3e0a66ae6cb",
  "seeded_objects": 3,
  "seeded_bytes": 32
}
```

Note the ids above: `adopt` built `ws_460a0b6f67fb107b` from a handoff naming
`ws_843fbd0cdf9b744b`. A handoff reference *names* a workspace; it does not carry
its identity. `workspace resume --handoff` returns the workspace the document names
— `ws_843fbd0cdf9b744b` above, not the copy — while `adopt` builds a different
workspace from the archive. And `resume` refuses `--handoff` alongside
`--registry-dir` on purpose:

```console
$ stow-s3 workspace resume --handoff task-42.handoff.json --registry-dir /elsewhere
workspace resume accepts --handoff or --id/--registry-dir/--team, not both
```

A caller that could redirect a handoff at a different registry would resume a
workspace somewhere the document never pointed at.

The last verb worth knowing is `delta`, which writes the difference between two
checkpoints to a document the other side can apply:

```sh
stow-s3 workspace delta --from cp_a4424a293103532626d4a507 \
  --to cp_77f5079ad25db3e0a66ae6cb --team platform --output change.stowdelta
```

```json
{
  "version": 1,
  "base_id": "cp_a4424a293103532626d4a507",
  "target_id": "cp_77f5079ad25db3e0a66ae6cb",
  "files": 1,
  "bytes": 16
}
```

`apply` verifies every precondition before it writes anything, so a delta whose
payload no longer matches its digest is refused rather than applied.

**Verify the document itself.** The digests inside a delta cover its content bytes
and nothing else, so a document altered in transit can keep every one of them
intact and still name a different destination than the sender chose. Pass the
digest `delta` reported:

```sh
stow-s3 workspace apply --delta change.stowdelta --base cp_a4424a293103532626d4a507 \
  --team platform --expect-sha256 58338f9f00c64e4d2...
```

```console
$ stow-s3 workspace apply --delta renamed.stowdelta --base cp_a4424a... --team platform \
    --expect-sha256 58338f9f00c64e4d2...
delta refused: delta document does not match the digest the sender published:
the document hashes to f7388c05885e824e8..., the sender published 58338f9f00c64e4d2...
```

The check is opt-in, and that is a real limitation rather than a convenience: a
receiver with no trusted copy of the digest cannot invent one, so it applies what
it was given. If your delta crossed a channel you do not control, treat the digest
as part of the delta and carry it with the document.

See [the task manifest and workspace lifecycle guide](docs/task-manifest.md)
for the schema, archive safety rules, and limitations. A workspace is filesystem
isolation for task editing, not an OS sandbox; direct filesystem writes by the
agent are not hard-limited by Stow's object API quotas. Task manifests can stage
explicit refs from local Git repositories; dirty files, older history,
submodules, and Git LFS payloads are not included.

## Install

**The npm and PyPI install surfaces are not yet usable.** The `v0.2.0` release
partially published the main npm package to GitHub Packages, but failed before
publishing its platform packages. PyPI was not published. Do not use the npm or
Python install commands below until a complete release is announced.

**The npm package is distributed through GitHub Packages, not npmjs.org.** This
organisation owns nothing on npmjs — the scope does not exist there — and a
scope-specific registry in `.npmrc` overrides `--registry`. GitHub Packages
requires authentication to install npm packages, including public packages.
Once a complete release is announced, configure the scope in the project
`.npmrc`:

```ini
# .npmrc
@chester-hill-solutions:registry=https://npm.pkg.github.com
```

Then authenticate with a GitHub personal access token (classic) that has the
`read:packages` scope. You can keep the token in your user-level npm config by
running:

```sh
npm login --scope=@chester-hill-solutions --auth-type=legacy --registry=https://npm.pkg.github.com
```

Use your GitHub username, the token as the password, and an email address at the
prompts. Do not commit the token to the project `.npmrc`. After authentication,
`npm install @chester-hill-solutions/stow-s3` resolves against GitHub Packages.
See [GitHub's npm registry authentication guide](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-npm-registry).
PyPI has no such configuration step.

The Go module is published and installs today:

```bash
go get github.com/chester-hill-solutions/stow-s3/pkg/stow
```

TypeScript and Python work from a source checkout:

```bash
git clone https://github.com/chester-hill-solutions/stow-s3
cd stow-s3
make build                        # bin/stow-s3 for your platform
export STOW_BIN="$PWD/bin/stow-s3"

# TypeScript client
cd packages/stow-s3 && npm install && npm run build && cd ../..

# Python client
pip install -e "packages/stow-s3-py[boto3]"
```

`STOW_BIN` tells a client which server binary to run. A published package carries
its own binary for your platform and does not need it. `stow-s3 doctor` reports
which side is broken if the binary cannot be found or run.

## Quick start: a scoped session

For a test that needs an S3 API and nothing else, a session is the shortest path
from nothing to a working client. It starts a private server on an ephemeral port,
creates a bucket, waits until the server reports itself ready, and hands back a
client that is already pointed at it. On close it stops the server and removes the
data directory.

TypeScript (not yet on npm — see [Install](#install)):

```bash
# from a source checkout
make build && export STOW_BIN="$PWD/bin/stow-s3"
cd packages/stow-s3 && npm install && npm run build
```

```ts
import { GetObjectCommand, PutObjectCommand } from "@aws-sdk/client-s3";
import { withStow } from "@chester-hill-solutions/stow-s3";

const body = await withStow(async (session) => {
  await session.s3.send(new PutObjectCommand({
    Bucket: session.bucket,
    Key: "input.json",
    Body: '{"task":"summarize"}',
  }));

  const fetched = await session.s3.send(new GetObjectCommand({
    Bucket: session.bucket,
    Key: "input.json",
  }));
  return fetched.Body?.transformToString();
});
```

`withStow` closes the session even if the callback throws, and reports both
errors if the close also fails. Use `openStow` when you need the session to
outlive a single callback.

Python (not yet on PyPI — see [Install](#install)):

```bash
make build && export STOW_BIN="$PWD/bin/stow-s3"
pip install -e "packages/stow-s3-py[boto3]"
```

```python
from stow_s3 import with_session

with with_session() as session:
    s3 = session.s3_client()
    bucket = session.new_bucket_name()
    s3.create_bucket(Bucket=bucket)
    s3.put_object(Bucket=bucket, Key="input.json", Body=b'{"task":"summarize"}')
    body = s3.get_object(Bucket=bucket, Key="input.json")["Body"].read()
```

The two clients differ in one deliberate way. The TypeScript session creates its
bucket for you and exposes it as `session.bucket`. The Python session performs
no S3 I/O at all: it hands back a client, and `new_bucket_name()` returns a name
that is very unlikely to collide with another session. Creating the bucket is
yours to do, which is what keeps `boto3` a genuinely optional extra.

### What a session guarantees

- **It cannot inherit your cloud configuration.** `STOW_*`, `S3_*`, and `AWS_*`
  are stripped from the child environment before anything is set, so a stray
  credential in your shell cannot turn a local session into a run-through one.
  A server you start yourself is safe for the same reason: ambient credentials no
  longer select a mode at all.
- **It cannot outlive you.** A session passes its parent's process ID, and the
  server exits if that process dies, including when it is killed rather than
  closed.
- **It reports the server's real limits, not assumed ones.** Capabilities and
  limits come from the readiness message.
- **It is bounded.** The default session holds at most 16 MiB and 1,000 objects,
  and a single request body is capped at 8 MiB. The limits are enforced by the
  server on every request, not by the client.
- **It is tuned for many-per-machine use.** A session's own server runs with a
  tighter garbage collector target than a long-lived server, because a session's
  peak memory matters more than its throughput. Set `GOGC` yourself to override
  it. A server you start with `stow serve` is never tuned this way.

## Diagnostics: stow doctor

When a session will not start, `stow doctor` reports what each side can see
rather than a single opaque failure. It checks both the client and the server and
merges the results into one list:

```bash
npx stow-doctor
npx stow-doctor --json
```

It never prints a credential. Checks that only apply in some setups are reported
as warnings rather than failures, so a local-only user is not failed for having
no upstream S3.

`npx stow-doctor` exits 0 when everything passed and 1 when a required check
failed. The `stow doctor` subcommand on the server side uses a third code, 2, for
a check that could not be run at all, so a broken installation is
distinguishable from a healthy one with a failing optional check.

## Quick start: CLI server

Build the binary:

```bash
make build
```

Start a filesystem-backed server:

```bash
./bin/stow-s3 serve --port 0 --data-dir .stow
```

`--port 0` asks for an ephemeral port, and the endpoint the server reports is the
one it actually bound — read it rather than assuming a port, because any port can
be taken and the credentials are generated unless you pass them. For a launcher or
a script, pass `--ready-fd 3` and read the JSON readiness record from that
descriptor instead of parsing stdout, and probe `/_stow/health` to confirm the
server is up. Both, plus the port and mode traps, are in
[`docs/running-and-probing.md`](docs/running-and-probing.md).

The server prints a machine-readable readiness line:

```text
STOW_READY endpoint=http://127.0.0.1:43127 access_key=... secret_key=... mode=local
```

Use the printed endpoint and credentials with an S3 client. The server supports path-style requests and uses `us-east-1` as its default region.

A hand-run server keeps running when its shell exits, which is usually what you
want. To make it exit when a specific parent process dies instead, pass
`--parent-pid`:

```bash
./bin/stow-s3 serve --port 0 --parent-pid 12345
```

This is opt-in for that reason. A session sets it for you; see
[Quick start: a scoped session](#quick-start-a-scoped-session).

The `STOW_READY` line above is the simple channel. Sessions use a stricter,
versioned channel that carries capabilities and limits as JSON over an inherited
file descriptor, so credentials never appear on a log. Both clients reject a
protocol version they do not speak rather than starting a half-working session.

For a temporary in-memory server:

```bash
./bin/stow-s3 serve --port 0 --backend memory
```

## Quick start: TypeScript

Start a managed server (see [Install](#install) for why this is not yet an
`npm install`):

```bash
export STOW_BIN="$PWD/bin/stow-s3"
```

```ts
import {
  GetObjectCommand,
  ListBucketsCommand,
  PutObjectCommand,
  S3Client,
} from "@aws-sdk/client-s3";
import { Stow } from "@chester-hill-solutions/stow-s3";

const stow = await Stow.start({
  backend: "memory",
  buckets: ["uploads"],
  port: 0,
});

try {
  const s3 = new S3Client(stow.awsSdkV3Config());

  await s3.send(new PutObjectCommand({
    Bucket: "uploads",
    Key: "hello.txt",
    Body: "hello",
  }));

  const response = await s3.send(new GetObjectCommand({
    Bucket: "uploads",
    Key: "hello.txt",
  }));

  console.log(await response.Body?.transformToString());
} finally {
  await stow.stop();
}
```

`Stow.start()` starts the `stow` executable, waits until it is ready, creates the requested buckets, and returns an AWS SDK configuration. When the executable is not in the repository's `bin/` directory, place `stow` on `PATH` or set `STOW_BIN`.

`Stow.start()` is the long-lived form: the server keeps running until you stop
it, and it uses the server's default collector settings. For a scoped,
throwaway bucket that cleans up after itself, use
[`withStow`](#quick-start-a-scoped-session) instead.

Use `Stow.connect()` when an S3 endpoint is already running:

```ts
const connection = Stow.connect({
  endpoint: "http://127.0.0.1:9000",
  accessKeyId: "access",
  secretAccessKey: "secret",
});

try {
  await connection.client.send(new ListBucketsCommand({}));
} finally {
  connection.disconnect();
}
```

## Embedded Go runtime

The public Go package provides an in-process, memory-backed runtime. It does not start an HTTP listener and does not require AWS credentials:

```go
package main

import (
  "context"
  "log"

  stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func main() {
  runtime, err := stow.Open(stow.Options{
    Backend:    stow.BackendMemory,
    MaxBytes:   10 << 20,
    MaxObjects: 1000,
  })
  if err != nil {
    log.Fatal(err)
  }
  defer runtime.Close()

  ctx := context.Background()
  if err := runtime.CreateBucket(ctx, "assets"); err != nil {
    log.Fatal(err)
  }

  if _, err := runtime.PutObject(ctx, "assets", "hello.txt", []byte("hello"), stow.PutOptions{
    ContentType: "text/plain",
  }); err != nil {
    log.Fatal(err)
  }
}
```

## Node.js, WebAssembly, and browser profiles

The Node.js WebAssembly profile provides an in-memory object runtime without an HTTP server:

```ts
import { EmbeddedStow } from "@chester-hill-solutions/stow-s3/embedded";
import { loadNodeWasmHost } from "@chester-hill-solutions/stow-s3/node-wasm";

const host = await loadNodeWasmHost();
const embedded = EmbeddedStow.open(host);

try {
  embedded.createBucket("assets");
  embedded.putObject("assets", "hello.txt", new TextEncoder().encode("hello"));
  const object = embedded.getObject("assets", "hello.txt");
  console.log(object.data);
} finally {
  embedded.close();
  await host.close();
}
```

The browser profile requires a compatible embedded host and persistence adapter. It can store snapshots in IndexedDB and restore them when the profile opens again.

### Where each surface can run

The object model is the same everywhere. What changes is the storage underneath, and one surface is not available at all without a filesystem.

| Surface | Needs a filesystem | Use it for |
|---|---|---|
| `serve` (S3 over HTTP) | yes | a local S3 endpoint, SDK compatibility |
| `workspace` | **yes** | giving an agent a real working directory |
| `/embedded` on Node | no | an in-process object runtime in Node |
| `/embedded` in a browser | no | a durable object store on a host with no filesystem |
| `/browser` with IndexedDB | no | the same, surviving a reload |

**The workspace is the agent surface, and it requires a real directory.** It
stores each object as a real file at a key-derived path, so a host with no
filesystem cannot provide one. That is a structural limit, not a configuration
choice, and `stow.OpenWorkspace` refuses rather than pretending.

**On a host with no filesystem — a browser, or an edge isolate — the deployment is
the embedded profile with a persistence adapter**, and the browser profile's
generation-checked IndexedDB adapter is the reference implementation. It records
which upstream state a local copy was derived from, refuses a commit whose
generation has moved, and validates its quota, so it is a durable object store
with generation-based conflict detection and no filesystem at all.

**Read `backend`, not `persistent`, to tell the cases apart.** The capability
report's `persistent` is a property of the backend and the host, not a setting you
can turn on: it is `false` on a memory backend because that host has nowhere to
persist, and that is the same answer a caller would get from forgetting to ask.
`backend` names what is underneath — `memory`, `filesystem`, `workspace`, or the
browser profile's `indexeddb` — and that is the field to branch on.

**An isolate is ephemeral.** A Worker or any other isolate can be reclaimed
between requests, so durability on that host comes from the platform's own storage
rather than from the process. A *durable workspace* is therefore not possible
there: the directory a workspace is, cannot outlive the isolate holding it. The
workspace contract rules out a network relay for the same reason.


## Run-through mode: the offline cache

Local mode keeps all object data in the selected local backend, and it is the
default. Run-through mode adds an upstream S3 client and a separate cache.

This is the second pillar, and the reason to care is the credential split. The
upstream keys are read by the stow server. An agent pointed at the local endpoint
gets a local key pair that means nothing to the bucket, so it can read and write
the cache and cannot touch the account behind it. Take the network away and the
cached data is still there.

```sh
stow-s3 serve --mode run-through \
  --data-dir /srv/agent-data \
  --cache-dir /srv/agent-cache \
  --upstream-endpoint https://s3.example.com \
  --upstream-bucket datasets
```

The cache is a separate directory from the data directory, and that separation is
load-bearing: the cache is a copy of someone else's data, and a data directory you
can reset should not be a directory that holds it.

A read of an already-cached object falls back to the cached copy when the upstream
cannot be reached, so losing the network does not become the agent's problem. This
is verified rather than asserted: with an upstream killed outright, a read of a
cached key still returns its bytes. A key that was never cached and cannot be
fetched returns an error rather than an empty body — the fallback covers a failed
revalidation, not a missing object.

A note on the shape of the cache: the bucket is created locally first, because
bucket namespace operations stay local. A first read against a bucket that exists
only upstream returns `NoSuchBucket` until the bucket exists locally. That is
deliberate — a run-through server does not create upstream buckets behind your
back — but it surprises people the first time, so it is written down here.

Bound it with `--cache-max-bytes` and `--cache-max-objects` (or
`STOW_CACHE_MAX_BYTES` and `STOW_CACHE_MAX_OBJECTS`), and give entries a lifetime
with `STOW_CACHE_TTL`. The limits are re-applied on startup rather than only on
writes, because the cache directory outlives the process: a server that only ever
reads would otherwise drift past its cap indefinitely.

Run-through is opt-in: pass `--mode run-through` or set `STOW_MODE=run-through`. It is never selected by the presence of credentials, because `AWS_*` variables are exported by CI runners and developer shells for unrelated tools — credentials decide how a requested upstream is authenticated, not whether one is used. `STOW_MODE=local` forces local-only.

In run-through mode, local data is authoritative. Upstream configuration comes from `STOW_*`, `S3_*`, or `AWS_*` environment variables, or from the corresponding command-line options.

`STOW_UPSTREAM_ADDRESSING` selects how stow addresses a bucket on the upstream:
`path` (the default) sends `https://endpoint/bucket/key`, and `virtual-hosted`
sends `https://bucket.endpoint/key`. Some providers serve only one of the two.
The default is path-style because every S3-compatible endpoint accepts it and it
works with bucket names a hostname cannot carry. An unrecognized value is
refused at startup rather than defaulted, because a misspelling that silently
became path-style would send every request somewhere the provider does not
answer.

This names *how* to address a bucket. It is not a request to use an upstream, so
setting it alone never selects one — that is `--mode run-through`, as above. Note
that virtual-hosted addressing needs a resolvable hostname: given a bare IP
address as the endpoint, the AWS SDK keeps the bucket in the path, since a bucket
name cannot be prefixed onto an IP.

Live upstream writes require all of the following:

- run-through mode;
- the filesystem backend;
- explicit live-write opt-in with `--allow-live-writes` or `STOW_ALLOW_LIVE_WRITES=true`;
- a durable coordinated outbox.

`STOW_POLICY=mirrorWrites` is **not** that opt-in. The policy chooses how reads
are routed; the flag is the consent to mutate a real provider, and only the flag
grants it. Setting the policy alone leaves every write local. An explicit
`STOW_ALLOW_LIVE_WRITES=false` is a refusal that the policy cannot override.
See [ADR 0005](docs/adr/0005-live-write-requires-explicit-consent.md).

Stow keeps bucket namespace operations local. It does not create upstream buckets automatically.

## Resetting a data directory

`Stow.start({ resetOwnedData: true })` deletes the data directory before starting.
It deletes only a directory stow created, identified by a `.stow-owner` marker
the server writes on startup. A directory without that marker is refused, as are
the filesystem root, your home directory, the working directory, and any ancestor
of them — so `dataDir: ".."` cannot reach your home. A directory that does not
exist is not an error, so resetting before a first run is fine.

`cleanSlate` is a deprecated alias with identical checks. A data directory
created before this check existed has no marker, so its first reset is refused;
nothing is deleted. See
[ADR 0006](docs/adr/0006-owned-data-directory-reset.md).

## Running on another host

The server can bind to a network interface:

```bash
./bin/stow-s3 serve --host 0.0.0.0 --port 9000
```

Clients on the same network can then connect to the host's port and use S3 operations. Keep the server behind a firewall or private network boundary. Put it behind a TLS-terminating proxy before sending traffic over an untrusted network.

The S3 routes require the generated or configured credentials. Use the current server for local development and controlled private deployments. It is not a hardened public or multi-tenant storage service.

## Admin and metrics routes

Diagnostics live under `/_stow/`, separate from S3 authentication. **`/_stow/health`
is the health endpoint; `/health` is not** — every path outside `/_stow/` is an S3
path requiring SigV4, so an unsigned `GET /health` is refused with `403`, which
means the probe used the wrong path rather than that the server is unhealthy.

| Route | Effect |
|---|---|
| `/_stow/health` | liveness |
| `/_stow/status` | mode, policies, version |
| `/_stow/inspect` | object and upload state |
| `/_stow/metrics` | request and quota counters |
| `/_stow/outbox/retry` | retries failed upstream propagation |
| `/_stow/outbox/discard` | discards an outbox entry |

The two `outbox` routes change what is propagated to a live provider, so they
always require the admin token — including on loopback, because loopback is not
a privilege boundary. The read-only routes work on loopback without a token, so
`stow doctor` and a local shell need nothing, and require the token from any
other address.

Set the token with `STOW_ADMIN_TOKEN` rather than the flag, so it does not appear
in the process list:

```bash
STOW_ADMIN_TOKEN=$(openssl rand -hex 24) ./bin/stow-s3 serve --host 0.0.0.0
curl -H "X-Stow-Admin: $STOW_ADMIN_TOKEN" http://host:9000/_stow/metrics
```

A request without a valid token gets `404`, not `403`, so the route's existence
is not advertised. With no token configured the outbox routes are not reachable
at all.

`--allow-public-admin` is **deprecated and ignored**. It used to be the only
gate, which made exposing the outbox routes an unauthenticated act rather than a
privileged one. The flag now logs a warning and changes nothing; use
`--admin-token`.

## Browser origins (CORS)

Stow permits loopback origins by default — `http://localhost:*` and
`http://127.0.0.1:*` — which is the case a local dev server actually has. It
previously reflected whatever `Origin` a request carried, which let any website
a developer visited read their local bucket using the credentials their SDK had
already placed in the page.

Name the origins you need instead:

```bash
./bin/stow-s3 serve --cors-origin https://app.example --cors-origin http://localhost:5173
```

`--cors-origin *` restores the old permissive behavior, but only when you ask
for it by name. Responses always carry `Vary: Origin`, without which a cache can
serve one origin's allow header to another.

## Development

Run the complete local checks:

```bash
make test-all
make standards
```

`make test-all` covers the Go server, the shared conformance suite across every
backend, the TypeScript client, the Python client, and the WebAssembly bridge.
`make standards` runs the quality ratchets; these are floors, so an improvement is
reported rather than failed and a regression fails the build.

The workspace verbs are specified once, in
[`conformance/workspace/cases.json`](conformance/workspace/cases.json), and
asserted by three independent drivers: the Go suite against the binary, the
TypeScript wrapper against itself, and the Python wrapper against itself. A client
cannot disagree with the engine about what a verb returns without one of them
failing. The reason that file exists is in
[its README](conformance/workspace/README.md); the short version is that both
client wrappers shipped the same bug for a long time, each hidden by a test double
that answered with a contract the production code did not have.

The Go version is pinned exactly, in `.go-version` and in the `toolchain` line of
`go.mod`. The WebAssembly artifact in `packages/stow-s3/dist` is committed and
`check-generated` rebuilds and diffs it, and a build from a different patch
release does not reproduce it byte for byte. Nothing has to be installed by hand:
the `toolchain` directive makes the Go command fetch the pinned version on
demand. To move to a new patch, change both files, then rebuild and commit the
artifact.

The repository contains the Go server, storage backends, runtime packages, WebAssembly bridge, TypeScript package, Python package, and shared conformance tests.

Measure session startup, first upload, request latency, memory scaling, parallel
connections and sessions, and the workspace handoff/checkpoint lifecycle with:

```bash
make benchmark
```

This is measurement rather than a gate, so it is deliberately not part of
`make standards`. It builds the binaries first. Session measurements need
loopback binding; workspace measurements run through the TypeScript wrapper and
native CLI. To capture the workspace CLI's high-water RSS separately, run
`node packages/stow-s3/scripts/benchmark-workspace.mjs --memory-only`. The
recorded machine-specific baseline and raw JSON are in
`docs/benchmarks/2026-09-27/`; the older session memory experiments remain in
`docs/benchmarks/session-baseline.md`.

## License

Apache License 2.0.
