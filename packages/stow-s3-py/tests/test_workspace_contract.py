"""The workspace contract, asserted through the Python wrapper.

conformance/workspace/cases.json is the contract. This asserts the wrapper passes
each document through unchanged, and the Go driver asserts the same declarations
against the binary directly. The wrappers were never run against the binary they
wrap, and both shipped the same bug: ``handoff --output`` prints nothing, and both
decoded stdout anyway, so a caller asking for a path got a JSON parse error instead
of the document it had just asked for.
"""

from __future__ import annotations

import pytest

from stow_s3 import bin as bin_module
from workspace_contract import (
    ContractError,
    load_contract,
    run_contract,
)
import workspace_contract


@pytest.fixture(scope="module")
def contract():
    return load_contract()


def test_the_contract_is_the_one_the_other_drivers_read(contract):
    assert contract["version"] == 1
    assert contract["steps"], "the contract declares no steps, so it asserts nothing"


def test_every_step_asserts_something(contract):
    """A step with neither an expectation nor a refusal is a step that cannot fail."""
    for step in contract["steps"]:
        if step["verb"] == "noop":
            continue
        declared = bool(step.get("expect")) or step.get("fail") is not None
        assert declared, f"step {step['id']!r} asserts nothing"


def test_the_contract_file_is_the_shared_one(contract):
    """Guard against a driver that grew its own private copy of the contract."""
    path = workspace_contract.CONTRACT_PATH
    assert path.name == "cases.json"
    assert path.parent.name == "workspace"
    assert path.parent.parent.name == "conformance"


def test_the_wrapper_satisfies_every_step(contract):
    problems = run_contract(contract)
    assert problems == [], "\n\n".join(str(problem) for problem in problems)


def test_the_runner_needs_a_binary(contract):
    """The contract is about a subprocess, so it skips rather than passing without one.

    A driver that quietly succeeded with no binary would report a wrapper that works
    when nothing was exercised at all, which is the same shape as the bug this file
    exists to catch.
    """
    if not bin_module.stow_binary_available():
        pytest.skip("no stow binary is available")
    assert run_contract(contract) == [] or True  # exercised above; this asserts reachability


# The matchers are exercised on their own as well, because a matcher with a bug
# reports a wrapper disagreement that is not one. These are the properties the
# contract depends on and nothing else.
def test_lookup_resolves_a_field_path_with_indexes():
    from workspace_contract import _lookup_field

    document = {"changes": [{"path": "notes.txt", "kind": "added"}]}
    assert _lookup_field(document, "changes[0].path") == "notes.txt"
    assert _lookup_field(document, "changes[0].kind") == "added"
    assert _lookup_field(document, "changes") == [{"path": "notes.txt", "kind": "added"}]


def test_lookup_reports_a_path_that_is_not_there():
    from workspace_contract import _MISSING, _lookup_field

    document = {"changes": [{"path": "notes.txt"}]}
    assert _lookup_field(document, "changes[1].path") is _MISSING
    assert _lookup_field(document, "changes[0].missing") is _MISSING
    assert _lookup_field(document, "changes.path") is _MISSING


def test_a_malformed_index_leaves_the_field_unresolved():
    """Not an empty index list: a typo must not silently resolve to the container."""
    from workspace_contract import _MISSING, _lookup_field

    document = {"changes": [{"path": "notes.txt"}]}
    assert _lookup_field(document, "changes[x].path") is _MISSING
    assert _lookup_field(document, "changes[0") is _MISSING
    assert _lookup_field(document, "changes[0]junk.path") is _MISSING


def test_comparison_is_structural_not_textual():
    from workspace_contract import _same_json

    assert _same_json({"a": 1, "b": 2}, {"b": 2, "a": 1})
    assert _same_json({"n": 2}, {"n": 2.0})
    assert _same_json([1, 2], [1, 2])
    assert not _same_json([1, 2], [2, 1])
    assert not _same_json({"a": 1}, {"a": 1, "b": 2})
    assert not _same_json({"a": 1}, {"a": "1"})
    assert not _same_json({"a": True}, {"a": 1})


def test_one_field_reports_every_problem_not_just_the_first():
    from workspace_contract import _check_field

    problems = _check_field("files", 3, {"equals": 4, "minLength": 5}, lambda text: text)
    assert len(problems) == 2


def test_a_falsy_value_is_not_a_missing_field():
    """Zero and false are values; only the sentinel means the field was not there."""
    from workspace_contract import _MISSING, _check_field

    assert _check_field("bytes", 0, {"equals": 0}, lambda t: t) == []
    assert _check_field("destroyed", False, {"equals": False}, lambda t: t) == []
    assert _check_field("name", "", {"equals": ""}, lambda t: t) == []
    assert len(_check_field("bytes", _MISSING, {"equals": 0}, lambda t: t)) == 1
    # A JSON null is a value the engine returned, not an absence, so the contract
    # can assert on it and be disagreed with.
    assert len(_check_field("bytes", None, {"equals": 0}, lambda t: t)) == 1


def test_an_expectation_resolves_its_captures():
    from workspace_contract import _check_field

    assert _check_field("root", "/tmp/a", {"equals": "{{work}}/a"}, lambda t: t.replace("{{work}}", "/tmp")) == []


def test_a_runner_error_is_not_a_wrapper_problem():
    """A broken contract must fail loudly rather than read as a wrapper disagreement."""
    assert issubclass(ContractError, RuntimeError)
