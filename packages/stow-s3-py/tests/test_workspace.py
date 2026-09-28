import json
import os
import json
import os
from types import SimpleNamespace

import pytest

from stow_s3 import WorkspaceCommandError, prepare_workspace, run_workspace_command
from stow_s3 import workspace
from stow_s3.bin import ResolvedBinary, BinarySource


def test_prepare_workspace_passes_paths_as_arguments(monkeypatch):
    calls = []

    def fake_run(command, **kwargs):
        calls.append((command, kwargs))
        return SimpleNamespace(returncode=0, stdout='{"workspace_id":"ws_test"}', stderr="")

    monkeypatch.setattr(workspace, "require_stow_binary", lambda: ResolvedBinary("/tools/stow-s3", BinarySource.PATH))
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
    monkeypatch.setattr(workspace, "require_stow_binary", lambda: ResolvedBinary("stow-s3", BinarySource.PATH))
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
    monkeypatch.setattr(workspace, "require_stow_binary", lambda: ResolvedBinary("stow-s3", BinarySource.PATH))
    monkeypatch.setattr(
        workspace.subprocess,
        "run",
        lambda *_args, **_kwargs: SimpleNamespace(returncode=0, stdout="[]", stderr=""),
    )

    with pytest.raises(ValueError, match="non-object JSON"):
        run_workspace_command("diff")


def test_workspace_lifecycle_against_native_cli(tmp_path, monkeypatch):
    binary = os.environ.get("STOW_BIN")
    if not binary:
        pytest.skip("STOW_BIN is required for the native workspace CLI integration")
    monkeypatch.setenv("STOW_BIN", binary)
    registry = tmp_path / "registry"
    root = tmp_path / "workspace"
    manifest = tmp_path / "task.json"
    handoff = tmp_path / "handoff.json"
    input_path = tmp_path / "seed.txt"
    input_path.write_text("seed")
    manifest.write_text(
        json.dumps(
            {
                "version": 1,
                "root": str(root),
                "working_directory": ".",
                "registry_dir": str(registry),
                "inputs": [{"source": str(input_path), "destination": "seed.txt"}],
            }
        )
    )

    prepared = workspace.prepare_workspace(str(manifest))
    result = workspace.handoff_workspace(
        str(prepared["workspace_id"]), registry_dir=str(registry), output=str(handoff)
    )
    assert json.loads(handoff.read_text()) == result
    resumed = workspace.resume_workspace(handoff=str(handoff))
    assert resumed["workspace_id"] == prepared["workspace_id"]
    destroyed = workspace.destroy_workspace(str(prepared["workspace_id"]), registry_dir=str(registry))
    assert destroyed["destroyed"] is True
    assert not root.exists()


def test_workspace_lifecycle_against_native_cli(tmp_path, monkeypatch):
    binary = os.environ.get("STOW_BIN")
    if not binary:
        pytest.skip("STOW_BIN is required for the native workspace CLI integration")
    monkeypatch.setenv("STOW_BIN", binary)
    registry = tmp_path / "registry"
    root = tmp_path / "workspace"
    manifest = tmp_path / "task.json"
    handoff = tmp_path / "handoff.json"
    input_path = tmp_path / "seed.txt"
    input_path.write_text("seed")
    manifest.write_text(
        json.dumps(
            {
                "version": 1,
                "root": str(root),
                "working_directory": ".",
                "registry_dir": str(registry),
                "inputs": [{"source": str(input_path), "destination": "seed.txt"}],
            }
        )
    )

    prepared = workspace.prepare_workspace(str(manifest))
    result = workspace.handoff_workspace(
        str(prepared["workspace_id"]), registry_dir=str(registry), output=str(handoff)
    )
    assert json.loads(handoff.read_text()) == result
    resumed = workspace.resume_workspace(handoff=str(handoff))
    assert resumed["workspace_id"] == prepared["workspace_id"]
    destroyed = workspace.destroy_workspace(str(prepared["workspace_id"]), registry_dir=str(registry))
    assert destroyed["destroyed"] is True
    assert not root.exists()
