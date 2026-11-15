import assert from "node:assert/strict";
import { test } from "node:test";
import { createEditingProxy, editingPrompt, editingSchema, editingTools, parseEditingEvents, validateToolNameMap } from "./editing-host.mjs";
import { closeProxy, listenLoopback } from "./run.mjs";

const names = Object.fromEntries(editingTools.map(name => [`stow_${name}`, name]));
const tool = name => ({ type: "function", function: { name, description: name, parameters: { type: "object", properties: {} } } });

test("editing proxy forwards only free-route schema and four explicit MCP tools", async () => {
  const seen = [];
  const enforced = [];
  const requests = [];
  const schema = editingSchema("host-preallocated-key");
  const server = createEditingProxy({ apiKey: "mock-only-key", toolNameMap: names, requestKey: "host-preallocated-key", schema, readRoots: ["docs"], enforced, record: item => requests.push(item),
    fetchImpl: async (url, init) => {
      seen.push({ url, body: JSON.parse(init.body) });
      return new Response(JSON.stringify({ model: "mock-free-result" }), { headers: { "content-type": "application/json" } });
    } });
  const address = await listenLoopback(server);
  try {
    const url = `http://127.0.0.1:${address.port}/api/v1/chat/completions`;
    const body = { model: "openrouter/free", tools: [...Object.keys(names).map(tool), tool("shell"), tool("stow_checkpoint_create")] };
    const response = await fetch(url, { method: "POST", body: JSON.stringify(body) });
    assert.equal(response.status, 200);
    assert.equal(seen[0].url, "https://openrouter.ai/api/v1/chat/completions");
    assert.deepEqual(seen[0].body.tools.map(item => item.function.name), Object.keys(names));
    assert.equal(Object.hasOwn(seen[0].body, "response_format"), false);
    assert.deepEqual(seen[0].body.tool_choice, { type: "function", function: { name: "stow_stow_workspace_file_read_for_save" } });
    assert.equal(seen[0].body.provider.require_parameters, true);
    assert.equal(enforced[0].toolNames.length, 4);
    assert.deepEqual({ phase: enforced[0].phase, initialReadRequired: enforced[0].initialReadRequired,
      initialReadVerified: enforced[0].initialReadVerified, schemaEnforced: enforced[0].schemaEnforced },
    { phase: "initial_read", initialReadRequired: true, initialReadVerified: false, schemaEnforced: false });
    assert.equal(JSON.stringify(enforced).includes("host-observation-secret"), false);
    assert.equal(JSON.stringify(requests).includes("host-observation-secret"), false);
    const denied = await fetch(url, { method: "POST", body: JSON.stringify({ ...body, model: "paid/model" }) });
    assert.equal(denied.status, 403);
    assert.equal(seen.length, 1);
    const incomplete = await fetch(url, { method: "POST", body: JSON.stringify({ ...body, tools: [tool(Object.keys(names)[0])] }) });
    assert.equal(incomplete.status, 502);
    assert.equal(seen.length, 1);
  } finally { await closeProxy(server); }
});

