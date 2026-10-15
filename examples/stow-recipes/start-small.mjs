// Store an artifact in a disposable S3 bucket and read it back.
//
//   make build
//   STOW_BIN=./bin/stow-s3 node examples/stow-recipes/start-small.mjs
//
// The application code takes an S3 client and a bucket and does not know Stow
// exists. Only the last block is Stow-specific: it runs that code against a
// private session and cleans it up.
import { GetObjectCommand, PutObjectCommand } from "@aws-sdk/client-s3";
import { withStow } from "@chester-hill-solutions/stow-s3";

// Ordinary application code: any S3 client and bucket will do.
async function publishReport(s3, bucket, report) {
  await s3.send(new PutObjectCommand({
    Bucket: bucket,
    Key: "reports/latest.json",
    Body: JSON.stringify(report),
    ContentType: "application/json",
  }));
  const stored = await s3.send(new GetObjectCommand({
    Bucket: bucket,
    Key: "reports/latest.json",
  }));
  return JSON.parse(await stored.Body.transformToString());
}

const report = { service: "checkout", ok: true, durationMs: 128 };

// Stow: a private bucket, generated credentials, cleanup on exit.
await withStow(async ({ s3, bucket }) => {
  const roundTripped = await publishReport(s3, bucket, report);
  console.log("stored reports/latest.json:", JSON.stringify(roundTripped));
});
