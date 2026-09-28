#!/usr/bin/env node
// Measure independent S3 connections and independent concurrent sessions.
// This is a measurement tool, not a timing gate.
//
//   node scripts/benchmark-concurrency.mjs [--runs N] [--payload-bytes N]
import { execFileSync } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { Agent } from "node:http";
import { cpus, totalmem, platform, arch, release, tmpdir } from "node:os";
import { performance } from "node:perf_hooks";
import { dirname, resolve } from "node:path";
import { NodeHttpHandler } from "@smithy/node-http-handler";
import {
  CreateBucketCommand,
  GetObjectCommand,
  PutObjectCommand,
  S3Client,
} from "@aws-sdk/client-s3";
import { Stow } from "../dist/index.js";

function argument(name, fallback) {
  const index = process.argv.indexOf(`--${name}`);
  return index === -1 ? fallback : process.argv[index + 1];
}

const RUNS = Number(argument("runs", "5"));
const PAYLOAD_BYTES = Number(argument("payload-bytes", String(1 << 20)));
const JSON_OUT = argument("json", resolve(tmpdir(), "stow-concurrency-baseline.json"));
const LEVELS = [1, 4, 8, 16];
const PARALLEL_SESSION_LEVELS = [1, 4, 8];

function round(value) {
  return Math.round(value * 100) / 100;
}

function percentile(values, fraction) {
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.ceil(fraction * sorted.length) - 1)];
}

function summarize(values) {
  return {
    samples: values.length,
    min: round(Math.min(...values)),
    p50: round(percentile(values, 0.5)),
    p95: round(percentile(values, 0.95)),
    max: round(Math.max(...values)),
  };
}

function childPids(parent) {
  try {
    return execFileSync("pgrep", ["-P", String(parent)], { encoding: "utf8" })
      .split("\n").map((line) => Number(line.trim()))
      .filter((pid) => Number.isInteger(pid) && pid > 0);
  } catch {
    return [];
  }
}

function rssKb(pid) {
  try {
    return Number(execFileSync("ps", ["-o", "rss=", "-p", String(pid)], { encoding: "utf8" }).trim()) || 0;
  } catch {
    return 0;
  }
}

function loadedServerRssKb() {
  return childPids(process.pid).reduce((sum, pid) => sum + rssKb(pid), 0);
}

function newClient(instance, label) {
  const agent = new Agent({ keepAlive: true, maxSockets: 1 });
  const client = new S3Client({
    ...instance.awsSdkV3Config(),
    requestHandler: new NodeHttpHandler({ httpAgent: agent }),
    maxAttempts: 1,
  });
  return { label, client, agent };
}

function connectionCount(agent) {
  const sockets = [...Object.values(agent.sockets), ...Object.values(agent.freeSockets)].flat();
  return sockets.length;
}

