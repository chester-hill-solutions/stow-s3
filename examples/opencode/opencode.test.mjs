import assert from "node:assert/strict";
import { test } from "node:test";
import { OpenCodeAgent, rejectProcessTools } from "./opencode.mjs";
import { jsonStore, acquireCallerState } from "./local-store.mjs";
import { mkdtemp, mkdir, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";

function agentWithResponses(responses) {
  let stored;
  const calls = [];
  const agent = new OpenCodeAgent({
    endpoint: "http://127.0.0.1:9999", password: "test", model: { providerID: "test", id: "test" }, directory: "/work",
    sessionStore: { load: async () => stored, save: async value => { stored = value; } },
    fetchImpl: async (url, options) => { calls.push({ url, options }); return responses.shift(); },
  });
  return { agent, calls };
}
const response = data => new Response(JSON.stringify(data), { status: 200 });

test("OpenCode waits and checks terminal outcome, not prompt acknowledgement", async () => {
  const { agent, calls } = agentWithResponses([
    response({ data: { id: "ses_test" } }),
    response({ data: { id: "msg_turn", time: { created: 10 } } }),
    new Response(null, { status: 204 }),
    response({ data: { outcome: "succeeded", time: { idle: 11 } } }),
  ]);
  const value = await agent.execute("work", "turn", new AbortController().signal);
  assert.equal(value.outcome, "succeeded");
  assert.match(calls[2].url, /experimental.*wait$/);
  assert.match(calls[3].url, /api\/session\/ses_test$/);
  const permissions = JSON.parse(calls[0].options.body).permissions;
  assert.deepEqual(permissions[0], { action: "*", resource: "*", effect: "deny" });
});

test("stale outcome cannot close a new turn", async () => {
  const { agent } = agentWithResponses([
    response({ data: { id: "ses_test" } }), response({ data: { id: "msg_turn", time: { created: 10 } } }),
    new Response(null, { status: 204 }), response({ data: { outcome: "succeeded", time: { idle: 9 } } }),
  ]);
  await assert.rejects(agent.execute("work", "turn", new AbortController().signal), /no terminal outcome/);
});

test("execution tools and non-loopback endpoints are refused", () => {
  assert.throws(() => rejectProcessTools({ data: [{ tool: "bash" }] }), /unsupported writer/);
  assert.throws(() => new OpenCodeAgent({ endpoint: "https://remote.example" }), /loopback/);
  rejectProcessTools({ data: [{ type: "tool", name: "edit", state: { status: "completed" } }] });
  assert.throws(() => rejectProcessTools({ type: "tool", name: "shell", state: { status: "completed" } }), /unsupported writer/);
  assert.throws(() => rejectProcessTools({ type: "tool", name: "write", state: { status: "running" } }), /not settled/);
});

test("caller state is durable, scoped and admits only one owner", async t => {
  const root = await mkdtemp(join(tmpdir(), "stow-caller-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const workspace = join(root, "workspace");
  const state = join(root, "state");
  await mkdir(workspace);
  const release = await acquireCallerState(state, workspace);
  await assert.rejects(acquireCallerState(state, workspace), /EEXIST/);
  const store = jsonStore(join(state, "turn.json"), "one");
  await store.save({ phase: "saving", request: { key: "persisted" } });
  assert.deepEqual(await store.load(), { phase: "saving", request: { key: "persisted" } });
  await assert.rejects(jsonStore(join(state, "turn.json"), "other").load(), /different configuration/);
  await release();
  await assert.rejects(acquireCallerState(join(workspace, "bad"), workspace), /outside/);
});

test("dot-dot-prefixed children still belong to the captured workspace", async t => {
  const root = await mkdtemp(join(tmpdir(), "stow-caller-boundary-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  await assert.rejects(acquireCallerState(join(root, "..state"), root), /outside/);
});
