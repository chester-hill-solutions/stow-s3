import type { ContentObjectHost } from "./content-object-host.js";
import type { ContentStore } from "./content-store.js";
import { EMBEDDED_BINARY_PROTOCOL_VERSION, EmbeddedStowError, type EmbeddedBinaryResponse } from "./embedded.js";
import type { IndexedDbContentAdapter } from "./indexeddb-content.js";

export interface WorkerContentSession {
  adapter: IndexedDbContentAdapter;
  host: ContentObjectHost;
  store: ContentStore;
}

export function isContentMaintenance(op: unknown): boolean {
  return ["collectGarbage", "inspectRecovery", "discardAbandonedStage"].includes(String(op));
}

export async function maintainWorkerContent(sessions: Map<number, WorkerContentSession>, request: Record<string, unknown>): Promise<EmbeddedBinaryResponse> {
  const session = typeof request.handle === "number" ? sessions.get(request.handle) : undefined;
  if (request.version !== EMBEDDED_BINARY_PROTOCOL_VERSION) throw new EmbeddedStowError("invalid_request", "maintenance requires binary protocol v2");
  if (session === undefined) throw new EmbeddedStowError("unsupported_backend", "maintenance requires an open persistent worker runtime");
  let result: unknown;
  if (request.op === "collectGarbage") {
    const usage = await session.adapter.collect();
    session.store.clearCache();
    result = { ...usage, cacheBytes: 0 };
  } else if (request.op === "inspectRecovery") {
    const [stages, protections] = await Promise.all([session.adapter.listStages(), session.adapter.listProtections()]);
    result = { stages, protections };
  } else if (request.op === "discardAbandonedStage") {
    if (typeof request.stageId !== "string" || !request.stageId) throw new EmbeddedStowError("invalid_request", "stage ID is required");
    if (session.host.hasActiveStage(request.stageId)) throw new EmbeddedStowError("stage_active", "an active write owns this stage");
    await session.adapter.releaseStage(request.stageId);
    result = null;
  } else throw new EmbeddedStowError("invalid_request", "unknown maintenance operation");
  return { metadata: JSON.stringify({ version: EMBEDDED_BINARY_PROTOCOL_VERSION, ok: true, result }) };
}
