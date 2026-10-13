import {
  type PersistenceAdapter,
  type PersistenceChange,
  type PersistenceSnapshot,
  type ObjectSyncRemote,
  type SyncRemoteObject,
  type SyncRemoteWrite,
  FileHistoryError,
} from "../dist/browser.js";
import { applyChanges, emptySnapshot } from "../dist/persistence-format.js";

export class HistoryPersistence implements PersistenceAdapter {
  snapshot: PersistenceSnapshot = emptySnapshot(0);
  failNextCommit = false;
  commits = 0;
  opened = false;

  async open(): Promise<void> { this.opened = true; }
  async load(): Promise<PersistenceSnapshot> { return structuredClone(this.snapshot); }
  async close(): Promise<void> { this.opened = false; }

  async commit(_namespace: string, expectedGeneration: number, changes: PersistenceChange[]): Promise<number> {
    if (!this.opened || expectedGeneration !== this.snapshot.generation) throw new Error("generation or ownership conflict");
    if (this.failNextCommit) {
      this.failNextCommit = false;
      throw new Error("disk unavailable");
    }
    this.snapshot = { ...applyChanges(this.snapshot, changes), generation: expectedGeneration + 1 };
    this.commits += 1;
    return this.snapshot.generation;
  }

  async clear(_namespace: string, expectedGeneration: number): Promise<number> {
    this.snapshot = emptySnapshot(expectedGeneration + 1);
    return this.snapshot.generation;
  }
}

export class HistoryRemote implements ObjectSyncRemote {
  readonly objects = new Map<string, SyncRemoteObject>();
  online = true;
  writes = 0;
  loseNextReply = false;
  afterWrite?: () => void;

  seed(key: string, text: string): void {
    this.objects.set(key, { data: new TextEncoder().encode(text), etag: crypto.randomUUID(), contentType: "text/plain" });
  }

  async getObject(_bucket: string, key: string): Promise<SyncRemoteObject | null> {
    this.requireOnline();
    return structuredClone(this.objects.get(key) ?? null);
  }

  async putObject(_bucket: string, key: string, data: Uint8Array, options: SyncRemoteWrite): Promise<{ etag: string }> {
    this.requireOnline();
    const current = this.objects.get(key);
    const matches = "etag" in options.basis ? current?.etag === options.basis.etag : current === undefined;
    if (!matches) throw new FileHistoryError("conflict", "remote changed");
    const etag = crypto.randomUUID();
    this.objects.set(key, { data: new Uint8Array(data), etag, saveId: options.saveId, contentType: options.contentType, metadata: options.metadata });
    this.writes += 1;
    this.afterWrite?.();
    if (this.loseNextReply) {
      this.loseNextReply = false;
      throw new Error("connection lost after remote commit");
    }
    return { etag };
  }

  private requireOnline(): void {
    if (!this.online) throw new Error("offline");
  }
}
