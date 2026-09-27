"""A spawn failure has to arrive as the spawn failure.

The server is started with subprocess.Popen, and the file descriptor carrying the
ready line is closed in a finally so the child sees EOF if it dies first. When Popen
itself raises - a missing binary, a file that is not executable, an exhausted
descriptor table - the except handler cleaned up too, and both handlers closed the
same descriptor.

The second close raises OSError(EBADF). Because it happens in the finally, it does
not merely add a confusing extra error: it replaces the exception on its way out,
so the caller who asked for a session and could not have one was told the descriptor
was bad rather than that the binary was missing. Every diagnosis of "the server
would not start" started from the wrong error.

These tests spawn nothing. Popen is replaced, so they are about the error path
rather than about the server, and they run in a fraction of a second.
"""

from __future__ import annotations

import errno
import subprocess

import pytest

from stow_s3 import session as session_module
from stow_s3 import stow_binary_available
from stow_s3.session import open_session

# The binary has to resolve for the test to reach Popen at all: require_stow_binary
# runs first and raises StowBinaryNotFoundError for a path that does not exist, which
# is a different failure from the one under test. So these use whatever binary the
# suite already found and replace only the spawn, which is the thing being examined.
needs_binary = pytest.mark.skipif(
    not stow_binary_available(), reason="no stow binary is available"
)


@needs_binary
@pytest.mark.parametrize(
    "raised",
    [
        FileNotFoundError(errno.ENOENT, "no such file or directory", "bin/stow-s3"),
        PermissionError(errno.EACCES, "permission denied", "bin/stow-s3"),
        OSError(errno.EMFILE, "too many open files"),
    ],
    ids=["missing-binary", "not-executable", "descriptor-exhausted"],
)
def test_open_session_surfaces_the_spawn_failure_itself(
    monkeypatch: pytest.MonkeyPatch, tmp_path, raised: OSError
) -> None:
    def refuse(*args: object, **kwargs: object) -> None:
        raise raised

    monkeypatch.setattr(session_module.subprocess, "Popen", refuse)

    with pytest.raises(type(raised)) as caught:
        open_session(data_dir=tmp_path / "data")

    # The decisive assertion: the error the caller sees is the one the spawn
    # produced. EBADF in this position means the cleanup path closed a descriptor
    # the finally was going to close again, which is the bug.
    assert not isinstance(caught.value, OSError) or caught.value.errno != errno.EBADF, (
        "the spawn failure was replaced by a bad-file-descriptor error: "
        f"{caught.value!r}"
    )
    assert caught.value is raised or caught.value.args == raised.args, (
        f"caller saw {caught.value!r}, the spawn raised {raised!r}"
    )


@needs_binary
def test_open_session_does_not_leak_the_ready_descriptor_on_spawn_failure(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    """The descriptor is closed once. Closing it twice is what produced EBADF.

    Counting the closes is the direct check. Asserting only on the raised error
    would pass if the fix were to swallow the second close instead of removing it,
    which would leave the handler's intent - clean up on the way out - untested.
    """
    closes: list[int] = []
    real_close = session_module.os.close

    def counting_close(fd: int) -> None:
        closes.append(fd)
        real_close(fd)

    def refuse(*args: object, **kwargs: object) -> None:
        raise FileNotFoundError(errno.ENOENT, "no such file or directory", "bin/stow-s3")

    monkeypatch.setattr(session_module.os, "close", counting_close)
    monkeypatch.setattr(session_module.subprocess, "Popen", refuse)

    with pytest.raises(FileNotFoundError):
        open_session(data_dir=tmp_path / "data")

    assert len(closes) == len(set(closes)), f"a descriptor was closed twice: {closes}"
