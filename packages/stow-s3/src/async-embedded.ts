import { EmbeddedStreamOperations, type EmbeddedObjectStream, type EmbeddedStreamPutOptions } from "./async-streams.js";
import type { ContentProtection, ContentUsage } from "./content-types.js";
import {
  decodeEmbeddedResponse,
  EMBEDDED_BINARY_PROTOCOL_VERSION,
  EmbeddedStowError,
  fromBridgeObject,
  type BridgeObject,
  type EmbeddedBucket,
  type EmbeddedCapabilities,
  type EmbeddedListOptions,
  type EmbeddedObject,
  type EmbeddedObjectPage,
  type EmbeddedPutOptions,
  type EmbeddedStowOptions,
  type EmbeddedUsage,
} from "./embedded.js";
import type {
  BridgeSaveObservation,
  BridgeSaveResult,
  EmbeddedSaveObservation,
  EmbeddedSaveOptions,
  EmbeddedSaveResult,
} from "./embedded-saves.js";
import type { AsyncEmbeddedHost, WorkerCallOptions } from "./worker-host.js";

export interface PersistentAsyncEmbeddedOptions extends EmbeddedStowOptions {
  namespace: string;
  databaseName?: string;
  cacheBytes?: number;
  physicalQuotaBytes?: number;
  maxStreams?: number;
}

export class AsyncEmbeddedStow {
  private closed = false;

  private constructor(
    private readonly host: AsyncEmbeddedHost,
    readonly handle: number,
    private readonly runtimeCapabilities: EmbeddedCapabilities,
  ) {}

  static async open(host: AsyncEmbeddedHost, options: EmbeddedStowOptions = {}): Promise<AsyncEmbeddedStow> {
    const result = await invoke<{ handle: number; capabilities: EmbeddedCapabilities }>(host, {
      op: "open", options: { backend: "memory", ...options },
    });
    return AsyncEmbeddedStow.attach(host, result.handle, result.capabilities);
  }

  static async openPersistent(host: AsyncEmbeddedHost, options: PersistentAsyncEmbeddedOptions): Promise<AsyncEmbeddedStow> {
    const result = await invoke<{ handle: number; capabilities: EmbeddedCapabilities }>(host, { op: "openHost", options });
    return AsyncEmbeddedStow.attach(host, result.handle, result.capabilities);
  }

  /** Attach the receipt from a host-backed open without opening a second runtime. */
  static attach(host: AsyncEmbeddedHost, handle: number, capabilities: EmbeddedCapabilities): AsyncEmbeddedStow {
    return new AsyncEmbeddedStow(host, handle, capabilities);
  }

  capabilities(): EmbeddedCapabilities {
    this.ensureOpen();
    return { ...this.runtimeCapabilities };
  }

  collectGarbage(): Promise<ContentUsage> {
    return this.maintain({ op: "collectGarbage" });
  }

  inspectRecovery(): Promise<{ stages: string[]; protections: ContentProtection[] }> {
    return this.maintain({ op: "inspectRecovery" });
  }

  discardAbandonedStage(stageId: string): Promise<void> {
    return this.maintain({ op: "discardAbandonedStage", stageId });
  }

  private maintain<T>(request: Record<string, unknown>): Promise<T> {
    this.ensureOpen();
    if (this.runtimeCapabilities.backend !== "host" || this.runtimeCapabilities.hostMaintenance !== true) {
      return Promise.reject(new EmbeddedStowError("unsupported_backend", "maintenance requires the persistent worker host"));
    }
    return this.invoke<T>(request);
  }

  usage(options?: WorkerCallOptions): Promise<EmbeddedUsage> {
    return this.invoke({ op: "usage" }, undefined, options);
  }

  createBucket(bucket: string): Promise<void> { return this.invoke({ op: "createBucket", bucket }); }
  deleteBucket(bucket: string): Promise<void> { return this.invoke({ op: "deleteBucket", bucket }); }
  deleteObject(bucket: string, key: string): Promise<void> { return this.invoke({ op: "deleteObject", bucket, key }); }
  reset(): Promise<void> { return this.invoke({ op: "reset" }); }

  async listBuckets(): Promise<EmbeddedBucket[]> {
    return (await this.invoke<{ buckets: EmbeddedBucket[] }>({ op: "listBuckets" })).buckets;
  }

  async putObject(bucket: string, key: string, data: Uint8Array, options: EmbeddedPutOptions = {}, call?: WorkerCallOptions): Promise<EmbeddedObject> {
    if ((options.ifMatch || options.ifNoneMatch) && this.runtimeCapabilities.conditionalWrites !== true) {
      throw new EmbeddedStowError("conditional_write_unsupported", "host does not support conditional writes");
    }
    return fromBridgeObject(await this.invoke<BridgeObject>({ op: "putObject", bucket, key, ...options }, data, call));
  }

  async getObject(bucket: string, key: string, options?: WorkerCallOptions): Promise<EmbeddedObject> {
    return fromBridgeObject(await this.invoke<BridgeObject>({ op: "getObject", bucket, key }, undefined, options));
  }

  async getObjectRange(bucket: string, key: string, range: { offset: number; length: number }, options?: WorkerCallOptions): Promise<EmbeddedObject> {
    return fromBridgeObject(await this.invoke<BridgeObject>({ op: "getObjectRange", bucket, key, range }, undefined, options));
  }

