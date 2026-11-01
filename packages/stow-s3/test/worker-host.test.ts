import assert from "node:assert/strict";
import { test } from "node:test";
import { setTimeout as delay } from "node:timers/promises";
import { AsyncEmbeddedStow } from "../dist/async-embedded.js";
import { EmbeddedStowError, type EmbeddedBinaryResponse } from "../dist/embedded.js";
import { WorkerEmbeddedHost, type AsyncEmbeddedHost } from "../dist/worker-host.js";
import { serveEmbeddedWorker } from "../dist/worker-server.js";
import type { WorkerEndpoint } from "../dist/worker-protocol.js";
import { loadNodeWasmHost } from "../dist/node-wasm-host.js";

class Endpoint implements WorkerEndpoint {
  readonly listeners = new Set<(event: MessageEvent<unknown>) => void>();
  peer?: Endpoint;

  postMessage(message: unknown, transfer: Transferable[] = []): void {
    const data: unknown = structuredClone(message, { transfer });
    queueMicrotask(() => {
      const event = new MessageEvent<unknown>("message", { data });
      for (const listener of this.peer?.listeners ?? []) listener(event);
    });
  }

  addEventListener(_type: "message", listener: (event: MessageEvent<unknown>) => void): void { this.listeners.add(listener); }
  removeEventListener(_type: "message", listener: (event: MessageEvent<unknown>) => void): void { this.listeners.delete(listener); }
}

function pair(): [Endpoint, Endpoint] {
  const client = new Endpoint();
  const server = new Endpoint();
  client.peer = server;
  server.peer = client;
  return [client, server];
}

function response(result: unknown, data?: Uint8Array): EmbeddedBinaryResponse {
  return { metadata: JSON.stringify({ version: 2, ok: true, result }), data };
}

test("worker binary transport preserves default buffers and explicitly transfers whole buffers", async () => {
  const [client, server] = pair();
  const stop = serveEmbeddedWorker(server, Promise.resolve({ callBinary: async (_metadata: string, data?: Uint8Array) => response({}, data) }));
  const host = new WorkerEmbeddedHost(client);
  try {
    const retained = new Uint8Array([0, 255, 7]);
    assert.deepEqual((await host.callBinary("{}", retained)).data, retained);
    assert.equal(retained.byteLength, 3);
    const transferred = new Uint8Array([1, 2, 3]);
    const result = await host.callBinary("{}", transferred, { transfer: true });
    assert.equal(transferred.byteLength, 0);
    assert.deepEqual(result.data, new Uint8Array([1, 2, 3]));
    await assert.rejects(host.callBinary("{}", retained.subarray(1), { transfer: true }), hasCode("invalid_transfer"));
  } finally {
    host.close();
    stop();
  }
});

test("cancelled calls retain their admission lease until the worker finishes", async () => {
  const [client, server] = pair();
  let complete: (value: EmbeddedBinaryResponse) => void = () => undefined;
  const operation = new Promise<EmbeddedBinaryResponse>((resolve) => { complete = resolve; });
  const stop = serveEmbeddedWorker(server, Promise.resolve({ callBinary: () => operation }));
  const host = new WorkerEmbeddedHost(client, { maxInFlight: 1 });
  try {
    const controller = new AbortController();
    const pending = host.callBinary("{}", new Uint8Array(8), { signal: controller.signal });
    const rejected = assert.rejects(pending, hasCode("cancelled"));
    await delay(0);
    controller.abort();
    await rejected;
    await assert.rejects(host.callBinary("{}"), hasCode("backpressure"));
    complete(response({}));
    await delay(0);
    assert.equal((await host.callBinary("{}")).metadata, response({}).metadata);
  } finally {
    host.close();
    stop();
  }
});

