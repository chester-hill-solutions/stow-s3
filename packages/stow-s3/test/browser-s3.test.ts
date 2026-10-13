import assert from "node:assert/strict";
import { Readable } from "node:stream";
import { test } from "node:test";
import { S3Client } from "@aws-sdk/client-s3";
import type { HttpRequest } from "@smithy/types";
import { S3ObjectSyncRemote } from "../dist/browser-s3.js";

function transportFixture(status = 200, responseHeaders: Record<string, string> = {}) {
  const requests: { method: string; headers: Record<string, string> }[] = [];
  const client = new S3Client({
    endpoint: "https://s3.example.invalid", region: "us-east-1", forcePathStyle: true, maxAttempts: 1,
    credentials: { accessKeyId: "fixture", secretAccessKey: "fixture" },
    requestHandler: {
      async handle(request: HttpRequest) {
        requests.push({ method: request.method, headers: request.headers });
        const body = status === 200 && request.method === "GET" ? "remote bytes"
          : status === 200 ? "" : `<Error><Code>${status === 404 ? "NoSuchKey" : "PreconditionFailed"}</Code></Error>`;
        return { response: { statusCode: status, headers: { etag: '"remote-etag"', "content-type": "text/plain", "x-amz-meta-stow-save-id": "receipt", "x-amz-meta-purpose": "test", ...responseHeaders }, body: Readable.from([body]) } };
      },
    },
  });
  return { client, remote: new S3ObjectSyncRemote(client), requests };
}

test("S3 transport serializes signed conditional writes and receipt metadata", async (t) => {
  const fixture = transportFixture(); t.after(() => fixture.client.destroy());
  for (const basis of [{ absent: true } as const, { etag: '"previous"' }]) {
    await fixture.remote.putObject("files", "draft.txt", new TextEncoder().encode("edit"), { basis, saveId: "receipt" });
  }
  assert.equal(fixture.requests[0]?.headers["if-none-match"], "*");
  assert.equal(fixture.requests[1]?.headers["if-match"], '"previous"');
  assert.equal(fixture.requests[0]?.headers["x-amz-meta-stow-save-id"], "receipt");
  assert.match(fixture.requests[0]?.headers.authorization ?? "", /AWS4-HMAC-SHA256/);
});

test("S3 reads preserve bytes and ETag, separating receipt from application metadata", async (t) => {
  const fixture = transportFixture(); t.after(() => fixture.client.destroy());
  const object = await fixture.remote.getObject("files", "draft.txt");
  assert.equal(new TextDecoder().decode(object?.data), "remote bytes");
  assert.equal(object?.etag, '"remote-etag"');
  assert.equal(object?.saveId, "receipt");
  assert.deepEqual(object?.metadata, { purpose: "test" });
});

for (const status of [403, 404, 409, 412]) {
  test(`S3 HTTP ${status} retains its appropriate failure meaning`, async (t) => {
    const fixture = transportFixture(status); t.after(() => fixture.client.destroy());
    if (status === 404) assert.equal(await fixture.remote.getObject("files", "missing"), null);
    else await assert.rejects(fixture.remote.getObject("files", "draft.txt"));
    await assert.rejects(
      fixture.remote.putObject("files", "draft.txt", new Uint8Array(), { basis: { absent: true }, saveId: "receipt" }),
      (error: unknown) => status === 412
        ? typeof error === "object" && error !== null && "code" in error && error.code === "conflict"
        : typeof error === "object" && error !== null && !("code" in error && error.code === "conflict"),
    );
  });
}

test("application metadata cannot replace the S3 reconciliation receipt", async (t) => {
  const fixture = transportFixture(); t.after(() => fixture.client.destroy());
  await assert.rejects(fixture.remote.putObject("files", "draft.txt", new Uint8Array(), {
    basis: { absent: true }, saveId: "receipt", metadata: { "Stow-Save-Id": "spoof" },
  }), { code: "invalid_options" });
  assert.equal(fixture.requests.length, 0);
});

test("S3 chunk transport supplies and reads provider-verified full-object SHA-256", async (t) => {
  const digest = "a".repeat(64);
  const base64 = Buffer.from(digest, "hex").toString("base64");
  const fixture = transportFixture(200, { "content-length": "3", "x-amz-checksum-sha256": base64, "x-amz-checksum-type": "FULL_OBJECT" });
  t.after(() => fixture.client.destroy());
  await fixture.remote.putObject("files", "chunk", new Uint8Array([1, 2, 3]), { basis: { absent: true }, saveId: "chunk", sha256: digest });
  assert.equal(fixture.requests[0]?.headers["x-amz-checksum-sha256"], base64);
  const head = await fixture.remote.headObject("files", "chunk");
  assert.equal(head?.sha256, digest);
  assert.equal(head?.size, 3);
  assert.equal(fixture.requests[1]?.headers["x-amz-checksum-mode"], "ENABLED");
});

for (const checksumType of ["COMPOSITE", undefined]) {
  test(`S3 ${checksumType ?? "unspecified"} checksums and copied metadata do not establish a chunk digest`, async (t) => {
    const fixture = transportFixture(200, {
      "content-length": "3", "x-amz-checksum-sha256": Buffer.alloc(32).toString("base64"),
      ...(checksumType ? { "x-amz-checksum-type": checksumType } : {}),
      "x-amz-meta-stow-content-sha256": "0".repeat(64),
    });
    t.after(() => fixture.client.destroy());
    assert.equal((await fixture.remote.headObject("files", "chunk"))?.sha256, undefined);
  });
}