  async readForSave(bucket: string, key: string, call?: WorkerCallOptions): Promise<EmbeddedSaveObservation> {
    this.requireGuardedSaves();
    const result = await this.invoke<BridgeSaveObservation>({ op: "readForSave", bucket, key }, undefined, call);
    return {
      object: fromBridgeObject(result.object),
      absent: result.absent,
      observationToken: result.observationToken,
    };
  }

  async saveObject(
    bucket: string,
    key: string,
    data: Uint8Array,
    options: EmbeddedSaveOptions = {},
    call?: WorkerCallOptions,
  ): Promise<EmbeddedSaveResult> {
    this.requireGuardedSaves();
    if (options.replace !== true && !options.observationToken) {
      throw new EmbeddedStowError("invalid_save_condition", "a save needs an observation token, or replace: true");
    }
    if (options.requestKey && this.runtimeCapabilities.durableSaveRequests !== true) {
      throw new EmbeddedStowError("save_requests_unsupported", "a request key needs a host that retains save requests");
    }
    const result = await this.invoke<BridgeSaveResult>({ op: "saveObject", bucket, key, replace: options.replace === true, observationToken: options.observationToken, contentType: options.contentType, metadata: options.metadata, requestKey: options.requestKey }, data, call);
    return { object: fromBridgeObject(result.object), outcome: result.outcome, replayed: result.replayed };
  }

  async resolveSave(bucket: string, key: string, requestKey: string, call?: WorkerCallOptions): Promise<EmbeddedSaveResult> {
    if (this.runtimeCapabilities.durableSaveRequests !== true) {
      throw new EmbeddedStowError("save_requests_unsupported", "host does not retain save requests");
    }
    const result = await this.invoke<BridgeSaveResult>({ op: "resolveSave", bucket, key, requestKey }, undefined, call);
    return { object: fromBridgeObject(result.object), outcome: result.outcome, replayed: result.replayed };
  }

  async releaseObservation(observationToken: string, call?: WorkerCallOptions): Promise<void> {
    await this.invoke<void>({ op: "releaseObservation", observationToken }, undefined, call);
  }

  private requireGuardedSaves(): void {
    if (this.runtimeCapabilities.guardedSaves !== true) {
      throw new EmbeddedStowError("guarded_saves_unsupported", "host does not support observation-based saves");
    }
  }

  putObjectStream(bucket: string, key: string, source: Uint8Array | AsyncIterable<Uint8Array>, options: EmbeddedStreamPutOptions): Promise<EmbeddedObject> {
    if ((options.ifMatch || options.ifNoneMatch) && this.runtimeCapabilities.conditionalWrites !== true) {
      return Promise.reject(new EmbeddedStowError("conditional_write_unsupported", "host does not support conditional writes"));
    }
    return this.streams().put(bucket, key, source, options);
  }

  getObjectStream(bucket: string, key: string, options: { signal?: AbortSignal } = {}): Promise<EmbeddedObjectStream> {
    return this.streams().openRead(bucket, key, options);
  }

  private streams(): EmbeddedStreamOperations {
    return new EmbeddedStreamOperations(<T>(request: Record<string, unknown>, data?: Uint8Array, options?: WorkerCallOptions) => this.invoke<T>(request, data, options));
  }

  async headObject(bucket: string, key: string): Promise<EmbeddedObject> {
    return fromBridgeObject(await this.invoke<BridgeObject>({ op: "headObject", bucket, key }));
  }

  listObjects(bucket: string, list: EmbeddedListOptions = {}): Promise<EmbeddedObjectPage> {
    return this.invoke({ op: "listObjects", bucket, list });
  }

  async copyObject(sourceBucket: string, sourceKey: string, destinationBucket: string, destinationKey: string): Promise<EmbeddedObject> {
    return fromBridgeObject(await this.invoke<BridgeObject>({ op: "copyObject", sourceBucket, sourceKey, destinationBucket, destinationKey }));
  }

  async close(): Promise<void> {
    if (this.closed) return;
    await this.invoke<void>({ op: "close" });
    this.closed = true;
  }

  private invoke<T>(request: Record<string, unknown>, data?: Uint8Array, options?: WorkerCallOptions): Promise<T> {
    this.ensureOpen();
    return invoke<T>(this.host, { ...request, handle: this.handle }, data, options);
  }

  private ensureOpen(): void {
    if (this.closed) throw new EmbeddedStowError("closed", "embedded runtime is closed");
  }
}

async function invoke<T>(host: AsyncEmbeddedHost, request: Record<string, unknown>, data?: Uint8Array, options?: WorkerCallOptions): Promise<T> {
  const response = await host.callBinary(JSON.stringify({ ...request, version: EMBEDDED_BINARY_PROTOCOL_VERSION }), data, options);
  let parsed: unknown;
  try {
    parsed = JSON.parse(response.metadata);
  } catch {
    throw new EmbeddedStowError("protocol", "embedded host returned invalid JSON metadata");
  }
  return decodeEmbeddedResponse<T>(parsed, EMBEDDED_BINARY_PROTOCOL_VERSION, response.data);
}
