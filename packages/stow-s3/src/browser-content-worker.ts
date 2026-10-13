import { ContentObjectHost } from "./content-object-host.js";
import { ContentStore } from "./content-store.js";
import { EmbeddedStowError, type EmbeddedBinaryResponse } from "./embedded.js";
import { IndexedDbContentAdapter } from "./indexeddb-content.js";
import { loadBrowserWasmBridge, type BrowserWasmBridge } from "./browser-wasm-host.js";
import { serveEmbeddedWorker } from "./worker-server.js";
import { isRecord, type WorkerEndpoint } from "./worker-protocol.js";
import { isContentMaintenance, maintainWorkerContent, type WorkerContentSession } from "./worker-content-maintenance.js";

const sessions = new Map<number, WorkerContentSession>();
const loading = loadBrowserWasmBridge();
void loading.catch(() => undefined);
serveEmbeddedWorker(globalThis as typeof globalThis & WorkerEndpoint, Promise.resolve({ callBinary }));

async function callBinary(metadata: string, data?: Uint8Array): Promise<EmbeddedBinaryResponse> {
  const bridge = await loading;
  const request: unknown = JSON.parse(metadata);
  if (!isRecord(request)) throw new EmbeddedStowError("invalid_request", "expected request metadata");
  if (request.op === "openHost") return openPersistent(bridge, request);
  if (isContentMaintenance(request.op)) return maintainWorkerContent(sessions, request);
  if (request.op === "close" && typeof request.handle === "number") {
    try { return await bridge.callBinaryAsync(metadata, data); } finally {
      const session = sessions.get(request.handle);
      sessions.delete(request.handle);
      await session?.adapter.close();
    }
  }
  return bridge.callBinaryAsync(metadata, data);
}

async function openPersistent(bridge: BrowserWasmBridge, request: Record<string, unknown>): Promise<EmbeddedBinaryResponse> {
  const options = isRecord(request.options) ? request.options : {};
  if (typeof options.namespace !== "string") throw new EmbeddedStowError("invalid_request", "namespace is required");
  const adapter = new IndexedDbContentAdapter({
    databaseName: optionalString(options.databaseName),
    quotaBytes: optionalNumber(options.physicalQuotaBytes),
  });
  await adapter.open(options.namespace);
  let accepted = false;
  try {
    const store = new ContentStore(adapter, { cacheBytes: optionalNumber(options.cacheBytes), chunkBytes: 256 * 1024 });
    const host = new ContentObjectHost(store, { maxStreams: optionalNumber(options.maxStreams) });
    const result = await bridge.openHost(JSON.stringify({ ...request, op: "open" }), host);
    const response: unknown = JSON.parse(result.metadata);
    if (isRecord(response) && response.ok === true && isRecord(response.result) && typeof response.result.handle === "number") {
      if (!isRecord(response.result.capabilities)) throw new EmbeddedStowError("protocol", "host capabilities are missing");
      response.result.capabilities.hostMaintenance = true;
      sessions.set(response.result.handle, { adapter, host, store });
      accepted = true;
      return { ...result, metadata: JSON.stringify(response) };
    }
    return result;
  } finally {
    if (!accepted) await adapter.close();
  }
}

function optionalString(value: unknown): string | undefined {
  if (value === undefined) return undefined;
  if (typeof value !== "string") throw new EmbeddedStowError("invalid_request", "expected a string option");
  return value;
}

function optionalNumber(value: unknown): number | undefined {
  if (value === undefined) return undefined;
  if (typeof value !== "number") throw new EmbeddedStowError("invalid_request", "expected a numeric option");
  return value;
}
