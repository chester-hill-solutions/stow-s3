import { spawn } from "node:child_process";
import { mkdir, mkdtemp, writeFile, rm, statfs } from "node:fs/promises";
import { randomBytes } from "node:crypto";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { waitForReady } from "../packages/stow-s3/dist/ready-reader.js";
import { stopChild } from "../packages/stow-s3/dist/start.js";
import { authenticatedReady, benchmarkClient, firstWriteProbe } from "./client.mjs";

export const targets = ["stow-memory", "s3proxy-memory", "stow-filesystem", "s3proxy-filesystem", "minio-filesystem", "rustfs-filesystem", "stow-memory-before", "stow-filesystem-before", "stow-memory-profile", "stow-memory-baseline", "stow-memory-staging", "stow-memory-after"];

export function validateTarget(input) {
  if (!targets.includes(input.target)) throw new Error("unknown benchmark target");
  for (const key of ["maxBytes", "maxObjects", "serverConcurrency", "bytes"]) {
    if (!Number.isSafeInteger(input[key]) || input[key] <= 0) throw new Error(`invalid ${key}`);
  }
  if (input.maxBytes > 1024 ** 3 || input.maxObjects > 100000 || input.serverConcurrency > 1024 || input.bytes > 128 * 1024 ** 2) throw new Error("target limit exceeds benchmark envelope");
  return input;
}

export async function launchTarget(input, root, port = 8080) {
  validateTarget(input);
  await mkdir(root, { recursive: true });
  const disk = await statfs(root);
  if (disk.bavail * disk.bsize < 1024 ** 3) throw new Error("less than 1 GiB free benchmark disk");
  const directory = await mkdtemp(join(root, "case-"));
  const credentials = { accessKeyId: randomBytes(10).toString("hex"), secretAccessKey: randomBytes(20).toString("hex") };
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !/^(STOW_|S3_|AWS_|RUSTFS_|MINIO_|BENCH_CONTROL_TOKEN)/.test(key)));
  const firstBody = randomBytes(4096);
  let child;
  let message;
  const began = performance.now();
  try {
    if (input.target.startsWith("stow-")) {
      const before = input.target.endsWith("-before");
      const profiling = input.target === "stow-memory-profile";
      const variant = input.target.match(/^stow-memory-(baseline|staging|after)$/)?.[1];
      child = spawn(variant ? `bin/stow-s3-${variant}` : profiling ? "bin/stow-s3-profile" : before ? "bin/stow-s3-before" : "bin/stow-s3", ["serve", "--mode", "local", "--host", "[::]", "--port", String(port),
        "--backend", input.target.slice(5).replace(/-(before|profile|baseline|staging|after)$/, ""), "--data-dir", directory, "--ready-fd", "3", "--parent-pid", String(process.pid),
        "--max-bytes", String(input.maxBytes), "--max-objects", String(input.maxObjects),
        "--max-request-bytes", String(Math.max(input.bytes, 1048576)), "--max-concurrent-requests", String(input.serverConcurrency)],
      { env, stdio: ["ignore", "pipe", "pipe", "pipe"] });
      const ready = await waitForReady(child, 30000);
      message = ready.message;
    } else if (input.target.startsWith("s3proxy-")) {
      const configPath = join(directory, "s3proxy.properties");
      const provider = input.target.endsWith("memory") ? "transient" : "filesystem";
      await mkdir(join(directory, "objects"));
      await writeFile(configPath, ["s3proxy.endpoint=http://[::]:" + port, "s3proxy.authorization=aws-v4",
        "s3proxy.identity=" + credentials.accessKeyId, "s3proxy.credential=" + credentials.secretAccessKey,
        "jclouds.provider=" + provider, "jclouds.identity=local", "jclouds.credential=local",
        "jclouds.filesystem.basedir=" + join(directory, "objects"), "s3proxy.ignore-unknown-headers=true"].join("\n"), { mode: 0o600 });
      child = spawn("java", ["-Xms64m", "-Xmx1024m", "-jar", "bin/s3proxy.jar", "--properties", configPath], { env, stdio: ["ignore", "pipe", "pipe"] });
      message = { ...credentials, region: "us-east-1" };
    } else if (input.target === "rustfs-filesystem") {
      child = spawn("bin/rustfs", [directory], { env: { ...env, RUSTFS_ADDRESS: `[::]:${port}`,
        RUSTFS_ACCESS_KEY: credentials.accessKeyId, RUSTFS_SECRET_KEY: credentials.secretAccessKey,
        RUSTFS_CONSOLE_ENABLE: "false", RUSTFS_CHECK_UPDATE: "false", RUSTFS_DURABILITY_MODE: "strict" }, stdio: ["ignore", "pipe", "pipe"] });
      message = { ...credentials, region: "us-east-1" };
    } else {
      child = spawn("bin/minio", ["server", "--address", `:${port}`, "--console-address", ":8083", directory],
        { env: { ...env, MINIO_ROOT_USER: credentials.accessKeyId, MINIO_ROOT_PASSWORD: credentials.secretAccessKey, MINIO_BROWSER: "off" }, stdio: ["ignore", "pipe", "pipe"] });
      message = { ...credentials, region: "us-east-1" };
    }
    let stderr = "";
    child.on("error", () => {});
    child.stdout.on("data", (chunk) => { stderr = (stderr + chunk.toString()).slice(-8192); });
    child.stderr.on("data", (chunk) => { stderr = (stderr + chunk.toString()).slice(-8192); });
    const endpoint = `http://127.0.0.1:${port}`;
    const probe = benchmarkClient(endpoint, { region: message.region, credentials: message }, 1, 1000);
    try {
    while (performance.now() - began < 30000) {
      if (child.exitCode !== null) throw new Error(`target exited ${child.exitCode}: ${stderr.replaceAll(message.secretAccessKey, "[redacted]").replaceAll(message.accessKeyId, "[redacted]")}`);
      const ready = await authenticatedReady(probe.client).then(() => true).catch(() => false);
      if (ready) {
        const startupMs = performance.now() - began;
        const bootstrap = benchmarkClient(endpoint, { region: message.region, credentials: message }, 1, 30000);
        try {
          const first = await firstWriteProbe(bootstrap.client, firstBody);
          return { child, credentials: message, directory, profiling: input.target === "stow-memory-profile", startupMs, firstWriteMs: first.writtenAt - began,
            firstWriteBytes: first.bytes, firstWriteVerified: first.verified,
            async close() { await stopChild(child); await rm(directory, { recursive: true, force: true }); } };
        } finally { bootstrap.close(); }
      }
      await delay(10);
    }
    throw new Error("target readiness timed out");
    } finally { probe.close(); }
  } catch (error) {
    if (child) await stopChild(child);
    await rm(directory, { recursive: true, force: true });
    throw error;
  }
}
