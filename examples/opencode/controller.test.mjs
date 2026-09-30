import assert from "node:assert/strict";
import { test } from "node:test";
import { TurnController, saveWithRetry } from "./controller.mjs";

function fixture(overrides = {}) {
  let state;
  const calls = [];
  const storage = {
    resolve: async () => ({ outcome: "not_found" }),
    capture: async () => ({ outcome: "committed", checkpoint: { id: "saved-one" } }),
    ...overrides.storage,
  };
  const controller = new TurnController({
    deadlineMs: 1000,
    store: { load: async () => state, save: async next => { state = structuredClone(next); calls.push(next.phase); } },
    agent: { execute: async () => { calls.push("execute"); return { outcome: "succeeded", sessionID: "s", messageID: "m" }; }, ...overrides.agent },
    storage,
    quiesce: overrides.quiesce ?? (async () => { calls.push("quiesce"); }),
    writeProgress: async () => { calls.push("progress"); },
    snapshot: overrides.snapshot,
  });
  return { controller, calls, state: () => state };
}

test("admission waits for capture and persists intent before execution", async () => {
  let complete;
  const f = fixture({ storage: { capture: () => new Promise(resolve => { complete = resolve; }) } });
  const first = f.controller.turn("work");
  while (!complete) await new Promise(resolve => setImmediate(resolve));
  await assert.rejects(f.controller.turn("too early"), /owns admission/);
  assert.deepEqual(f.calls, ["running", "execute", "quiesce", "progress", "saving"]);
  complete({ outcome: "committed", checkpoint: { id: "one" } });
  assert.equal((await first).lastCheckpointID, "one");
});

test("lost reply resolves original result without rerunning prompt", async () => {
  let committed = false;
  let captures = 0;
  const f = fixture({ storage: {
    resolve: async () => committed ? { outcome: "committed", checkpoint: { id: "original" } } : { outcome: "not_found" },
    capture: async () => { captures++; committed = true; throw new Error("lost reply"); },
  } });
  assert.equal((await f.controller.turn("work")).lastCheckpointID, "original");
  assert.equal(captures, 1);
  assert.equal(f.calls.filter(x => x === "execute").length, 1);
});

test("save exhaustion keeps admission closed and recovery only resolves", async () => {
  let captures = 0;
  let available = false;
  const f = fixture({ storage: {
    resolve: async () => available ? { outcome: "committed", checkpoint: { id: "resolved" } } : { outcome: "not_found" },
    capture: async () => { captures++; throw Object.assign(new Error("disk full"), { retryable: false }); },
  } });
  await assert.rejects(f.controller.turn("work"), /disk full/);
  await assert.rejects(f.controller.turn("next"), /admission is closed/);
  await assert.rejects(f.controller.recover(), /explicit new capture/);
  available = true;
  assert.equal((await f.controller.recover()).lastCheckpointID, "resolved");
  assert.equal(captures, 1);
});

test("transient failures use bounded jitter and cancellation stops backoff", async () => {
  const sleeps = [];
  let attempts = 0;
  const storage = { resolve: async () => ({ outcome: "not_found" }), capture: async () => { attempts++; throw Object.assign(new Error("busy"), { retryable: true }); } };
  await assert.rejects(saveWithRetry(storage, { key: "same" }, { signal: new AbortController().signal, random: () => 0.5, sleep: async ms => sleeps.push(ms) }), /busy/);
  assert.equal(attempts, 3);
  assert.deepEqual(sleeps, [125, 250]);
  const abort = new AbortController();
  attempts = 0;
  await assert.rejects(saveWithRetry(storage, {}, { signal: abort.signal, sleep: async () => { abort.abort(); } }));
  assert.equal(attempts, 1);
});

test("failed execution is saved but further work requires explicit decision", async () => {
  const f = fixture({ agent: { execute: async () => ({ outcome: "failed" }) } });
  assert.equal((await f.controller.turn("work")).phase, "saved");
  await assert.rejects(f.controller.turn("next"), /execution failed/);
});

test("successful no-op is saved with review evidence and closes admission", async () => {
  const f = fixture({ snapshot: async () => ({ "repo/notes.md": "unchanged" }) });
  const result = await f.controller.turn("append a summary");
  assert.equal(result.phase, "saved");
  assert.equal(result.terminal, "succeeded");
  assert.equal(result.reviewRequired, true);
  assert.deepEqual(result.changes, { added: [], modified: [], deleted: [] });
  assert.ok(f.calls.includes("progress"));
  await assert.rejects(f.controller.turn("next"), /explicit caller review/);
  assert.equal(f.calls.filter(x => x === "execute").length, 1);
});

test("task changes are measured after quiescence and before caller progress", async () => {
  let snapshots = 0;
  const f = fixture({ snapshot: async () => {
    snapshots++;
    if (snapshots === 2) {
      assert.ok(f.calls.includes("quiesce"));
      assert.ok(!f.calls.includes("progress"));
    }
    return { "repo/notes.md": snapshots === 1 ? "before" : "after" };
  } });
  const result = await f.controller.turn("append a summary");
  assert.equal(result.reviewRequired, false);
  assert.deepEqual(result.changes.modified, ["repo/notes.md"]);
});

test("no-op evidence survives lost capture replies and receipt-only recovery", async () => {
  let committed = false;
  const f = fixture({ snapshot: async () => ({}), storage: {
    resolve: async () => {
      if (committed) throw new Error("reply unavailable");
      return { outcome: "not_found" };
    },
    capture: async () => { committed = true; throw new Error("lost reply"); },
  } });
  await assert.rejects(f.controller.turn("work"), /Could not establish/);
  f.controller.storage.resolve = async () => ({ outcome: "committed", checkpoint: { id: "original" } });
  const recovered = await f.controller.recover();
  assert.equal(recovered.lastCheckpointID, "original");
  assert.equal(recovered.reviewRequired, true);
  await assert.rejects(f.controller.turn("next"), /explicit caller review/);
});

test("unknown execution and untracked writers cannot be called saved", async () => {
  const f = fixture({ quiesce: async () => { throw new Error("untracked writer"); } });
  await assert.rejects(f.controller.turn("work"), /untracked writer/);
  assert.equal(f.state().phase, "running");
  await assert.rejects(f.controller.recover(), /no reconcilable capture/);
});

test("resolution contention backs off without creating a capture", async () => {
  let resolves = 0;
  const sleeps = [];
  const storage = {
    resolve: async () => { resolves++; throw Object.assign(new Error("in_progress"), { retryable: true, outcome: "not_committed" }); },
    capture: async () => { assert.fail("must resolve before capturing"); },
  };
  await assert.rejects(saveWithRetry(storage, {}, { signal: new AbortController().signal, random: () => 1, sleep: async ms => sleeps.push(ms) }), /in_progress/);
  assert.equal(resolves, 3);
  assert.deepEqual(sleeps, [250, 500]);
});

test("recovery resolves a saved receipt without starting or quiescing an agent", async () => {
  let state = { phase: "saving", request: { key: "persisted" } };
  const controller = new TurnController({
    deadlineMs: 1000,
    store: { load: async () => state, save: async value => { state = value; } },
    storage: { resolve: async () => ({ outcome: "committed", checkpoint: { id: "previous-save" } }) },
    quiesce: () => assert.fail("receipt resolution must not need an agent"),
  });
  assert.equal((await controller.recover()).lastCheckpointID, "previous-save");
});
