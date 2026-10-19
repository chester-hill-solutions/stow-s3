"""Reject a stale update: overwrite only the version you actually read.

    make build
    export STOW_BIN="$PWD/bin/stow-s3"
    python examples/stow-recipes/conditional_write.py

A conditional write compares the object's current ETag with the one you pass
and refuses the write if they differ. Two writers can read the same version; the
second update is rejected instead of silently overwriting the first.
"""

from botocore.exceptions import ClientError

from stow_s3 import with_session

with with_session() as session:
    s3 = session.s3_client()
    bucket = session.new_bucket_name()
    s3.create_bucket(Bucket=bucket)

    created = s3.put_object(Bucket=bucket, Key="config.json", Body=b'{"version":1}')

    # The first writer updates the version it read.
    s3.put_object(Bucket=bucket, Key="config.json", Body=b'{"version":2}', IfMatch=created["ETag"])

    # The second writer still holds the original ETag, so its update is refused.
    # The refusal has to be the precondition failure and not some other error:
    # a request that failed for another reason has proved nothing about the
    # write it was supposed to make.
    try:
        s3.put_object(Bucket=bucket, Key="config.json", Body=b'{"version":3}', IfMatch=created["ETag"])
    except ClientError as error:
        code = error.response["Error"]["Code"]
        status = error.response["ResponseMetadata"]["HTTPStatusCode"]
        if code != "PreconditionFailed" and status != 412:
            raise
        print("stale update rejected:", code, status)
    else:
        raise AssertionError("the stale update was accepted")

    # The version that was saved is still the one that is stored. A refusal is
    # only worth anything if the object it refused to replace is intact.
    retained = s3.get_object(Bucket=bucket, Key="config.json")
    try:
        body = retained["Body"].read().decode()
    finally:
        retained["Body"].close()
    if body != '{"version":2}':
        raise AssertionError(f"the stored object is {body}, want the version that was saved")
    print("retained:", body)
    s3.close()
