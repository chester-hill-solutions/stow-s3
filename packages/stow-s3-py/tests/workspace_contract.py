"""Runs conformance/workspace/cases.json through the Python wrapper.

The case file is the contract. This module is a driver for it and nothing more: if
a judgement about what a verb returns lives here, it is a fact about the wrapper
rather than about the contract, and the other two drivers cannot see it.

Every verb goes through the exported wrapper rather than through subprocess
directly. That is the whole point. The wrapper is a veneer over the same binary
the Go driver runs, and a veneer that mistranslates a document is invisible to any
test that skips it -- which is how this wrapper shipped the same bug as the
TypeScript one: ``handoff --output`` prints nothing, and both decoded stdout
anyway, so a caller asking for a path got a JSON parse error instead of the
document it had just asked for.
"""

from __future__ import annotations

import base64
import json
import re
import shutil
import tempfile
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

# _MISSING and _parse_indexes are re-exported for the matcher tests rather than
# used here: the runner reads declared values through _lookup_declared and never
# needs to know how a path is resolved.
from workspace_contract_matchers import (
    _check_field,
    _lookup_declared,
    _lookup_field,
    _MISSING,  # noqa: F401  re-exported for the matcher tests
    _parse_indexes,  # noqa: F401  re-exported for the matcher tests
    _same_json,
)
from stow_s3 import (
    adopt_workspace_handoff,
    apply_workspace_delta,
    checkpoint_workspace,
    collect_workspaces,
    create_workspace_delta,
    destroy_workspace,
    diff_workspaces,
    handoff_workspace,
    list_workspaces,
    prepare_workspace,
    resume_workspace,
)

CONTRACT_PATH = Path(__file__).resolve().parents[3] / "conformance" / "workspace" / "cases.json"
CAPTURE_PATTERN = re.compile(r"\{\{([A-Za-z0-9_]+)\}\}")
RETURNS_FILE = "file"
REGISTRY_A = "a"
CAPTURE_PATTERN = re.compile(r"\{\{([A-Za-z0-9_]+)\}\}")


@dataclass
class Problem:
    """One thing the wrapper did that the contract does not allow."""

    step: str
    field: str
    message: str

    def __str__(self) -> str:
        return f"  {self.step}\n    {self.field} {self.message}"


@dataclass
class Run:
    """The state one pass through the scenario accumulates."""

    work: Path
    captures: dict[str, str] = field(default_factory=dict)
    problems: list[Problem] = field(default_factory=list)

    def substitute(self, text: str) -> str:
        for name, value in self.captures.items():
            text = text.replace("{{" + name + "}}", value)
        return text

    def require_captures(self, step: dict[str, Any], value: str) -> None:
        """Fail on a reference to a capture no earlier step produced.

        A silently empty argument is a scenario that quietly stopped testing what it
        says it tests, which is the one failure a shared contract cannot have.
        """
        for reference in CAPTURE_PATTERN.findall(value):
            if reference not in self.captures:
                raise ContractError(
                    f"step {step['id']!r}: an argument refers to {{{{{reference}}}}}, "
                    "which no earlier step captured"
                )


class ContractError(RuntimeError):
    """The contract or the runner is wrong, rather than the wrapper."""


def load_contract() -> dict[str, Any]:
    return json.loads(CONTRACT_PATH.read_text())


def run_contract(contract: dict[str, Any]) -> list[Problem]:
    """Run every step in order and return everything the wrapper got wrong.

    The run stops at the first failing step: the steps share a registry and a work
    directory, so continuing past a failure reports one missing workspace as a dozen
    failures and buries the one that broke.
    """
    if contract.get("version") != 1:
        raise ContractError(f"the contract is version {contract.get('version')}, and this driver reads 1")
    work = Path(tempfile.mkdtemp(prefix="stow-workspace-contract-"))
    try:
        run = Run(work=work, captures={"work": str(work)})
        for step in contract["steps"]:
            if run.problems:
                break
            _run_step(run, step)
        return run.problems
    finally:
        shutil.rmtree(work, ignore_errors=True)


def _run_step(run: Run, step: dict[str, Any]) -> None:
    verb = step["verb"]
    try:
        if verb == "noop":
            _apply_writes(run, step)
            return
        if verb == "read":
            _judge(run, step, _read_tree(run, step["root"]))
            return
        _judge(run, step, _invoke(run, step))
    except ContractError:
        raise
    except Exception as error:  # noqa: BLE001 - a refusal is an expected outcome
        if step.get("fail") is not None:
            _judge_refusal(run, step, error)
            return
        raise ContractError(f"step {step['id']!r}: {error}") from error


