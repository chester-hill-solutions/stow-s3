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
  // The refusal has to be the precondition failure and not some other error:
  // a request that failed for another reason has proved nothing about the
  // write it was supposed to make.
  let rejection;
  try {
    await s3.send(new PutObjectCommand({
      Bucket: bucket,
      Key: "config.json",
      Body: '{"version":3}',
      IfMatch: created.ETag,
    }));
  } catch (error) {
    rejection = error;
  }
  if (!rejection) throw new Error("the stale update was accepted");
  if (rejection.name !== "PreconditionFailed" && rejection.$metadata?.httpStatusCode !== 412) {
    throw rejection;
  }
  console.log("stale update rejected:", rejection.name, rejection.$metadata?.httpStatusCode);

  // The version that was saved is still the one that is stored. A refusal is
  // only worth anything if the object it refused to replace is intact.
  const stored = await s3.send(new GetObjectCommand({ Bucket: bucket, Key: "config.json" }));
  const retained = await stored.Body.transformToString();
  if (retained !== '{"version":2}') {
    throw new Error(`the stored object is ${retained}, want the version that was saved`);
  }
  console.log("retained:", retained);
});
