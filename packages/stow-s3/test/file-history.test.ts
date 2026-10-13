import { deferred } from "./deferred.js";
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  BrowserFileHistory, FileHistoryError, openBrowserEmbeddedStow,
  type FileReadResult,
} from "../dist/browser.js";
import { sharedWasmHost } from "./wasm-host-fixture.js";
import { HistoryPersistence, HistoryRemote } from "./file-history-fixtures.js";

const wasmHost = sharedWasmHost();
const encode = (text: string) => new TextEncoder().encode(text);
const text = (result: FileReadResult) => new TextDecoder().decode(result.data);

async function setup(maxVersions = 50, maxBytes = 1_000_000) {
  const persistence = new HistoryPersistence();
  const remote = new HistoryRemote();
  const open = () => openBrowserEmbeddedStow(wasmHost(), { namespace: crypto.randomUUID(), persistence, maxBytes, maxObjects: 200 });
  let store = await open();
  const history = await BrowserFileHistory.open(store, { remoteBucket: "files", remote, maxVersions });
  return {
    store, history, persistence, remote,
    async reopen() {
      await store.close();
      store = await open();
      return BrowserFileHistory.open(store, { remoteBucket: "files", remote, maxVersions });
    },
    async close() { await store.close(); },
  };
}

async function prepareUncertainWrite(fixture: Awaited<ReturnType<typeof setup>>, contents = "attempted") {
  await fixture.history.save("draft.txt", encode(contents));
  fixture.remote.loseNextReply = true;
  await assert.rejects(fixture.history.sync("draft.txt"));
}

test("real WASM file history survives reopen with its undo/redo cursor", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  await fixture.history.save("draft.txt", encode("one"));
  await fixture.history.save("draft.txt", encode("two"), { label: "Before formatting" });
  assert.equal(text(await fixture.history.undo("draft.txt")), "one");
  const reopened = await fixture.reopen();
  assert.equal(text(await reopened.read("draft.txt")), "one");
  assert.equal(text(await reopened.redo("draft.txt")), "two");
  assert.equal((await reopened.status("draft.txt")).versions[1]?.label, "Before formatting");
  assert.equal(fixture.remote.writes, 0);
});

test("read miss fetches remote, then cached reads and edits work offline", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  fixture.remote.seed("draft.txt", "remote");
  assert.equal((await fixture.history.read("draft.txt")).status.pending, false);
  fixture.remote.online = false;
  assert.equal(text(await fixture.history.read("draft.txt")), "remote");
  await fixture.history.save("draft.txt", encode("offline edit"));
  await assert.rejects(fixture.history.sync("draft.txt"), /offline/);
  const reopened = await fixture.reopen();
  assert.equal(text(await reopened.read("draft.txt")), "offline edit");
  fixture.remote.online = true;
  assert.equal((await reopened.sync("draft.txt")).pending, false);
  assert.equal(new TextDecoder().decode(fixture.remote.objects.get("draft.txt")?.data), "offline edit");
});

test("sync coalesces stages and publishes only the selected version", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  for (const value of ["first", "second", "third"]) await fixture.history.save("draft.txt", encode(value));
  await fixture.history.undo("draft.txt");
  await fixture.history.sync("draft.txt");
  assert.equal(fixture.remote.writes, 1);
  assert.equal(new TextDecoder().decode(fixture.remote.objects.get("draft.txt")?.data), "second");
  assert.equal((await fixture.history.sync("draft.txt")).pending, false);
  assert.equal(fixture.remote.writes, 1);
  assert.equal((await fixture.history.redo("draft.txt")).status.pending, true);
});

test("remote conflict keeps local history; adoption and undo permit explicit replacement", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  fixture.remote.seed("draft.txt", "original");
  await fixture.history.read("draft.txt");
  await fixture.history.save("draft.txt", encode("local edit"));
  fixture.remote.seed("draft.txt", "other writer");
  await assert.rejects(fixture.history.sync("draft.txt"), { code: "conflict" });
  assert.equal(text(await fixture.history.read("draft.txt")), "local edit");
  assert.equal((await fixture.history.status("draft.txt")).hasUncertainWrite, false);
  assert.equal(text(await fixture.history.adoptRemote("draft.txt")), "other writer");
  assert.equal(text(await fixture.history.undo("draft.txt")), "local edit");
  await fixture.history.sync("draft.txt");
  assert.equal(fixture.remote.writes, 1);
});

