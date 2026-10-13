import { EmbeddedStowError } from "./embedded.js";
import { isWorkerControl, isRecord, isWorkerRequest } from "./worker-protocol.js";
/** Cancellation stops calls awaiting initialization; started Go operations finish. */
export function serveEmbeddedWorker(endpoint, host, options = {}) {
    const cancelled = new Set();
    const admitted = new Set();
    const controls = new Set();
    const maxBytes = options.maxInFlightBytes ?? 64 * 1024 * 1024;
    const maxResponseBytes = options.maxResponseBytes ?? 64 * 1024 * 1024;
    const maxInFlight = options.maxInFlight ?? 4;
    let bytes = 0;
    let controlBytes = 0;
    let closed = false;
    const receive = (event) => {
        const message = event.data;
        if (isRecord(message) && message.type === "stow-cancel" && typeof message.id === "number") {
            if (admitted.has(message.id))
                cancelled.add(message.id);
            return;
        }
        if (!isWorkerRequest(message) || closed)
            return;
        const size = new TextEncoder().encode(message.metadata).byteLength + (message.data?.byteLength ?? 0);
        const control = isWorkerControl(message.metadata, message.data);
        const full = control ? controls.size >= 1 : admitted.size - controls.size >= maxInFlight || bytes - controlBytes + size > maxBytes;
        if (admitted.has(message.id) || full) {
            endpoint.postMessage({ type: "stow-error", id: message.id, message: "worker admission limit reached" });
            return;
        }
        admitted.add(message.id);
        bytes += size;
        if (control) {
            controls.add(message.id);
            controlBytes += size;
        }
        void (async () => {
            try {
                const bridge = await host;
                if (closed || cancelled.has(message.id))
                    throw new EmbeddedStowError("cancelled", "worker call cancelled before execution");
                if (bridge.callBinary === undefined)
                    throw new Error("worker requires a binary bridge");
                const result = await bridge.callBinary(message.metadata, message.data);
                if (new TextEncoder().encode(result.metadata).byteLength + (result.data?.byteLength ?? 0) > maxResponseBytes)
                    throw new Error("worker response exceeds payload limit");
                const transfer = result.data?.buffer instanceof ArrayBuffer ? [result.data.buffer] : [];
                endpoint.postMessage({ type: "stow-result", id: message.id, ...result }, transfer);
            }
            catch (error) {
                const code = workerErrorCode(error);
                endpoint.postMessage({ type: "stow-error", id: message.id, code, message: error instanceof Error ? error.message : String(error) });
            }
            finally {
                admitted.delete(message.id);
                cancelled.delete(message.id);
                bytes -= size;
                if (controls.delete(message.id))
                    controlBytes -= size;
            }
        })().catch(() => {
            closed = true;
            endpoint.removeEventListener("message", receive);
        });
    };
    endpoint.addEventListener("message", receive);
    return () => {
        closed = true;
        endpoint.removeEventListener("message", receive);
    };
}
function workerErrorCode(error) {
    return typeof error === "object" && error !== null && "code" in error && typeof error.code === "string" ? error.code : "worker_error";
}
//# sourceMappingURL=worker-server.js.map