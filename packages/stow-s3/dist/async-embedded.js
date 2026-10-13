import { EmbeddedStreamOperations } from "./async-streams.js";
import { decodeEmbeddedResponse, EMBEDDED_BINARY_PROTOCOL_VERSION, EmbeddedStowError, fromBridgeObject, } from "./embedded.js";
export class AsyncEmbeddedStow {
    host;
    handle;
    runtimeCapabilities;
    closed = false;
    constructor(host, handle, runtimeCapabilities) {
        this.host = host;
        this.handle = handle;
        this.runtimeCapabilities = runtimeCapabilities;
    }
    static async open(host, options = {}) {
        const result = await invoke(host, {
            op: "open", options: { backend: "memory", ...options },
        });
        return AsyncEmbeddedStow.attach(host, result.handle, result.capabilities);
    }
    static async openPersistent(host, options) {
        const result = await invoke(host, { op: "openHost", options });
        return AsyncEmbeddedStow.attach(host, result.handle, result.capabilities);
    }
    /** Attach the receipt from a host-backed open without opening a second runtime. */
    static attach(host, handle, capabilities) {
        return new AsyncEmbeddedStow(host, handle, capabilities);
    }
    capabilities() {
        this.ensureOpen();
        return { ...this.runtimeCapabilities };
    }
    collectGarbage() {
        return this.maintain({ op: "collectGarbage" });
    }
    inspectRecovery() {
        return this.maintain({ op: "inspectRecovery" });
    }
    discardAbandonedStage(stageId) {
        return this.maintain({ op: "discardAbandonedStage", stageId });
    }
    maintain(request) {
        this.ensureOpen();
        if (this.runtimeCapabilities.backend !== "host" || this.runtimeCapabilities.hostMaintenance !== true) {
            return Promise.reject(new EmbeddedStowError("unsupported_backend", "maintenance requires the persistent worker host"));
        }
        return this.invoke(request);
    }
    usage(options) {
        return this.invoke({ op: "usage" }, undefined, options);
    }
    createBucket(bucket) { return this.invoke({ op: "createBucket", bucket }); }
    deleteBucket(bucket) { return this.invoke({ op: "deleteBucket", bucket }); }
    deleteObject(bucket, key) { return this.invoke({ op: "deleteObject", bucket, key }); }
    reset() { return this.invoke({ op: "reset" }); }
    async listBuckets() {
        return (await this.invoke({ op: "listBuckets" })).buckets;
    }
    async putObject(bucket, key, data, options = {}, call) {
        if ((options.ifMatch || options.ifNoneMatch) && this.runtimeCapabilities.conditionalWrites !== true) {
            throw new EmbeddedStowError("conditional_write_unsupported", "host does not support conditional writes");
        }
        return fromBridgeObject(await this.invoke({ op: "putObject", bucket, key, ...options }, data, call));
    }
    async getObject(bucket, key, options) {
        return fromBridgeObject(await this.invoke({ op: "getObject", bucket, key }, undefined, options));
    }
    async getObjectRange(bucket, key, range, options) {
        return fromBridgeObject(await this.invoke({ op: "getObjectRange", bucket, key, range }, undefined, options));
    }
    putObjectStream(bucket, key, source, options) {
        if ((options.ifMatch || options.ifNoneMatch) && this.runtimeCapabilities.conditionalWrites !== true) {
            return Promise.reject(new EmbeddedStowError("conditional_write_unsupported", "host does not support conditional writes"));
        }
        return this.streams().put(bucket, key, source, options);
    }
    getObjectStream(bucket, key, options = {}) {
        return this.streams().openRead(bucket, key, options);
    }
    streams() {
        return new EmbeddedStreamOperations((request, data, options) => this.invoke(request, data, options));
    }
    async headObject(bucket, key) {
        return fromBridgeObject(await this.invoke({ op: "headObject", bucket, key }));
    }
    listObjects(bucket, list = {}) {
        return this.invoke({ op: "listObjects", bucket, list });
    }
    async copyObject(sourceBucket, sourceKey, destinationBucket, destinationKey) {
        return fromBridgeObject(await this.invoke({ op: "copyObject", sourceBucket, sourceKey, destinationBucket, destinationKey }));
    }
    async close() {
        if (this.closed)
            return;
        await this.invoke({ op: "close" });
        this.closed = true;
    }
    invoke(request, data, options) {
        this.ensureOpen();
        return invoke(this.host, { ...request, handle: this.handle }, data, options);
    }
    ensureOpen() {
        if (this.closed)
            throw new EmbeddedStowError("closed", "embedded runtime is closed");
    }
}
async function invoke(host, request, data, options) {
    const response = await host.callBinary(JSON.stringify({ ...request, version: EMBEDDED_BINARY_PROTOCOL_VERSION }), data, options);
    let parsed;
    try {
        parsed = JSON.parse(response.metadata);
    }
    catch {
        throw new EmbeddedStowError("protocol", "embedded host returned invalid JSON metadata");
    }
    return decodeEmbeddedResponse(parsed, EMBEDDED_BINARY_PROTOCOL_VERSION, response.data);
}
//# sourceMappingURL=async-embedded.js.map