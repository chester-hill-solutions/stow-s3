import type { EmbeddedBinaryResponse } from "./embedded.js";

export interface WorkerEndpoint {
  postMessage(message: unknown, transfer?: Transferable[]): void;
  addEventListener(type: "message", listener: (event: MessageEvent<unknown>) => void): void;
  removeEventListener(type: "message", listener: (event: MessageEvent<unknown>) => void): void;
}

export interface WorkerRequest {
  type: "stow-call";
  id: number;
  metadata: string;
  data?: Uint8Array;
}

export interface WorkerReply extends EmbeddedBinaryResponse {
  type: "stow-result";
  id: number;
}

export interface WorkerFailure {
  type: "stow-error";
  id: number;
  message: string;
  code?: string;
}

export function isWorkerRequest(value: unknown): value is WorkerRequest {
  return isRecord(value) && value.type === "stow-call" && isId(value.id) &&
    typeof value.metadata === "string" &&
    (value.data === undefined || value.data instanceof Uint8Array);
}

export function isWorkerReply(value: unknown): value is WorkerReply | WorkerFailure {
  if (!isRecord(value) || !isId(value.id)) return false;
  if (value.type === "stow-error") return typeof value.message === "string" &&
    (value.code === undefined || typeof value.code === "string");
  return value.type === "stow-result" && typeof value.metadata === "string" &&
    (value.data === undefined || value.data instanceof Uint8Array);
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function isWorkerControl(metadata: string, data?: Uint8Array): boolean {
  if (new TextEncoder().encode(metadata).byteLength > 4096 || (data?.byteLength ?? 0) > 0) return false;
  try {
    const request: unknown = JSON.parse(metadata);
    return isRecord(request) && ["abortObjectWrite", "closeObjectRead", "close"].includes(String(request.op));
  } catch { return false; }
}

function isId(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}