def _invoke(run: Run, step: dict[str, Any]) -> Any:
    args = _resolve_args(run, step)
    registry_dir = _registry_dir_for(run, step)
    team = step.get("team")
    verb = step["verb"]

    if verb == "prepare":
        return _prepare(run, step)
    if verb == "checkpoint":
        return checkpoint_workspace(
            args["id"],
            registry_dir=registry_dir,
            team=team,
            parent=args.get("parent"),
        )
    if verb == "diff":
        return diff_workspaces(args["from"], args["to"], registry_dir=registry_dir, team=team)
    if verb == "handoff":
        return handoff_workspace(
            args["id"],
            registry_dir=registry_dir,
            team=team,
            checkpoint_id=args.get("checkpoint-id"),
            archive=args.get("archive"),
            output=args.get("output"),
        )
    if verb == "adopt":
        return adopt_workspace_handoff(args["handoff"], args["root"])
    if verb == "delta":
        return create_workspace_delta(
            from_id=args["from"],
            to_id=args["to"],
            output=args["output"],
            registry_dir=registry_dir,
            team=team,
        )
    if verb == "apply":
        delta = args["delta"]
        if step.get("tamper") is not None:
            delta = _tamper(run, step)
        renamed = step.get("renameInTransit")
        if renamed is not None:
            delta = _rename_in_transit(run, renamed)
        return apply_workspace_delta(
            delta,
            base=args["base"],
            expect_sha256=args.get("expect-sha256"),
            registry_dir=registry_dir,
            team=team,
        )
    if verb == "resume":
        return resume_workspace(handoff=args["handoff"])
    if verb == "list":
        return list_workspaces(
            registry_dir=_registry_dir_for(run, step),
            team=step.get("team"),
            all=step.get("all") is True,
        )
    if verb == "collect":
        return collect_workspaces(registry_dir=_registry_dir_for(run, step))
    if verb == "destroy":
        return destroy_workspace(args["id"], registry_dir=_registry_dir_for(run, step))
    raise ContractError(f"the contract runner has no wrapper call for {verb!r}")


def _resolve_args(run: Run, step: dict[str, Any]) -> dict[str, str]:
    resolved: dict[str, str] = {}
    for name, value in (step.get("args") or {}).items():
        substituted = run.substitute(value)
        run.require_captures(step, substituted)
        resolved[name] = substituted
    return resolved


def _registry_dir_for(run: Run, step: dict[str, Any]) -> str | None:
    """The registry directory a step's verb is pointed at.

    A step that names ``--registry-dir`` itself is authoritative, and that is how the
    contract reaches a team partition: prepare reports the partition it created, and
    collect and destroy -- which take ``--registry-dir`` but no ``--team`` -- are
    pointed straight at it. Every other verb gets the driver's own directory plus
    the team's ``--team`` flag and lets the CLI compose the two, because a driver
    that reconstructed the partition path itself would be asserting a belief about
    the store rather than about the verb.
    """
    declared = (step.get("args") or {}).get("registry-dir")
    if declared is not None:
        return run.substitute(declared)
    if step.get("registry") == REGISTRY_A:
        return str(run.work / "registry-a")
    return None


def _prepare(run: Run, step: dict[str, Any]) -> Any:
    spec = step.get("manifest")
    if spec is None:
        raise ContractError(f"step {step['id']!r}: prepare has no manifest")
    inputs = []
    for declared in spec.get("inputs") or []:
        destination = run.substitute(declared["destination"])
        source = run.work / "inputs" / destination
        source.parent.mkdir(parents=True, exist_ok=True)
        source.write_text(declared["body"])
        inputs.append({"source": str(source), "destination": destination})
    manifest_path = run.work / f"manifest-{step['id'].replace(' ', '-')}.json"
    manifest_path.write_text(
        json.dumps(
            {
                "version": 1,
                "root": run.substitute(spec["root"]),
                "team": spec.get("team"),
                "registry_dir": str(run.work / "registry-a"),
                "inputs": inputs,
            }
        )
    )
    return prepare_workspace(str(manifest_path))


def _apply_writes(run: Run, step: dict[str, Any]) -> None:
    root = run.captures.get("root")
    if root is None:
        raise ContractError("a step writes into the workspace before any step has prepared one")
    for declared in step.get("write") or []:
        full = Path(root) / run.substitute(declared["path"])
        full.parent.mkdir(parents=True, exist_ok=True)
        full.write_text(declared["body"])


def _read_tree(run: Run, root: str) -> dict[str, Any]:
    """Turn a workspace root into the flat path map a "read" step asserts against."""
    files: dict[str, Any] = {}
    base = Path(run.substitute(root))
    for path in sorted(base.rglob("*")):
        if path.is_file():
            files[path.relative_to(base).as_posix()] = path.read_text()
    return files


