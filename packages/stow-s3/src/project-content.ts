import { type ContentStore } from "./content-store.js";
import type { ContentReference } from "./content-types.js";
import { ProjectError } from "./project-types.js";

/** Fixed-length replacement hashes and stages only the overlapping chunks. */
export async function replaceContentRange(
  store: ContentStore, content: ContentReference,
  replacement: { offset: number; data: Uint8Array; stageId: string; signal?: AbortSignal },
): Promise<ContentReference> {
  const { offset, data, stageId, signal } = replacement;
  if (!Number.isSafeInteger(offset) || offset < 0 || offset + data.byteLength > content.size) throw new ProjectError("invalid_options", "range replacement must fit the existing file");
  const chunks = [];
  let start = 0;
  for (const chunk of content.chunks) {
    const end = start + chunk.size;
    if (end > offset && start < offset + data.byteLength) {
      const bytes = await store.readRange({ size: chunk.size, chunks: [chunk] });
      const first = Math.max(offset, start);
      const last = Math.min(offset + data.byteLength, end);
      bytes.set(data.subarray(first - offset, last - offset), first - start);
      const staged = await store.stage(bytes, { stageId, signal });
      chunks.push(...staged.content.chunks);
    } else chunks.push(chunk);
    start = end;
  }
  return { size: content.size, chunks };
}
