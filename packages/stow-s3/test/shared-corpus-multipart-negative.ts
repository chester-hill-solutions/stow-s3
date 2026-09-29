import assert from "node:assert/strict";
import { AbortMultipartUploadCommand, CompleteMultipartUploadCommand, CreateMultipartUploadCommand, UploadPartCommand, type CompletedPart, type S3Client } from "@aws-sdk/client-s3";
import { assertFailure } from "./shared-corpus-assert.js";
import type { CorpusCase } from "./shared-corpus-types.js";

export async function runMultipartFailure(client: S3Client, testCase: CorpusCase): Promise<void> {
  const ref = { Bucket: testCase.bucket, Key: testCase.key };
  const created = await client.send(new CreateMultipartUploadCommand(ref));
  assert.ok(created.UploadId);
  const upload = { ...ref, UploadId: created.UploadId };
  try {
    const parts: CompletedPart[] = [];
    for (const part of testCase.parts ?? []) {
      const output = await client.send(new UploadPartCommand({ ...upload, PartNumber: part.number, Body: Buffer.from(part.body.repeat(part.repeat ?? 1)) }));
      parts.push({ PartNumber: part.number, ETag: output.ETag });
    }
    const command = new CompleteMultipartUploadCommand({ ...upload, MultipartUpload: { Parts: invalidCompletion(parts, testCase.multipartFailure) } });
    if (testCase.multipartFailure === "aborted") await client.send(new AbortMultipartUploadCommand(upload));
    if (testCase.multipartFailure === "repeated") await client.send(command);
    await assertFailure(() => client.send(command), testCase.expect, testCase.id);
  } finally {
    await client.send(new AbortMultipartUploadCommand(upload)).catch(() => undefined);
  }
}

function invalidCompletion(parts: CompletedPart[], failure: string | undefined): CompletedPart[] {
  const first = parts[0];
  assert.ok(first);
  switch (failure) {
    case "empty": return [];
    case "duplicate": return [...parts, first];
    case "unsorted": {
      const second = parts[1];
      assert.ok(second);
      return [second, first];
    }
    case "wrong-etag": return [{ ...first, ETag: '"wrong-etag"' }];
    default: return parts;
  }
}
