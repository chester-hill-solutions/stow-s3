import { EmbeddedStowError, type EmbeddedBinaryResponse } from "./embedded.js";
import { isWorkerControl, isWorkerReply, type WorkerEndpoint, type WorkerRequest } from "./worker-protocol.js";

export interface WorkerCallOptions {
  signal?: AbortSignal;
  /** Detach the entire input ArrayBuffer. Defaults to copying; views must span it. */
  transfer?: boolean;
}

export interface WorkerHostOptions {
  maxInFlight?: number;
  maxInFlightBytes?: number;
  maxResponseBytes?: number;
}

export interface AsyncEmbeddedHost {
  callBinary(metadata: string, data?: Uint8Array, options?: WorkerCallOptions): Promise<EmbeddedBinaryResponse>;
}

interface Pending {
  bytes: number;
  control: boolean;
  resolve(value: EmbeddedBinaryResponse): void;
  reject(error: unknown): void;
  detachAbort(): void;
  cancelled: boolean;
}

// One runtime permits 16 streams. Cleanup has its own bounded queue so closing
// those streams together does not discard releases when the reserved lane is busy.
const MAX_PENDING_CONTROLS = 32;

/** Bounds admitted calls instead of retaining an unbounded queue of copied payloads. */
export class WorkerEmbeddedHost implements AsyncEmbeddedHost {
  private readonly pending = new Map<number, Pending>();
  private nextId = 1;
  private pendingBytes = 0;
  private controls = 0;
  private controlBytes = 0;
  private activeControl: number | undefined;
  private readonly controlQueue: { id: number; send(): void }[] = [];
  private closed = false;
  private readonly maxInFlight: number;
  private readonly maxInFlightBytes: number;
  private readonly maxResponseBytes: number;

  constructor(private readonly endpoint: WorkerEndpoint, options: WorkerHostOptions = {}) {
    this.maxInFlight = positiveLimit(options.maxInFlight ?? 4);
    this.maxInFlightBytes = positiveLimit(options.maxInFlightBytes ?? 64 * 1024 * 1024);
    this.maxResponseBytes = positiveLimit(options.maxResponseBytes ?? 64 * 1024 * 1024);
    endpoint.addEventListener("message", this.receive);
  }

  callBinary(metadata: string, data?: Uint8Array, options: WorkerCallOptions = {}): Promise<EmbeddedBinaryResponse> {
    if (this.closed) return Promise.reject(new EmbeddedStowError("closed", "worker host is closed"));
    if (options.signal?.aborted) return Promise.reject(abortError());
    const bytes = new TextEncoder().encode(metadata).byteLength + (data?.byteLength ?? 0);
    const control = isWorkerControl(metadata, data);
    if (control ? this.controls >= MAX_PENDING_CONTROLS :
      this.pending.size - this.controls >= this.maxInFlight || this.pendingBytes - this.controlBytes + bytes > this.maxInFlightBytes) {
      return Promise.reject(new EmbeddedStowError("backpressure", "worker in-flight limit reached"));
    }
    const id = this.nextId++;
    return new Promise((resolve, reject) => {
      const pending: Pending = { bytes, control, resolve, reject, cancelled: false, detachAbort: () => undefined };
      const abort = () => {
        pending.cancelled = true;
        pending.detachAbort();
        reject(abortError());
        try { this.endpoint.postMessage({ type: "stow-cancel", id }); } catch { this.close(); }
      };
      options.signal?.addEventListener("abort", abort, { once: true });
      pending.detachAbort = () => options.signal?.removeEventListener("abort", abort);
      this.pending.set(id, pending);
      this.pendingBytes += bytes;
      if (control) { this.controls++; this.controlBytes += bytes; }
      const send = () => {
        if (pending.cancelled) { this.release(id, pending); return; }
        try {
          const transfer = inputTransfer(data, options.transfer === true);
          const request: WorkerRequest = { type: "stow-call", id, metadata, data };
          this.endpoint.postMessage(request, transfer);
        } catch (error) {
          this.release(id, pending);
          reject(error);
        }
      };
      if (control) {
        this.controlQueue.push({ id, send });
        this.dispatchControl();
      } else send();
    });
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.controlQueue.length = 0;
    this.endpoint.removeEventListener("message", this.receive);
    for (const [id, pending] of this.pending) {
      this.release(id, pending);
      pending.reject(new EmbeddedStowError("closed", "worker host is closed"));
    }
  }

  private readonly receive = (event: MessageEvent<unknown>): void => {
    const reply = event.data;
    if (!isWorkerReply(reply)) return;
    const pending = this.pending.get(reply.id);
    if (pending === undefined) return;
    this.release(reply.id, pending);
    if (pending.cancelled) return;
    if (reply.type === "stow-error") {
      pending.reject(new EmbeddedStowError(reply.code ?? "worker_error", reply.message));
    } else if (new TextEncoder().encode(reply.metadata).byteLength + (reply.data?.byteLength ?? 0) > this.maxResponseBytes) {
      pending.reject(new EmbeddedStowError("payload_limit", "worker response exceeds payload limit"));
    } else {
      pending.resolve({ metadata: reply.metadata, data: reply.data });
    }
  };

  private release(id: number, pending: Pending): void {
    this.pending.delete(id);
    this.pendingBytes -= pending.bytes;
    if (pending.control) {
      this.controls--; this.controlBytes -= pending.bytes;
      if (this.activeControl === id) this.activeControl = undefined;
      this.dispatchControl();
    }
    pending.detachAbort();
  }

  private dispatchControl(): void {
    if (this.closed || this.activeControl !== undefined) return;
    const next = this.controlQueue.shift();
    if (next === undefined) return;
    this.activeControl = next.id;
    next.send();
  }
}

function inputTransfer(data: Uint8Array | undefined, transfer: boolean): Transferable[] {
  if (!transfer || data === undefined) return [];
  if (!(data.buffer instanceof ArrayBuffer) || data.byteOffset !== 0 || data.byteLength !== data.buffer.byteLength) {
    throw new EmbeddedStowError("invalid_transfer", "transferred data must span an ordinary ArrayBuffer");
  }
  return [data.buffer];
}

function positiveLimit(value: number): number {
  if (!Number.isSafeInteger(value) || value < 1) throw new RangeError("worker limits must be positive safe integers");
  return value;
}

function abortError(): EmbeddedStowError {
  return new EmbeddedStowError("cancelled", "worker call cancelled; a started mutation may have committed");
}