test("worker rejects oversized admission and response and releases failed calls", async () => {
  const [client, server] = pair();
  const stop = serveEmbeddedWorker(server, Promise.resolve({ callBinary: async () => response({}, new Uint8Array(8)) }));
  const host = new WorkerEmbeddedHost(client, { maxInFlightBytes: 16, maxResponseBytes: 4 });
  try {
    await assert.rejects(host.callBinary("{}", new Uint8Array(15)), hasCode("backpressure"));
    await assert.rejects(host.callBinary("{}"), hasCode("payload_limit"));
    await assert.rejects(host.callBinary("{}"), hasCode("payload_limit"));
    const abort = new AbortController();
    abort.abort();
    await assert.rejects(host.callBinary("{}", undefined, { signal: abort.signal }), hasCode("cancelled"));
    host.close();
    await assert.rejects(host.callBinary("{}"), hasCode("closed"));
  } finally {
    host.close();
    stop();
  }
});

test("cancellation during initialization skips dispatch and close releases pending callers", async () => {
  const [client, server] = pair();
  let initialize: (host: AsyncEmbeddedHost) => void = () => undefined;
  const ready = new Promise<AsyncEmbeddedHost>((resolve) => { initialize = resolve; });
  let calls = 0;
  const stop = serveEmbeddedWorker(server, ready);
  const host = new WorkerEmbeddedHost(client);
  try {
    const controller = new AbortController();
    const pending = host.callBinary("{}", undefined, { signal: controller.signal });
    const rejected = assert.rejects(pending, hasCode("cancelled"));
    await delay(0);
    controller.abort();
    await rejected;
    initialize({ callBinary: async () => { calls += 1; return response({}); } });
    await delay(0);
    assert.equal(calls, 0);
    const active = host.callBinary("{}");
    const closed = assert.rejects(active, hasCode("closed"));
    host.close();
    await closed;
    assert.equal(client.listeners.size, 0);
  } finally {
    host.close();
    stop();
  }
});

test("async runtime uses the binary envelope and propagates structured failures", async () => {
  const [client, server] = pair();
  const requests: Record<string, unknown>[] = [];
  const stop = serveEmbeddedWorker(server, Promise.resolve({
    callBinary: async (metadata: string, data?: Uint8Array) => {
      const request: Record<string, unknown> = JSON.parse(metadata);
      requests.push(request);
      if (request.op === "open") return response({ handle: 3, capabilities: { backend: "memory", conditionalWrites: true } });
      if (request.op === "putObject") return response({ bucket: request.bucket, key: request.key, size: data?.length, etag: "etag" });
      if (request.op === "getObject") return response({ bucket: request.bucket, key: request.key, size: 3, etag: "etag" }, new Uint8Array([0, 127, 255]));
      if (request.op === "headObject") return { metadata: JSON.stringify({ version: 2, ok: false, error: { code: "object_not_found", message: "missing" } }) };
      return response(undefined);
    },
  }));
  const host = new WorkerEmbeddedHost(client);
  try {
    const runtime = await AsyncEmbeddedStow.open(host);
    await assert.rejects(runtime.collectGarbage(), hasCode("unsupported_backend"));
    await assert.rejects(runtime.inspectRecovery(), hasCode("unsupported_backend"));
    await assert.rejects(runtime.discardAbandonedStage("stage"), hasCode("unsupported_backend"));
    await runtime.putObject("assets", "binary", new Uint8Array([0, 127, 255]), { ifNoneMatch: "*" });
    assert.equal(requests[1]?.data, undefined);
    assert.equal(requests[1]?.ifNoneMatch, "*");
    assert.deepEqual((await runtime.getObject("assets", "binary")).data, new Uint8Array([0, 127, 255]));
    await assert.rejects(runtime.headObject("assets", "missing"), hasCode("object_not_found"));
    await runtime.close();
    assert.throws(() => runtime.usage(), hasCode("closed"));
    assert.throws(() => runtime.inspectRecovery(), hasCode("closed"));
  } finally {
    host.close();
    stop();
  }
});

