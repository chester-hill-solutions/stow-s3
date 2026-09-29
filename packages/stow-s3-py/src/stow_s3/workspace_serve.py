"""Own a persistent workspace's serving process without deleting its data."""

from __future__ import annotations

import contextlib
import json
import os
import subprocess
import tempfile
from dataclasses import dataclass, field
from typing import IO, Any

from .bin import require_stow_binary
from .session import _read_ready_line, _stop_process, build_child_env


@dataclass
class ServedWorkspace:
    ready: dict[str, Any]
    _process: subprocess.Popen[bytes] = field(repr=False)
    _log: IO[bytes] = field(repr=False)
    _closed: bool = field(default=False, repr=False)

    def close(self) -> None:
        if self._closed:
            return
        _stop_process(self._process, grace_seconds=11.0)
        self._log.close()
        self._closed = True

    def __enter__(self) -> ServedWorkspace:
        return self

    def __exit__(self, *_: object) -> None:
        self.close()


def serve_workspace(
    workspace_id: str,
    *,
    registry_dir: str | None = None,
    team: str | None = None,
    max_bytes: int | None = None,
    max_objects: int | None = None,
    timeout: float = 15.0,
    max_request_bytes: int | None = None,
    max_concurrent_requests: int | None = None,
) -> ServedWorkspace:
    if not workspace_id:
        raise ValueError("workspace id is required")
    if max_concurrent_requests is not None and (type(max_concurrent_requests) is not int or max_concurrent_requests <= 0):
        raise ValueError("max_concurrent_requests must be a positive integer")
    if max_request_bytes is not None and (type(max_request_bytes) is not int or max_request_bytes <= 0):
        raise ValueError("max_request_bytes must be a positive integer")
    binary = require_stow_binary()
    read_fd, write_fd = os.pipe()
    log = tempfile.TemporaryFile()
    process = None
    reader_owns_fd = False
    try:
        args = [binary.path, "workspace", "serve", "--id", workspace_id,
                "--ready-fd", str(write_fd), "--parent-pid", str(os.getpid())]
        flags = {"max-request-bytes": max_request_bytes, "max-concurrent-requests": max_concurrent_requests, "registry-dir": registry_dir, "team": team,
                 "max-bytes": max_bytes, "max-objects": max_objects}
        for key, value in flags.items():
            if value is not None:
                args.extend([f"--{key}", str(value)])
        process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                                   stderr=log, pass_fds=(write_fd,), env=build_child_env())
        os.close(write_fd)
        write_fd = -1
        reader_owns_fd = True
        raw = _read_ready_line(read_fd, process, None, log, timeout)
        ready = _parse_ready(raw)
        return ServedWorkspace(ready, process, log)
    except BaseException:
        if process is not None:
            _stop_process(process, grace_seconds=11.0)
        log.close()
        raise
    finally:
        if write_fd >= 0:
            os.close(write_fd)
        if not reader_owns_fd:
            with contextlib.suppress(OSError):
                os.close(read_fd)


def _parse_ready(raw: str) -> dict[str, Any]:
    if len(raw) > 262144:
        raise ValueError("workspace readiness exceeds limit")
    ready = json.loads(raw)
    if not isinstance(ready, dict) or ready.get("version") != 1:
        raise ValueError("unsupported workspace readiness record")
    for key in ("workspace_id", "root", "bucket", "endpoint", "region", "access_key_id", "secret_access_key"):
        if not isinstance(ready.get(key), str) or not ready[key]:
            raise ValueError(f"invalid workspace readiness field {key}")
    return ready