test("editing proxy waits for a correlated committed save before terminal schema", async () => {
  const seen = [];
  const enforced = [];
  const requestKey = "request-key";
  const server = createEditingProxy({ apiKey: "mock-only-key", toolNameMap: names, requestKey, readRoots: ["docs"],
    schema: editingSchema(requestKey), enforced,
    fetchImpl: async (url, init) => {
      seen.push(JSON.parse(init.body));
      return new Response(JSON.stringify({ model: "mock-free-result" }), { headers: { "content-type": "application/json" } });
    } });
  const address = await listenLoopback(server);
  try {
    const url = `http://127.0.0.1:${address.port}/api/v1/chat/completions`;
    const initial = await fetch(url, { method: "POST", body: JSON.stringify({ model: "openrouter/free", tools: Object.keys(names).map(tool) }) });
    assert.equal(initial.status, 200);
    const tools = Object.keys(names).map(tool);
    const edit = await fetch(url, { method: "POST", body: JSON.stringify({ model: "openrouter/free", tools, messages: initialReadHistory() }) });
    assert.equal(edit.status, 200);
    assert.equal(seen[1].tool_choice, "required");
    assert.equal(Object.hasOwn(seen[1], "response_format"), false);
    assert.equal(enforced[1].phase, "editing_before_commit");
    assert.equal(enforced[1].initialReadVerified, true);
    assert.equal(enforced[1].commitProved, false);
    const committed = await fetch(url, { method: "POST", body: JSON.stringify({ model: "openrouter/free", tools,
      messages: committedSaveHistory({ requestKey }) }) });
    assert.equal(committed.status, 200);
    const terminal = seen[2];
    assert.equal(terminal.tool_choice, "none");
    assert.equal(terminal.provider.require_parameters, true);
    assert.deepEqual(terminal.response_format.json_schema.schema.properties.generation.enum, ["native-generation"]);
    assert.equal(terminal.response_format.json_schema.schema.properties.total_units.type, "integer");
    assert.equal(enforced[2].phase, "terminal_after_commit");
    assert.equal(enforced[2].initialReadVerified, true);
    assert.equal(enforced[2].commitProved, true);
    assert.equal(enforced[2].schemaEnforced, true);
    assert.equal(JSON.stringify(enforced).includes("host-observation-secret"), false);
  } finally { await closeProxy(server); }
});

test("a successful read alone leaves terminal schema locked", async () => {
  const seen = [];
  const server = createEditingProxy({ apiKey: "mock-only-key", toolNameMap: names, requestKey: "request-key", readRoots: ["docs"],
    schema: editingSchema("request-key"), fetchImpl: async (_url, init) => {
      seen.push(JSON.parse(init.body)); return new Response("{}", { headers: { "content-type": "application/json" } });
    } });
  const address = await listenLoopback(server);
  try {
    const url = "http://127.0.0.1:" + address.port + "/api/v1/chat/completions";
    const tools = Object.keys(names).map(tool);
    await fetch(url, { method: "POST", body: JSON.stringify({ model: "openrouter/free", tools }) });
    const response = await fetch(url, { method: "POST", body: JSON.stringify({ model: "openrouter/free", tools, messages: initialReadHistory() }) });
    assert.equal(response.status, 200);
    assert.equal(seen.length, 2);
    assert.equal(seen[1].tool_choice, "required");
    assert.equal(Object.hasOwn(seen[1], "response_format"), false);
  } finally { await closeProxy(server); }
});

test("save proof refuses wrong identity, body status, duplicate or unmatched history before upstream", async () => {
  const cases = [
    { requestKey: "wrong-key" },
    { saveArgs: { path: "docs/private.json" } },
    { saveArgs: { observation_token: "other-token" } },
    { saveResult: { outcome: "unknown", path: "docs/STATE.json", generation: "g" } },
    { saveResult: { ...validCommittedSave(), isError: true } },
    { saveResult: { ...validCommittedSave(), error: { code: "denied" } } },
    { saveResult: { ...validCommittedSave(), generation: "" } },
    { resultId: "unmatched-result" },
    { readResultAfterSaveCall: true },
    { duplicateSave: true },
    { duplicateCallID: true },
    { duplicateResult: true },
    { unknownCall: true },
    { forbiddenCall: "stow_stow_workspace_file_resolve_save" },
    { forbiddenCall: "stow_stow_workspace_file_release_observation" },
  ];
  for (const options of cases) await assertSaveGateRefuses(options);
});

async function assertSaveGateRefuses(options) {
  const seen = [];
  const server = createEditingProxy({ apiKey: "mock-only-key", toolNameMap: names, requestKey: "request-key", readRoots: ["docs"],
    schema: editingSchema("request-key"), fetchImpl: async (_url, init) => {
      seen.push(JSON.parse(init.body)); return new Response("{}", { headers: { "content-type": "application/json" } });
    } });
  const address = await listenLoopback(server);
  try {
    const url = "http://127.0.0.1:" + address.port + "/api/v1/chat/completions";
    const tools = Object.keys(names).map(tool);
    await fetch(url, { method: "POST", body: JSON.stringify({ model: "openrouter/free", tools }) });
    const response = await fetch(url, { method: "POST", body: JSON.stringify({ model: "openrouter/free", tools,
      messages: committedSaveHistory(options) }) });
    assert.equal(response.status, 502);
    assert.equal(seen.length, 1, "invalid commit evidence reached the provider");
  } finally { await closeProxy(server); }
}

