import { readFile } from "node:fs/promises";
import { S3Client, ListObjectsV2Command, DeleteObjectsCommand, GetBucketCorsCommand } from "@aws-sdk/client-s3";
import { openStow } from "../dist/index.js";
import { S3ObjectSyncRemote } from "../dist/browser-s3.js";

export async function openBrowserTestRemote() {
  const path = process.env.STOW_BROWSER_REMOTE_CONFIG;
  if (!path) {
    const session = await openStow();
    return {
      remote: new S3ObjectSyncRemote(session.s3), client: session.s3, bucket: session.bucket,
      prefix: "", provider: "local", description: "private local S3 fixture",
      close: () => session.close(),
    };
  }
  if (process.env.STOW_BROWSER_ALLOW_LIVE !== "1") throw new Error("Live browser tests require STOW_BROWSER_ALLOW_LIVE=1");
  let raw;
  try { raw = JSON.parse(await readFile(path, "utf8")); }
  catch { throw new Error("Cannot read the private browser remote configuration"); }
  const config = raw.data?.bucketS3Credentials?.[0] ?? raw;
  for (const key of ["endpoint", "bucketName", "accessKeyId", "secretAccessKey"]) {
    if (typeof config[key] !== "string" || !config[key]) throw new Error(`Missing live browser configuration: ${key}`);
  }
  if (new URL(config.endpoint).protocol !== "https:") throw new Error("Live browser tests require an HTTPS endpoint");
  const clientOptions = {
    endpoint: config.endpoint, region: config.region ?? "auto", maxAttempts: 2,
    forcePathStyle: ["path", "path-style"].includes(config.urlStyle),
    credentials: { accessKeyId: config.accessKeyId, secretAccessKey: config.secretAccessKey, sessionToken: config.sessionToken },
    requestHandler: { requestTimeout: 30_000, connectionTimeout: 10_000 },
  };
  const client = new S3Client(clientOptions);
  const signingClient = new S3Client({ ...clientOptions, requestChecksumCalculation: "WHEN_REQUIRED" });
  const prefix = config.testPrefix ?? `stow-browser-tests/${crypto.randomUUID()}/`;
  if (!/^stow-browser-tests\/[0-9a-f-]{36}\/$/.test(prefix)) throw new Error("Live tests require a unique disposable test prefix");
  return {
    remote: new S3ObjectSyncRemote(client), client, signingClient, bucket: config.bucketName, prefix,
    provider: "railway", description: `Railway testing bucket (${config.bucketName})`,
    async close() {
      try { await removeTestPrefix(client, config.bucketName, prefix); }
      finally { client.destroy(); signingClient.destroy(); }
    },
  };
}

async function removeTestPrefix(client, bucket, prefix) {
  let continuation;
  do {
    const listed = await client.send(new ListObjectsV2Command({ Bucket: bucket, Prefix: prefix, ContinuationToken: continuation }));
    const objects = (listed.Contents ?? []).flatMap((object) => object.Key?.startsWith(prefix) ? [{ Key: object.Key }] : []);
    if (objects.length) {
      const removed = await client.send(new DeleteObjectsCommand({ Bucket: bucket, Delete: { Objects: objects } }));
      if (removed.Errors?.length) throw new Error("Could not clean up disposable browser-test objects");
    }
    continuation = listed.IsTruncated ? listed.NextContinuationToken : undefined;
  } while (continuation);
  const remaining = await client.send(new ListObjectsV2Command({ Bucket: bucket, Prefix: prefix, MaxKeys: 1 }));
  if (remaining.Contents?.length) throw new Error("Disposable browser-test prefix is not empty after cleanup");
}

export async function inspectCors(client, bucket) {
  try {
    const result = await client.send(new GetBucketCorsCommand({ Bucket: bucket }));
    return result.CORSRules ?? [];
  } catch (error) {
    if (error.$metadata?.httpStatusCode === 404) return [];
    throw error;
  }
}
