import { BROWSER_PERSISTENCE_FORMAT_VERSION, BrowserPersistenceError, openBrowserEmbeddedStow, type BrowserEmbeddedStow, type PersistenceAdapter, type PersistenceChange, type PersistenceSnapshot } from "../dist/browser.js";
import type { EmbeddedHost } from "../dist/index.js";

interface StoredObject {
  data: Uint8Array;
  size: number;
  etag: string;
  contentType?: string;
  metadata?: Record<string, string>;
  lastModified: string;
}

export class TransactionalPersistence implements PersistenceAdapter {
  static readonly owners = new Set<string>();
  readonly events: string[];
  generation = 0;
  snapshot: PersistenceSnapshot = emptySnapshot();
  failNextCommit = false;
  opened = false;
  closes = 0;

  constructor(events: string[]) {
    this.events = events;
  }

  async open(namespace: string): Promise<void> {
    this.events.push(`open:${namespace}`);
    if (TransactionalPersistence.owners.has(namespace)) {
      throw new BrowserPersistenceError("already_open", "namespace is owned");
    }
    TransactionalPersistence.owners.add(namespace);
    this.opened = true;
  }

  async load(namespace: string): Promise<PersistenceSnapshot | null> {
    this.events.push(`load:${namespace}`);
    this.requireOpen();
    return this.snapshot;
  }

  async commit(
    namespace: string,
    expectedGeneration: number,
    changes: PersistenceChange[],
  ): Promise<number> {
    this.events.push(`commit:start:${namespace}`);
    this.requireOpen();
    if (this.failNextCommit) {
      this.failNextCommit = false;
      throw new BrowserPersistenceError("persistence_error", "injected commit failure");
    }
    if (expectedGeneration !== this.generation) {
      throw new BrowserPersistenceError("persistence_error", "generation conflict");
    }
    this.snapshot = applyChanges(this.snapshot, changes);
    this.generation += 1;
    this.snapshot = { ...this.snapshot, generation: this.generation };
    this.events.push(`commit:end:${namespace}`);
    return this.generation;
  }

  async clear(namespace: string, expectedGeneration: number): Promise<number> {
    this.events.push(`clear:${namespace}`);
    this.requireOpen();
    if (expectedGeneration !== this.generation) {
      throw new BrowserPersistenceError("persistence_error", "generation conflict");
    }
    this.generation += 1;
    this.snapshot = emptySnapshot(this.generation);
    return this.generation;
  }

  async close(): Promise<void> {
    this.events.push("close");
    this.closes += 1;
    if (this.opened) {
      TransactionalPersistence.owners.delete(this.namespace());
      this.opened = false;
    }
  }

  private namespace(): string {
    return this.events.find((event) => event.startsWith("open:"))?.slice(5) ?? "";
  }

  private requireOpen(): void {
    if (!this.opened) {
      throw new BrowserPersistenceError("closed", "persistence fake is closed");
    }
  }
}

export class MemoryHost implements EmbeddedHost {
  readonly events: string[];
  private readonly buckets = new Map<string, Map<string, StoredObject>>();

  constructor(events: string[]) {
    this.events = events;
  }

  call(request: string): string {
    const parsed = JSON.parse(request) as Record<string, unknown>;
    this.events.push(`inner:${String(parsed.op)}`);
    switch (parsed.op) {
      case "open":
        return response({
          handle: 1,
          capabilities: {
            backend: "memory",
            maxBytes: 1_000_000,
            maxObjects: 100,
            persistent: false,
            multipart: false,
            upstream: false,
          },
        });
      case "createBucket":
        this.createBucket(stringValue(parsed, "bucket"));
        return response(undefined);
      case "deleteBucket":
        this.buckets.delete(stringValue(parsed, "bucket"));
        return response(undefined);
      case "listBuckets":
        return response({
          buckets: [...this.buckets.keys()].sort().map((name) => ({ name })),
        });
      case "putObject":
        return response(this.put(stringValue(parsed, "bucket"), stringValue(parsed, "key"), parsed));
      case "getObject":
        return response(this.get(stringValue(parsed, "bucket"), stringValue(parsed, "key"), true));
      case "headObject":
        return response(this.get(stringValue(parsed, "bucket"), stringValue(parsed, "key"), false));
      case "listObjects":
        return response(this.list(stringValue(parsed, "bucket"), parsed.list));
      case "deleteObject":
        this.buckets.get(stringValue(parsed, "bucket"))?.delete(stringValue(parsed, "key"));
        return response(undefined);
      case "copyObject":
        return response(this.copy(parsed));
      case "usage":
        return response(this.usage());
      case "reset":
        this.buckets.clear();
        this.events.push("inner:reset-state");
        return response(undefined);
      case "close":
        return response(undefined);
      default:
        throw new Error(`unexpected operation ${String(parsed.op)}`);
    }
  }

  private createBucket(name: string): void {
    if (this.buckets.has(name)) {
      throw new Error(`bucket exists: ${name}`);
    }
    this.buckets.set(name, new Map());
  }

  private put(bucket: string, key: string, request: Record<string, unknown>): BridgeObject {
    const objects = this.buckets.get(bucket);
    if (objects === undefined) throw new Error(`bucket not found: ${bucket}`);
    const data = Buffer.from(stringValue(request, "data"), "base64");
    const metadata = stringRecord(request.metadata);
    const contentType = optionalString(request.contentType);
    const object: StoredObject = {
      data: new Uint8Array(data),
      size: data.byteLength,
      etag: `etag-${key}`,
      ...(contentType === undefined ? {} : { contentType }),
      ...(metadata === undefined ? {} : { metadata }),
      lastModified: "2026-01-01T00:00:00.000Z",
    };
    objects.set(key, object);
    return bridgeObject(bucket, key, object, true);
  }

