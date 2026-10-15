"""Keep the artifacts: the same round trip, but the objects outlive the session.

    make build
    export STOW_BIN="$PWD/bin/stow-s3"
    python examples/stow-recipes/keep_artifacts.py ./artifacts

The only change from start_small.py is passing ``data_dir``. A named directory
selects the filesystem backend, so the bytes survive close, and the caller's
directory is left in place for the next session.
"""

import sys
from pathlib import Path

from stow_s3 import with_session

data_dir = Path(sys.argv[1] if len(sys.argv) > 1 else "./artifacts")
bucket = "artifacts"

with with_session(data_dir=data_dir) as session:
    s3 = session.s3_client()
    s3.create_bucket(Bucket=bucket)
    s3.put_object(
        Bucket=bucket,
        Key="notes/design.md",
        Body=b"# Design\n\nFirst draft.\n",
        ContentType="text/markdown",
    )
    s3.close()
print(f"wrote {bucket}/notes/design.md; server stopped")

with with_session(data_dir=data_dir) as reopened:
    s3 = reopened.s3_client()
    body = s3.get_object(Bucket=bucket, Key="notes/design.md")["Body"].read()
    print("read back after reopen:", body.decode())
    s3.close()
