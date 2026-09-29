import assert from "node:assert/strict";
import {
  CreateBucketCommand,
  DeleteBucketCommand,
  DeleteObjectsCommand,
  HeadBucketCommand,
  HeadObjectCommand,
  ListBucketsCommand,
  type S3Client,
} from "@aws-sdk/client-s3";
import { assertFailure } from "./shared-corpus-assert.js";
import type { CorpusCase } from "./shared-corpus-types.js";

export async function runBucketLifecycle(client: S3Client, testCase: CorpusCase): Promise<void> {
  const bucket = { Bucket: testCase.bucket };
  const listed = await client.send(new ListBucketsCommand({}));
  assert.ok(listed.Buckets?.some((entry) => entry.Name === testCase.bucket));
  await client.send(new HeadBucketCommand(bucket));
  if (testCase.expect.status >= 400) {
    await assertFailure(() => client.send(new DeleteBucketCommand(bucket)), testCase.expect, "DeleteBucket");
    return;
  }
  const deleted = await client.send(new DeleteBucketCommand(bucket));
  assert.equal(deleted.$metadata.httpStatusCode, testCase.expect.status);
  await assertFailure(() => client.send(new HeadBucketCommand(bucket)), { status: 404 }, "HeadBucket");
  await client.send(new CreateBucketCommand(bucket));
  await client.send(new HeadBucketCommand(bucket));
}

export async function runBatchDelete(client: S3Client, testCase: CorpusCase): Promise<void> {
  const output = await client.send(new DeleteObjectsCommand({
    Bucket: testCase.bucket,
    Delete: { Objects: (testCase.deleteKeys ?? []).map((Key) => ({ Key })), Quiet: testCase.quiet },
  }));
  assert.equal(output.$metadata.httpStatusCode, testCase.expect.status);
  assert.deepEqual(output.Errors ?? [], []);
  assert.deepEqual((output.Deleted ?? []).map((entry) => entry.Key).sort(), testCase.expect.deleted ?? []);
  for (const Key of testCase.deleteKeys ?? []) {
    await assertFailure(() => client.send(new HeadObjectCommand({ Bucket: testCase.bucket, Key })), { status: 404 }, "HeadObject");
  }
}
