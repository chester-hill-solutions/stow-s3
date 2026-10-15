# Examples

Stow is lightweight S3-compatible storage for tests, local applications and
processing pipelines. These examples start with a disposable bucket and grow,
one change at a time, into storage that survives a restart and can be replayed.

Every example runs against a real S3 client. The Stow-specific part is small: a
session starts a private loopback server, hands back an ordinary client, and
cleans up. The application code that stores and reads objects does not change
between examples.

## Build once

```sh
git clone https://github.com/chester-hill-solutions/stow-s3
cd stow-s3
make build
export STOW_BIN="$PWD/bin/stow-s3"
```

TypeScript and Python clients are built from a source checkout; they are not yet
published to npm or PyPI. The TypeScript examples import the AWS SDK and the
local client, so install both once:

```sh
npm ci --ignore-scripts --omit=optional --prefix packages/stow-s3
npm run build --prefix packages/stow-s3
npm install --prefix examples/stow-recipes
```

For Python:

```sh
python -m venv .venv
. .venv/bin/activate
pip install -e "packages/stow-s3-py[boto3]"
```

`STOW_BIN` tells the client which server binary to start. A published package
would carry its own binary and would not need it.

## 1. Start small

A session is a private, loopback S3 endpoint with generated credentials and a
generated bucket. The callback form cleans up the server and its temporary data
when it returns, even if the callback throws.

<!-- include:examples/stow-recipes/start-small.mjs -->

<!-- include:examples/stow-recipes/start_small.py -->

Only the last block is Stow-specific. `publishReport` takes an S3 client and a
bucket, so the same function runs against a real bucket, a test session, or a
local pipeline. TypeScript's session creates the bucket and exposes it as
`session.bucket`; Python's session creates no bucket, so the example names one
with `session.new_bucket_name()` and creates it.

A session starts a separate server process, not an in-process client. It is
isolated from your `STOW_*`, `S3_*` and `AWS_*` environment, it cannot outlive
your process, and it is bounded (16 MiB and 1,000 objects by default). Read your
instance's capabilities rather than assuming a limit.

## 2. Keep the artifacts

Name a data directory and the objects outlive the session. The directory is
yours: the server stops, but the bytes stay, and the next session on the same
directory reads them.

<!-- include:examples/stow-recipes/keep-artifacts.mjs -->

<!-- include:examples/stow-recipes/keep_artifacts.py -->

The change from the first example is one option. A named directory selects the
filesystem backend; a session with no directory stays memory-backed and
disposable. Because the TypeScript session generates a bucket name per session,
the example also passes a fixed `bucket` so the reopened session finds the
objects it wrote.

## 3. Re-run a processing stage

A stage reads input objects by key and writes derived results. Retaining both
means you can inspect a result, add documents, and run the stage again.

<!-- include:examples/stow-recipes/processing-stage.mjs -->

<!-- include:examples/stow-recipes/processing_stage.py -->

Stow stores the inputs and the results. Your application owns the queue, decides
what runs and when, and retries what fails. Stow is not a durable job queue and
does not promise exactly-once processing: if a worker is interrupted, the
application reruns that key, and a rerun overwrites the same result.

## 4. Reject a stale update

A conditional write compares the ETag you pass with the object's current ETag
and refuses the write if they differ, so two writers cannot silently clobber one
another.

<!-- include:examples/stow-recipes/conditional-write.mjs -->

<!-- include:examples/stow-recipes/conditional_write.py -->

The refused update returns `412 PreconditionFailed` and leaves the successful
version in place. This is an ordinary S3 conditional write; it needs no Stow
extension.

## Run the examples

Each example is a complete, runnable file:

| Example | File |
| --- | --- |
| Start small (TypeScript) | [start-small.mjs](start-small.mjs) |
| Start small (Python) | [start_small.py](start_small.py) |
| Keep the artifacts (TypeScript) | [keep-artifacts.mjs](keep-artifacts.mjs) |
| Keep the artifacts (Python) | [keep_artifacts.py](keep_artifacts.py) |
| Re-run a stage (TypeScript) | [processing-stage.mjs](processing-stage.mjs) |
| Re-run a stage (Python) | [processing_stage.py](processing_stage.py) |
| Reject a stale update (TypeScript) | [conditional-write.mjs](conditional-write.mjs) |
| Reject a stale update (Python) | [conditional_write.py](conditional_write.py) |

Go uses the embedded object runtime instead of a server. It stores and reads
objects in process with no HTTP endpoint, so it is the right surface only when
the application is written in Go:

<!-- include:examples/stow-recipes/go/start-small/main.go -->

Next: the [getting started guide](../../docs/getting-started.md), the
[TypeScript client](../../packages/stow-s3/README.md) and the
[Python client](../../packages/stow-s3-py/README.md).
