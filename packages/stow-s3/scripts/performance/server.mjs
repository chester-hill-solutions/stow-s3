import { spawn } from "node:child_process";
import { Agent } from "node:http";
import { mkdir } from "node:fs/promises";
import { S3Client } from "@aws-sdk/client-s3";
import { NodeHttpHandler } from "@smithy/node-http-handler";
import { waitForReady } from "../../dist/ready-reader.js";
import { stopChild } from "../../dist/start.js";

export async function startServer(binary, directory, scenario, config) {
  await mkdir(directory, { recursive: true });
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !/^(STOW_|S3_|AWS_)/.test(key)));
  const child = spawn(binary, ["serve", "--host", "127.0.0.1", "--port", "0", "--mode", "local",
    "--backend", scenario.backend, "--data-dir", directory, "--ready-fd", "3", "--parent-pid", String(process.pid),
    "--max-request-bytes", String(Math.max(scenario.bytes, 1048576)),
    "--max-concurrent-requests", String(config.serverConcurrency),
    "--max-bytes", String(config.maxBytes), "--max-objects", String(config.maxObjects)],
  { env, stdio: ["ignore", "pipe", "pipe", "pipe"] });
  try {
    const { message: ready } = await waitForReady(child, 10000);
    if (ready.mode !== "local" || ready.backend !== scenario.backend) throw new Error("unexpected server profile");
    child.stdout.resume(); child.stderr.resume();
    const agent = new Agent({ keepAlive: true, maxSockets: scenario.concurrency, maxFreeSockets: scenario.concurrency });
    const client = new S3Client({ endpoint: ready.endpoint, region: ready.region, forcePathStyle: true,
      credentials: { accessKeyId: ready.accessKeyId, secretAccessKey: ready.secretAccessKey },
      maxAttempts: 1, requestHandler: new NodeHttpHandler({ httpAgent: agent, connectionTimeout: config.timeoutMs, requestTimeout: config.timeoutMs }) });
    return { child, client, capabilities: ready.capabilities, binaryVersion: ready.binaryVersion,
      async close(signal = "SIGTERM") {
        client.destroy(); agent.destroy();
        if (signal === "SIGKILL") child.kill(signal);
        await stopChild(child);
      } };
  } catch (error) { await stopChild(child); throw error; }
}
