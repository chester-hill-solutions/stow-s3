import assert from "node:assert/strict";
import {
  ListObjectsCommand,
  ListObjectsV2Command,
  type EncodingType,
  type S3Client,
} from "@aws-sdk/client-s3";
import { assertFailure } from "./shared-corpus-assert.js";
import type { CorpusCase, CorpusPage } from "./shared-corpus-types.js";

type CorpusListPage = {
  contents: string[];
  commonPrefixes: string[];
  isTruncated: boolean;
  nextCursor: string | undefined;
  keyCount: number | undefined;
};

function listPagesFor(testCase: CorpusCase): CorpusPage[] {
  return testCase.expect.pages ?? [{
    contents: testCase.expect.contents,
    commonPrefixes: testCase.expect.commonPrefixes,
    keyCount: testCase.expect.keyCount ?? 0,
    isTruncated: testCase.expect.isTruncated ?? false,
  }];
}

function listRows(output: { Contents?: { Key?: string }[]; CommonPrefixes?: { Prefix?: string }[] }): { contents: string[]; commonPrefixes: string[] } {
  return {
    contents: (output.Contents ?? []).flatMap((object) => object.Key ?? []),
    commonPrefixes: (output.CommonPrefixes ?? []).flatMap((entry) => entry.Prefix ?? []),
  };
}

/** The listing parameters both versions share; the cursor is added per version. */
function listInput(testCase: CorpusCase): { Bucket: string; Prefix?: string; Delimiter?: string; MaxKeys?: number; EncodingType?: EncodingType } {
  return {
    Bucket: testCase.bucket,
    Prefix: testCase.prefix,
    Delimiter: testCase.delimiter,
    MaxKeys: testCase.maxKeys,
    EncodingType: testCase.encodingType as EncodingType | undefined,
  };
}

function toListPage(
  output: { Contents?: { Key?: string }[]; CommonPrefixes?: { Prefix?: string }[]; IsTruncated?: boolean },
  nextCursor: string | undefined,
  keyCount: number | undefined,
): CorpusListPage {
  return {
    ...listRows(output),
    isTruncated: output.IsTruncated ?? false,
    nextCursor,
    keyCount,
  };
}

/**
 * Drives either listing version through its expected pages. The versions differ
 * only in their cursor: V2 pages with an opaque continuation token, V1 with a
 * marker that names a row of the listing. assertCursor is where a version whose
 * corpus cases state the cursor asserts it rather than merely following it,
 * because a page reporting the wrong cursor sends the next request back into a
 * row this page already returned.
 */
async function runListPaging(
  testCase: CorpusCase,
  initialCursor: string | undefined,
  send: (cursor: string | undefined) => Promise<CorpusListPage>,
  assertCursor?: (page: CorpusListPage, expected: CorpusPage, index: number) => void,
): Promise<void> {
  const pages = listPagesFor(testCase);
  let cursor = initialCursor;
  for (let index = 0; index < pages.length; index += 1) {
    const expected = pages[index];
    assert.ok(expected, `missing expected list page ${index + 1}`);
    const page = await send(cursor);
    assert.deepEqual(page.contents, expected.contents ?? [], `page ${index + 1} contents`);
    assert.deepEqual(page.commonPrefixes, expected.commonPrefixes ?? [], `page ${index + 1} common prefixes`);
    assert.equal(page.isTruncated, expected.isTruncated, `page ${index + 1} truncation`);
    if (page.keyCount !== undefined) {
      assert.equal(page.keyCount, expected.keyCount, `page ${index + 1} key count`);
    }
    assertCursor?.(page, expected, index);
    if (index < pages.length - 1) {
      assert.ok(page.nextCursor, `page ${index + 1} needs a cursor for the next page`);
    }
    cursor = page.nextCursor;
  }
}

export async function runListObjects(client: S3Client, testCase: CorpusCase): Promise<void> {
  if (testCase.expect.status >= 400) {
    await assertFailure(() => client.send(new ListObjectsV2Command({ Bucket: testCase.bucket, ContinuationToken: testCase.continuationToken })), testCase.expect, testCase.id);
    return;
  }
  await runListPaging(testCase, testCase.continuationToken, async (cursor) => {
    const output = await client.send(new ListObjectsV2Command({ ...listInput(testCase), ContinuationToken: cursor }));
    assert.equal(output.$metadata.httpStatusCode, 200);
    return toListPage(output, output.NextContinuationToken, output.KeyCount);
  });
}

export async function runListObjectsV1(client: S3Client, testCase: CorpusCase): Promise<void> {
  if (testCase.expect.status >= 400) {
    await assertFailure(() => client.send(new ListObjectsCommand({ Bucket: testCase.bucket, Marker: testCase.marker })), testCase.expect, testCase.id);
    return;
  }
  await runListPaging(testCase, testCase.marker, async (cursor) => {
    const output = await client.send(new ListObjectsCommand({ ...listInput(testCase), Marker: cursor }));
    assert.equal(output.$metadata.httpStatusCode, 200);
    // No keyCount: ListObjects (V1) does not return one, and leaving it undefined
    // is what tells the driver not to assert it.
    return toListPage(output, output.NextMarker, undefined);
  }, (page, expected, index) => {
    assert.equal(page.nextCursor ?? "", expected.nextMarker ?? "", `page ${index + 1} next marker`);
  });
}
