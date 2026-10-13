import { reconcileRemote } from "./remote-reconciliation.js";
import { contentHash } from "./content-store.js";
import type { ContentRecord } from "./content-types.js";
import { FileHistoryError, type ObjectSyncRemote, type SyncRemoteObject } from "./file-history-types.js";
import { type ProjectStorage } from "./project-storage.js";
import { ProjectError, type ProjectAttempt, type ProjectStatus } from "./project-types.js";

type Exclusive = <T>(operation: () => Promise<T>) => Promise<T>;
interface PreparedProject { attempt: ProjectAttempt; record: ContentRecord; reconcile: boolean }

export async function syncProject(
  storage: ProjectStorage, remote: ObjectSyncRemote, bucket: string, exclusive: Exclusive,
): Promise<ProjectStatus> {
  const prepared = await exclusive(() => prepare(storage));
  if (prepared) {
    await publishRemote(storage, prepared, remote, bucket, exclusive);
    if (prepared.reconcile) {
      const next = await exclusive(() => prepare(storage));
      if (next) await publishRemote(storage, next, remote, bucket, exclusive);
    }
  }
  return exclusive(async () => storage.status(await storage.load()));
}
async function prepare(storage: ProjectStorage): Promise<PreparedProject | null> {
  let loaded = await storage.load();
  const reconcile = loaded.state.attempt !== null;
  let attempt = loaded.state.attempt;
  if (!attempt) {
    const revisionId = storage.current(loaded.state);
    if (!revisionId || revisionId === loaded.state.syncedVersionId) return null;
    attempt = { id: crypto.randomUUID(), revisionId, basis: loaded.state.basis };
    loaded = await storage.publish(loaded, { ...loaded.state, attempt });
  }
  return { attempt, record: await storage.requireRecord(attempt.revisionId), reconcile };
}
async function publishRemote(
  storage: ProjectStorage, prepared: PreparedProject, remote: ObjectSyncRemote, bucket: string, exclusive: Exclusive,
): Promise<void> {
  const prefix = `stow-projects/${encodeURIComponent(storage.options.projectId)}`;
  const key = `${prefix}/manifest.json`;
  const digest = await contentHash(prepared.record.metadata);
  const reconciled = () => reconcileRemote(
    () => remote.getObject(bucket, key),
    (object) => matchesManifest(object, prepared, digest),
    (object) => acknowledge(storage, prepared.attempt, object.etag, exclusive),
  );
  if (prepared.reconcile && await reconciled()) return;
  await publishChunks(storage, prepared.record, remote, { bucket, prefix });
  try {
    const result = await remote.putObject(bucket, key, prepared.record.metadata, {
      basis: prepared.attempt.basis, saveId: prepared.attempt.id, contentType: "application/json",
      metadata: { "stow-project-revision": prepared.attempt.revisionId, "stow-project-sha256": digest },
    });
    await acknowledge(storage, prepared.attempt, result.etag, exclusive);
  } catch (error) {
    if (!(error instanceof FileHistoryError && error.code === "conflict")) throw error;
    if (await reconciled()) return;
    await exclusive(async () => {
      const loaded = await matching(storage, prepared.attempt);
      await storage.publish(loaded, { ...loaded.state, attempt: null });
    });
    throw new ProjectError("conflict", "remote project changed; local revisions are retained");
  }
}
async function publishChunks(
  storage: ProjectStorage, record: ContentRecord, remote: ObjectSyncRemote, target: { bucket: string; prefix: string },
): Promise<void> {
  const unique = new Map(record.contents.flatMap((content) => content.chunks).map((chunk) => [chunk.hash, chunk]));
  for (const chunk of unique.values()) {
    const key = `${target.prefix}/chunks/${chunk.hash}`;
    if (await remoteHasChunk(remote, target.bucket, key, chunk)) continue;
    const bytes = await storage.store.readRange({ size: chunk.size, chunks: [chunk] });
    try {
      await remote.putObject(target.bucket, key, bytes, {
        basis: { absent: true }, saveId: chunk.hash, contentType: "application/octet-stream", sha256: chunk.hash, metadata: { "stow-content-sha256": chunk.hash },
      });
    } catch (error) {
      if (!(error instanceof FileHistoryError && error.code === "conflict")) throw error;
      const observed = await remote.getObject(target.bucket, key);
      if (!observed || observed.data.byteLength !== chunk.size || await contentHash(observed.data) !== chunk.hash) {
        throw new ProjectError("conflict", "remote immutable content does not match its identity");
      }
    }
  }
}
async function remoteHasChunk(remote: ObjectSyncRemote, bucket: string, key: string, chunk: { hash: string; size: number }): Promise<boolean> {
  if (remote.headObject) {
    const head = await remote.headObject(bucket, key);
    if (head === null) return false;
    if (head.size === chunk.size && head.sha256 === chunk.hash) return true;
  }
  const observed = await remote.getObject(bucket, key);
  return observed !== null && observed.data.byteLength === chunk.size && await contentHash(observed.data) === chunk.hash;
}
function matchesManifest(object: SyncRemoteObject | null, prepared: PreparedProject, digest: string): object is SyncRemoteObject {
  return object !== null && object.etag.length > 0 && object.saveId === prepared.attempt.id
    && object.metadata?.["stow-project-revision"] === prepared.attempt.revisionId
    && object.metadata?.["stow-project-sha256"] === digest && object.contentType === "application/json"
    && object.data.byteLength === prepared.record.metadata.byteLength
    && object.data.every((byte, index) => byte === prepared.record.metadata[index]);
}
async function matching(storage: ProjectStorage, attempt: ProjectAttempt) {
  const loaded = await storage.load();
  if (loaded.state.attempt?.id !== attempt.id) throw new ProjectError("uncertain_write", "project attempt changed before acknowledgement");
  return loaded;
}
async function acknowledge(storage: ProjectStorage, attempt: ProjectAttempt, etag: string, exclusive: Exclusive): Promise<void> {
  if (!etag) throw new ProjectError("uncertain_write", "remote returned no confirmed identity");
  await exclusive(async () => {
    const loaded = await matching(storage, attempt);
    await storage.publish(loaded, { ...loaded.state, attempt: null, basis: { etag }, syncedVersionId: attempt.revisionId });
  });
}
