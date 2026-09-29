import assert from "node:assert/strict";
import { test } from "node:test";
import { shutdownPilot } from "./shutdown.mjs";

function fixture(failing) {
  const calls = [];
  const step = name => async () => { calls.push(name); if (name === failing) throw new Error(name); };
  return { calls, resources: { server: { close: step("agent") }, holder: { close: step("holder") }, release: step("admission") } };
}

test("cleanup confirms writer exit before releasing either claim", async () => {
  const f = fixture();
  await shutdownPilot(f.resources);
  assert.deepEqual(f.calls, ["agent", "holder", "admission"]);
});

test("unconfirmed writer stop retains storage and caller admission", async () => {
  const f = fixture("agent");
  await assert.rejects(shutdownPilot(f.resources), /agent/);
  assert.deepEqual(f.calls, ["agent"]);
});

test("startup stop failure cannot release claims through outer cleanup", async () => {
  const f = fixture();
  await assert.rejects(shutdownPilot(f.resources, Object.assign(new Error("startup"), { agentStopUnconfirmed: true })), /startup/);
  assert.deepEqual(f.calls, []);
});

test("storage close failure retains caller admission for explicit recovery", async () => {
  const f = fixture("holder");
  await assert.rejects(shutdownPilot(f.resources), /holder/);
  assert.deepEqual(f.calls, ["agent", "holder"]);
});
