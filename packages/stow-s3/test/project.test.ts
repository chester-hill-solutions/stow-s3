import { deferred } from "./deferred.js";
import assert from "node:assert/strict";
import { test } from "node:test";
import { ContentStore } from "../src/content-store.js";
import { BrowserProject } from "../src/project.js";
import type { ObjectSyncRemote, SyncRemoteObject, SyncRemoteWrite } from "../src/file-history-types.js";
import { FileHistoryError } from "../src/file-history-types.js";
import { MemoryContentAdapter } from "./content-fixtures.js";

const bytes = (value: string) => new TextEncoder().encode(value);
async function fixture(maxVersions = 50, quota = Number.MAX_SAFE_INTEGER) {
  const adapter = new MemoryContentAdapter(undefined, quota);
  await adapter.open("project");
  const store = new ContentStore(adapter, { chunkBytes: 4, cacheBytes: 8 });
  const remote = new ProjectRemote();
  const project = await BrowserProject.open(store, { projectId: "example", maxVersions, remote, remoteBucket: "projects" });
  return { adapter, store, remote, project };
}
class ProjectRemote implements ObjectSyncRemote {
  readonly objects = new Map<string, SyncRemoteObject>();
  writes = 0;
  manifestWrites = 0;
  loseManifestReply = false;
  pause?: () => Promise<void>;
  async headObject(_bucket: string, key: string) {
    const item = this.objects.get(key);
    return item ? { etag: item.etag, size: item.data.byteLength, metadata: item.metadata } : null;
  }
  async getObject(_bucket: string, key: string) { return structuredClone(this.objects.get(key) ?? null); }
  async putObject(_bucket: string, key: string, data: Uint8Array, options: SyncRemoteWrite) {
    if (key.endsWith("manifest.json")) await this.pause?.();
    const previous = this.objects.get(key);
    if (("etag" in options.basis && previous?.etag !== options.basis.etag) || ("absent" in options.basis && previous)) throw new FileHistoryError("conflict", "remote changed");
    const etag = crypto.randomUUID();
    this.objects.set(key, { data: data.slice(), etag, contentType: options.contentType, metadata: options.metadata, saveId: options.saveId });
    this.writes++;
    if (key.endsWith("manifest.json")) {
      this.manifestWrites++;
      if (this.loseManifestReply) { this.loseManifestReply = false; throw new Error("lost reply"); }
    }
    return { etag };
  }
}

test("project commits multiple files atomically and rolls back a failed publication", async () => {
  const { adapter, project } = await fixture();
  await project.commit([{ type: "put", key: "doc", data: bytes("one") }, { type: "put", key: "asset", data: bytes("abcdabcd") }]);
  adapter.failNextPublish = true;
  await assert.rejects(project.commit([{ type: "put", key: "doc", data: bytes("two") }, { type: "delete", key: "asset" }]));
  assert.equal(new TextDecoder().decode(await project.read("doc")), "one");
  assert.equal((await project.listFiles()).length, 2);
  assert.equal((await project.status()).versions.length, 1);
});

test("large unchanged assets share chunks through document history and lazy reopen", async () => {
  const { adapter, store, project } = await fixture();
  await project.commit([{ type: "put", key: "asset", data: bytes("abcdefgh") }, { type: "put", key: "doc", data: bytes("one") }]);
  await project.commit([{ type: "put", key: "doc", data: bytes("two") }]);
  assert.equal((await adapter.usage()).chunks, 4);
  const reads = adapter.chunkReads;
  const reopened = await BrowserProject.open(new ContentStore(adapter), { projectId: "example" });
  await reopened.listFiles();
  assert.equal(adapter.chunkReads, reads);
  await project.undo();
  assert.equal(new TextDecoder().decode(await project.read("doc")), "one");
  await project.redo();
  assert.equal(new TextDecoder().decode(await project.read("doc")), "two");
  assert.ok(((await store.usage()).cacheBytes ?? 0) <= 8);
});

test("fixed range edits stage only changed chunks and preserve the old revision", async () => {
  const { adapter, project } = await fixture();
  await project.commit([{ type: "put", key: "file", data: bytes("abcdefghijkl") }]);
  const before = await adapter.usage();
  await project.commit([{ type: "replaceRange", key: "file", offset: 5, data: bytes("XY") }]);
  assert.equal((await adapter.usage()).chunks - before.chunks, 1);
  assert.equal(new TextDecoder().decode(await project.read("file")), "abcdeXYhijkl");
  await project.undo();
  assert.equal(new TextDecoder().decode(await project.read("file")), "abcdefghijkl");
});

