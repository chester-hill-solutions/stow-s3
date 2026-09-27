import assert from "node:assert/strict";
import { GetObjectCommand, HeadObjectCommand, type GetObjectCommandOutput, type S3Client } from "@aws-sdk/client-s3";
import { assertFailure, assertMetadata } from "./shared-corpus-assert.js";
import type { CorpusCase } from "./shared-corpus-types.js";

// A range read is checked on three independent surfaces: the status, the bytes,
// and the whole Content-Range value. Status and body together are what a client
// actually reads, and they are not sufficient on their own — a server can return
// the right bytes for a range it mis-parsed, and it can return a correct
// Content-Range beside a body assembled from the wrong offset.
//
// An unsatisfiable request is a different shape. @aws-sdk/client-s3 surfaces 416
// as a thrown error rather than a GetObjectOutput, so it goes through the shared
// failure assertion. The size in the refusal is then checked separately, because
// it is the only thing a client can use to work out what to ask for instead; a
// wrong total there sends the caller into a loop of unsatisfiable requests and
// nothing else in the corpus would notice.
export async function runRangeGet(client: S3Client, testCase: CorpusCase): Promise<void> {
  const action = (): Promise<GetObjectCommandOutput> => client.send(new GetObjectCommand({
    Bucket: testCase.bucket,
    Key: testCase.key,
    Range: testCase.range,
  }));

  if (testCase.expect.status === 416) {
    await assertFailure(action, testCase.expect, `range ${testCase.range}`);
    await assertRefusalTotal(client, testCase);
    return;
  }

  const output = await action();
  assert.equal(output.$metadata.httpStatusCode, testCase.expect.status);
  const body = await output.Body?.transformToString() ?? "";
  assert.equal(body, testCase.expect.body ?? "");
  assertMetadata(output.Metadata, testCase.expect.metadata, "GetObject");
  assert.equal(output.ContentRange, testCase.expect.contentRange, "Content-Range");
}

// assertRefusalTotal confirms the size in a 416 is the real object length, read
// back with a HeadObject rather than taken from the range parser that produced
// the refusal.
async function assertRefusalTotal(client: S3Client, testCase: CorpusCase): Promise<void> {
  const head = await client.send(new HeadObjectCommand({
    Bucket: testCase.bucket,
    Key: testCase.key,
  }));
  const want = contentRangeTotal(testCase.expect.contentRange);
  assert.equal(head.ContentLength, want, "a 416 must report the real object size");
}

// contentRangeTotal pulls the object size out of a Content-Range value, accepting
// both the satisfied form (bytes 0-4/10) and the refusal form
// (bytes-star-slash-10).
function contentRangeTotal(contentRange: string | undefined): number | undefined {
  if (!contentRange) return undefined;
  const slash = contentRange.lastIndexOf("/");
  if (slash < 0) return undefined;
  const total = Number.parseInt(contentRange.slice(slash + 1), 10);
  return Number.isNaN(total) ? undefined : total;
}
