import { execFileSync, spawn } from "node:child_process";
import { mkdtemp, writeFile, rm, readFile, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { PutBucketCorsCommand, DeleteBucketCorsCommand } from "@aws-sdk/client-s3";
import { openBrowserTestRemote, inspectCors } from "./browser-remote.mjs";
import { waitForBrowserCors } from "./browser-cors-probe.mjs";

const packageRoot = fileURLToPath(new URL("../", import.meta.url));
const ids = {
  project: "8d90188e-0f1e-4c47-9379-9399ae06e6fa",
  environment: "00b05557-0e3a-4876-8502-9257a9eff51d",
  bucket: "8264f13e-ab49-450b-b919-735b254611c7",
};
for (let index = 2; index < process.argv.length; index += 2) {
  const name = process.argv[index]?.replace(/^--/, "");
  if (!(name in ids) || !/^[0-9a-f-]{36}$/i.test(process.argv[index + 1] ?? "")) throw new Error("Use --project, --environment and --bucket with Railway IDs");
  ids[name] = process.argv[index + 1];
}

let config;
if (process.env.STOW_BROWSER_REMOTE_CONFIG) {
  try {
    const raw = JSON.parse(await readFile(process.env.STOW_BROWSER_REMOTE_CONFIG, "utf8"));
    config = raw.data?.bucketS3Credentials?.[0] ?? raw;
  } catch { throw new Error("Cannot read the private live browser configuration"); }
} else {
  const query = `query { bucketS3Credentials(projectId: "${ids.project}", environmentId: "${ids.environment}", bucketId: "${ids.bucket}") { endpoint bucketName region urlStyle accessKeyId secretAccessKey } }`;
  try {
    const raw = JSON.parse(execFileSync("railway", ["api", query], {
      encoding: "utf8", stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, RAILWAY_CALLER: "skill:use-railway@1.6.1", RAILWAY_AGENT_SESSION: process.env.RAILWAY_AGENT_SESSION ?? `stow-browser-${crypto.randomUUID()}` },
    }));
    config = raw.data.bucketS3Credentials[0];
  } catch { throw new Error("Unable to resolve the Railway testing bucket credentials"); }
}
const directory = await mkdtemp(join(tmpdir(), "stow-browser-railway-"));
const path = join(directory, "credentials.json");
const runId = crypto.randomUUID();
config.testPrefix = `stow-browser-tests/${runId}/`;
process.env.STOW_BROWSER_REMOTE_CONFIG = path;
process.env.STOW_BROWSER_ALLOW_LIVE = "1";
let fixture;
const corsId = `stow-browser-${runId}`;
let corsAttempted = false;
let exitCode = 1;
let operationError;
const cleanupErrors = [];
const selection = process.env.STOW_BROWSER_TEST_GREP;
const receipt = { provider: "railway", projectId: ids.project, bucketId: ids.bucket, bucket: config.bucketName, runId, prefix: config.testPrefix, browser: "chromium", testSelection: selection ?? "all", status: "failed", cleaned: false, corsRestored: false, credentialsRemoved: false };

try {
  await writeFile(path, JSON.stringify(config), { mode: 0o600 });
  fixture = await openBrowserTestRemote();
  const previous = await inspectCors(fixture.client, fixture.bucket);
  corsAttempted = true;
  await fixture.client.send(new PutBucketCorsCommand({ Bucket: fixture.bucket, CORSConfiguration: { CORSRules: [...previous, {
    ID: corsId, AllowedOrigins: ["http://127.0.0.1:4180"], AllowedMethods: ["GET", "PUT", "HEAD"],
    AllowedHeaders: ["content-type", "if-match", "if-none-match", "x-amz-meta-stow-save-id", "x-amz-meta-purpose"],
    ExposeHeaders: ["ETag", "x-amz-meta-stow-save-id", "x-amz-meta-purpose"], MaxAgeSeconds: 60,
  }] } }));
  process.stdout.write(`Running Chromium against Railway bucket ${fixture.bucket}; disposable prefix ${config.testPrefix}\n`);
  await waitForBrowserCors(fixture);
  exitCode = await new Promise((resolve, reject) => {
    const child = spawn(process.execPath, ["node_modules/@playwright/test/cli.js", "test", "--config", "scripts/browser-playwright.config.mjs", ...(selection ? ["--grep", selection] : [])], {
      cwd: packageRoot, env: process.env, stdio: "inherit",
    });
    child.once("error", reject); child.once("exit", (code) => resolve(code ?? 1));
  });
  receipt.status = exitCode === 0 ? selection ? "partial-pass" : "passed" : "failed";
} catch (error) {
  operationError = error;
} finally {
  if (fixture) {
    try { await restoreCors(fixture, corsId, corsAttempted); receipt.corsRestored = true; }
    catch (error) { cleanupErrors.push(error); }
    try { await fixture.close(); receipt.cleaned = true; }
    catch (error) { cleanupErrors.push(error); }
  }
  try { await rm(directory, { recursive: true, force: true }); receipt.credentialsRemoved = true; }
  catch (error) { cleanupErrors.push(error); }
  const output = join(packageRoot, "../../.cache/browser-live-receipts");
  await mkdir(output, { recursive: true });
  await writeFile(join(output, `${runId}.json`), JSON.stringify(receipt, null, 2));
  await writeFile(join(output, "latest.json"), JSON.stringify(receipt, null, 2));
  process.stdout.write(`Railway qualification: ${receipt.status}; objects cleaned: ${receipt.cleaned}; CORS restored: ${receipt.corsRestored}\n`);
}
for (const error of [operationError, ...cleanupErrors].filter(Boolean)) {
  let message = String(error.message ?? error);
  for (const key of ["accessKeyId", "secretAccessKey", "sessionToken"]) if (config[key]) message = message.replaceAll(config[key], "[redacted]");
  process.stderr.write(`${error.name ?? "Error"}: ${message.replace(/https?:\/\/\S+/g, "[URL]")}\n`);
}
process.exitCode = operationError || cleanupErrors.length ? 1 : exitCode;

async function restoreCors(fixture, corsId, attempted) {
  if (!attempted) return;
  const current = await inspectCors(fixture.client, fixture.bucket);
  const retained = current.filter((rule) => rule.ID !== corsId);
  if (retained.length !== current.length) {
    await fixture.client.send(retained.length
      ? new PutBucketCorsCommand({ Bucket: fixture.bucket, CORSConfiguration: { CORSRules: retained } })
      : new DeleteBucketCorsCommand({ Bucket: fixture.bucket }));
  }
  const restored = await inspectCors(fixture.client, fixture.bucket);
  if (!restored.some((rule) => rule.ID === corsId)) return;
  const deadline = Date.now() + 90_000;
  while (Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 250));
    const observed = await inspectCors(fixture.client, fixture.bucket);
    if (!observed.some((rule) => rule.ID === corsId)) return;
  }
  throw new Error("Temporary browser CORS rule remains installed");
}