test("cleanup controls have a bounded reserved lane when normal admission is full", async () => {
  const [client, server] = pair();
  const stalled = new Promise<EmbeddedBinaryResponse>(() => undefined);
  const stop = serveEmbeddedWorker(server, Promise.resolve({
    callBinary: async (metadata: string) => {
      const request: Record<string, unknown> = JSON.parse(metadata);
      return request.op === "blocked" ? stalled : response(null);
    },
  }), { maxInFlight: 1, maxInFlightBytes: 32 });
  const host = new WorkerEmbeddedHost(client, { maxInFlight: 1, maxInFlightBytes: 32 });
  try {
    const blocked = host.callBinary(JSON.stringify({ op: "blocked" }));
    const closed = assert.rejects(blocked, hasCode("closed"));
    await delay(0);
    await assert.rejects(host.callBinary("{}"), hasCode("backpressure"));
    assert.equal((await host.callBinary(JSON.stringify({ op: "abortObjectWrite", token: "token" }))).metadata, response(null).metadata);
    host.close();
    await closed;
  } finally {
    host.close();
    stop();
  }
});

test("async streams split large input into bounded owned transfers and close readers early", async () => {
  const [client, server] = pair();
  const writes: number[] = [];
  const source = new Uint8Array(600_001).fill(7);
  let readOffset = 0;
  let closedRead = 0;
  const object = { bucket: "assets", key: "large", size: source.byteLength, etag: "etag" };
  const stop = serveEmbeddedWorker(server, Promise.resolve({
    callBinary: async (metadata: string, data?: Uint8Array) => {
      const request: Record<string, unknown> = JSON.parse(metadata);
      if (request.op === "open") return response({ handle: 9, capabilities: { backend: "host", conditionalWrites: true } });
      if (request.op === "openObjectWrite") return response({ token: "write" });
      if (request.op === "writeObjectChunk") { writes.push(data?.byteLength ?? 0); return response(null); }
      if (request.op === "commitObjectWrite") return response(object);
      if (request.op === "openObjectRead") return response({ token: "read", object });
      if (request.op === "closeObjectRead") { closedRead++; return response(null); }
      if (request.op === "readObjectChunk") {
        const length = Number(request.length);
        const chunk = source.slice(readOffset, readOffset + length);
        readOffset += chunk.byteLength;
        return response({ eof: readOffset === source.byteLength }, chunk);
      }
      return response(null);
    },
  }));
  const host = new WorkerEmbeddedHost(client, { maxInFlightBytes: 300_000, maxResponseBytes: 300_000 });
  try {
    const runtime = await AsyncEmbeddedStow.open(host);
    await assert.rejects(runtime.collectGarbage(), hasCode("unsupported_backend"));
    const saved = await runtime.putObjectStream("assets", "large", source, { size: source.byteLength });
    assert.equal(saved.size, source.byteLength);
    assert.equal(source.byteLength, 600_001);
    assert.deepEqual(writes, [262_144, 262_144, 75_713]);
    const read = await runtime.getObjectStream("assets", "large");
    for await (const chunk of read.chunks) { assert.equal(chunk.byteLength, 262_144); break; }
    assert.equal(closedRead, 1);
    assert.equal(readOffset, 262_144);
    await runtime.close();
  } finally {
    host.close();
    stop();
  }
});

function hasCode(code: string): (error: unknown) => boolean {
  return (error) => error instanceof EmbeddedStowError && error.code === code;
}

test("concurrent stream closes release every real WASM reader slot", async () => {
  const wasm = await loadNodeWasmHost();
  const [client, server] = pair();
  const stop = serveEmbeddedWorker(server, Promise.resolve({
    callBinary: (metadata: string, data?: Uint8Array) => wasm.callBinaryAsync(metadata, data),
  }));
  const host = new WorkerEmbeddedHost(client);
  const runtime = await AsyncEmbeddedStow.open(host);
  try {
    await runtime.createBucket("assets");
    await runtime.putObject("assets", "key", new Uint8Array([1, 2, 3]));
    for (let round = 0; round < 2; round++) {
      const readers = [];
      for (let index = 0; index < 16; index++) readers.push(await runtime.getObjectStream("assets", "key"));
      await Promise.all(readers.flatMap((reader) => [reader.close(), reader.close()]));
    }
  } finally {
    await runtime.close();
    host.close();
    stop();
    await wasm.close();
  }
});

