import { type ContentStore } from "./content-store.js";
import type { ContentHead, ContentRecord, ContentReference } from "./content-types.js";
import type { EmbeddedBucket, EmbeddedObject } from "./embedded.js";
import { decodeContent, isRecord } from "./project-format.js";
import { EmbeddedStowError } from "./embedded.js";

export interface CatalogEntry { id: string; object: EmbeddedObject }
export interface ObjectCatalog { formatVersion: 1; buckets: EmbeddedBucket[]; objects: CatalogEntry[] }
export interface LoadedCatalog { head: ContentHead; catalog: ObjectCatalog }
export class ContentObjectCatalog {
  constructor(readonly store: ContentStore, private readonly headName: string) {}
  async load(): Promise<LoadedCatalog> {
    const head = await this.store.adapter.getHead(this.headName);
    if (!head.recordId) return { head, catalog: { formatVersion: 1, buckets: [], objects: [] } };
    const record = await this.store.adapter.getRecord(head.recordId);
    if (!record) throw corrupt("object catalogue is missing");
    const value: unknown = JSON.parse(new TextDecoder().decode(record.metadata));
    if (!isRecord(value) || value.formatVersion !== 1 || !Array.isArray(value.buckets) || !Array.isArray(value.objects)) throw corrupt("invalid object catalogue");
    const buckets = value.buckets.map((item: unknown) => {
      if (!isRecord(item) || typeof item.name !== "string") throw corrupt("invalid bucket metadata");
      return { name: item.name, creationDate: typeof item.creationDate === "string" ? item.creationDate : undefined };
    });
    const objects = value.objects.map((item: unknown) => {
      if (!isRecord(item) || typeof item.id !== "string") throw corrupt("invalid object entry");
      return { id: item.id, object: decodeObject(item.object) };
    });
    if (new Set(buckets.map((bucket) => bucket.name)).size !== buckets.length ||
      new Set(objects.map((entry) => JSON.stringify([entry.object.bucket, entry.object.key]))).size !== objects.length ||
      JSON.stringify([...record.references].sort()) !== JSON.stringify(objects.map((entry) => entry.id).sort())) throw corrupt("object catalogue references are inconsistent");
    return { head, catalog: { formatVersion: 1, buckets, objects } };
  }
  requireBucket(catalog: ObjectCatalog, bucket: string): void {
    if (!catalog.buckets.some((item) => item.name === bucket)) throw new EmbeddedStowError("bucket_not_found", "bucket is missing");
  }
  entry(catalog: ObjectCatalog, bucket: string, key: string): CatalogEntry {
    this.requireBucket(catalog, bucket);
    const entry = catalog.objects.find((item) => item.object.bucket === bucket && item.object.key === key);
    if (!entry) throw new EmbeddedStowError("object_not_found", "object is missing");
    return entry;
  }
  async content(entry: CatalogEntry): Promise<ContentReference> {
    const record = await this.store.adapter.getRecord(entry.id);
    if (!record || record.contents.length !== 1) throw corrupt("object content record is missing");
    const content = record.contents[0];
    if (!content) throw corrupt("object content is missing");
    if (content.size !== entry.object.size) throw corrupt("object size differs from its content");
    const descriptor: unknown = JSON.parse(new TextDecoder().decode(record.metadata));
    if (!isRecord(descriptor) || descriptor.formatVersion !== 1 || objectIdentity(decodeObject(descriptor.object)) !== objectIdentity(entry.object)) throw corrupt("object metadata differs from its catalogue entry");
    return content;
  }
  async publish(loaded: LoadedCatalog, catalog: ObjectCatalog, stageId: string = crypto.randomUUID(), records: ContentRecord[] = []): Promise<void> {
    await this.store.adapter.publish({
      head: this.headName, expectedGeneration: loaded.head.generation, stageId,
      record: { id: crypto.randomUUID(), metadata: new TextEncoder().encode(JSON.stringify(catalog)), contents: [], references: catalog.objects.map((entry) => entry.id) },
      additionalRecords: records,
    });
  }
  record(object: EmbeddedObject, content: ContentReference): ContentRecord {
    return { id: crypto.randomUUID(), metadata: new TextEncoder().encode(JSON.stringify({ formatVersion: 1, object })), contents: [content], references: [] };
  }
}
function objectIdentity(object: EmbeddedObject): string {
  return JSON.stringify([object.bucket, object.key, object.size, object.etag, object.contentType,
    object.lastModified, Object.entries(object.metadata ?? {}).sort(([a], [b]) => a.localeCompare(b))]);
}
function decodeObject(value: unknown): EmbeddedObject {
  if (!isRecord(value) || typeof value.bucket !== "string" || typeof value.key !== "string" ||
    typeof value.etag !== "string" || typeof value.size !== "number" || !Number.isSafeInteger(value.size) || value.size < 0) throw corrupt("invalid object metadata");
  let metadata: Record<string, string> | undefined;
  if (value.metadata !== undefined) {
    if (!isRecord(value.metadata) || !Object.values(value.metadata).every((item) => typeof item === "string")) throw corrupt("invalid user metadata");
    metadata = Object.fromEntries(Object.entries(value.metadata).map(([key, item]) => [key, String(item)]));
  }
  return { bucket: value.bucket, key: value.key, size: value.size, etag: value.etag,
    contentType: typeof value.contentType === "string" ? value.contentType : undefined,
    lastModified: typeof value.lastModified === "string" ? value.lastModified : undefined, metadata };
}
export function hostContent(value: unknown): ContentReference { return decodeContent(value); }
function corrupt(message: string): EmbeddedStowError { return new EmbeddedStowError("persistence_corrupt", message); }
