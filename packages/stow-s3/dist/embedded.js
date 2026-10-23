export const EMBEDDED_PROTOCOL_VERSION = 1;
export const EMBEDDED_BINARY_PROTOCOL_VERSION = 2;
const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
export class EmbeddedStowError extends Error {
    code;
    constructor(code, message) {
        super(message);
        this.name = "EmbeddedStowError";
        this.code = code;
    }
}
export class EmbeddedStow {
    host;
    runtimeHandle;
    runtimeCapabilities;
    closed = false;
    constructor(host, runtimeHandle, runtimeCapabilities) {
        this.host = host;
        this.runtimeHandle = runtimeHandle;
        this.runtimeCapabilities = runtimeCapabilities;
    }
    static open(host, options = {}) {
        const result = invoke(host, {
            version: EMBEDDED_PROTOCOL_VERSION,
            op: "open",
            options: {
                backend: "memory",
                maxBytes: options.maxBytes,
                maxObjects: options.maxObjects,
            },
        });
        return new EmbeddedStow(host, result.handle, result.capabilities);
    }
    get handle() {
        return this.runtimeHandle;
    }
    capabilities() {
        this.ensureOpen();
        return { ...this.runtimeCapabilities };
    }
    usage() {
        return this.invoke({ op: "usage" });
    }
    createBucket(bucket) {
        this.invoke({ op: "createBucket", bucket });
    }
    deleteBucket(bucket) {
        this.invoke({ op: "deleteBucket", bucket });
    }
    listBuckets() {
        const result = this.invoke({ op: "listBuckets" });
        return result.buckets;
    }
    putObject(bucket, key, data, options = {}) {
        if ((options.ifMatch || options.ifNoneMatch) && this.runtimeCapabilities.conditionalWrites !== true) {
            throw new EmbeddedStowError("conditional_write_unsupported", "host does not support conditional writes");
        }
        const result = this.invoke({
            op: "putObject",
            bucket,
            key,
            data: this.host.callBinary === undefined ? bytesToBase64(data) : undefined,
            contentType: options.contentType,
            metadata: options.metadata,
            ifMatch: options.ifMatch,
            ifNoneMatch: options.ifNoneMatch,
        }, data);
        return fromBridgeObject(result);
    }
    getObject(bucket, key) {
        return fromBridgeObject(this.invoke({ op: "getObject", bucket, key }));
    }
    headObject(bucket, key) {
        return fromBridgeObject(this.invoke({ op: "headObject", bucket, key }));
    }
    listObjects(bucket, options = {}) {
        const result = this.invoke({
            op: "listObjects",
            bucket,
            list: { prefix: options.prefix, cursor: options.cursor, limit: options.limit },
        });
        return {
            objects: result.objects.map(fromBridgeObject),
            truncated: result.truncated,
            nextCursor: result.nextCursor,
        };
    }
    deleteObject(bucket, key) {
        this.invoke({ op: "deleteObject", bucket, key });
    }
    /**
     * Observe an object, or its absence, before changing it.
     *
     * The bytes come back with the observation, so a caller cannot read one version
     * and save against another.
     */
    readForSave(bucket, key) {
        this.requireGuardedSaves();
        const result = this.invoke({ op: "readForSave", bucket, key });
        return {
            object: fromBridgeObject(result.object),
            absent: result.absent,
            observationToken: result.observationToken,
        };
    }
    /**
     * Publish a save that compares at publication, or replaces deliberately.
     *
     * A replacement is still a save with a request key: consent to replace is not
     * consent to write twice.
     */
    saveObject(bucket, key, data, options = {}) {
        this.requireGuardedSaves();
        if (options.replace !== true && !options.observationToken) {
            throw new EmbeddedStowError("invalid_save_condition", "a save needs an observation token, or replace: true");
        }
        if (options.requestKey && this.runtimeCapabilities.durableSaveRequests !== true) {
            throw new EmbeddedStowError("save_requests_unsupported", "a request key needs a host that retains save requests");
        }
        const result = this.invoke({
            op: "saveObject",
            bucket,
            key,
            data: this.host.callBinary === undefined ? bytesToBase64(data) : undefined,
            observationToken: options.observationToken,
            replace: options.replace === true,
            contentType: options.contentType,
            metadata: options.metadata,
            requestKey: options.requestKey,
        }, data);
        return {
            object: fromBridgeObject(result.object),
            outcome: result.outcome,
            replayed: result.replayed,
        };
    }
    /** Ask what a request key did, instead of writing again. */
    resolveSave(bucket, key, requestKey) {
        if (this.runtimeCapabilities.durableSaveRequests !== true) {
            throw new EmbeddedStowError("save_requests_unsupported", "host does not retain save requests");
        }
        const result = this.invoke({ op: "resolveSave", bucket, key, requestKey });
        return {
            object: fromBridgeObject(result.object),
            outcome: result.outcome,
            replayed: result.replayed,
        };
    }
    /** Drop a token this caller will not use, rather than waiting for its expiry. */
    releaseObservation(observationToken) {
        this.invoke({ op: "releaseObservation", observationToken });
    }
    requireGuardedSaves() {
        if (this.runtimeCapabilities.guardedSaves !== true) {
            throw new EmbeddedStowError("guarded_saves_unsupported", "host does not support observation-based saves");
        }
    }
    copyObject(sourceBucket, sourceKey, destinationBucket, destinationKey) {
        return fromBridgeObject(this.invoke({
            op: "copyObject",
            sourceBucket,
            sourceKey,
            destinationBucket,
            destinationKey,
        }));
    }
    reset() {
        this.invoke({ op: "reset" });
    }
    close() {
        if (this.closed) {
            return;
        }
        this.invoke({ op: "close" });
        this.closed = true;
    }
    invoke(request, data) {
        this.ensureOpen();
        return invoke(this.host, {
            version: EMBEDDED_PROTOCOL_VERSION,
            ...request,
            handle: this.runtimeHandle,
        }, data);
    }
    ensureOpen() {
        if (this.closed) {
            throw new EmbeddedStowError("closed", "embedded runtime is closed");
        }
    }
}
function invoke(host, request, data) {
    const version = host.callBinary === undefined ? EMBEDDED_PROTOCOL_VERSION : EMBEDDED_BINARY_PROTOCOL_VERSION;
    let parsed;
    let payload;
    try {
        const metadata = JSON.stringify({ ...request, version });
        if (host.callBinary === undefined) {
            parsed = JSON.parse(host.call(metadata));
        }
        else {
            const result = host.callBinary(metadata, data);
            parsed = JSON.parse(result.metadata);
            payload = result.data;
        }
    }
    catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        throw new EmbeddedStowError("host_error", `embedded host call failed: ${message}`);
    }
    return decodeEmbeddedResponse(parsed, version, payload);
}
export function decodeEmbeddedResponse(parsed, version, payload) {
    if (!isRecord(parsed) || typeof parsed.version !== "number" || typeof parsed.ok !== "boolean") {
        throw new EmbeddedStowError("protocol", "embedded host returned an invalid response");
    }
    if (parsed.version !== version) {
        throw new EmbeddedStowError("protocol_version", `unsupported embedded protocol version ${parsed.version}`);
    }
    if (!parsed.ok) {
        if (!isRecord(parsed.error) ||
            typeof parsed.error.code !== "string" ||
            typeof parsed.error.message !== "string") {
            throw new EmbeddedStowError("protocol", "embedded host returned an invalid error response");
        }
        throw new EmbeddedStowError(parsed.error.code, parsed.error.message);
    }
    if (payload !== undefined) {
        if (!(payload instanceof Uint8Array) || !isRecord(parsed.result)) {
            throw new EmbeddedStowError("protocol", "embedded host returned an invalid binary payload");
        }
        parsed.result.data = payload;
    }
    return parsed.result;
}
function isRecord(value) {
    return typeof value === "object" && value !== null;
}
export function fromBridgeObject(object) {
    return {
        ...object,
        data: typeof object.data === "string" ? base64ToBytes(object.data) : object.data,
    };
}
function bytesToBase64(bytes) {
    let result = "";
    for (let index = 0; index < bytes.length; index += 3) {
        const first = bytes[index] ?? 0;
        const hasSecond = index + 1 < bytes.length;
        const second = hasSecond ? bytes[index + 1] ?? 0 : 0;
        const hasThird = index + 2 < bytes.length;
        const third = hasThird ? bytes[index + 2] ?? 0 : 0;
        result += base64Alphabet.charAt(first >> 2);
        result += base64Alphabet.charAt(((first & 3) << 4) | (second >> 4));
        result += hasSecond ? base64Alphabet.charAt(((second & 15) << 2) | (third >> 6)) : "=";
        result += hasThird ? base64Alphabet.charAt(third & 63) : "=";
    }
    return result;
}
function base64ToBytes(value) {
    const normalized = value.replace(/[\r\n]/g, "");
    const padding = normalized.endsWith("==") ? 2 : normalized.endsWith("=") ? 1 : 0;
    const output = new Uint8Array(Math.max(0, Math.floor((normalized.length * 3) / 4) - padding));
    let outputIndex = 0;
    for (let index = 0; index < normalized.length; index += 4) {
        const first = base64Value(normalized[index]);
        const second = base64Value(normalized[index + 1]);
        const third = normalized[index + 2] === "=" ? 0 : base64Value(normalized[index + 2]);
        const fourth = normalized[index + 3] === "=" ? 0 : base64Value(normalized[index + 3]);
        if (outputIndex < output.length) {
            output[outputIndex++] = (first << 2) | (second >> 4);
        }
        if (normalized[index + 2] !== "=" && outputIndex < output.length) {
            output[outputIndex++] = ((second & 15) << 4) | (third >> 2);
        }
        if (normalized[index + 3] !== "=" && outputIndex < output.length) {
            output[outputIndex++] = ((third & 3) << 6) | fourth;
        }
    }
    return output;
}
function base64Value(character) {
    if (character === undefined) {
        return 0;
    }
    const value = base64Alphabet.indexOf(character);
    if (value < 0) {
        throw new EmbeddedStowError("invalid_data", "invalid base64 object data");
    }
    return value;
}
//# sourceMappingURL=embedded.js.map