test("failed stream cleanup remains visible and can be retried", async () => {
  let closes = 0;
  const runtime = AsyncEmbeddedStow.attach({
    callBinary: async (metadata: string) => {
      const request: Record<string, unknown> = JSON.parse(metadata);
      if (request.op === "openObjectRead") return response({ token: "read", object: { bucket: "assets", key: "key", size: 1, etag: "etag" } });
      if (request.op === "closeObjectRead" && closes++ === 0) throw new EmbeddedStowError("backpressure", "busy");
      return response(null);
    },
  }, 1, { backend: "memory", maxBytes: 1024, maxObjects: 16, persistent: false, multipart: false, upstream: false });
  const reader = await runtime.getObjectStream("assets", "key");
  await assert.rejects(reader.close(), hasCode("backpressure"));
  await reader.close();
  assert.equal(closes, 2);
  await runtime.close();
});

function cleanupQueue(afterFirst: () => Promise<EmbeddedBinaryResponse>) {
  const [client, server] = pair();
  let finish: (value: EmbeddedBinaryResponse) => void = () => undefined;
  const gate = new Promise<EmbeddedBinaryResponse>((resolve) => { finish = resolve; });
  const calls: string[] = [];
  const stop = serveEmbeddedWorker(server, Promise.resolve({
    callBinary: async (metadata: string) => {
      calls.push(metadata);
      return calls.length === 1 ? gate : afterFirst();
    },
  }));
  const host = new WorkerEmbeddedHost(client);
  const metadata = JSON.stringify({ op: "closeObjectRead", token: "read" });
  return { host, metadata, calls, finish, stop };
}

test("queued cleanup is bounded, skips cancelled calls, and closes pending callers", async () => {
  const { host, metadata, calls, finish, stop } = cleanupQueue(() => new Promise<EmbeddedBinaryResponse>(() => undefined));
  try {
    const first = host.callBinary(metadata);
    const controller = new AbortController();
    const cancelledMetadata = JSON.stringify({ op: "closeObjectRead", token: "cancelled" });
    const cancelled = host.callBinary(cancelledMetadata, undefined, { signal: controller.signal });
    const rejected = assert.rejects(cancelled, hasCode("cancelled"));
    const queued = Array.from({ length: 30 }, () => assert.rejects(host.callBinary(metadata), hasCode("closed")));
    await assert.rejects(host.callBinary(metadata), hasCode("backpressure"));
    await delay(0);
    controller.abort();
    await rejected;
    finish(response(null));
    await first;
    host.close();
    await Promise.all(queued);
    assert.equal(calls.includes(cancelledMetadata), false);
    assert.ok(calls.length <= 2);
  } finally {
    host.close();
    stop();
  }
});

test("cancelling undispatched cleanup immediately frees every reserved slot", async () => {
  const { host, metadata, calls, finish, stop } = cleanupQueue(async () => response(null));
  try {
    const first = host.callBinary(metadata);
    await delay(0);
    const controllers = Array.from({ length: 31 }, () => new AbortController());
    const cancelled = controllers.map((controller) => assert.rejects(
      host.callBinary(JSON.stringify({ op: "closeObjectRead", token: "cancelled" }), undefined, { signal: controller.signal }),
      hasCode("cancelled"),
    ));
    await assert.rejects(host.callBinary(metadata), hasCode("backpressure"));
    controllers.forEach((controller) => controller.abort());
    await Promise.all(cancelled);
    const replacements = assert.doesNotReject(Promise.all(Array.from({ length: 31 }, () => host.callBinary(metadata))));
    await assert.rejects(host.callBinary(metadata), hasCode("backpressure"));
    assert.equal(calls.length, 1);
    finish(response(null));
    await first;
    await replacements;
    assert.equal(calls.length, 32);
    assert.equal(calls.every((call) => call === metadata), true);
  } finally {
    finish(response(null));
    host.close();
    stop();
  }
});
