"""Independent consumer exercise: real boto3 pipeline and portable workspace.

This is intentionally an integration consumer, not a mocked API call. It uses
the public Python context manager, boto3 HTTP requests, and native workspace CLI.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import urllib.request

import pytest
from botocore.exceptions import ClientError
from botocore.config import Config
from stow_s3 import with_session


def _run(binary, *args):
    proc = subprocess.run(
        [binary, "workspace", *args],
        text=True, capture_output=True, check=True, timeout=30,
    )
    return json.loads(proc.stdout)


def _application_job(s3, bucket):
    """A consumer that knows nothing about Stow: ingest JSON, compute, archive."""
    received = s3.get_object(Bucket=bucket, Key="in/report.json")
    source = json.loads(received["Body"].read())
    result = {
        "customer": source["customer"],
        "total_cents": sum(entry["amount_cents"] for entry in source["entries"]),
        "source_sha256": hashlib.sha256(json.dumps(source, sort_keys=True).encode()).hexdigest(),
    }
    output = json.dumps(result, sort_keys=True).encode()
    s3.put_object(Bucket=bucket, Key="out/summary.json", Body=output,
                  ContentType="application/json", Metadata={"job": "independent-smoke"})
    return output


def test_real_sdk_application_pipeline(tmp_path, monkeypatch):
    # An accidental AWS environment should not turn the disposable session
    # into real-cloud storage.
    monkeypatch.setenv("AWS_ACCESS_KEY_ID", "not-a-real-account")
    monkeypatch.setenv("AWS_SECRET_ACCESS_KEY", "not-a-real-secret")
    monkeypatch.setenv("STOW_ENDPOINT", "https://example.invalid")
    saved = tmp_path / "session-storage"
    source = {"customer": "c-142", "entries": [
        {"amount_cents": 3299}, {"amount_cents": 1099}, {"amount_cents": 502}
    ]}
    with with_session(data_dir=saved, max_bytes=8 * 1024 * 1024, max_objects=30) as session:
        # Ordinary SDK requests work without this, but botocore may choose
        # SigV2 for presigned S3 URLs unless explicitly configured.
        s3 = session.s3_client(config=Config(signature_version="s3v4"))
        bucket = session.new_bucket_name()
        s3.create_bucket(Bucket=bucket)
        s3.put_object(Bucket=bucket, Key="in/report.json",
                      Body=json.dumps(source).encode(), ContentType="application/json")
        output = _application_job(s3, bucket)
        assert json.loads(output)["total_cents"] == 4900
        data = s3.get_object(Bucket=bucket, Key="out/summary.json")["Body"].read()
        assert data == output
        meta = s3.head_object(Bucket=bucket, Key="out/summary.json")
        assert meta["Metadata"]["job"] == "independent-smoke"
        assert meta["ContentType"] == "application/json"

        copied = "archive/summary.json"
        s3.copy_object(Bucket=bucket, Key=copied,
                       CopySource={"Bucket": bucket, "Key": "out/summary.json"})
        assert s3.get_object(Bucket=bucket, Key=copied)["Body"].read() == output
        ranged = s3.get_object(Bucket=bucket, Key=copied,
                               Range="bytes=0-12")["Body"].read()
        assert ranged == output[:13]

        url = s3.generate_presigned_url(
            "get_object", Params={"Bucket": bucket, "Key": copied},
            ExpiresIn=45,
        )
        assert "X-Amz-Algorithm=AWS4-HMAC-SHA256" in url
        with urllib.request.urlopen(url, timeout=10) as response:
            assert response.read() == output

        # Exercise actual pagination, not just a one-object list.
        for index in range(11):
            s3.put_object(Bucket=bucket, Key=f"parts/{index:02}.txt",
                          Body=f"part {index}".encode())
        page = s3.list_objects_v2(Bucket=bucket, Prefix="parts/", MaxKeys=4)
        keys = [e["Key"] for e in page.get("Contents", [])]
        while page.get("IsTruncated"):
            page = s3.list_objects_v2(
                Bucket=bucket, Prefix="parts/", MaxKeys=4,
                ContinuationToken=page["NextContinuationToken"],
            )
            keys.extend(e["Key"] for e in page.get("Contents", []))
        assert keys == [f"parts/{i:02}.txt" for i in range(11)]

        # A refused conditional write must leave previously committed bytes.
        with pytest.raises(ClientError) as exc:
            s3.put_object(Bucket=bucket, Key=copied, Body=b"destroy",
                          IfNoneMatch="*")
        assert exc.value.response["ResponseMetadata"]["HTTPStatusCode"] == 412
        assert s3.get_object(Bucket=bucket, Key=copied)["Body"].read() == output

    # User-supplied filesystem storage survives the session's process exit.
    assert saved.is_dir()
    with with_session(data_dir=saved, max_bytes=8 * 1024 * 1024, max_objects=30) as reopened:
        s3 = reopened.s3_client()
        assert s3.get_object(Bucket=bucket, Key=copied)["Body"].read() == output


def test_real_agent_workspace_checkpoint_and_restored_output(tmp_path):
    binary = os.environ["STOW_BIN"]
    root = tmp_path / "task"
    registry = tmp_path / "registry"
    source = tmp_path / "assignment.md"
    source.write_text("Summarize ledger entries in plain text.\n")
    manifest = tmp_path / "manifest.json"
    manifest.write_text(json.dumps({
        "version": 1, "root": str(root), "registry_dir": str(registry),
        "inputs": [{"source": str(source), "destination": "TASK.md"}],
    }))
    prepared = _run(binary, "prepare", "--manifest", str(manifest))
    wid = prepared["workspace_id"]
    assert (root / "TASK.md").read_bytes() == source.read_bytes()

    base = _run(binary, "checkpoint", "--id", wid,
                "--registry-dir", str(registry))["checkpoint_id"]

    # Simulate an agent doing work in its own working directory.
    (root / "report.md").write_text("Total: $49.00\nStatus: complete\n")
    changed = _run(binary, "checkpoint", "--id", wid,
                   "--parent", base, "--registry-dir", str(registry))["checkpoint_id"]
    delta = _run(binary, "diff", "--from", base, "--to", changed,
                 "--registry-dir", str(registry))
    assert any(change["path"] == "report.md" and change["kind"] == "added"
               for change in delta["changes"])

    archive = tmp_path / "transfer.tar.gz"
    _run(binary, "export", "--checkpoint-id", changed,
         "--registry-dir", str(registry), "--output", str(archive))
    assert archive.stat().st_size > 0

    restored = tmp_path / "restored"
    _run(binary, "restore", "--checkpoint-id", changed,
         "--registry-dir", str(registry), "--root", str(restored))
    assert (restored / "report.md").read_text() == "Total: $49.00\nStatus: complete\n"
    assert (restored / "TASK.md").read_bytes() == source.read_bytes()
