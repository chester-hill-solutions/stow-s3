// A classify stage: workers read input objects and write category results.
//
//   make build
//   STOW_BIN=./bin/stow-s3 node examples/stow-recipes/processing-stage.mjs ./pipeline
//
// Inputs and results are retained, so the stage can be rerun after adding
// documents. Stow stores the bytes; the application owns the queue, the
// scheduling and the retries. This is not a durable job queue and does not
// promise exactly-once processing: if a worker is interrupted, rerun the key.
import { GetObjectCommand, ListObjectsV2Command, PutObjectCommand } from "@aws-sdk/client-s3";
import { openStow } from "@chester-hill-solutions/stow-s3";

const dataDir = process.argv[2] ?? "./pipeline";
const bucket = "pipeline";
const inputs = {
  "invoices/march.txt": "Invoice 1001 for March services.",
  "invoices/april.txt": "Invoice 1002 for April services.",
  "support/tickets.txt": "Support ticket: cannot sign in.",
};

// Stands in for a real classifier.
function categorize(text) {
  return /\binvoice\b/i.test(text) ? "invoice" : "support";
}

// #region excerpt
// One unit of work: read an input key, write its result under results/.
async function classifyOne(s3, key) {
  const source = await s3.send(new GetObjectCommand({ Bucket: bucket, Key: `inputs/${key}` }));
  const category = categorize(await source.Body.transformToString());
  await s3.send(new PutObjectCommand({
    Bucket: bucket,
    Key: `results/${key}.json`,
    Body: JSON.stringify({ input: key, category }),
    ContentType: "application/json",
  }));
  return `${key} -> ${category}`;
}

// The application builds the queue and runs two workers over it. Stow does not
// schedule this or retry it; if a worker fails, the application retries the key.
async function runWorkers(s3, keys) {
  const queue = [...keys];
  const worker = async () => {
    for (let key = queue.shift(); key !== undefined; key = queue.shift()) {
      console.log(await classifyOne(s3, key));
    }
  };
  await Promise.all([worker(), worker()]);
}
// #endregion excerpt

const session = await openStow({ dataDir, bucket });
try {
  const s3 = session.s3;
  // Seed the inputs. Rerunning overwrites the same keys, so it is idempotent.
  for (const [key, text] of Object.entries(inputs)) {
    await s3.send(new PutObjectCommand({ Bucket: bucket, Key: `inputs/${key}`, Body: text, ContentType: "text/plain" }));
  }
  await runWorkers(s3, Object.keys(inputs));

  // Inputs and results are retained, so the stage can be inspected or rerun.
  const results = await s3.send(new ListObjectsV2Command({ Bucket: bucket, Prefix: "results/" }));
  console.log("retained results:", (results.Contents ?? []).map((object) => object.Key).sort());
} finally {
  await session.close();
}
