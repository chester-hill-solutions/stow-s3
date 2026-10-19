"""A stand-in for the Stow client package, used to run a recipe against a
scripted service.

The recipe body is not changed to make this work. The test puts this directory
on PYTHONPATH, so the recipe's imports resolve here instead of to the installed
packages, and the recipe runs exactly as it is published.

The scenario is chosen with STOW_RECIPE_SCENARIO. Each one is a service that
misbehaves in exactly one way, so a check can show that the recipe notices.
"""

import os

from botocore.exceptions import ClientError

SCENARIOS = {
    # The service refuses the stale write for the right reason and keeps the
    # version that was saved. The recipe passes.
    "stale-refused": {
        "refusal": ("PreconditionFailed", 412),
        "retained": '{"version":2}',
    },
    # The service refuses the request for an unrelated reason. The recipe has
    # proved nothing about the write it was meant to make.
    "access-denied": {
        "refusal": ("AccessDenied", 403),
        "retained": '{"version":2}',
    },
    # The service refuses the stale write correctly but stores something else.
    # A refusal is only worth anything if the object it refused to replace is
    # intact.
    "wrong-retained": {
        "refusal": ("PreconditionFailed", 412),
        "retained": '{"version":999}',
    },
}


def _scenario():
    name = os.environ.get("STOW_RECIPE_SCENARIO", "stale-refused")
    chosen = SCENARIOS.get(name)
    if chosen is None:
        raise RuntimeError(f"unknown recipe scenario: {name}")
    return chosen


class _Body:
    def __init__(self, data):
        self._data = data

    def read(self):
        return self._data

    def close(self):
        pass


class _Client:
    def __init__(self, chosen):
        self._chosen = chosen
        self._conditional_writes = 0

    def create_bucket(self, **_kwargs):
        return {}

    def put_object(self, **_kwargs):
        if _kwargs.get("IfMatch") is None:
            return {"ETag": '"etag-1"'}
        self._conditional_writes += 1
        if self._conditional_writes == 1:
            return {"ETag": '"etag-2"'}
        code, status = self._chosen["refusal"]
        raise ClientError(
            {
                "Error": {"Code": code, "Message": "the request was refused"},
                "ResponseMetadata": {"HTTPStatusCode": status},
            },
            "PutObject",
        )

    def get_object(self, **_kwargs):
        return {"Body": _Body(self._chosen["retained"].encode())}

    def close(self):
        pass


class _Session:
    def __init__(self, chosen):
        self._chosen = chosen

    def __enter__(self):
        return self

    def __exit__(self, *_exception):
        return False

    def s3_client(self):
        return _Client(self._chosen)

    def new_bucket_name(self):
        return "probe"


def with_session():
    return _Session(_scenario())
