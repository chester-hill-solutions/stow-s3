import { EmbeddedStowError, fromBridgeObject, type BridgeObject, type EmbeddedObject, type EmbeddedPutOptions } from "./embedded.js";
import type { WorkerCallOptions } from "./worker-host.js";

export const EMBEDDED_STREAM_CHUNK_BYTES = 256 * 1024;

export interface EmbeddedStreamPutOptions extends EmbeddedPutOptions {
  size: number;
  signal?: AbortSignal;
}

export interface EmbeddedObjectStream {
  object: EmbeddedObject;
  chunks: AsyncIterable<Uint8Array>;
  close(): Promise<void>;
}

export interface AsyncOperationInvoker {
  <T>(request: Record<string, unknown>, data?: Uint8Array, options?: WorkerCallOptions): Promise<T>;
}

export class EmbeddedStreamOperations {
  constructor(private readonly invoke: AsyncOperationInvoker) {}

  async put(bucket: string, key: string, source: Uint8Array | AsyncIterable<Uint8Array>, options: EmbeddedStreamPutOptions): Promise<EmbeddedObject> {
    const { size, signal, ...putOptions } = options;
    if (!Number.isSafeInteger(size) || size < 0) throw new RangeError("stream size must be a non-negative safe integer");
    signal?.throwIfAborted();
    // Capture the token before honouring cancellation, so a successful open can be cleaned up.
    const { token } = await this.invoke<{ token: string }>({ op: "openObjectWrite", bucket, key, size, ...putOptions });
    let written = 0;
    try {
      for await (const part of source instanceof Uint8Array ? single(source) : source) {
        signal?.throwIfAborted();
        if (!(part instanceof Uint8Array) || written + part.byteLength > size) throw new RangeError("stream exceeds declared size or contains invalid bytes");
        for (let offset = 0; offset < part.byteLength; offset += EMBEDDED_STREAM_CHUNK_BYTES) {
          const chunk = part.slice(offset, offset + EMBEDDED_STREAM_CHUNK_BYTES);
          const length = chunk.byteLength;
          await this.invoke<void>({ op: "writeObjectChunk", token }, chunk, { signal, transfer: true });
          written += length;
        }
      }
      signal?.throwIfAborted();
      if (written !== size) throw new RangeError("stream differs from declared size");
      const object = await this.invoke<BridgeObject>({ op: "commitObjectWrite", token }, undefined, { signal });
      return fromBridgeObject(object);
    } catch (error) {
      try { await this.cleanup("abortObjectWrite", token); }
      catch (cleanupError) { throw new AggregateError([error, cleanupError], "stream write and cleanup both failed", { cause: cleanupError }); }
      throw error;
    }
  }

  async openRead(bucket: string, key: string, options: { signal?: AbortSignal } = {}): Promise<EmbeddedObjectStream> {
    options.signal?.throwIfAborted();
    const opened = await this.invoke<{ token: string; object: BridgeObject }>({ op: "openObjectRead", bucket, key });
    const token = opened.token;
    const object = fromBridgeObject(opened.object);
    let closed = false;
    let closePromise: Promise<void> | undefined;
    const close = () => {
      if (closePromise !== undefined) return closePromise;
      closed = true;
      closePromise = this.cleanup("closeObjectRead", token).catch((error: unknown) => {
        closed = false;
        closePromise = undefined;
        throw error;
      });
      return closePromise;
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
          const result = await invoke<{ eof: boolean; data?: Uint8Array }>({ op: "readObjectChunk", token, length }, undefined, options);
          if (result.data === undefined || result.data.byteLength === 0 || result.data.byteLength > length) {
            throw new EmbeddedStowError("protocol", "object stream returned an invalid chunk");
          }
          read += result.data.byteLength;
          yield result.data;
          if (result.eof && read !== object.size) throw new EmbeddedStowError("protocol", "object stream ended before its declared size");
        }
      } finally {
        await close();
      }
    })();
    return { object, chunks, close };
  }

  private async cleanup(op: string, token: string): Promise<void> {
    try { await this.invoke<void>({ op, token }); } catch (error) {
      // A closed host/runtime has already released resources. Admission and
      // persistence failures still need acknowledgement or a caller retry.
      if (!(error instanceof EmbeddedStowError && error.code === "closed")) throw error;
    }
  }
}

async function* single(bytes: Uint8Array): AsyncIterable<Uint8Array> { yield bytes; }