  private get(bucket: string, key: string, includeData: boolean): BridgeObject {
    const object = this.buckets.get(bucket)?.get(key);
    if (object === undefined) throw new Error(`object not found: ${bucket}/${key}`);
    return bridgeObject(bucket, key, object, includeData);
  }

  private list(bucket: string, rawOptions: unknown): BridgePage {
    const objects = this.buckets.get(bucket);
    if (objects === undefined) throw new Error(`bucket not found: ${bucket}`);
    const options = isRecord(rawOptions) ? rawOptions : {};
    const prefix = optionalString(options.prefix) ?? "";
    const offset = Number.parseInt(optionalString(options.cursor) ?? "0", 10);
    const limit = typeof options.limit === "number" ? options.limit : 1_000;
    const entries = [...objects.entries()]
      .filter(([key]) => key.startsWith(prefix))
      .sort(([left], [right]) => left.localeCompare(right));
    const page = entries.slice(offset, offset + limit);
    const nextOffset = offset + page.length;
    const truncated = nextOffset < entries.length;
    return {
      objects: page.map(([key, object]) => bridgeObject(bucket, key, object, false)),
      truncated,
      ...(truncated ? { nextCursor: String(nextOffset) } : {}),
    };
  }

  private copy(request: Record<string, unknown>): BridgeObject {
    const sourceBucket = stringValue(request, "sourceBucket");
    const sourceKey = stringValue(request, "sourceKey");
    const source = this.buckets.get(sourceBucket)?.get(sourceKey);
    const destinationBucket = stringValue(request, "destinationBucket");
    const destinationKey = stringValue(request, "destinationKey");
    const destination = this.buckets.get(destinationBucket);
    if (source === undefined || destination === undefined) {
      throw new Error("copy source or destination not found");
    }
    destination.set(destinationKey, { ...source, data: new Uint8Array(source.data) });
    return bridgeObject(destinationBucket, destinationKey, source, true);
  }

  private usage(): { bytes: number; objects: number } {
    let bytes = 0;
    let objects = 0;
    for (const bucket of this.buckets.values()) {
      for (const object of bucket.values()) {
        bytes += object.data.byteLength;
        objects += 1;
      }
    }
    return { bytes, objects };
  }
}

interface BridgeObject {
  bucket: string;
  key: string;
  data?: string;
  size: number;
  etag: string;
  contentType?: string;
  metadata?: Record<string, string>;
  lastModified: string;
}

interface BridgePage {
  objects: BridgeObject[];
  truncated: boolean;
  nextCursor?: string;
}

function bridgeObject(
  bucket: string,
  key: string,
  object: StoredObject,
  includeData: boolean,
): BridgeObject {
  return {
    bucket,
    key,
    ...(includeData ? { data: Buffer.from(object.data).toString("base64") } : {}),
    size: object.size,
    etag: object.etag,
    ...(object.contentType === undefined ? {} : { contentType: object.contentType }),
    ...(object.metadata === undefined ? {} : { metadata: object.metadata }),
    lastModified: object.lastModified,
  };
}

function response(result: unknown, version = 1): string {
  return JSON.stringify({ version, ok: true, result });
}

function stringValue(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  if (typeof value !== "string") throw new Error(`${key} is not a string`);
  return value;
}

function optionalString(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

function stringRecord(value: unknown): Record<string, string> | undefined {
  if (!isRecord(value)) return undefined;
  const result: Record<string, string> = {};
  for (const [key, item] of Object.entries(value)) {
    if (typeof item !== "string") return undefined;
    result[key] = item;
  }
  return result;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function deleteBucketObjects(
  objects: Map<string, { bucket: string; key: string }>,
  bucket: string,
): void {
  const prefix = `${bucket}\u0000`;
  for (const key of objects.keys()) {
    if (key.startsWith(prefix)) objects.delete(key);
  }
}

export function emptySnapshot(generation = 0): PersistenceSnapshot {
  return {
    formatVersion: BROWSER_PERSISTENCE_FORMAT_VERSION,
    generation,
    buckets: [],
    objects: [],
  };
}

function applyChanges(
  snapshot: PersistenceSnapshot,
  changes: PersistenceChange[],
): PersistenceSnapshot {
  const buckets = new Map(snapshot.buckets.map((bucket) => [bucket.name, bucket]));
  const objects = new Map(
    snapshot.objects.map((object) => [`${object.bucket}\u0000${object.key}`, object]),
  );
  for (const change of changes) {
    if (change.type === "putBucket") buckets.set(change.bucket.name, change.bucket);
    if (change.type === "deleteBucket") {
      buckets.delete(change.bucket);
      deleteBucketObjects(objects, change.bucket);
    }
    if (change.type === "putObject") {
      objects.set(`${change.object.bucket}\u0000${change.object.key}`, change.object);
    }
    if (change.type === "deleteObject") {
      objects.delete(`${change.bucket}\u0000${change.key}`);
    }
  }
  return {
    ...snapshot,
    buckets: [...buckets.values()],
    objects: [...objects.values()],
  };
}

export async function openProfile(events: string[]): Promise<{
  adapter: TransactionalPersistence;
  stow: BrowserEmbeddedStow;
}> {
  const adapter = new TransactionalPersistence(events);
  const stow = await openBrowserEmbeddedStow(new MemoryHost(events), { namespace: "profile", persistence: adapter });
  return { adapter, stow };
}