def _tamper(run: Run, step: dict[str, Any]) -> str:
    """Substitute one byte of one file's payload and re-encode.

    The document stays well formed and only its content stops matching the digest it
    carries, which is the substitution a receiver has to catch. A raw byte flip
    usually lands in the JSON and is caught by the parser, so a test built on one
    passes for a reason that has nothing to do with integrity.
    """
    source = _work_path(run, step["tamper"])
    document = json.loads(source.read_text())
    content = document.get("content")
    if not content:
        raise ContractError(f"{source} carries no content, so there is nothing to substitute")
    name = sorted(content)[0]
    payload = bytearray(base64.b64decode(content[name]))
    if not payload:
        raise ContractError(f"{source}: the content of {name} is empty, so there is nothing to substitute")
    payload[0] ^= 0x01
    content[name] = base64.b64encode(bytes(payload)).decode()
    target = _work_path(run, f"tampered-{step['tamper']}")
    target.write_text(json.dumps(document, separators=(",", ":")))
    return str(target)


def _rename_in_transit(run: Run, rename: dict[str, str]) -> str:
    """Build the substitution the document digest exists to catch.

    The change's declared path moves, in the change list and in the change's own
    to-metadata, and the payload moves with it in the content map. All three have to
    move together or the document stops being self-consistent and is refused by the
    decoder, which is a different refusal and would make the digest look like it was
    working when it was not the thing under test.

    The result passes every check the document makes about itself: the per-file
    content digest still matches the bytes it carries, and an addition's precondition
    passes because the new name is absent from the base. The only thing that can
    catch it is a digest over the document as a whole.
    """
    # Substituted, like every other path a case file names. Reading the raw
    # "{{work}}/..." would look for a directory of that name and report a missing
    # file, which reads as a broken fixture rather than a driver that skipped a step.
    source = _work_path(run, run.substitute(rename["from"]))
    document = json.loads(source.read_text())

    changes = document.get("changes")
    if not isinstance(changes, list):
        raise ContractError(f"{source} carries no change list, so there is nothing to substitute")
    moved = False
    for change in changes:
        if change.get("path") != rename["path"]:
            continue
        change["path"] = rename["to"]
        target = change.get("to")
        if isinstance(target, dict):
            target["path"] = rename["to"]
        moved = True
    if not moved:
        raise ContractError(f"{source} has no change naming {rename['path']!r}")

    content = document.get("content")
    if not isinstance(content, dict) or rename["path"] not in content:
        raise ContractError(f"{source} carries no content for {rename['path']!r}")
    content[rename["to"]] = content.pop(rename["path"])

    target_path = _work_path(run, "renamed.stowdelta")
    target_path.write_text(json.dumps(document, separators=(",", ":")))
    return str(target_path)


def _work_path(run: Run, name: str) -> Path:
    path = Path(name)
    return path if path.is_absolute() else run.work / name


def _judge(run: Run, step: dict[str, Any], result: Any) -> None:
    if step.get("alsoWritten") is not None:
        _judge_also_written(run, step, result)
    for name in sorted(step.get("expect") or {}):
        want = step["expect"][name]
        value = _lookup_declared(step, result, name)
        for message in _check_field(name, value, want, run.substitute):
            run.problems.append(Problem(step=step["id"], field=name, message=message))
    for name in sorted(step.get("capture") or {}):
        field_name = step["capture"][name]
        value = _lookup_field(result, field_name)
        if not isinstance(value, str):
            raise ContractError(
                f"step {step['id']!r}: nothing captured {name!r} because {field_name!r} holds "
                f"{type(value).__name__}, and only a string can be substituted into a later argument"
            )
        run.captures[name] = value


def _judge_also_written(run: Run, step: dict[str, Any], result: Any) -> None:
    path = _work_path(run, step["alsoWritten"])
    if not path.exists():
        raise ContractError(
            f"step {step['id']!r}: the verb was asked to write {step['alsoWritten']} and did not"
        )
    if step.get("returns") != RETURNS_FILE:
        return
    written = json.loads(path.read_text())
    if not _same_json(written, result):
        run.problems.append(
            Problem(
                step=step["id"],
                field=step["alsoWritten"],
                message=(
                    f"the document written to {step['alsoWritten']} is not the one the wrapper returned.\n"
                    f"on disk: {json.dumps(written)}\nreturned: {json.dumps(result)}"
                ),
            )
        )


def _judge_refusal(run: Run, step: dict[str, Any], error: Exception) -> None:
    message = _describe(error)
    wanted = step["fail"].get("contains", "")
    if wanted and wanted not in message:
        run.problems.append(
            Problem(
                step=step["id"],
                field="refusal",
                message=f"the refusal does not say {wanted!r}.\n{message}",
            )
        )


def _describe(error: Exception) -> str:
    """The child's stderr, which is where the refusal text a step asserts on lives."""
    stderr = getattr(error, "stderr", None)
    if isinstance(stderr, bytes):
        stderr = stderr.decode(errors="replace")
    if isinstance(stderr, str) and stderr.strip():
        return f"{error}\n{stderr}"
    return str(error)
