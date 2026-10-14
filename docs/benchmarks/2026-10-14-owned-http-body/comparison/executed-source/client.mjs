import { createRequire } from "node:module";
import { Agent } from "node:http";
import { createHash, randomBytes } from "node:crypto";

const require = createRequire(new URL("../packages/stow-s3/package.json", import.meta.url));
const { S3Client, ListBucketsCommand } = require("@aws-sdk/client-s3");
const { CreateBucketCommand, PutObjectCommand, GetObjectCommand, DeleteObjectCommand, DeleteBucketCommand } = require("@aws-sdk/client-s3");
const { NodeHttpHandler } = require("@smithy/node-http-handler");

export async function authenticatedReady(client) {
  await client.send(new ListBucketsCommand({}), { abortSignal: AbortSignal.timeout(1000) });
}

export async function firstWriteProbe(client, body) {
  const Bucket = "agent-" + randomBytes(6).toString("hex"), Key = "first";
  const options = () => ({ abortSignal: AbortSignal.timeout(30000) });
  await client.send(new CreateBucketCommand({ Bucket }), options());
  await client.send(new PutObjectCommand({ Bucket, Key, Body: body }), options());
  const writtenAt = performance.now();
  const result = await client.send(new GetObjectCommand({ Bucket, Key }), options());
  const received = await result.Body.transformToByteArray();
  const digest = bytes => createHash("sha256").update(bytes).digest("hex");
  if (received.length !== body.length || digest(received) !== digest(body)) throw new Error("first write failed content verification");
  await client.send(new DeleteObjectCommand({ Bucket, Key }), options());
  await client.send(new DeleteBucketCommand({ Bucket }), options());
  return { writtenAt, verified: true, bytes: body.length };
}

export function benchmarkClient(endpoint, ready, concurrency, timeoutMs) {
  const agent = new Agent({ keepAlive: true, maxSockets: concurrency, maxFreeSockets: concurrency });
  const client = new S3Client({ endpoint, region: ready.region, credentials: ready.credentials, forcePathStyle: true,
    maxAttempts: 1, requestHandler: new NodeHttpHandler({ httpAgent: agent, connectionTimeout: timeoutMs, requestTimeout: timeoutMs }) });
  return { client, close() { client.destroy(); agent.destroy(); } };
}
