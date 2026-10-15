// Reject a stale update: overwrite only the version you actually read.
//
//   make build
//   STOW_BIN=./bin/stow-s3 node examples/stow-recipes/conditional-write.mjs
//
// A conditional write compares the object's current ETag with the one you pass
// and refuses the write if they differ. Two writers can read the same version;
// the second update is rejected instead of silently overwriting the first.
import { GetObjectCommand, PutObjectCommand } from "@aws-sdk/client-s3";
import { withStow } from "@chester-hill-solutions/stow-s3";

await withStow(async ({ s3, bucket }) => {
  const created = await s3.send(new PutObjectCommand({
    Bucket: bucket,
    Key: "config.json",
    Body: '{"version":1}',
  }));

  // The first writer updates the version it read.
  await s3.send(new PutObjectCommand({
    Bucket: bucket,
    Key: "config.json",
    Body: '{"version":2}',
    IfMatch: created.ETag,
  }));

  // The second writer still holds the original ETag, so its update is refused.
  let rejected = false;
  try {
    await s3.send(new PutObjectCommand({
      Bucket: bucket,
      Key: "config.json",
      Body: '{"version":3}',
      IfMatch: created.ETag,
    }));
  } catch (error) {
    rejected = true;
    console.log("stale update rejected:", error.name, error.$metadata?.httpStatusCode);
  }
  if (!rejected) throw new Error("the stale update was accepted");

  const stored = await s3.send(new GetObjectCommand({ Bucket: bucket, Key: "config.json" }));
  console.log("retained:", await stored.Body.transformToString());
});
