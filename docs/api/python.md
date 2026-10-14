# Python API

Import session, workspace and binary helpers from `stow_s3`. All operations are synchronous.

## Defaults and limits

| Setting | Value |
| --- | --- |
| max_bytes / max_objects | 16 MiB / 1,000 objects; zero means unlimited |
| max_request_bytes / max_concurrent_requests | 8 MiB / 16; supplied values must be positive integers |
| data_dir | A session-owned temporary directory |
| Session startup timeout | 15 seconds |
| serve_workspace timeout | 15 seconds |
| Archives | 1 GiB / 100,000 files |

## Behavior

- with_session is a context manager.
- open_session returns a Session you must close.
- Sessions create no buckets: call s3.create_bucket explicitly.
- new_bucket_name only returns a name.
- A supplied data_dir remains on close.
- Workspace helpers return dictionaries from the native command.
- ServedWorkspace supports with blocks and preserves workspace data on exit.
- Keep registry_dir and team consistent.
- Resolve uncertain capture replies with the original request key and options.

## Errors

- Negative limits and invalid positive request limits raise ValueError.
- StowBinaryNotFoundError exposes code and the attempted path.
- StowProtocolError exposes code for readiness failures.
- WorkspaceCommandError exposes command, returncode and stderr.
- s3_client raises ImportError when boto3 is unavailable.
- S3 operations raise the selected SDK's errors.

## Example

```python
from stow_s3 import with_session

with with_session() as session:
    s3 = session.s3_client()
    bucket = session.new_bucket_name()
    s3.create_bucket(Bucket=bucket)
    s3.put_object(Bucket=bucket, Key="note.txt", Body=b"hello")
    data = s3.get_object(Bucket=bucket, Key="note.txt")["Body"].read()
```

## Symbols

