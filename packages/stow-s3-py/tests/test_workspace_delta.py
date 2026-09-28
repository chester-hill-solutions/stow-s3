from pathlib import Path
from types import SimpleNamespace

import pytest

from stow_s3 import (
    adopt_workspace_handoff,
    apply_workspace_delta,
    create_workspace_delta,
    handoff_workspace,
)
from stow_s3 import workspace


@pytest.fixture
def calls(monkeypatch):
    """Record every command the wrappers build, and answer each with one object."""
    recorded: list[list[str]] = []

    def fake_run(command, **kwargs):
        recorded.append(command)
        # The two commands that take --output do not agree about stdout, and the
        # double has to model each one rather than guessing:
        #   handoff  writes the document to the path and prints nothing, and the
        #            wrapper reads it back from there;
        #   delta    writes the transported document to the path AND prints its own
        #            result, which the wrapper decodes from stdout.
        # A double that printed for both hid the handoff disagreement, which is
        # the one the lifecycle test then caught against the real binary.
        printed = '{"ok":true}'
        if "--output" in command:
            Path(command[command.index("--output") + 1]).write_text(printed)
            if command[1] == "handoff":
                printed = ""
        return SimpleNamespace(returncode=0, stdout=printed, stderr="")

    # The double has to answer with the same shape the real resolver returns.
    # require_stow_binary() returns a ResolvedBinary, and the wrapper passes its
    # .path into argv, so returning a bare string here left the test asserting
    # against a contract the production code does not have. It passed while
    # subprocess.run was mocked, because nothing validated argv.
    monkeypatch.setattr(
        workspace, "require_stow_binary", lambda: SimpleNamespace(path="/tools/stow-s3")
    )
    monkeypatch.setattr(workspace.subprocess, "run", fake_run)
    return recorded


def test_handoff_carries_the_team_and_the_archive(calls):
    handoff_workspace(
        "ws_1",
        registry_dir="/registry",
        team="platform",
        checkpoint_id="cp_1",
        archive="/tmp/cp.tar.gz",
        output="/tmp/handoff.json",
    )

    assert calls == [
        [
            "/tools/stow-s3",
            "workspace",
            "handoff",
            "--id",
            "ws_1",
            "--registry-dir",
            "/registry",
            "--team",
            "platform",
            "--checkpoint-id",
            "cp_1",
            "--archive",
            "/tmp/cp.tar.gz",
            "--output",
            "/tmp/handoff.json",
        ]
    ]


def test_adopt_names_the_document_and_the_root(calls):
    adopt_workspace_handoff("/tmp/handoff.json", "/tmp/adopted", registry_dir="/registry")

    assert calls == [
        [
            "/tools/stow-s3",
            "workspace",
            "adopt",
            "--handoff",
            "/tmp/handoff.json",
            "--root",
            "/tmp/adopted",
            "--registry-dir",
            "/registry",
        ]
    ]


def test_a_delta_names_both_ends_and_its_destination(calls):
    create_workspace_delta(
        from_id="cp_base", to_id="cp_target", output="/tmp/change.stowdelta", max_bytes=4096
    )

    assert calls == [
        [
            "/tools/stow-s3",
            "workspace",
            "delta",
            "--from",
            "cp_base",
            "--to",
            "cp_target",
            "--output",
            "/tmp/change.stowdelta",
            "--max-bytes",
            "4096",
        ]
    ]


def test_applying_a_delta_always_names_its_base(calls):
    apply_workspace_delta("/tmp/change.stowdelta", base="cp_base")

    assert calls == [
        [
            "/tools/stow-s3",
            "workspace",
            "apply",
            "--delta",
            "/tmp/change.stowdelta",
            "--base",
            "cp_base",
        ]
    ]


def test_a_conflict_from_the_cli_is_reported_rather_than_swallowed(calls, monkeypatch):
    monkeypatch.setattr(
        workspace.subprocess,
        "run",
        lambda *_args, **_kwargs: SimpleNamespace(
            returncode=1, stdout="", stderr="delta refused: the base has diverged from the delta"
        ),
    )

    with pytest.raises(workspace.WorkspaceCommandError, match="diverged"):
        apply_workspace_delta("/tmp/change.stowdelta", base="cp_base")