test("editing proxy refuses missing, forged, mismatched, failed and catalog-bypassing read history", async () => {
  const histories = [
    [],
    [{ role: "assistant", content: "I read docs/STATE.json successfully." }],
    initialReadHistory({ toolCallId: "expected-id", resultId: "different-id" }),
    initialReadHistory({ result: { outcome: "unknown", available: false, path: "docs/STATE.json", observation_token: "token", data_base64: "e30=" } }),
    initialReadHistory({ result: { ...validStateRead(), isError: true } }),
    initialReadHistory({ result: { ...validStateRead(), error: { code: "denied" } } }),
    initialReadHistory({ argumentsText: '{"path":"docs/batch-a.csv"}' }),
    initialReadHistory({ argumentsText: '{"path":"docs-private/STATE.json"}', result: { ...validStateRead(), path: "docs-private/STATE.json" } }),
    initialReadHistory({ argumentsText: '{"path":"docs/STATE.json","extra":true}' }),
    failedBatchReadHistory({ result: { ...validStateRead(), path: "docs/batch-a.csv", isError: true } }),
    failedBatchReadHistory({ result: { ...validStateRead(), path: "docs/wrong.csv" } }),
    duplicateToolCallHistory(),
  ];
  for (const messages of histories) await assertReadGateRefuses({ messages });
  await assertReadGateRefuses({ messages: initialReadHistory(), tools: [tool(Object.keys(names)[0])] });
  await assertReadGateRefuses({ messages: initialReadHistory() }, ["docs-private"]);
  assert.throws(() => createEditingProxy({ toolNameMap: names, requestKey: "key", schema: editingSchema("key") }), /read roots/i);
});

async function assertReadGateRefuses(overrides, readRoots = ["docs"]) {
  const seen = [];
  const server = createEditingProxy({ apiKey: "mock-only-key", toolNameMap: names, requestKey: "key", readRoots, schema: editingSchema("key"),
    fetchImpl: async (_url, init) => {
      seen.push(JSON.parse(init.body));
      return new Response("{}", { headers: { "content-type": "application/json" } });
    } });
  const address = await listenLoopback(server);
  try {
    const url = `http://127.0.0.1:${address.port}/api/v1/chat/completions`;
    const tools = Object.keys(names).map(tool);
    assert.equal((await fetch(url, { method: "POST", body: JSON.stringify({ model: "openrouter/free", tools }) })).status, 200);
    const body = { model: "openrouter/free", tools, ...overrides };
    assert.equal((await fetch(url, { method: "POST", body: JSON.stringify(body) })).status, 502);
    assert.equal(seen.length, 1, "unverified history was forwarded upstream");
  } finally { await closeProxy(server); }
}

function initialReadHistory({ toolCallId = "read-id", resultId = toolCallId, result = validStateRead(),
  argumentsText = '{"path":"docs/STATE.json"}' } = {}) {
  return [
    { role: "assistant", tool_calls: [{ id: toolCallId, type: "function", function: {
      name: "stow_stow_workspace_file_read_for_save", arguments: argumentsText,
    } }] },
    { role: "tool", tool_call_id: resultId, content: JSON.stringify(result) },
  ];
}

