import { EmbeddedStowError, fromBridgeObject } from "./embedded.js";
export const EMBEDDED_STREAM_CHUNK_BYTES = 256 * 1024;
export class EmbeddedStreamOperations {
    invoke;
    constructor(invoke) {
        this.invoke = invoke;
    }
    async put(bucket, key, source, options) {
        const { size, signal, ...putOptions } = options;
        if (!Number.isSafeInteger(size) || size < 0)
            throw new RangeError("stream size must be a non-negative safe integer");
        signal?.throwIfAborted();
        // Capture the token before honouring cancellation, so a successful open can be cleaned up.
        const { token } = await this.invoke({ op: "openObjectWrite", bucket, key, size, ...putOptions });
        let committed = false;
        let written = 0;
        try {
            for await (const part of source instanceof Uint8Array ? single(source) : source) {
                signal?.throwIfAborted();
                if (!(part instanceof Uint8Array) || written + part.byteLength > size)
                    throw new RangeError("stream exceeds declared size or contains invalid bytes");
                for (let offset = 0; offset < part.byteLength; offset += EMBEDDED_STREAM_CHUNK_BYTES) {
                    const chunk = part.slice(offset, offset + EMBEDDED_STREAM_CHUNK_BYTES);
                    const length = chunk.byteLength;
                    await this.invoke({ op: "writeObjectChunk", token }, chunk, { signal, transfer: true });
                    written += length;
                }
            }
            signal?.throwIfAborted();
            if (written !== size)
                throw new RangeError("stream differs from declared size");
            const object = await this.invoke({ op: "commitObjectWrite", token }, undefined, { signal });
            committed = true;
            return fromBridgeObject(object);
        }
        finally {
            if (!committed)
                await this.cleanup("abortObjectWrite", token);
        }
    }
    async openRead(bucket, key, options = {}) {
        options.signal?.throwIfAborted();
        const opened = await this.invoke({ op: "openObjectRead", bucket, key });
        const token = opened.token;
        const object = fromBridgeObject(opened.object);
        let closed = false;
        const close = async () => {
            if (closed)
                return;
            closed = true;
            await this.cleanup("closeObjectRead", token);
        };
        if (options.signal?.aborted) {
            await close();
            options.signal.throwIfAborted();
        }
        const invoke = this.invoke;
        const chunks = (async function* () {
            let read = 0;
            try {
                while (!closed && read < object.size) {
                    options.signal?.throwIfAborted();
                    const length = Math.min(EMBEDDED_STREAM_CHUNK_BYTES, object.size - read);
                    const result = await invoke({ op: "readObjectChunk", token, length }, undefined, options);
                    if (result.data === undefined || result.data.byteLength === 0 || result.data.byteLength > length) {
                        throw new EmbeddedStowError("protocol", "object stream returned an invalid chunk");
                    }
                    read += result.data.byteLength;
                    yield result.data;
                    if (result.eof && read !== object.size)
                        throw new EmbeddedStowError("protocol", "object stream ended before its declared size");
                }
            }
            finally {
                await close();
            }
        })();
        return { object, chunks, close };
    }
    async cleanup(op, token) {
        try {
            await this.invoke({ op, token });
        }
        catch {
            // Closing the runtime or terminating its worker also releases stream resources.
        }
    }
}
async function* single(bytes) { yield bytes; }
//# sourceMappingURL=async-streams.js.map