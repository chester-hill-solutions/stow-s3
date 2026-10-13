import assert from "node:assert/strict";
import { beforeEach, describe, it } from "node:test";
import { BrowserPersistenceError, IndexedDbPersistenceAdapter, openBrowserEmbeddedStow } from "../dist/browser.js";
import { TransactionalPersistence, MemoryHost, emptySnapshot, openProfile } from "./browser-fixtures.js";

describe("openBrowserEmbeddedStow", () => {
  beforeEach(() => {
    TransactionalPersistence.owners.clear();
  });

  it("commits mutations before resolving and serializes them FIFO", async () => {
    const events: string[] = [];
    const adapter = new TransactionalPersistence(events);
    const host = new MemoryHost(events);
    const stow = await openBrowserEmbeddedStow(host, {
      namespace: "profile",
      persistence: adapter,
      maxBytes: 1_000,
      maxObjects: 10,
    });

    await stow.createBucket("assets");
    const first = stow.putObject("assets", "a.txt", new TextEncoder().encode("a"));
    const second = stow.putObject("assets", "b.txt", new TextEncoder().encode("b"));
    await first;
    events.push("resolved:first");
    await second;
    events.push("resolved:second");

    const firstPut = events.indexOf("inner:putObject");
    const firstCommit = events.indexOf("commit:start:profile", firstPut);
    assert.ok(firstCommit > firstPut);
    assert.ok(events.indexOf("commit:end:profile", firstCommit) < events.indexOf("resolved:first"));
    assert.ok(events.lastIndexOf("inner:putObject") < events.lastIndexOf("commit:end:profile"));
    assert.ok(events.indexOf("resolved:second") < events.length);
    assert.deepEqual(await stow.usage(), { bytes: 2, objects: 2 });
    await stow.close();
  });

  it("restores the last durable generation when a commit fails", async () => {
    const events: string[] = [];
    const { adapter, stow } = await openProfile(events);
    await stow.createBucket("assets");
    await stow.putObject("assets", "hello.txt", new TextEncoder().encode("before"));

    adapter.failNextCommit = true;
    await assert.rejects(
      stow.putObject("assets", "hello.txt", new TextEncoder().encode("after")),
      (error: unknown) => error instanceof BrowserPersistenceError && error.code === "persistence_error",
    );
    assert.deepEqual(
      (await stow.getObject("assets", "hello.txt")).data,
      new TextEncoder().encode("before"),
    );
    assert.equal(adapter.snapshot.objects[0]?.data?.[0], "b".charCodeAt(0));
    await stow.close();
  });

  it("captures queued writes and persists their bytes without reading them back", async () => {
    const events: string[] = [];
    const { adapter, stow } = await openProfile(events);
    await stow.createBucket("assets");
    events.length = 0;
    const data = new Uint8Array([1, 2, 3]);
    const options = { metadata: { label: "original" } };
    const writing = stow.putObject("assets", "queued", data, options);
    data.fill(9);
    options.metadata.label = "changed";
    await writing;
    assert.equal(events.includes("inner:getObject"), false);
    assert.deepEqual(adapter.snapshot.objects[0]?.data, new Uint8Array([1, 2, 3]));
    assert.deepEqual(adapter.snapshot.objects[0]?.metadata, { label: "original" });
    await stow.close();
  });

  it("isolates durable recovery buffers from adapters and returned objects", async () => {
    const events: string[] = [];
    const { adapter, stow } = await openProfile(events);
    await stow.createBucket("assets");
    const saved = await stow.putObject("assets", "saved", new Uint8Array([1, 2]), {
      metadata: { label: "original" },
    });
    const loaded = await stow.getObject("assets", "saved");
    loaded.data?.fill(8);
    if (saved.metadata !== undefined) saved.metadata.label = "returned";
    adapter.snapshot.objects[0]?.data?.fill(9);
    const adapterMetadata = adapter.snapshot.objects[0]?.metadata;
    if (adapterMetadata !== undefined) adapterMetadata.label = "adapter";
    adapter.failNextCommit = true;
    await assert.rejects(stow.putObject("assets", "failed", new Uint8Array([3])));
    const recovered = await stow.getObject("assets", "saved");
    assert.deepEqual(recovered.data, new Uint8Array([1, 2]));
    assert.deepEqual(recovered.metadata, { label: "original" });
    await stow.close();
  });

  it("clears persisted state on reset and keeps the profile usable", async () => {
    const events: string[] = [];
    const { adapter, stow } = await openProfile(events);
    await stow.createBucket("assets");
    await stow.putObject("assets", "hello.txt", new TextEncoder().encode("hello"));
    await stow.reset();
    assert.deepEqual(await stow.listBuckets(), []);
    assert.deepEqual(adapter.snapshot, emptySnapshot(adapter.generation));
    await stow.createBucket("fresh");
    await stow.close();
    assert.equal(adapter.closes, 1);
  });

  it("closes idempotently, retains data, and rejects later operations", async () => {
    const events: string[] = [];
    const { adapter, stow } = await openProfile(events);
    await stow.createBucket("assets");
    const firstClose = stow.close();
    const secondClose = stow.close();
    assert.strictEqual(firstClose, secondClose);
    await firstClose;
    await assert.rejects(stow.createBucket("late"), (error: unknown) =>
      error instanceof BrowserPersistenceError && error.code === "closed");
    assert.equal(adapter.closes, 1);
    assert.equal(adapter.snapshot.buckets[0]?.name, "assets");
    assert.equal(events.filter((event) => event === "inner:close").length, 1);
  });

  it("rejects a second live opener of the same namespace", async () => {
    const events: string[] = [];
    const firstAdapter = new TransactionalPersistence(events);
    const first = await openBrowserEmbeddedStow(new MemoryHost(events), { namespace: "profile", persistence: firstAdapter });
    const secondAdapter = new TransactionalPersistence(events);
    await assert.rejects(
      openBrowserEmbeddedStow(new MemoryHost(events), { namespace: "profile", persistence: secondAdapter }),
      (error: unknown) => error instanceof BrowserPersistenceError && error.code === "already_open",
    );
    await first.close();
  });

  it("rejects persisted data that exceeds requested quotas", async () => {
    const events: string[] = [];
    const adapter = new TransactionalPersistence(events);
    adapter.snapshot = {
      ...emptySnapshot(1),
      buckets: [{ name: "assets" }],
      objects: [{
        bucket: "assets",
        key: "big",
        data: new Uint8Array([1, 2, 3]),
        size: 3,
        etag: "etag",
      }],
    };
    await assert.rejects(
      openBrowserEmbeddedStow(new MemoryHost(events), { namespace: "profile", persistence: adapter, maxBytes: 2 }),
      (error: unknown) => error instanceof BrowserPersistenceError && error.code === "quota_exceeded",
    );
  });
});

describe("IndexedDbPersistenceAdapter", () => {
  it("rejects a missing IndexedDB host as unsupported", async () => {
    const adapter = new IndexedDbPersistenceAdapter();
    await assert.rejects(
      adapter.open("profile"),
      (error: unknown) =>
        error instanceof BrowserPersistenceError && error.code === "persistence_unsupported",
    );
  });
});