function committedSaveHistory(options = {}) {
  const readID = "read-id";
  const saveID = options.duplicateCallID ? readID : "save-id";
  const saveArgs = { path: "docs/STATE.json", request_key: options.requestKey ?? "request-key",
    observation_token: "host-observation-secret", text: '{"state":"model-produced"}', ...options.saveArgs };
  const saveCall = { id: saveID, type: "function", function: {
    name: "stow_stow_workspace_file_save", arguments: JSON.stringify(saveArgs),
  } };
  const saveCalls = [saveCall];
  if (options.duplicateSave) saveCalls.push({ ...saveCall, id: "save-two" });
  if (options.unknownCall) saveCalls.push({ id: "unknown-id", type: "function", function: {
    name: "native_shell", arguments: "{}",
  } });
  if (options.forbiddenCall) saveCalls.push({ id: "forbidden-id", type: "function", function: {
    name: options.forbiddenCall, arguments: "{}",
  } });
  const messages = [
    { role: "assistant", tool_calls: [{ id: readID, type: "function", function: {
      name: "stow_stow_workspace_file_read_for_save", arguments: '{"path":"docs/STATE.json"}',
    } }] },
    { role: "tool", tool_call_id: readID, content: JSON.stringify(validStateRead()) },
    { role: "assistant", tool_calls: saveCalls },
    { role: "tool", tool_call_id: options.resultId ?? saveID, content: JSON.stringify(options.saveResult ?? validCommittedSave()) },
  ];
  if (options.readResultAfterSaveCall) {
    return [
      { role: "assistant", tool_calls: [messages[0].tool_calls[0], saveCall] },
      messages[1], messages[3],
    ];
  }
  if (options.duplicateResult) messages.push({ role: "tool", tool_call_id: saveID, content: JSON.stringify(validCommittedSave()) });
  return messages;
}

function failedBatchReadHistory({ result }) {
  return [
    ...initialReadHistory(),
    { role: "assistant", tool_calls: [{ id: "batch-id", type: "function", function: {
      name: "stow_stow_workspace_file_read_for_save", arguments: '{"path":"docs/batch-a.csv"}',
    } }] },
    { role: "tool", tool_call_id: "batch-id", content: JSON.stringify(result) },
  ];
}

function validStateRead() {
  return { outcome: "observed", available: true, path: "docs/STATE.json", observation_token: "host-observation-secret",
    data_base64: Buffer.from('{"total_units":11,"entry_count":2,"next_batch":"docs/batch-a.csv"}').toString("base64") };
}

function validCommittedSave() {
  return { outcome: "committed", committed: true, path: "docs/STATE.json", generation: "native-generation" };
}

function duplicateToolCallHistory() {
  const history = initialReadHistory({ toolCallId: "duplicate-id" });
  history.splice(1, 0, { role: "assistant", tool_calls: [{ id: "duplicate-id", type: "function",
    function: { name: "stow_stow_workspace_file_save", arguments: '{"path":"docs/STATE.json"}' } }] });
  return history;
}

test("editing event parser rejects terminal prose and preserves every unknown tool", () => {
  const call = { type: "tool_use", part: { tool: Object.keys(names)[0], state: {
    status: "completed", input: { path: "docs/STATE.json" }, output: JSON.stringify({ available: true }) } } };
  const text = value => ({ type: "text", part: { messageID: "terminal", id: "text", text: value } });
  const lines = events => events.map(event => JSON.stringify(event)).join("\n");
  const good = parseEditingEvents(lines([call, text('{"outcome":"committed"}')]), names);
  assert.equal(good.toolTrace[0].name, editingTools[0]);
  const unknown = structuredClone(call); unknown.part.tool = "native_shell";
  assert.equal(parseEditingEvents(lines([unknown, text("{}")]), names).toolTrace[0].name, "native_shell");
  assert.throws(() => parseEditingEvents(lines([call, text('{}\nAll done.')]), names));
  assert.throws(() => parseEditingEvents(lines([call, text('```json\n{}\n```')]), names));
  assert.throws(() => parseEditingEvents(lines([text("{}"), call]), names));
  assert.throws(() => parseEditingEvents(lines([{ type: "error", error: "failure" }, text("{}")]), names));
  assert.throws(() => parseEditingEvents("not JSON lines", names));
});

test("editing prompt and schema contain no expected arithmetic answers", () => {
  const value = editingPrompt("fresh-key") + JSON.stringify(editingSchema("fresh-key"));
  assert.ok(!/\b(?:11|26|56|13|17)\b/.test(value));
  assert.match(value, /fresh-key/);
  assert.match(editingPrompt("fresh-key"), /Do not call resolve_save or release_observation/);
  const aliases = Object.fromEntries(editingTools.map((name, index) => [`arbitrary_${index}`, name]));
  assert.throws(() => validateToolNameMap(aliases));
});