test("an offline-created file cannot silently overwrite an existing remote key", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  fixture.remote.seed("draft.txt", "existing");
  await fixture.history.save("draft.txt", encode("new locally"));
  await assert.rejects(fixture.history.sync("draft.txt"), { code: "conflict" });
  assert.equal(fixture.remote.writes, 0);
});

for (const failure of ["lost network reply", "failed acknowledgement commit"]) {
  test(`${failure} reconciles after reopen without duplicating the remote write`, async (t) => {
    const fixture = await setup(); t.after(() => fixture.close());
    await fixture.history.save("draft.txt", encode("saved"));
    if (failure === "lost network reply") fixture.remote.loseNextReply = true;
    else fixture.remote.afterWrite = () => { fixture.persistence.failNextCommit = true; };
    await assert.rejects(fixture.history.sync("draft.txt"));
    assert.equal(fixture.remote.writes, 1);
    const reopened = await fixture.reopen();
    assert.equal((await reopened.status("draft.txt")).hasUncertainWrite, true);
    assert.equal((await reopened.sync("draft.txt")).pending, false);
    assert.equal(fixture.remote.writes, 1);
  });
}

test("an uncertain older write survives history pruning and later edits", async (t) => {
  const fixture = await setup(2); t.after(() => fixture.close());
  await prepareUncertainWrite(fixture);
  await assert.rejects(fixture.history.adoptRemote("draft.txt"), { code: "uncertain_write" });
  await fixture.history.save("draft.txt", encode("middle"));
  await fixture.history.save("draft.txt", encode("latest"));
  const reopened = await fixture.reopen();
  assert.equal((await reopened.sync("draft.txt")).pending, false);
  assert.equal(fixture.remote.writes, 2);
  assert.equal(new TextDecoder().decode(fixture.remote.objects.get("draft.txt")?.data), "latest");
  assert.equal(fixture.persistence.snapshot.objects.filter((object) => object.key.includes("/revisions/")).length, 2);
});

test("same bytes with a different receipt do not acknowledge an uncertain write", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  await prepareUncertainWrite(fixture, "same");
  fixture.remote.seed("draft.txt", "same");
  await assert.rejects(fixture.history.sync("draft.txt"), { code: "conflict" });
  assert.equal((await fixture.history.status("draft.txt")).pending, true);
});

test("a copied receipt with changed metadata does not acknowledge the attempted version", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  await fixture.history.save("draft.txt", encode("same"), { metadata: { purpose: "original" } });
  fixture.remote.loseNextReply = true;
  await assert.rejects(fixture.history.sync("draft.txt"));
  const object = fixture.remote.objects.get("draft.txt");
  assert.ok(object);
  fixture.remote.objects.set("draft.txt", { ...object, etag: crypto.randomUUID(), metadata: { purpose: "changed" } });
  await assert.rejects(fixture.history.sync("draft.txt"), { code: "conflict" });
});

test("conflicting pruned attempts release their protected object after reconciliation", async (t) => {
  const fixture = await setup(2); t.after(() => fixture.close());
  await prepareUncertainWrite(fixture);
  for (const value of ["middle", "latest"]) await fixture.history.save("draft.txt", encode(value));
  fixture.remote.seed("draft.txt", "other writer");
  await assert.rejects(fixture.history.sync("draft.txt"), { code: "conflict" });
  assert.equal(fixture.persistence.snapshot.objects.length, 3);
});

test("failed history commit restores both stages and cursor", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  await fixture.history.save("draft.txt", encode("one"));
  fixture.persistence.failNextCommit = true;
  await assert.rejects(fixture.history.save("draft.txt", encode("two")), /disk unavailable/);
  assert.equal(text(await fixture.history.read("draft.txt")), "one");
  assert.equal((await fixture.history.status("draft.txt")).versions.length, 1);
  assert.equal(fixture.persistence.snapshot.objects.length, 2);
});