async function runConnectionLoad(connectionCountToUse) {
  const payload = Buffer.alloc(PAYLOAD_BYTES, 0x61);
  const started = performance.now();
  const instance = await Stow.start({ backend: "memory" });
  const readyMs = performance.now() - started;
  const clients = Array.from({ length: connectionCountToUse }, (_, index) => newClient(instance, index));
  const bucket = `bench-${process.pid}-${Math.random().toString(36).slice(2, 8)}`;
  const setupClient = clients[0].client;
  await setupClient.send(new CreateBucketCommand({ Bucket: bucket }));
  const bucketReady = performance.now();
  const uploadMs = [];
  const downloadMs = [];
  const batchMs = [];
  const downloadBatchMs = [];
  let firstUploadTotalMs = 0;

  try {
    for (let roundIndex = 0; roundIndex < 2; roundIndex += 1) {
      const batchStart = performance.now();
      const puts = await Promise.all(clients.map(async ({ client }, index) => {
        const start = performance.now();
        await client.send(new PutObjectCommand({
          Bucket: bucket, Key: `payload-${roundIndex}-${index}.bin`, Body: payload,
        }));
        const done = performance.now();
        return done - start;
      }));
      const batchDone = performance.now();
      uploadMs.push(...puts);
      batchMs.push(batchDone - batchStart);
      if (roundIndex === 0) firstUploadTotalMs = batchDone - started;

      const getBatchStart = performance.now();
      const gets = await Promise.all(clients.map(async ({ client }, index) => {
        const start = performance.now();
        const fetched = await client.send(new GetObjectCommand({
          Bucket: bucket, Key: `payload-${roundIndex}-${index}.bin`,
        }));
        const body = await fetched.Body.transformToByteArray();
        if (body.length !== PAYLOAD_BYTES) throw new Error("download size mismatch");
        return performance.now() - start;
      }));
      downloadMs.push(...gets);
      downloadBatchMs.push(performance.now() - getBatchStart);
    }

    const establishedConnections = clients.reduce((sum, entry) => sum + connectionCount(entry.agent), 0);
    const loadedRssKb = loadedServerRssKb();
    const totalBytes = PAYLOAD_BYTES * connectionCountToUse * 2;
    const totalBatchSeconds = batchMs.reduce((sum, value) => sum + value, 0) / 1000;
    return {
      connections: connectionCountToUse,
      readyMs,
      firstUploadTotalMs,
      uploadLatencyMs: uploadMs,
      downloadLatencyMs: downloadMs,
      uploadBatchMs: batchMs,
      uploadMiBPerSecond: totalBytes / 1048576 / totalBatchSeconds,
      loadedServerRssKb: loadedRssKb,
      establishedConnections,
      bucketSetupMs: bucketReady - started - readyMs,
      downloadBatchMs,
      downloadMiBPerSecond: (PAYLOAD_BYTES * connectionCountToUse * 2 / 1048576) /
        (downloadBatchMs.reduce((sum, value) => sum + value, 0) / 1000),
    };
  } finally {
    await Promise.all(clients.map(({ client, agent }) => {
      client.destroy();
      agent.destroy();
    }));
    await instance.stop();
  }
}

async function runParallelSessions(count) {
  const payload = Buffer.alloc(PAYLOAD_BYTES, 0x61);
  const started = performance.now();
  const instances = await Promise.all(Array.from({ length: count }, () => Stow.start({ backend: "memory" })));
  const clients = instances.map((instance, index) => newClient(instance, index));
  const bucketNames = instances.map((_, index) => `bench-${process.pid}-${index}-${Math.random().toString(36).slice(2, 6)}`);
  try {
    await Promise.all(clients.map(({ client }, index) => client.send(new CreateBucketCommand({ Bucket: bucketNames[index] }))));
    const putStart = performance.now();
    const latencies = await Promise.all(clients.map(async ({ client }, index) => {
      const start = performance.now();
      await client.send(new PutObjectCommand({ Bucket: bucketNames[index], Key: "first.bin", Body: payload }));
      const done = performance.now();
      return { uploadMs: done - start, roundtripMs: 0 };
    }));
    const firstUploadDone = performance.now();
    const roundtrips = await Promise.all(clients.map(async ({ client }, index) => {
      const start = performance.now();
      const fetched = await client.send(new GetObjectCommand({ Bucket: bucketNames[index], Key: "first.bin" }));
      await fetched.Body.transformToByteArray();
      return performance.now() - start;
    }));
    const done = performance.now();
    const loadedRssKb = loadedServerRssKb();
    return {
      sessions: count,
      timeToFirstUploadWallMs: firstUploadDone - started,
      concurrentUploadBatchMs: firstUploadDone - putStart,
      uploadMs: latencies.map((value) => value.uploadMs),
      roundtripMs: roundtrips,
      aggregateMiBPerSecond: (PAYLOAD_BYTES * count / 1048576) / ((firstUploadDone - putStart) / 1000),
      aggregateServerRssKb: loadedRssKb,
    };
  } finally {
    await Promise.all(clients.map(({ client, agent }) => {
      client.destroy();
      agent.destroy();
    }));
    await Promise.all(instances.map((instance) => instance.stop()));
  }
}

