"""Reading a declared expectation, and comparing it against what a verb returned.

Split out of workspace_contract.py because that file had grown past the size gate,
and because these are the two things a driver needs from the contract that have
nothing to do with running a verb: resolving a key, and judging a value.

The two are kept together because they share the field-path vocabulary. A "changes"
key means a dotted path in one and a literal file name in the other, and the
distinction is stated in lookup_declared rather than spread across the callers.
"""

from __future__ import annotations

import json
import re
from typing import Any

_MISSING = object()

CAPTURE_PATTERN = re.compile(r"\{\{([A-Za-z0-9_]+)\}\}")


def _lookup_declared(step: dict[str, Any], result: Any, name: str) -> Any:
    """A "read" step's keys are file paths and are taken literally.

    A workspace is full of names that contain dots -- "seed.txt" is a file, not a
    field called seed inside a field called txt. Every other verb's keys are field
    paths into the returned document.
    """
    if step["verb"] == "read" and isinstance(result, dict):
        return result.get(name)
    return _lookup_field(result, name)



def _lookup_field(document: Any, path: str) -> Any:
    """Resolve a dotted path with optional indexes, so a case can say changes[0].path."""
    current = document
    for segment in path.split("."):
        name, bracket, tail = segment.partition("[")
        if not isinstance(current, dict) or name not in current:
            return _MISSING
        current = current[name]
        indexes = _parse_indexes(bracket + tail)
        if indexes is None:
            return _MISSING
        for index in indexes:
            if not isinstance(current, list) or index >= len(current):
                return _MISSING
            current = current[index]
    return current


def _parse_indexes(tail: str) -> list[int] | None:
    """Read the "[0][1]" tail of a field path, or None if it is malformed.

    None rather than an empty list is the point: an unparseable tail must leave the
    field unresolved, because the alternative is that a typo silently resolves to
    the container and the assertion quietly stops testing the field it names. The Go
    and TypeScript drivers do the same, and the case file is shared, so all three
    have to agree about what a typo means.
    """
    indexes: list[int] = []
    rest = tail
    while rest.startswith("["):
        closing = rest.find("]")
        if closing < 0:
            return None
        try:
            indexes.append(int(rest[1:closing]))
        except ValueError:
            return None
        rest = rest[closing + 1 :]
    return indexes if rest == "" else None


def _check_field(name: str, value: Any, want: dict[str, Any], substitute) -> list[str]:
    """Check one field and return every problem with it, not just the first."""
    if want.get("absent"):
        if value is not _MISSING:
            return [f"should be absent, but it is {_show(value)}"]
        return []
    if value is _MISSING:
        return ["is missing from the result"]
    problems: list[str] = []
    if "equals" in want:
        expected = _resolve_expected(want["equals"], substitute)
        if not _same_json(expected, value):
            problems.append(f"is {_show(value)}, and the contract says {_show(expected)}")
    if "notEquals" in want:
        expected = _resolve_expected(want["notEquals"], substitute)
        if _same_json(expected, value):
            problems.append(f"is {_show(value)}, and the contract says it must differ from that")
    if "matches" in want:
        if not isinstance(value, str):
            problems.append(f"holds {type(value).__name__}, and a pattern needs a string")
        elif re.search(want["matches"], value) is None:
            problems.append(f"is {_show(value)}, which does not match {want['matches']!r}")
    for key, comparison in (("length", "=="), ("minLength", ">=")):
        if key in want:
            length = _length_of(value)
            failed = length != want[key] if comparison == "==" else length < want[key]
            if failed:
                bound = "exactly" if comparison == "==" else "at least"
                problems.append(f"holds {length} entries, and the contract says {bound} {want[key]}")
    if "min" in want:
        if not isinstance(value, (int, float)) or isinstance(value, bool):
            problems.append(f"holds {type(value).__name__}, and a bound needs a number")
        elif value < want["min"]:
            problems.append(f"is {value}, and the contract says at least {want['min']}")
    return problems


def _resolve_expected(value: Any, substitute) -> Any:
    return substitute(value) if isinstance(value, str) else value


def _same_json(want: Any, got: Any) -> bool:
    """Compare structurally, so key order and 2 versus 2.0 are not disagreements."""
    if isinstance(want, bool) or isinstance(got, bool):
        return want is got
    if isinstance(want, (int, float)) and isinstance(got, (int, float)):
        return float(want) == float(got)
    if isinstance(want, dict) and isinstance(got, dict):
        return want.keys() == got.keys() and all(_same_json(want[k], got[k]) for k in want)
    if isinstance(want, list) and isinstance(got, list):
        return len(want) == len(got) and all(_same_json(a, b) for a, b in zip(want, got))
    return type(want) is type(got) and want == got


def _length_of(value: Any) -> int:
    if isinstance(value, (list, str)):
        return len(value)
    if isinstance(value, dict):
        return len(value)
    return -1


def _show(value: Any) -> str:
    return json.dumps(value)