test("a WASM quota failure midway through a batch restores the original state", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  await fixture.store.createBucket("batch");
  const original = fixture.persistence.commits;
  await assert.rejects(fixture.store.writeBatch([
    { type: "put", bucket: "batch", key: "first", data: encode("small") },
    { type: "put", bucket: "batch", key: "second", data: new Uint8Array(1_000_001) },
  ]), { code: "quota_exceeded" });
  assert.equal((await fixture.store.listObjects("batch")).objects.length, 0);
  assert.equal(fixture.persistence.commits, original);
});

test("retention bounds and branching release unreachable revision objects", async (t) => {
  const fixture = await setup(3); t.after(() => fixture.close());
  for (const value of ["one", "two", "three", "four"]) await fixture.history.save("draft.txt", encode(value));
  await fixture.history.undo("draft.txt");
  await fixture.history.save("draft.txt", encode("branch"));
  await assert.rejects(fixture.history.redo("draft.txt"), { code: "history_boundary" });
  assert.equal(fixture.persistence.snapshot.objects.length, 4);
  assert.equal(text(await fixture.history.undo("draft.txt")), "three");
});

test("facades over the same browser store serialize concurrent edits", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  const second = await BrowserFileHistory.open(fixture.store, { remoteBucket: "files", remote: fixture.remote });
  await Promise.all([
    fixture.history.save("draft.txt", encode("first")),
    second.save("draft.txt", encode("second")),
  ]);
  assert.equal((await fixture.history.status("draft.txt")).versions.length, 2);
  assert.equal(text(await fixture.history.undo("draft.txt")), "first");
});

test("queued saves capture caller bytes and metadata", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  const data = encode("one"); const options = { metadata: { purpose: "original" } };
  const saved = fixture.history.save("draft.txt", data, options);
  data.fill(0); options.metadata.purpose = "changed";
  const result = await saved;
  assert.equal(text(result), "one");
  assert.equal(result.version.metadata?.purpose, "original");
});

test("corrupt history fails closed rather than fetching over local edits", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  await fixture.history.save("draft.txt", encode("local"));
  await fixture.store.putObject("stow-file-history", "files/files/draft.txt/state.json", encode('{"formatVersion":2}'));
  await assert.rejects(fixture.history.read("draft.txt"), { code: "history_corrupt" });
});

test("invalid options and missing remote do not cause implicit network writes", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  await assert.rejects(BrowserFileHistory.open(fixture.store, { remoteBucket: "files", maxVersions: 1 }), FileHistoryError);
  const local = await BrowserFileHistory.open(fixture.store, { remoteBucket: "local" });
  await local.save("draft.txt", encode("local only"));
  await assert.rejects(local.sync("draft.txt"), { code: "remote_unavailable" });
  await assert.rejects(local.read("missing.txt"), { code: "remote_unavailable" });
});

test("save receipts commit without reading revision bytes back", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  const reads: string[] = [];
  const get = fixture.store.getObject.bind(fixture.store);
  fixture.store.getObject = async (bucket, key) => { reads.push(key); return get(bucket, key); };
  const receipt = await fixture.history.saveReceipt("receipt.txt", encode("saved"));
  assert.ok(receipt.version.id);
  assert.equal(receipt.status.pending, true);
  assert.equal(reads.some((key) => key.includes("/revisions/")), false);
  assert.equal(text(await fixture.history.read("receipt.txt")), "saved");
});

test("a slow file sync permits saves and preserves newer pending revisions", async (t) => {
  const fixture = await setup(); t.after(() => fixture.close());
  await fixture.history.save("draft.txt", encode("old"));
  const started = deferred();
  const gate = deferred();
  const put = fixture.remote.putObject.bind(fixture.remote);
  fixture.remote.putObject = async (...args) => { started.resolve(); await gate.promise; return put(...args); };
  const syncing = fixture.history.sync("draft.txt");
  await started.promise;
  await fixture.history.saveReceipt("draft.txt", encode("new"));
  gate.resolve();
  assert.equal((await syncing).pending, true);
  assert.equal(text(await fixture.history.read("draft.txt")), "new");
  assert.equal((await fixture.history.sync("draft.txt")).pending, false);
});
