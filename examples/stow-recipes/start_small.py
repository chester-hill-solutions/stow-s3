"""Store an artifact in a disposable S3 bucket and read it back.

    make build
    export STOW_BIN="$PWD/bin/stow-s3"
    python examples/stow-recipes/start_small.py

The application code takes an S3 client and a bucket and does not know Stow
exists. Only the ``with_session`` block is Stow-specific. A Python session
starts a private loopback server and cleans it up; it does not create a bucket,
so the example names one and creates it.
"""

import json

from stow_s3 import with_session


def publish_report(s3, bucket, report):
    """Ordinary application code: any S3 client and bucket will do."""
    s3.put_object(
        Bucket=bucket,
        Key="reports/latest.json",
        Body=json.dumps(report).encode(),
        ContentType="application/json",
    )
    body = s3.get_object(Bucket=bucket, Key="reports/latest.json")["Body"].read()
    return json.loads(body)


report = {"service": "checkout", "ok": True, "duration_ms": 128}

with with_session() as session:
    s3 = session.s3_client()
    bucket = session.new_bucket_name()
    s3.create_bucket(Bucket=bucket)
    print("stored reports/latest.json:", json.dumps(publish_report(s3, bucket, report)))
    s3.close()