test("stream pins survive pruning and GC until the reader releases them", async () => {
  const { adapter, project } = await fixture(2);
  await project.commit([{ type: "put", key: "file", data: bytes("abcdefghijkl") }]);
  const reader = project.stream("file")[Symbol.asyncIterator]();
  assert.equal(new TextDecoder().decode((await reader.next()).value), "abcd");
  await project.commit([{ type: "put", key: "file", data: bytes("new1") }]);
  await project.commit([{ type: "put", key: "file", data: bytes("new2") }]);
  await adapter.collect();
  assert.equal(new TextDecoder().decode((await reader.next()).value), "efgh");
  await reader.return?.();
  await adapter.collect();
  assert.equal((await adapter.usage()).chunks, 2);
});

test("slow publication permits local edits and acknowledges only its captured revision", async () => {
  const { project, remote } = await fixture();
  await project.commit([{ type: "put", key: "doc", data: bytes("one") }]);
  const started = deferred();
  const gate = deferred();
  remote.pause = async () => { started.resolve(); await gate.promise; };
  const syncing = project.sync();
  await started.promise;
  await project.commit([{ type: "put", key: "doc", data: bytes("two") }]);
  gate.resolve();
  assert.equal((await syncing).pending, true);
  assert.equal(new TextDecoder().decode(await project.read("doc")), "two");
  remote.pause = undefined;
  assert.equal((await project.sync()).pending, false);
  assert.equal(remote.manifestWrites, 2);
});

test("uncertain manifest attempts remain protected after restart and pruning", async () => {
  const { adapter, store, project, remote } = await fixture(2);
  await project.commit([{ type: "put", key: "doc", data: bytes("one") }]);
  remote.loseManifestReply = true;
  await assert.rejects(project.sync(), /lost reply/);
  await project.commit([{ type: "put", key: "doc", data: bytes("two") }]);
  await project.commit([{ type: "put", key: "doc", data: bytes("three") }]);
  await adapter.collect();
  const reopened = await BrowserProject.open(store, { projectId: "example", maxVersions: 2, remote, remoteBucket: "projects" });
  assert.equal((await reopened.status()).hasUncertainWrite, true);
  assert.equal((await reopened.sync()).pending, false);
  assert.equal(remote.manifestWrites, 2);
  assert.equal(new TextDecoder().decode(await reopened.read("doc")), "three");
});

test("repeated project sync uploads unchanged chunks once and refuses a competing manifest", async () => {
  const { project, remote } = await fixture();
  await project.commit([{ type: "put", key: "asset", data: bytes("abcdefgh") }, { type: "put", key: "doc", data: bytes("one") }]);
  await project.sync();
  const writes = remote.writes;
  await project.commit([{ type: "put", key: "doc", data: bytes("two") }]);
  await project.sync();
  assert.equal(remote.writes - writes, 2);
  const key = "stow-projects/example/manifest.json";
  const manifest = remote.objects.get(key);
  assert.ok(manifest);
  remote.objects.set(key, { ...manifest, etag: "competing" });
  await project.commit([{ type: "put", key: "doc", data: bytes("three") }]);
  await assert.rejects(project.sync(), { code: "conflict" });
  assert.equal(new TextDecoder().decode(await project.read("doc")), "three");
});

test("streamed input stays chunked, range materialization is bounded, and failed quota leaves the head intact", async () => {
  const { project, adapter } = await fixture();
  async function* source() { yield bytes("abcdef"); yield bytes("ghijkl"); }
  await project.commit([{ type: "put", key: "file", data: source() }]);
  const limited = await BrowserProject.open(new ContentStore(adapter), { projectId: "example", maxReadBytes: 4 });
  await assert.rejects(limited.read("file"), { code: "read_too_large" });
  assert.equal(new TextDecoder().decode(await limited.readRange("file", 4, 8)), "efgh");
  const small = await fixture(2, 3);
  await assert.rejects(small.project.commit([{ type: "put", key: "file", data: bytes("abcdef") }]), { code: "quota_exceeded" });
  assert.equal((await small.project.status()).currentVersionId, null);
});

test("remote content cannot be reused by copying unverified hash metadata", async () => {
  const { project, remote } = await fixture();
  await project.commit([{ type: "put", key: "doc", data: bytes("one") }]);
  await project.sync();
  const chunk = [...remote.objects.entries()].find(([key]) => key.includes("/chunks/"));
  assert.ok(chunk);
  remote.objects.set(chunk[0], { ...chunk[1], data: bytes("bad") });
  await project.commit([]);
  await assert.rejects(project.sync(), { code: "conflict" });
  assert.equal(remote.manifestWrites, 1);
  assert.equal(new TextDecoder().decode(await project.read("doc")), "one");
});

test("migration tokens are own data properties even when named __proto__", async () => {
  const { project } = await fixture();
  await project.commit([{ type: "put", key: "doc", data: bytes("one") }], { migrationToken: "__proto__" });
  const revision = (await project.status()).currentVersionId;
  assert.equal(await project.migrationProgress("__proto__"), revision);
  await project.commit([{ type: "put", key: "doc", data: bytes("two") }], { migrationToken: "__proto__" });
  assert.equal((await project.status()).versions.length, 1);
  assert.equal(new TextDecoder().decode(await project.read("doc")), "one");
});
