# stow-s3

Python helpers for private S3 test servers and native file workspaces.
A session starts a local server, returns its endpoint and credentials, and stops it on close.

## Install

The 0.3.0 candidate is unpublished. From the repository root:

```sh
make build
export STOW_BIN="$PWD/bin/stow-s3"
python3 -m pip install -e 'packages/stow-s3-py[boto3]'
```

`boto3` is optional. Omit the extra if you supply another S3 client.
Use `STOW_BIN` to select the built native server.
<!-- repo-only:start -->
See the [release checkpoint](../../docs/release-verification-2026-10-03.md) for package availability.
<!-- repo-only:end -->

## Use

```python
from stow_s3 import with_session

def test_my_handler():
    with with_session() as session:
        s3 = session.s3_client()
        bucket = session.new_bucket_name()
        s3.create_bucket(Bucket=bucket)

        s3.put_object(Bucket=bucket, Key="input.json", Body=b'{"task":"summarize"}')

        result = my_handler(s3, bucket)  # a normal boto3 client
        assert result == "summarize"
```

When the block ends, the server stops and its data directory is removed. If your
test fails, cleanup happens anyway.

`with_session` takes limits that the server then enforces on every request, so a
test that accidentally writes too much fails instead of consuming the disk:

```python
with with_session(max_bytes=1024 * 1024, max_objects=10) as session:
    ...
```

Defaults are 16 MiB and 1,000 objects.
<!-- repo-only:start -->
See the [session measurements](../../docs/benchmarks/session-baseline.md).
<!-- repo-only:end -->

## Without boto3

A session performs no S3 requests itself. Use its endpoint and credentials with your chosen S3 library.
For example, you can construct a boto3 client directly:

```python
import boto3  # or whatever you prefer

with with_session() as session:
    s3 = boto3.client(
        "s3",
        endpoint_url=session.endpoint,
        aws_access_key_id=session.access_key_id,
        aws_secret_access_key=session.secret_access_key,
        region_name=session.region,
    )
```

## Isolation

A session does not inherit `STOW_*`, `S3_*` or `AWS_*` from your shell, so a
stray credential cannot silently turn a local test into one that writes to a real
bucket.

Credentials never appear on the child's stdout. They arrive over a versioned
readiness channel, and a client that does not understand the protocol version
raises `StowProtocolError` rather than guessing.

## When something does not work

Run the server's own diagnostic. It reports whether a binary exists for this
platform, whether the temporary directory is writable, whether a backend opens,
and whether a real endpoint can bind and answer a health check:

```sh
stow doctor
stow doctor --json
```

If the Python package cannot find a binary at all, `StowBinaryNotFoundError`
names the resolution order it used.

## Ready-to-work agent workspaces

The workspace helpers wrap the same native CLI contract. Prepare a task from its
JSON manifest, then start the agent process with the returned
`working_directory` as its cwd:

```python
from stow_s3 import prepare_workspace, checkpoint_workspace

task = prepare_workspace("task.json")
# Start the agent with cwd=task["working_directory"].
snapshot = checkpoint_workspace(task["workspace_id"])
```

The package also exports resume, same-machine handoff, diff, restore, portable
checkpoint export, and import helpers. Results are JSON objects from the Go CLI;
`WorkspaceCommandError` includes the failed command and stderr. The platform
binary must be installed or selected with `STOW_BIN`. Workspaces persist beyond
the process and are separate from disposable `with_session` sessions. See
[`docs/task-manifest.md`](../../docs/task-manifest.md) for manifest fields,
archive safeguards, and the isolation boundary.

## License

Apache-2.0
