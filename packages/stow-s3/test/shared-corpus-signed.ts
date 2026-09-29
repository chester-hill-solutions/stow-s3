import assert from "node:assert/strict";
import { createHash, createHmac, type Hash, type Hmac } from "node:crypto";
import { request } from "node:http";
import { SignatureV4 } from "@smithy/signature-v4";
import type { SourceData } from "@smithy/types";
import { GetObjectCommand, type S3Client } from "@aws-sdk/client-s3";
import type { CorpusCase } from "./shared-corpus-types.js";

function bytes(input: SourceData): string | Buffer {
  if (typeof input === "string") return input;
  return ArrayBuffer.isView(input) ? Buffer.from(input.buffer, input.byteOffset, input.byteLength) : Buffer.from(input);
}
class SHA256 {
  private readonly hash: Hash | Hmac;
  constructor(secret?: SourceData) { this.hash = secret === undefined ? createHash("sha256") : createHmac("sha256", bytes(secret)); }
  update(input: SourceData): void { this.hash.update(bytes(input)); }
  async digest(): Promise<Uint8Array> { return this.hash.digest(); }
}

export async function runSignedRequest(client: S3Client, testCase: CorpusCase): Promise<void> {
  const endpoint = await client.config.endpoint?.();
  assert.ok(endpoint);
  const hostname = testCase.virtualHost ? `${testCase.bucket}.${endpoint.hostname}` : endpoint.hostname;
  const host = `${hostname}${endpoint.port ? `:${endpoint.port}` : ""}`;
  const path = testCase.virtualHost ? `/${testCase.key}` : `/${testCase.bucket}/${testCase.key}`;
  const signer = new SignatureV4({ credentials: client.config.credentials, region: client.config.region, service: "s3", sha256: SHA256, uriEscapePath: false });
  const signed = await signer.presign({ protocol: endpoint.protocol, hostname, port: endpoint.port, method: testCase.method ?? "GET", path, headers: { host, "x-amz-content-sha256": "UNSIGNED-PAYLOAD" }, query: {} }, { expiresIn: 300, signingDate: new Date(Date.now() + (testCase.signingOffsetSeconds ?? 0) * 1000) });
  const query = signedQuery(signed.query);
  const response = await new Promise<{ status: number | undefined; body: string }>((resolve, reject) => {
    const outgoing = request({ hostname: endpoint.hostname, port: endpoint.port, method: signed.method, path: `${signed.path}?${query}`, headers: signed.headers }, (incoming) => {
      const chunks: Buffer[] = [];
      incoming.on("data", (chunk: Buffer) => chunks.push(chunk));
      incoming.on("end", () => resolve({ status: incoming.statusCode, body: Buffer.concat(chunks).toString() }));
      incoming.on("error", reject);
    });
    outgoing.on("error", reject);
    outgoing.end(testCase.body);
  });
  assert.equal(response.status, testCase.expect.status, response.body);
  if (testCase.expect.errorCode) assert.ok(response.body.includes(`<Code>${testCase.expect.errorCode}</Code>`), response.body);
  if (testCase.method === "GET" && response.status === 200) assert.equal(response.body, testCase.expect.body);
  if (testCase.method === "PUT" && response.status === 200) {
    const object = await client.send(new GetObjectCommand({ Bucket: testCase.bucket, Key: testCase.key }));
    assert.equal(await object.Body?.transformToString(), testCase.expect.body);
  }
}

function signedQuery(values: Record<string, string | string[] | null> | undefined): URLSearchParams {
 const query = new URLSearchParams();
 for (const [key,value] of Object.entries(values ?? {})) {
 for (const item of Array.isArray(value) ? value : [value ?? ""]) query.append(key,item);
 }
 return query;
}
