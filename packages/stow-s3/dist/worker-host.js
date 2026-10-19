import { EmbeddedStowError } from "./embedded.js";
import { isWorkerControl, isWorkerReply } from "./worker-protocol.js";
// One runtime permits 16 streams. Cleanup has its own bounded queue so closing
// those streams together does not discard releases when the reserved lane is busy.
const MAX_PENDING_CONTROLS = 32;
/** Bounds admitted calls instead of retaining an unbounded queue of copied payloads. */
export class WorkerEmbeddedHost {
    endpoint;
    pending = new Map();
    nextId = 1;
    closed = false;
    maxInFlight;
    maxInFlightBytes;
    maxResponseBytes;
    constructor(endpoint, options = {}) {
        this.endpoint = endpoint;
        this.maxInFlight = positiveLimit(options.maxInFlight ?? 4);
        this.maxInFlightBytes = positiveLimit(options.maxInFlightBytes ?? 64 * 1024 * 1024);
        this.maxResponseBytes = positiveLimit(options.maxResponseBytes ?? 64 * 1024 * 1024);
        endpoint.addEventListener("message", this.receive);
    }
    callBinary(metadata, data, options = {}) {
        if (this.closed)
            return Promise.reject(new EmbeddedStowError("closed", "worker host is closed"));
        if (options.signal?.aborted)
            return Promise.reject(abortError());
        const bytes = new TextEncoder().encode(metadata).byteLength + (data?.byteLength ?? 0);
        const control = isWorkerControl(metadata, data);
        const admitted = [...this.pending.values()].filter((pending) => pending.control === control);
        if (admitted.length >= (control ? MAX_PENDING_CONTROLS : this.maxInFlight) ||
            !control && admitted.reduce((total, pending) => total + pending.bytes, bytes) > this.maxInFlightBytes) {
            return Promise.reject(new EmbeddedStowError("backpressure", "worker in-flight limit reached"));
        }
        const id = this.nextId++;
        return new Promise((resolve, reject) => {
            const send = () => {
                pending.state = { kind: "sent" };
                try {
                    const transfer = inputTransfer(data, options.transfer === true);
                    const request = { type: "stow-call", id, metadata, data };
                    this.endpoint.postMessage(request, transfer);
                }
                catch (error) {
                    this.release(id, pending);
                    reject(error);
                }
            };
            const pending = { bytes, control, resolve, reject, detachAbort: () => undefined, state: { kind: "queued", send } };
            const abort = () => {
                const queued = pending.state.kind === "queued";
                pending.state = { kind: "cancelled" };
                pending.detachAbort();
                reject(abortError());
                if (queued)
                    this.release(id, pending);
                else
                    try {
                        this.endpoint.postMessage({ type: "stow-cancel", id });
                    }
                    catch {
                        this.close();
                    }
            };
            options.signal?.addEventListener("abort", abort, { once: true });
            pending.detachAbort = () => options.signal?.removeEventListener("abort", abort);
            this.pending.set(id, pending);
            if (control)
                this.dispatchControl();
            else
                send();
        });
    }
    close() {
        if (this.closed)
            return;
        this.closed = true;
        this.endpoint.removeEventListener("message", this.receive);
        for (const [id, pending] of this.pending) {
            this.release(id, pending);
            pending.reject(new EmbeddedStowError("closed", "worker host is closed"));
        }
    }
    receive = (event) => {
        const reply = event.data;
        if (!isWorkerReply(reply))
            return;
        const pending = this.pending.get(reply.id);
        if (pending === undefined)
            return;
        this.release(reply.id, pending);
        if (pending.state.kind === "cancelled")
            return;
        if (reply.type === "stow-error") {
            pending.reject(new EmbeddedStowError(reply.code ?? "worker_error", reply.message));
        }
        else if (new TextEncoder().encode(reply.metadata).byteLength + (reply.data?.byteLength ?? 0) > this.maxResponseBytes) {
            pending.reject(new EmbeddedStowError("payload_limit", "worker response exceeds payload limit"));
        }
        else {
            pending.resolve({ metadata: reply.metadata, data: reply.data });
        }
    };
    release(id, pending) {
        this.pending.delete(id);
        pending.detachAbort();
        if (pending.control)
            this.dispatchControl();
    }
    dispatchControl() {
        if (this.closed)
            return;
        for (const pending of this.pending.values()) {
            if (pending.control && pending.state.kind !== "queued")
                return;
        }
        for (const pending of this.pending.values()) {
            if (pending.control && pending.state.kind === "queued") {
                pending.state.send();
                return;
            }
        }
    }
}
function inputTransfer(data, transfer) {
    if (!transfer || data === undefined)
        return [];
    if (!(data.buffer instanceof ArrayBuffer) || data.byteOffset !== 0 || data.byteLength !== data.buffer.byteLength) {
        throw new EmbeddedStowError("invalid_transfer", "transferred data must span an ordinary ArrayBuffer");
    }
    return [data.buffer];
}
function positiveLimit(value) {
    if (!Number.isSafeInteger(value) || value < 1)
        throw new RangeError("worker limits must be positive safe integers");
    return value;
}
function abortError() {
    return new EmbeddedStowError("cancelled", "worker call cancelled; a started mutation may have committed");
}
//# sourceMappingURL=worker-host.js.map