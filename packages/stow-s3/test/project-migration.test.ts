import assert from "node:assert/strict";
import { test } from "node:test";
import { BrowserFileHistory, openBrowserEmbeddedStow } from "../dist/browser.js";
import { sharedWasmHost } from "./wasm-host-fixture.js";
import { BrowserProject } from "../src/project.js";
import { ContentStore } from "../src/content-store.js";
import { migrateFileHistory } from "../src/project-migration.js";
import { MemoryContentAdapter } from "./content-fixtures.js";
import { HistoryPersistence, HistoryRemote } from "./file-history-fixtures.js";

const wasmHost = sharedWasmHost();
const bytes = (text: string) => new TextEncoder().encode(text);
async function fixture(retention = 10) {
  const persistence = new HistoryPersistence();
  const remote = new HistoryRemote();
  const store = await openBrowserEmbeddedStow(wasmHost(), { namespace: "source", persistence, maxBytes: 1_000_000 });
  const source = await BrowserFileHistory.open(store, { remoteBucket: "files", remote });
  await source.save("file", bytes("one"));
  await source.save("file", bytes("two"));
  await source.undo("file");
  const adapter = new MemoryContentAdapter();
  await adapter.open("destination");
  const project = await BrowserProject.open(new ContentStore(adapter), { projectId: "import", maxVersions: retention });
  return { source, project, adapter, remote, close: () => store.close() };
}
test("legacy migration preserves retained stages and cursor and is repeatable", async (t) => {
  const fixture = await fixtureForTest(); t.after(fixture.close);
  const status = await migrateFileHistory(fixture.source, "file", fixture.project);
  assert.equal(status.versions.length, 2);
  assert.equal(new TextDecoder().decode(await fixture.project.read("file")), "one");
  await fixture.project.redo();
  assert.equal(new TextDecoder().decode(await fixture.project.read("file")), "two");
  const repeated = await migrateFileHistory(fixture.source, "file", fixture.project);
  assert.equal(repeated.versions.length, 2);
  assert.equal(new TextDecoder().decode(await fixture.project.read("file")), "one");
  assert.equal((await fixture.source.status("file")).versions.length, 2);
});
async function fixtureForTest() { return fixture(); }

test("migration refuses insufficient retention before changing the destination", async (t) => {
  const value = await fixture(2); t.after(value.close);
  await value.source.redo("file");
  await value.source.save("file", bytes("three"));
  await assert.rejects(migrateFileHistory(value.source, "file", value.project), { code: "invalid_options" });
  assert.equal((await value.project.status()).versions.length, 0);
});

test("migration leaves uncertain original remote saves intact", async (t) => {
  const value = await fixture(); t.after(value.close);
  value.remote.loseNextReply = true;
  await assert.rejects(value.source.sync("file"));
  await assert.rejects(migrateFileHistory(value.source, "file", value.project), { code: "uncertain_write" });
  assert.equal((await value.source.status("file")).hasUncertainWrite, true);
  assert.equal((await value.project.status()).versions.length, 0);
});
