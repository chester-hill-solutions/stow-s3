"""A stand-in for the part of botocore the recipes use.

Only the exception shape matters: the recipes read the error code and the
status out of the response, so that is what this carries. The shared corpus
pins the wire-level 412 and its PreconditionFailed code, so the shape here is
the one botocore produces for that response.
"""


class ClientError(Exception):
    def __init__(self, error_response, operation_name):
        super().__init__(str(error_response))
        self.response = error_response
        self.operation_name = operation_name