[__version__](#__version__) · [adopt_workspace_handoff](#adopt_workspace_handoff) · [apply_workspace_delta](#apply_workspace_delta) · [BinarySource](#binarysource) · [build_child_env](#build_child_env) · [checkpoint_workspace](#checkpoint_workspace) · [collect_workspaces](#collect_workspaces) · [create_workspace_delta](#create_workspace_delta) · [DEFAULT_SESSION_MAX_BYTES](#default_session_max_bytes) · [DEFAULT_SESSION_MAX_OBJECTS](#default_session_max_objects) · [describe_source](#describe_source) · [destroy_workspace](#destroy_workspace) · [diff_workspaces](#diff_workspaces) · [export_workspace_checkpoint](#export_workspace_checkpoint) · [handoff_workspace](#handoff_workspace) · [import_workspace_checkpoint](#import_workspace_checkpoint) · [open_session](#open_session) · [parse_ready_message](#parse_ready_message) · [prepare_workspace](#prepare_workspace) · [prune_workspaces](#prune_workspaces) · [READY_PROTOCOL_VERSION](#ready_protocol_version) · [require_stow_binary](#require_stow_binary) · [resolve_stow_binary](#resolve_stow_binary) · [resolve_stow_binary_detailed](#resolve_stow_binary_detailed) · [ResolvedBinary](#resolvedbinary) · [restore_workspace_checkpoint](#restore_workspace_checkpoint) · [resume_workspace](#resume_workspace) · [run_workspace_command](#run_workspace_command) · [serve_workspace](#serve_workspace) · [ServedWorkspace](#servedworkspace) · [Session](#session) · [SESSION_GOGC](#session_gogc) · [stow_binary_available](#stow_binary_available) · [StowBinaryNotFoundError](#stowbinarynotfounderror) · [StowCapabilities](#stowcapabilities) · [StowProtocolError](#stowprotocolerror) · [StowReady](#stowready) · [with_session](#with_session) · [WorkspaceCommandError](#workspacecommanderror) · [WorkspaceJSON](#workspacejson)

## __version__



```python
__version__ = '0.3.0'
```

## adopt_workspace_handoff

Adopt a portable handoff on the machine that received it.

```python
def adopt_workspace_handoff(handoff: str, root: str, *, registry_dir: str | None=None, team: str | None=None, max_bytes: int | None=None, max_files: int | None=None, include_sensitive: bool=False) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `handoff` | `str` | Yes | — |  |
| `root` | `str` | Yes | — | New workspace directory; its parent must already exist. |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |
| `team` | `str \| None` | No | None | Registry team partition; it is not an authorization identity. |
| `max_bytes` | `int \| None` | No | None | Maximum managed object bytes; zero means unlimited. |
| `max_files` | `int \| None` | No | None | Maximum files accepted by the archive operation. |
| `include_sensitive` | `bool` | No | False | Explicitly permit paths rejected by the sensitive-filename filter. |

Returns: `WorkspaceJSON`.

## apply_workspace_delta

Bring a base checkpoint to the state a delta describes.

```python
def apply_workspace_delta(delta: str, *, base: str, expect_sha256: str | None=None, registry_dir: str | None=None, team: str | None=None) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `delta` | `str` | Yes | — | Verified change document to encode or apply. |
| `base` | `str` | Yes | — |  |
| `expect_sha256` | `str \| None` | No | None |  |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |
| `team` | `str \| None` | No | None | Registry team partition; it is not an authorization identity. |

Returns: `WorkspaceJSON`.

## BinarySource

Which rule produced the resolved path.

```python
class BinarySource(str, Enum):
    PLATFORM_WHEEL = 'platform-wheel'
    ENVIRONMENT = 'environment'
    MONOREPO = 'monorepo'
    PATH = 'path'
```

## build_child_env

Copy the process environment, remove STOW_*, S3_* and AWS_*, then apply the supplied extra values.

```python
def build_child_env(extra: dict[str, str] | None=None) -> dict[str, str]:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `extra` | `dict[str, str] \| None` | No | None |  |

Returns: `dict[str, str]`.

## checkpoint_workspace



```python
def checkpoint_workspace(id: str, *, registry_dir: str | None=None, team: str | None=None, parent: str | None=None, max_bytes: int | None=None, max_files: int | None=None, include_sensitive: bool=False) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `id` | `str` | Yes | — | Stable identity of this workspace, file stage, revision or immutable record. |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |
| `team` | `str \| None` | No | None | Registry team partition; it is not an authorization identity. |
| `parent` | `str \| None` | No | None | Optional parent checkpoint identity. |
| `max_bytes` | `int \| None` | No | None | Maximum managed object bytes; zero means unlimited. |
| `max_files` | `int \| None` | No | None | Maximum files accepted by the archive operation. |
| `include_sensitive` | `bool` | No | False | Explicitly permit paths rejected by the sensitive-filename filter. |

Returns: `WorkspaceJSON`.

## collect_workspaces



```python
def collect_workspaces(*, registry_dir: str | None=None) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |

Returns: `WorkspaceJSON`.

## create_workspace_delta

Write the difference between two checkpoints to a document the other side can apply.

```python
def create_workspace_delta(*, from_id: str, to_id: str, output: str, registry_dir: str | None=None, team: str | None=None, max_bytes: int | None=None, max_files: int | None=None, include_sensitive: bool=False) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `from_id` | `str` | Yes | — |  |
| `to_id` | `str` | Yes | — |  |
| `output` | `str` | Yes | — | Destination path for the transfer document or archive. |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |
| `team` | `str \| None` | No | None | Registry team partition; it is not an authorization identity. |
| `max_bytes` | `int \| None` | No | None | Maximum managed object bytes; zero means unlimited. |
| `max_files` | `int \| None` | No | None | Maximum files accepted by the archive operation. |
| `include_sensitive` | `bool` | No | False | Explicitly permit paths rejected by the sensitive-filename filter. |

Returns: `WorkspaceJSON`.

## DEFAULT_SESSION_MAX_BYTES

Default session storage limit: 16 MiB.

```python
DEFAULT_SESSION_MAX_BYTES = 16 * 1024 * 1024
```

## DEFAULT_SESSION_MAX_OBJECTS

Default session object count: 1,000.

```python
DEFAULT_SESSION_MAX_OBJECTS = 1000
```

## describe_source

A human explanation of a resolution source.

```python
def describe_source(source: BinarySource) -> str:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `source` | `BinarySource` | Yes | — | Complete bytes or an asynchronous sequence of bounded byte chunks. |

Returns: `str`.

## destroy_workspace



```python
def destroy_workspace(id: str, *, registry_dir: str | None=None) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `id` | `str` | Yes | — | Stable identity of this workspace, file stage, revision or immutable record. |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |

Returns: `WorkspaceJSON`.

## diff_workspaces



```python
def diff_workspaces(from_id: str, to_id: str, *, registry_dir: str | None=None, team: str | None=None) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `from_id` | `str` | Yes | — |  |
| `to_id` | `str` | Yes | — |  |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |
| `team` | `str \| None` | No | None | Registry team partition; it is not an authorization identity. |

Returns: `WorkspaceJSON`.

## export_workspace_checkpoint



```python
def export_workspace_checkpoint(checkpoint_id: str, output: str, *, registry_dir: str | None=None, max_bytes: int | None=None, max_files: int | None=None, include_sensitive: bool=False) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `checkpoint_id` | `str` | Yes | — | Immutable checkpoint identity. |
| `output` | `str` | Yes | — | Destination path for the transfer document or archive. |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |
| `max_bytes` | `int \| None` | No | None | Maximum managed object bytes; zero means unlimited. |
| `max_files` | `int \| None` | No | None | Maximum files accepted by the archive operation. |
| `include_sensitive` | `bool` | No | False | Explicitly permit paths rejected by the sensitive-filename filter. |

Returns: `WorkspaceJSON`.

## handoff_workspace



```python
def handoff_workspace(id: str, *, registry_dir: str | None=None, team: str | None=None, checkpoint_id: str | None=None, archive: str | None=None, output: str | None=None) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `id` | `str` | Yes | — | Stable identity of this workspace, file stage, revision or immutable record. |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |
| `team` | `str \| None` | No | None | Registry team partition; it is not an authorization identity. |
| `checkpoint_id` | `str \| None` | No | None | Immutable checkpoint identity. |
| `archive` | `str \| None` | No | None | Path to a portable checkpoint archive. |
| `output` | `str \| None` | No | None | Destination path for the transfer document or archive. |

Returns: `WorkspaceJSON`.

## import_workspace_checkpoint



```python
def import_workspace_checkpoint(archive: str, *, registry_dir: str | None=None, max_bytes: int | None=None, max_files: int | None=None, include_sensitive: bool=False) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `archive` | `str` | Yes | — | Path to a portable checkpoint archive. |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |
| `max_bytes` | `int \| None` | No | None | Maximum managed object bytes; zero means unlimited. |
| `max_files` | `int \| None` | No | None | Maximum files accepted by the archive operation. |
| `include_sensitive` | `bool` | No | False | Explicitly permit paths rejected by the sensitive-filename filter. |

Returns: `WorkspaceJSON`.

## open_session

Start a private native S3 server. Call Session.close when finished.

```python
def open_session(*, max_bytes: int=DEFAULT_SESSION_MAX_BYTES, max_objects: int=DEFAULT_SESSION_MAX_OBJECTS, data_dir: str | Path | None=None, max_request_bytes: int | None=None, max_concurrent_requests: int | None=None) -> Session:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `max_bytes` | `int` | No | DEFAULT_SESSION_MAX_BYTES | Maximum managed object bytes; zero means unlimited. |
| `max_objects` | `int` | No | DEFAULT_SESSION_MAX_OBJECTS | Maximum object count; zero means unlimited. |
| `data_dir` | `str \| Path \| None` | No | None | Native data directory. A supplied session directory is preserved on close. |
| `max_request_bytes` | `int \| None` | No | None | Positive HTTP request-body byte limit; native default is 8 MiB. |
| `max_concurrent_requests` | `int \| None` | No | None | Positive active HTTP handler limit; native default is 16. |

Returns: `Session`.

## parse_ready_message

Parse the readiness object.

```python
def parse_ready_message(line: str) -> StowReady:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `line` | `str` | Yes | — | One JSON or legacy readiness record to decode. |

Returns: `StowReady`.

## prepare_workspace



```python
def prepare_workspace(manifest_path: str) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `manifest_path` | `str` | Yes | — | Path to the task manifest used to prepare a workspace. |

Returns: `WorkspaceJSON`.

## prune_workspaces

Remove registry entries whose workspace directory no longer exists.

```python
def prune_workspaces(*, registry_dir: str | None=None, team: str | None=None, include_adopted: bool=False) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |
| `team` | `str \| None` | No | None | Registry team partition; it is not an authorization identity. |
| `include_adopted` | `bool` | No | False | Permit pruning records for already-missing adopted directories. |

Returns: `WorkspaceJSON`.

## READY_PROTOCOL_VERSION



```python
READY_PROTOCOL_VERSION = 1
```

## require_stow_binary

Resolve the binary, raising an actionable error when it is not runnable.

```python
def require_stow_binary() -> ResolvedBinary:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |

Returns: `ResolvedBinary`.

## resolve_stow_binary



```python
def resolve_stow_binary() -> str:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |

Returns: `str`.

## resolve_stow_binary_detailed

Resolve the binary path and report which rule produced it.

```python
def resolve_stow_binary_detailed() -> ResolvedBinary:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |

Returns: `ResolvedBinary`.

## ResolvedBinary



```python
class ResolvedBinary:
    path: str
    source: BinarySource
```

## restore_workspace_checkpoint



```python
def restore_workspace_checkpoint(checkpoint_id: str, root: str, *, registry_dir: str | None=None, team: str | None=None) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `checkpoint_id` | `str` | Yes | — | Immutable checkpoint identity. |
| `root` | `str` | Yes | — | New workspace directory; its parent must already exist. |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |
| `team` | `str \| None` | No | None | Registry team partition; it is not an authorization identity. |

Returns: `WorkspaceJSON`.

## resume_workspace



```python
def resume_workspace(*, id: str | None=None, handoff: str | None=None, registry_dir: str | None=None) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `id` | `str \| None` | No | None | Stable identity of this workspace, file stage, revision or immutable record. |
| `handoff` | `str \| None` | No | None |  |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |

Returns: `WorkspaceJSON`.

## run_workspace_command

Run a workspace CLI command directly, without a shell, and decode JSON.

```python
def run_workspace_command(*args: str) -> WorkspaceJSON:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `*args` | `str` | No | — |  |

Returns: `WorkspaceJSON`.

## serve_workspace



```python
def serve_workspace(workspace_id: str, *, registry_dir: str | None=None, team: str | None=None, max_bytes: int | None=None, max_objects: int | None=None, timeout: float=15.0, max_request_bytes: int | None=None, max_concurrent_requests: int | None=None) -> ServedWorkspace:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `workspace_id` | `str` | Yes | — |  |
| `registry_dir` | `str \| None` | No | None | Native registry directory for workspace metadata. |
| `team` | `str \| None` | No | None | Registry team partition; it is not an authorization identity. |
| `max_bytes` | `int \| None` | No | None | Maximum managed object bytes; zero means unlimited. |
| `max_objects` | `int \| None` | No | None | Maximum object count; zero means unlimited. |
| `timeout` | `float` | No | 15.0 |  |
| `max_request_bytes` | `int \| None` | No | None | Positive HTTP request-body byte limit; native default is 8 MiB. |
| `max_concurrent_requests` | `int \| None` | No | None | Positive active HTTP handler limit; native default is 16. |

Returns: `ServedWorkspace`.

## ServedWorkspace



```python
class ServedWorkspace:
    ready: dict[str, Any]
```

### ServedWorkspace.close

Release this handle. Do not submit new operations after closing it.

```python
def close(self) -> None:
```

Returns: `None`.

### ServedWorkspace.__enter__



```python
def __enter__(self) -> ServedWorkspace:
```

Returns: `ServedWorkspace`.

### ServedWorkspace.__exit__



```python
def __exit__(self, *_: object) -> None:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `*_` | `object` | No | — |  |

Returns: `None`.

## Session

A running stow server and the parameters needed to reach it.

```python
class Session:
    endpoint: str
    access_key_id: str
    secret_access_key: str
    region: str
    data_dir: Path
    max_bytes: int
    max_objects: int
    mode: str
    backend: str
    binary_version: str
    protocol_version: int
    capabilities: Any
    owns_data_dir: bool
```

### Session.stdout_text

Whatever the server wrote to stdout so far.

```python
def stdout_text(self) -> str:
```

Returns: `str`.

### Session.stderr_text



```python
def stderr_text(self) -> str:
```

Returns: `str`.

### Session.new_bucket_name

Return a collision-resistant bucket name without creating a bucket.

```python
def new_bucket_name(self) -> str:
```

Returns: `str`.

### Session.s3_client

Build a boto3 client from this session's endpoint and credentials. boto3 must be installed separately.

```python
def s3_client(self, **kwargs: Any) -> Any:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `**kwargs` | `Any` | No | — |  |

Returns: `Any`.

### Session.close

Release this handle. Do not submit new operations after closing it.

```python
def close(self) -> None:
```

Returns: `None`.

## SESSION_GOGC



```python
SESSION_GOGC = '50'
```

## stow_binary_available

Whether a resolved path is actually runnable.

```python
def stow_binary_available() -> bool:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |

Returns: `bool`.

## StowBinaryNotFoundError

No runnable server binary was found.

```python
class StowBinaryNotFoundError(Exception):
    code = 'binary_not_found'
```

### StowBinaryNotFoundError.__init__



```python
def __init__(self, resolved: str) -> None:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `resolved` | `str` | Yes | — |  |

Returns: `None`.

## StowCapabilities

What a caller may rely on before issuing operations.

```python
class StowCapabilities:
    persistent: bool
    multipart: bool
    upstream: bool
    conditional_writes: bool
    presigned_urls: bool
    max_bytes: int
    max_objects: int
    max_request_bytes: int
    max_concurrent_requests: int = 0
```

## StowProtocolError

A ready message this client cannot safely act on.

```python
class StowProtocolError(Exception):
    pass
```

### StowProtocolError.__init__



```python
def __init__(self, code: str, message: str) -> None:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `code` | `str` | Yes | — | Structured error code; inspect the code rather than matching message text. |
| `message` | `str` | Yes | — | Human-readable error message. |

Returns: `None`.

## StowReady

The single readiness object the server writes.

```python
class StowReady:
    protocol_version: int
    binary_version: str
    endpoint: str
    region: str
    access_key_id: str
    secret_access_key: str
    mode: str
    backend: str
    capabilities: StowCapabilities
```

## with_session

Use as a context manager; exit stops the server and removes only session-owned data.

```python
def with_session(*, max_bytes: int=DEFAULT_SESSION_MAX_BYTES, max_objects: int=DEFAULT_SESSION_MAX_OBJECTS, data_dir: str | Path | None=None, max_request_bytes: int | None=None, max_concurrent_requests: int | None=None) -> Iterator[Session]:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `max_bytes` | `int` | No | DEFAULT_SESSION_MAX_BYTES | Maximum managed object bytes; zero means unlimited. |
| `max_objects` | `int` | No | DEFAULT_SESSION_MAX_OBJECTS | Maximum object count; zero means unlimited. |
| `data_dir` | `str \| Path \| None` | No | None | Native data directory. A supplied session directory is preserved on close. |
| `max_request_bytes` | `int \| None` | No | None | Positive HTTP request-body byte limit; native default is 8 MiB. |
| `max_concurrent_requests` | `int \| None` | No | None | Positive active HTTP handler limit; native default is 16. |

Returns: `Iterator[Session]`.

## WorkspaceCommandError

A workspace CLI command failed and returned a nonzero exit status.

```python
class WorkspaceCommandError(RuntimeError):
    pass
```

### WorkspaceCommandError.__init__



```python
def __init__(self, command: Sequence[str], returncode: int, stderr: str) -> None:
```

| Parameter | Type | Required | Default | Meaning |
| --- | --- | --- | --- | --- |
| `command` | `Sequence[str]` | Yes | — |  |
| `returncode` | `int` | Yes | — |  |
| `stderr` | `str` | Yes | — |  |

Returns: `None`.

## WorkspaceJSON



```python
WorkspaceJSON = dict[str, object]
```
