"""Thin Python wrappers around the canonical ``stow-s3 workspace`` CLI."""

from __future__ import annotations

import json
import subprocess
from collections.abc import Sequence

from .bin import require_stow_binary

WorkspaceJSON = dict[str, object]


class WorkspaceCommandError(RuntimeError):
    """A workspace CLI command failed and returned a nonzero exit status."""

    def __init__(self, command: Sequence[str], returncode: int, stderr: str) -> None:
        detail = stderr.strip() or "no error details were returned"
        super().__init__(f"stow-s3 {' '.join(command)} failed ({returncode}): {detail}")
        self.command = tuple(command)
        self.returncode = returncode
        self.stderr = stderr


def run_workspace_command(*args: str) -> WorkspaceJSON:
    """Run a workspace CLI command directly, without a shell, and decode JSON."""

    command = ("workspace", *args)
    result = subprocess.run(
        [require_stow_binary().path, *command],
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise WorkspaceCommandError(command, result.returncode, result.stderr)
    try:
        value = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise ValueError("stow-s3 workspace command returned invalid JSON") from error
    if not isinstance(value, dict):
        raise ValueError("stow-s3 workspace command returned a non-object JSON value")
    return value


def prepare_workspace(manifest_path: str) -> WorkspaceJSON:
    return run_workspace_command("prepare", "--manifest", manifest_path)


def resume_workspace(
    *, id: str | None = None, handoff: str | None = None, registry_dir: str | None = None
) -> WorkspaceJSON:
    args = ["resume"]
    _append_flag(args, "--id", id)
    _append_flag(args, "--handoff", handoff)
    _append_flag(args, "--registry-dir", registry_dir)
    return run_workspace_command(*args)


def destroy_workspace(id: str, *, registry_dir: str | None = None) -> WorkspaceJSON:
    args = ["destroy", "--id", id]
    _append_flag(args, "--registry-dir", registry_dir)
    return run_workspace_command(*args)


def collect_workspaces(*, registry_dir: str | None = None) -> WorkspaceJSON:
    args = ["collect"]
    _append_flag(args, "--registry-dir", registry_dir)
    return run_workspace_command(*args)


def handoff_workspace(
    id: str,
    *,
    registry_dir: str | None = None,
    team: str | None = None,
    checkpoint_id: str | None = None,
    archive: str | None = None,
    output: str | None = None,
) -> WorkspaceJSON:
    args = ["handoff", "--id", id]
    _append_flag(args, "--registry-dir", registry_dir)
    _append_flag(args, "--team", team)
    _append_flag(args, "--checkpoint-id", checkpoint_id)
    _append_flag(args, "--archive", archive)
    _append_flag(args, "--output", output)
    return run_workspace_command(*args)


def adopt_workspace_handoff(
    handoff: str,
    root: str,
    *,
    registry_dir: str | None = None,
    team: str | None = None,
    max_bytes: int | None = None,
    max_files: int | None = None,
    include_sensitive: bool = False,
) -> WorkspaceJSON:
    """Adopt a portable handoff on the machine that received it.

    The document and the archive it names are verified before anything is
    written, so a handoff that arrived over a channel is checked rather than
    trusted.
    """
    args = ["adopt", "--handoff", handoff, "--root", root]
    _append_archive_options(args, registry_dir, max_bytes, max_files, include_sensitive)
    _append_flag(args, "--team", team)
    return run_workspace_command(*args)


def create_workspace_delta(
    *,
    from_id: str,
    to_id: str,
    output: str,
    registry_dir: str | None = None,
    max_bytes: int | None = None,
    max_files: int | None = None,
    include_sensitive: bool = False,
) -> WorkspaceJSON:
    """Write the difference between two checkpoints to a document the other side can apply."""
    args = ["delta", "--from", from_id, "--to", to_id, "--output", output]
    _append_archive_options(args, registry_dir, max_bytes, max_files, include_sensitive)
    return run_workspace_command(*args)


def apply_workspace_delta(
    delta: str,
    *,
    base: str,
    registry_dir: str | None = None,
) -> WorkspaceJSON:
    """Bring a base checkpoint to the state a delta describes.

    The base is named rather than defaulted, because a delta states what a path
    was and is now and the point it applies to is part of that meaning.
    """
    args = ["apply", "--delta", delta, "--base", base]
    _append_flag(args, "--registry-dir", registry_dir)
    return run_workspace_command(*args)


def checkpoint_workspace(
    id: str,
    *,
    registry_dir: str | None = None,
    parent: str | None = None,
    max_bytes: int | None = None,
    max_files: int | None = None,
    include_sensitive: bool = False,
) -> WorkspaceJSON:
    args = ["checkpoint", "--id", id]
    _append_flag(args, "--registry-dir", registry_dir)
    _append_flag(args, "--parent", parent)
    _append_flag(args, "--max-bytes", max_bytes)
    _append_flag(args, "--max-files", max_files)
    _append_bool(args, "--include-sensitive", include_sensitive)
    return run_workspace_command(*args)


def diff_workspaces(from_id: str, to_id: str, *, registry_dir: str | None = None) -> WorkspaceJSON:
    args = ["diff", "--from", from_id, "--to", to_id]
    _append_flag(args, "--registry-dir", registry_dir)
    return run_workspace_command(*args)


def restore_workspace_checkpoint(
    checkpoint_id: str, root: str, *, registry_dir: str | None = None
) -> WorkspaceJSON:
    args = ["restore", "--checkpoint-id", checkpoint_id, "--root", root]
    _append_flag(args, "--registry-dir", registry_dir)
    return run_workspace_command(*args)


def export_workspace_checkpoint(
    checkpoint_id: str,
    output: str,
    *,
    registry_dir: str | None = None,
    max_bytes: int | None = None,
    max_files: int | None = None,
    include_sensitive: bool = False,
) -> WorkspaceJSON:
    args = ["export", "--checkpoint-id", checkpoint_id, "--output", output]
    _append_archive_options(args, registry_dir, max_bytes, max_files, include_sensitive)
    return run_workspace_command(*args)


def import_workspace_checkpoint(
    archive: str,
    *,
    registry_dir: str | None = None,
    max_bytes: int | None = None,
    max_files: int | None = None,
    include_sensitive: bool = False,
) -> WorkspaceJSON:
    args = ["import", "--archive", archive]
    _append_archive_options(args, registry_dir, max_bytes, max_files, include_sensitive)
    return run_workspace_command(*args)


def _append_archive_options(
    args: list[str],
    registry_dir: str | None,
    max_bytes: int | None,
    max_files: int | None,
    include_sensitive: bool,
) -> None:
    _append_flag(args, "--registry-dir", registry_dir)
    _append_flag(args, "--max-bytes", max_bytes)
    _append_flag(args, "--max-files", max_files)
    _append_bool(args, "--include-sensitive", include_sensitive)


def _append_flag(args: list[str], name: str, value: str | int | None) -> None:
    if value is not None:
        args.extend((name, str(value)))


def _append_bool(args: list[str], name: str, value: bool) -> None:
    if value:
        args.append(name)
