import { type ContentStore } from "./content-store.js";
import { ContentObjectCatalog, type ObjectCatalog } from "./content-object-catalog.js";
import type { ContentReference } from "./content-types.js";
import { decodeEmbeddedResponse, EmbeddedStowError, type EmbeddedBinaryResponse, type EmbeddedObject } from "./embedded.js";
import { isRecord } from "./project-format.js";
import type { HostObjectStoreBridge } from "./wasm-bridge.js";

interface PutSession { bucket: string; key: string; request: Record<string, unknown>; content: ContentReference }
interface ReadSession { recordId: string; content: ContentReference }
const queues = new WeakMap<ContentStore, Promise<void>>();

/** Host persistence beneath the shared Go object runtime; bytes are never replayed at open. */
export class ContentObjectHost implements HostObjectStoreBridge {
  /** Installed by the Go host-backed runtime to evaluate its shared atomic write predicates. */
  checkConditions?: (metadata: string) => string;
  private readonly catalog: ContentObjectCatalog;
  private readonly puts = new Map<string, PutSession>();
  private readonly reads = new Map<string, ReadSession>();
  private closed = false;
  constructor(readonly store: ContentStore, options: { name?: string; maxStreams?: number } = {}) {
    this.catalog = new ContentObjectCatalog(store, options.name ?? "embedded-object-catalog");
    this.maxStreams = options.maxStreams ?? 16;
    if (!Number.isSafeInteger(this.maxStreams) || this.maxStreams < 1) throw new RangeError("maxStreams must be positive");
  }
  private readonly maxStreams: number;
  hasActiveStage(stageId: string): boolean { return this.puts.has(stageId); }
  call(metadata: string, data: Uint8Array): Promise<EmbeddedBinaryResponse> {
    const captured = new Uint8Array(data);
    const result = (queues.get(this.store) ?? Promise.resolve()).then(async () => {
      try {
        const request: unknown = JSON.parse(metadata);
        if (!isRecord(request) || request.version !== 2 || typeof request.op !== "string") throw new EmbeddedStowError("invalid_request", "invalid host request");
        if (this.closed && request.op !== "close") throw new EmbeddedStowError("closed", "object host is closed");
        const output = await this.dispatch(request, captured);
        return { metadata: JSON.stringify({ version: 2, ok: true, result: output.result }), data: output.data };
      } catch (error) {
        const code = error instanceof EmbeddedStowError ? error.code : error instanceof Error && "code" in error ? String(error.code) : "persistence_error";
        return { metadata: JSON.stringify({ version: 2, ok: false, error: { code, message: error instanceof Error ? error.message : "host storage failed" } }) };
      }
    });
    queues.set(this.store, result.then(() => undefined, () => undefined));
    return result;
  }
  private async dispatch(request: Record<string, unknown>, data: Uint8Array): Promise<{ result: unknown; data?: Uint8Array }> {
    const op = String(request.op);
    if (op === "startPut") return { result: this.startPut(request) };
    if (op === "writePut") { await this.writePut(request, data); return { result: null }; }
    if (op === "commitPut") return { result: await this.commitPut(request) };
    if (op === "abortPut") { const token = field(request, "token"); await this.store.adapter.releaseStage(token); this.puts.delete(token); return { result: null }; }
    if (op === "readObject") return { result: null, data: await this.readObject(request) };
    if (op === "closeObject") { await this.closeRead(field(request, "token")); return { result: null }; }
    if (op === "close") { await this.close(); return { result: null }; }
    if (op === "openObject") return { result: await this.openObject(request) };
    return { result: await this.catalogOperation(op, request) };
  }
  private async catalogOperation(op: string, request: Record<string, unknown>): Promise<unknown> {
    const loaded = await this.catalog.load();
    const catalog = loaded.catalog;
    if (op === "listBuckets") return { buckets: catalog.buckets };
    const bucket = field(request, op === "copyObject" ? "destinationBucket" : "bucket");
    if (op === "createBucket") {
      if (catalog.buckets.some((item) => item.name === bucket)) throw new EmbeddedStowError("bucket_exists", "bucket exists");
      catalog.buckets.push({ name: bucket, creationDate: new Date().toISOString() });
    } else if (op === "deleteBucket") {
      this.catalog.requireBucket(catalog, bucket);
      if (catalog.objects.some((entry) => entry.object.bucket === bucket)) throw new EmbeddedStowError("bucket_not_empty", "bucket is not empty");
      catalog.buckets = catalog.buckets.filter((item) => item.name !== bucket);
    } else if (op === "listObjects") return this.listObjects(catalog, bucket, request.list);
    else if (op === "headObject") return this.catalog.entry(catalog, bucket, field(request, "key")).object;
    else if (op === "deleteObject") {
      this.catalog.requireBucket(catalog, bucket);
      const key = field(request, "key");
      catalog.objects = catalog.objects.filter((entry) => entry.object.bucket !== bucket || entry.object.key !== key);
    } else if (op === "copyObject") {
      this.catalog.requireBucket(catalog, bucket);
      const source = this.catalog.entry(catalog, field(request, "sourceBucket"), field(request, "sourceKey"));
      const object = { ...source.object, bucket, key: field(request, "destinationKey"), lastModified: new Date().toISOString() };
      const record = this.catalog.record(object, await this.catalog.content(source));
      catalog.objects = replaceEntry(catalog, { id: record.id, object });
      await this.catalog.publish(loaded, catalog, crypto.randomUUID(), [record]);
      return object;
    } else throw new EmbeddedStowError("invalid_request", `unknown host operation ${op}`);
    await this.catalog.publish(loaded, catalog);
    return null;
  }
  private listObjects(catalog: ObjectCatalog, bucket: string, raw: unknown) {
    this.catalog.requireBucket(catalog, bucket);
    const options = isRecord(raw) ? raw : {};
    const prefix = typeof options.prefix === "string" ? options.prefix : "";
    const delimiter = typeof options.delimiter === "string" ? options.delimiter : "";
    const after = typeof options.cursor === "string" ? options.cursor : "";
    const limit = typeof options.limit === "number" && options.limit > 0 ? options.limit : 1000;
    // One row per key, or per delimiter group, so a page can end on a group and the
    // cursor names that group rather than a key inside it. Handing back a key from
    // inside a group would repeat the group on the next page.
    const rows = new Map<string, EmbeddedObject | null>();
    for (const entry of catalog.objects) {
      const { object } = entry;
      if (object.bucket !== bucket || !object.key.startsWith(prefix)) continue;
      const rest = object.key.slice(prefix.length);
      const cut = delimiter === "" ? -1 : rest.indexOf(delimiter);
      if (cut >= 0) {
        rows.set(prefix + rest.slice(0, cut + delimiter.length), null);
        continue;
      }
      rows.set(object.key, object);
    }
    const ordered = [...rows.entries()].filter(([value]) => value > after).sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0));
    const page = ordered.slice(0, limit);
    const truncated = ordered.length > limit;
    return {
      objects: page.flatMap(([, object]) => (object === null ? [] : [object])),
      commonPrefixes: page.flatMap(([value, object]) => (object === null ? [value] : [])),
      truncated,
      nextCursor: truncated ? page[page.length - 1]?.[0] : undefined,
    };
  }
  private startPut(request: Record<string, unknown>): { token: string } {
    this.checkStreamLimit();
    const token = crypto.randomUUID();
    this.puts.set(token, { bucket: field(request, "bucket"), key: field(request, "key"), request, content: { size: 0, chunks: [] } });
    return { token };
  }
  private async writePut(request: Record<string, unknown>, data: Uint8Array): Promise<void> {
    const token = field(request, "token");
    const session = this.puts.get(token);
    if (!session || data.byteLength > 256 * 1024 || numberField(request, "offset") !== session.content.size) throw new EmbeddedStowError("invalid_request", "invalid staged write");
    const staged = await this.store.stage(data, { stageId: token });
    session.content.chunks.push(...staged.content.chunks);
    session.content.size += data.byteLength;
  }
  private async commitPut(request: Record<string, unknown>): Promise<EmbeddedObject> {
    const token = field(request, "token");
    const session = this.puts.get(token);
    if (!session || numberField(request, "size") !== session.content.size) throw new EmbeddedStowError("invalid_request", "staged size differs");
    const loaded = await this.catalog.load();
    this.catalog.requireBucket(loaded.catalog, session.bucket);
    const previous = loaded.catalog.objects.find((entry) => entry.object.bucket === session.bucket && entry.object.key === session.key);
    const ifMatch = session.request.ifMatch;
    const ifNoneMatch = session.request.ifNoneMatch;
    if (ifMatch || ifNoneMatch) {
      if (!this.checkConditions) throw new EmbeddedStowError("conditional_write_unsupported", "conditional writes require the shared runtime predicate");
      const result: unknown = JSON.parse(this.checkConditions(JSON.stringify({ ifMatch, ifNoneMatch, existing: previous ? { etag: previous.object.etag } : null })));
      decodeEmbeddedResponse<void>(result, 2);
    }
    const object: EmbeddedObject = { bucket: session.bucket, key: session.key, size: session.content.size, etag: field(request, "etag"), lastModified: field(request, "lastModified") };
    if (typeof session.request.contentType === "string") object.contentType = session.request.contentType;
    if (isRecord(session.request.metadata)) object.metadata = Object.fromEntries(Object.entries(session.request.metadata).map(([key, value]) => [key, String(value)]));
    const record = this.catalog.record(object, session.content);
    loaded.catalog.objects = replaceEntry(loaded.catalog, { id: record.id, object });
    await this.catalog.publish(loaded, loaded.catalog, token, [record]);
    this.puts.delete(token);
    return object;
  }
  private async openObject(request: Record<string, unknown>) {
    this.checkStreamLimit();
    const loaded = await this.catalog.load();
    const entry = this.catalog.entry(loaded.catalog, field(request, "bucket"), field(request, "key"));
    const content = await this.catalog.content(entry);
    const token = crypto.randomUUID();
    await this.store.adapter.protect(token, [entry.id], { lifetime: "handle" });
    this.reads.set(token, { recordId: entry.id, content });
    return { token, object: entry.object };
  }
  private async readObject(request: Record<string, unknown>): Promise<Uint8Array> {
    const session = this.reads.get(field(request, "token"));
    const start = numberField(request, "offset");
    const length = numberField(request, "length");
    if (!session || start < 0 || length < 0 || length > 256 * 1024 || start > session.content.size) throw new EmbeddedStowError("invalid_request", "invalid stream range");
    return this.store.readRange(session.content, start, Math.min(start + length, session.content.size));
  }
  private async closeRead(token: string): Promise<void> { await this.store.adapter.protect(token, [], { lifetime: "handle" }); this.reads.delete(token); }
  private checkStreamLimit(): void {
    if (this.puts.size + this.reads.size >= this.maxStreams) throw new EmbeddedStowError("quota_exceeded", "too many open streams");
  }
  private async close(): Promise<void> {
    for (const token of this.puts.keys()) await this.store.adapter.releaseStage(token);
    for (const token of this.reads.keys()) await this.closeRead(token);
    this.puts.clear(); this.closed = true;
  }
}
function field(request: Record<string, unknown>, key: string): string {
  const value = request[key];
  if (typeof value !== "string" || !value) throw new EmbeddedStowError("invalid_request", `missing ${key}`);
  return value;
}
function numberField(request: Record<string, unknown>, key: string): number {
  const value = request[key] ?? 0;
  if (typeof value !== "number" || !Number.isSafeInteger(value)) throw new EmbeddedStowError("invalid_request", `invalid ${key}`);
  return value;
}
function replaceEntry(catalog: ObjectCatalog, entry: { id: string; object: EmbeddedObject }) {
  return [...catalog.objects.filter((item) => item.object.bucket !== entry.object.bucket || item.object.key !== entry.object.key), entry];
}