const connectionRuns = [];
for (const connections of LEVELS) {
  for (let run = 0; run < RUNS; run += 1) {
    connectionRuns.push(await runConnectionLoad(connections));
    process.stdout.write(`\r${connections} connections: run ${run + 1}/${RUNS}`);
  }
  process.stdout.write("\n");
}

const parallelSessionRuns = [];
for (const sessions of PARALLEL_SESSION_LEVELS) {
  for (let run = 0; run < RUNS; run += 1) {
    parallelSessionRuns.push(await runParallelSessions(sessions));
    process.stdout.write(`\r${sessions} parallel sessions: run ${run + 1}/${RUNS}`);
  }
  process.stdout.write("\n");
}

const connectionSummary = Object.fromEntries(LEVELS.map((connections) => {
  const rows = connectionRuns.filter((row) => row.connections === connections);
  return [String(connections), {
    concurrentUploads: connections,
    samples: rows.length,
    readyMs: summarize(rows.map((row) => row.readyMs)),
    timeToFirstUploadMs: summarize(rows.map((row) => row.firstUploadTotalMs)),
    perUploadLatencyMs: summarize(rows.flatMap((row) => row.uploadLatencyMs)),
    perDownloadLatencyMs: summarize(rows.flatMap((row) => row.downloadLatencyMs)),
    uploadBatchMs: summarize(rows.flatMap((row) => row.uploadBatchMs)),
    uploadThroughputMiBPerSecond: summarize(rows.map((row) => row.uploadMiBPerSecond)),
    downloadBatchMs: summarize(rows.flatMap((row) => row.downloadBatchMs)),
    downloadThroughputMiBPerSecond: summarize(rows.map((row) => row.downloadMiBPerSecond)),
    loadedServerRssMb: summarize(rows.map((row) => row.loadedServerRssKb / 1024)),
    establishedConnections: summarize(rows.map((row) => row.establishedConnections)),
  }];
}));

const sessionSummary = Object.fromEntries(PARALLEL_SESSION_LEVELS.map((sessions) => {
  const rows = parallelSessionRuns.filter((row) => row.sessions === sessions);
  return [String(sessions), {
    sessions,
    samples: rows.length,
    timeToFirstUploadWallMs: summarize(rows.map((row) => row.timeToFirstUploadWallMs)),
    concurrentUploadBatchMs: summarize(rows.map((row) => row.concurrentUploadBatchMs)),
    perUploadLatencyMs: summarize(rows.flatMap((row) => row.uploadMs)),
    perRoundtripLatencyMs: summarize(rows.flatMap((row) => row.roundtripMs)),
    aggregateUploadThroughputMiBPerSecond: summarize(rows.map((row) => row.aggregateMiBPerSecond)),
    aggregateServerRssMb: summarize(rows.map((row) => row.aggregateServerRssKb / 1024)),
  }];
}));

const report = {
  environment: {
    measuredAt: new Date().toISOString(),
    node: process.version,
    os: `${platform()} ${arch()} ${release()}`,
    cpu: cpus()[0]?.model ?? "unknown",
    cpuCount: cpus().length,
    totalMemoryMb: Math.round(totalmem() / 1048576),
    payloadBytes: PAYLOAD_BYTES,
    runsPerPoint: RUNS,
    notes: [
      "Connection tests create one Stow server and N independent keep-alive HTTP agents, each limited to one socket.",
      "Parallel-session tests create N independent Stow servers and upload one object to each concurrently.",
      "RSS is sampled after the payloads are resident; it is loaded RSS, not a time-sampled peak.",
      "Upload throughput uses two concurrent upload batches per connection point and excludes GET traffic.",
    ],
  },
  connections: connectionSummary,
  parallelSessions: sessionSummary,
};
const output = resolve(JSON_OUT);
await mkdir(dirname(output), { recursive: true });
await writeFile(output, `${JSON.stringify(report, null, 2)}\n`);
console.log(JSON.stringify(report, null, 2));
console.log(`\nwrote ${output}`);
