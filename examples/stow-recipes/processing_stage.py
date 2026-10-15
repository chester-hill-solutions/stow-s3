"""A classify stage: workers read input objects and write category results.

    make build
    export STOW_BIN="$PWD/bin/stow-s3"
    python examples/stow-recipes/processing_stage.py ./pipeline

Inputs and results are retained, so the stage can be rerun after adding
documents. Stow stores the bytes; the application owns the queue, the
scheduling and the retries. This is not a durable job queue and does not promise
exactly-once processing: if a worker is interrupted, rerun the key.
"""

import json
import re
import sys
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from stow_s3 import with_session

data_dir = Path(sys.argv[1] if len(sys.argv) > 1 else "./pipeline")
bucket = "pipeline"
inputs = {
    "invoices/march.txt": "Invoice 1001 for March services.",
    "invoices/april.txt": "Invoice 1002 for April services.",
    "support/tickets.txt": "Support ticket: cannot sign in.",
}


def categorize(text):
    """Stands in for a real classifier."""
    return "invoice" if re.search(r"\binvoice\b", text, re.IGNORECASE) else "support"


# #region excerpt
def classify_one(s3, key):
    """One unit of work: read an input key, write its result under results/."""
    response = s3.get_object(Bucket=bucket, Key=f"inputs/{key}")
    try:
        source = response["Body"].read()
    finally:
        response["Body"].close()
    category = categorize(source.decode())
    s3.put_object(
        Bucket=bucket,
        Key=f"results/{key}.json",
        Body=json.dumps({"input": key, "category": category}).encode(),
        ContentType="application/json",
    )
    return f"{key} -> {category}"


def run_workers(s3, keys):
    """The application builds the queue and runs two workers over it.

    Stow does not schedule this or retry it; if a worker fails, the application
    retries the key.
    """
    queue = list(keys)
    with ThreadPoolExecutor(max_workers=2) as pool:
        for message in pool.map(lambda key: classify_one(s3, key), queue):
            print(message)
# #endregion excerpt


with with_session(data_dir=data_dir) as session:
    s3 = session.s3_client()
    s3.create_bucket(Bucket=bucket)
    # Seed the inputs. Rerunning overwrites the same keys, so it is idempotent.
    for key, text in inputs.items():
        s3.put_object(Bucket=bucket, Key=f"inputs/{key}", Body=text.encode(), ContentType="text/plain")
    run_workers(s3, list(inputs))

    # Inputs and results are retained, so the stage can be inspected or rerun.
    retained = s3.list_objects_v2(Bucket=bucket, Prefix="results/")
    print("retained results:", sorted(item["Key"] for item in retained.get("Contents", [])))
    s3.close()
