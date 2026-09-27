from types import SimpleNamespace

import pytest

from stow_s3 import WorkspaceCommandError, prepare_workspace, run_workspace_command
from stow_s3 import workspace


def test_prepare_workspace_passes_paths_as_arguments(monkeypatch):
    calls = []

    def fake_run(command, **kwargs):
        calls.append((command, kwargs))
        return SimpleNamespace(returncode=0, stdout='{"workspace_id":"ws_test"}', stderr="")

    monkeypatch.setattr(workspace, "require_stow_binary", lambda: "/tools/stow-s3")
    monkeypatch.setattr(workspace.subprocess, "run", fake_run)

    result = prepare_workspace("/tasks/task manifest.json")

    assert result == {"workspace_id": "ws_test"}
    assert calls == [
        (
            ["/tools/stow-s3", "workspace", "prepare", "--manifest", "/tasks/task manifest.json"],
            {"check": False, "capture_output": True, "text": True},
        )
    ]


def test_workspace_command_reports_cli_failure(monkeypatch):
    monkeypatch.setattr(workspace, "require_stow_binary", lambda: "stow-s3")
    monkeypatch.setattr(
        workspace.subprocess,
        "run",
        lambda *_args, **_kwargs: SimpleNamespace(returncode=2, stdout="", stderr="invalid manifest"),
    )

    with pytest.raises(WorkspaceCommandError, match="invalid manifest") as failure:
        run_workspace_command("prepare", "--manifest", "task.json")

    assert failure.value.returncode == 2
    assert failure.value.command == ("workspace", "prepare", "--manifest", "task.json")


def test_workspace_command_rejects_non_object_json(monkeypatch):
    monkeypatch.setattr(workspace, "require_stow_binary", lambda: "stow-s3")
    monkeypatch.setattr(
        workspace.subprocess,
        "run",
        lambda *_args, **_kwargs: SimpleNamespace(returncode=0, stdout="[]", stderr=""),
    )

    with pytest.raises(ValueError, match="non-object JSON"):
        run_workspace_command("diff")
