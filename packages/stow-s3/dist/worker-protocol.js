export function isWorkerRequest(value) {
    return isRecord(value) && value.type === "stow-call" && isId(value.id) &&
        typeof value.metadata === "string" &&
        (value.data === undefined || value.data instanceof Uint8Array);
}
export function isWorkerReply(value) {
    if (!isRecord(value) || !isId(value.id))
        return false;
    if (value.type === "stow-error")
        return typeof value.message === "string" &&
            (value.code === undefined || typeof value.code === "string");
    return value.type === "stow-result" && typeof value.metadata === "string" &&
        (value.data === undefined || value.data instanceof Uint8Array);
}
export function isRecord(value) {
    return typeof value === "object" && value !== null;
}
export function isWorkerControl(metadata, data) {
    if (new TextEncoder().encode(metadata).byteLength > 4096 || (data?.byteLength ?? 0) > 0)
        return false;
    try {
        const request = JSON.parse(metadata);
        return isRecord(request) && ["abortObjectWrite", "closeObjectRead", "close"].includes(String(request.op));
    }
    catch {
        return false;
    }
}
function isId(value) {
    return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}
//# sourceMappingURL=worker-protocol.js.map