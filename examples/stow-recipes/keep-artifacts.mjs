// Keep the artifacts: the same round trip, but the objects outlive the session.
//
//   make build
//   STOW_BIN=./bin/stow-s3 node examples/stow-recipes/keep-artifacts.mjs ./artifacts
//
// The only change from start-small.mjs is naming a data directory and a bucket:
// withStow(...) becomes openStow({ dataDir, bucket }). A named directory selects
// the filesystem backend, so the bytes survive close, and a fixed bucket name
// lets the next session find them.
import { GetObjectCommand, PutObjectCommand } from "@aws-sdk/client-s3";
import { openStow } from "@chester-hill-solutions/stow-s3";

const dataDir = process.argv[2] ?? "./artifacts";
const bucket = "artifacts";

// First session: write the artifact, then stop the server.
const first = await openStow({ dataDir, bucket });
await first.s3.send(new PutObjectCommand({
  Bucket: bucket,
  Key: "notes/design.md",
  Body: "# Design\n\nFirst draft.\n",
  ContentType: "text/markdown",
}));
await first.close();
console.log(`wrote ${bucket}/notes/design.md; server stopped`);

// Second session on the same directory: the caller-owned data is still there.
const second = await openStow({ dataDir, bucket });
const stored = await second.s3.send(new GetObjectCommand({
  Bucket: bucket,
  Key: "notes/design.md",
}));
console.log("read back after reopen:", JSON.stringify(await stored.Body.transformToString()));
await second.close();
