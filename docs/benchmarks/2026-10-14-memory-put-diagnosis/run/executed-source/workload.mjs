import { createHash, randomBytes } from "node:crypto";
import { performance } from "node:perf_hooks";
import { setTimeout as delay } from "node:timers/promises";
import { CreateBucketCommand, PutObjectCommand, GetObjectCommand, DeleteObjectCommand, ListObjectsV2Command } from "@aws-sdk/client-s3";

const bucket = "stow-performance";
export function digest(bytes) { return createHash("sha256").update(bytes).digest("hex"); }

export async function verifiedRead(client, key, expected, signal) {
  const response = await client.send(new GetObjectCommand({ Bucket: bucket, Key: key }), { abortSignal: signal });
  const hash = createHash("sha256");
  let bytes = 0;
  const onAbort = () => response.Body.destroy?.(signal.reason);
  signal?.addEventListener("abort", onAbort, { once: true });
  try {
    if (signal?.aborted) onAbort();
    for await (const chunk of response.Body) { signal?.throwIfAborted(); hash.update(chunk); bytes += chunk.length; }
    signal?.throwIfAborted();
  } finally { signal?.removeEventListener("abort", onAbort); }
  if (bytes !== expected.bytes || hash.digest("hex") !== expected.sha256) {
    const error = new Error("download failed SHA-256 or length verification");
    error.name = "IntegrityFailure"; throw error;
  }
}

export async function prepare(client, scenario, config) {
  await client.send(new CreateBucketCommand({ Bucket: bucket }));
  const fixtures = [];
  for (let index = 0; index < config.retainedObjects; index++) {
    const payload = randomBytes(scenario.bytes);
    const key = `fixture-${index}`;
    const expected = { key, bytes: payload.length, sha256: digest(payload) };
    await client.send(new PutObjectCommand({ Bucket: bucket, Key: key, Body: payload }), { abortSignal: AbortSignal.timeout(config.timeoutMs) });
    await verifiedRead(client, key, expected, AbortSignal.timeout(config.timeoutMs));
    fixtures.push(expected);
  }
  return { fixtures, payloads: Array.from({ length: scenario.concurrency }, () => randomBytes(scenario.bytes)), latest: new Map(), unknown: new Set() };
}

export function workload(client, scenario, config, state, measurements) {
  async function operation(name, command, bytes, job, read) {
    const start = performance.now();
    let error;
    try {
      if (read) await read();
      else await client.send(command, { abortSignal: job.signal });
    } catch (failure) { error = failure; throw failure; }
    finally { measurements.record(name, performance.now() - start, bytes, error, performance.now() <= job.deadline); }
  }
  return async (job) => {
    if (scenario.workload === "get") {
      const fixture = state.fixtures[job.sequence % state.fixtures.length];
      await operation("get", null, fixture.bytes, job, () => verifiedRead(client, fixture.key, fixture, job.signal));
      return;
    }
    const body = state.payloads[job.worker];
    body.writeBigUInt64LE(BigInt(job.sequence), 0);
    const expected = { bytes: body.length, sha256: digest(body) };
    const workerKey = job.worker + (state.workerOffset ?? 0);
    const key = scenario.workload === "put" ? `writer-${workerKey}` : `quarantine-${workerKey}`;
    try {
      await operation("put", new PutObjectCommand({ Bucket: bucket, Key: key, Body: body, ContentType: "application/octet-stream" }), body.length, job);
    } catch (error) { state.latest.delete(key); state.unknown.add(key); throw error; }
    state.latest.set(key, expected);
    state.unknown.delete(key);
    if (scenario.workload === "put") return;
    await operation("get", null, body.length, job, () => verifiedRead(client, key, expected, job.signal));
    if (config.scannerDelayMs) await delay(config.scannerDelayMs, undefined, { signal: job.signal });
    const verdictKey = `${key}.verdict`;
    const verdict = JSON.stringify({ sha256: expected.sha256, verdict: "simulated-clean" });
    await operation("verdict", new PutObjectCommand({ Bucket: bucket, Key: verdictKey, Body: verdict, ContentType: "application/json" }), Buffer.byteLength(verdict), job);
    try { await operation("delete", new DeleteObjectCommand({ Bucket: bucket, Key: key }), 0, job); }
    catch (error) { state.latest.delete(key); state.unknown.add(key); throw error; }
    state.latest.delete(key);
    await operation("delete", new DeleteObjectCommand({ Bucket: bucket, Key: verdictKey }), 0, job);
  };
}

export async function verifyFinal(client, state, timeoutMs) {
  for (const fixture of state.fixtures) await verifiedRead(client, fixture.key, fixture, AbortSignal.timeout(timeoutMs));
  for (const [key, expected] of state.latest) await verifiedRead(client, key, expected, AbortSignal.timeout(timeoutMs));
  return { verifiedObjects: state.fixtures.length + state.latest.size, algorithm: "SHA-256", complete: state.unknown.size === 0, unknownOutcomeKeys: [...state.unknown] };
}

export async function leftovers(client, timeoutMs) {
  const result = await client.send(new ListObjectsV2Command({ Bucket: bucket, Prefix: "quarantine-", MaxKeys: 1000 }), { abortSignal: AbortSignal.timeout(timeoutMs) });
  return { count: result.Contents?.length ?? 0, truncated: result.IsTruncated ?? false };
}

export async function recoveryProbe(binary, directory, config, startServer) {
  const scenario = { backend: "filesystem", bytes: 4096, concurrency: 1 };
  let server = await startServer(binary, directory, scenario, config);
  const expected = [];
  try {
    await server.client.send(new CreateBucketCommand({ Bucket: bucket }));
    for (let i = 0; i < 8; i++) {
      const body = randomBytes(4096), key = `recovery-${i}`;
      await server.client.send(new PutObjectCommand({ Bucket: bucket, Key: key, Body: body }));
      expected.push({ key, bytes: body.length, sha256: digest(body) });
    }
    await server.close("SIGKILL");
    const began = performance.now();
    server = await startServer(binary, directory, scenario, config);
    const readyMs = performance.now() - began;
    for (const item of expected) await verifiedRead(server.client, item.key, item, AbortSignal.timeout(config.timeoutMs));
    return { kind: "process SIGKILL after acknowledged PUTs", objects: expected.length, verified: true,
      restartToReadyMs: readyMs, restartToVerifiedMs: performance.now() - began,
      powerLossTested: false, writesInFlightTested: false };
  } finally { await server.close(); }
}
