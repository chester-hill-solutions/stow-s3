import assert from "node:assert/strict";
import { test } from "node:test";
import { createEditingProxy, editingSchema, editingToolNameMap, editingTools } from "./editing-host.mjs";
import { summarizeEnforcement, summarizeRequestMetadata } from "./editing-runner.mjs";
import { closeProxy, listenLoopback } from "./run.mjs";

const tools = Object.keys(editingToolNameMap).map(name => ({ type: "function", function: { name, parameters: {} } }));

test("incomplete catalog records a guard refusal before fake upstream fetch", async () => {
  const records = [];
  const enforcement = [];
  let upstreamCalls = 0;
  const response = await sendRequest({ tools: tools.slice(0, 3), records, enforcement,
    fetchImpl: async () => { upstreamCalls++; return new Response("{}"); },
    messages: [{ role: "user", content: "PROMPT_SENTINEL" }] });
  assert.equal(response.status, 502);
  assert.equal(upstreamCalls, 0);
  assert.deepEqual(enforcement.map(item => item.phase), ["catalog_refused"]);
  assert.deepEqual(pickDiagnostic(records[0]), { stage: "guard", failureKind: "catalog_refused", upstreamAttempted: false });
  const metadata = summarizeRequestMetadata(records);
  assert.equal(metadata.paidFallbackObserved, false);
  assert.equal(metadata.nullModelErrorCount, 0);
  assert.equal(metadata.nonFreeModelRequestCount, 0);
  assert.equal(metadata.requests[0].requestedModel, "openrouter/free");
  assert.equal(metadata.requests[0].proxyStage, "guard");
  assert.equal(metadata.requests[0].upstreamAttempted, false);
  const stageScoped = summarizeRequestMetadata([{ ...records[0], stage: "stage2", proxyStage: records[0].stage }]);
  assert.equal(stageScoped.requests[0].stage, "stage2");
  assert.equal(stageScoped.requests[0].proxyStage, "guard");
  const enforcementSummary = summarizeEnforcement(enforcement, editingToolNameMap);
  assert.equal(enforcementSummary.catalogRefusalCount, 1);
  assert.equal(enforcementSummary.historyRefusalCount, 0);
  assertPrivacy(metadata, enforcement, await response.text());
});

test("fake upstream exception is distinguished from guard refusal without logging its message", async () => {
  const records = [];
  const enforcement = [];
  const response = await sendRequest({ tools, records, enforcement,
    fetchImpl: async () => { throw new Error("CREDENTIAL_SENTINEL_BODY_SENTINEL"); },
    messages: [{ role: "user", content: "PROMPT_SENTINEL" }] });
  assert.equal(response.status, 502);
  assert.deepEqual(enforcement.map(item => item.phase), ["initial_read"]);
  assert.deepEqual(pickDiagnostic(records[0]), { stage: "upstream", failureKind: "upstream_exception", upstreamAttempted: true });
  assert.equal(summarizeRequestMetadata(records).paidFallbackObserved, false);
  assertPrivacy(records, enforcement, await response.text());
});

test("history refusal is counted as a guard and does not make another upstream call", async () => {
  const records = [];
  const enforcement = [];
  let upstreamCalls = 0;
  const server = createProxy({ records, enforcement, fetchImpl: async () => {
    upstreamCalls++;
    return new Response(JSON.stringify({ model: "liquid/model:free" }), { status: 200,
      headers: { "content-type": "application/json" } });
  } });
  const address = await listenLoopback(server);
  try {
    assert.equal((await post(address.port, { model: "openrouter/free", tools })).status, 200);
    const response = await post(address.port, { model: "openrouter/free", tools, messages: [] });
    assert.equal(response.status, 502);
    assert.equal(upstreamCalls, 1);
    assert.deepEqual(enforcement.map(item => item.phase), ["initial_read", "history_refused"]);
    assert.deepEqual(pickDiagnostic(records[1]), { stage: "guard", failureKind: "history_refused", upstreamAttempted: false });
    const summary = summarizeEnforcement(enforcement, editingToolNameMap);
    assert.equal(summary.historyRefusalCount, 1);
    assert.equal(summary.catalogRefusalCount, 0);
    const metadata = summarizeRequestMetadata(records);
    assert.equal(metadata.nullModelErrorCount, 0);
    assert.equal(metadata.paidFallbackObserved, false);
  } finally { await closeProxy(server); }
});

test("explicit non-free selector is recorded as blocked, not as an upstream paid fallback", async () => {
  const records = [];
  const enforcement = [];
  let upstreamCalls = 0;
  const response = await sendRequest({ tools, records, enforcement, model: "vendor/paid-model",
    fetchImpl: async () => { upstreamCalls++; return new Response("{}"); } });
  assert.equal(response.status, 403);
  assert.equal(upstreamCalls, 0);
  assert.deepEqual(pickDiagnostic(records[0]), { stage: "guard", failureKind: "request_invalid", upstreamAttempted: false });
  const metadata = summarizeRequestMetadata(records);
  assert.equal(metadata.nonFreeModelRequestCount, 1);
  assert.equal(metadata.paidFallbackObserved, false);
  assert.equal(metadata.nullModelErrorCount, 0);
  assert.equal(metadata.requests[0].requestedModel, "vendor/paid-model");
  assert.equal(metadata.requests[0].upstreamAttempted, false);
});

test("upstream HTTP errors and invalid response bodies retain bounded failure categories", async () => {
  const httpRecords = [];
  const httpResponse = await sendRequest({ tools, records: httpRecords, enforcement: [],
    fetchImpl: async () => new Response(JSON.stringify({ error: "provider returned a private payload" }), { status: 503,
      headers: { "content-type": "application/json" } }) });
  assert.equal(httpResponse.status, 503);
  assert.deepEqual(pickDiagnostic(httpRecords[0]), { stage: "upstream", failureKind: "upstream_http_error", upstreamAttempted: true });

  const records = [];
  const enforcement = [];
  const response = await sendRequest({ tools, records, enforcement,
    fetchImpl: async () => new Response("not json", { status: 200, headers: { "content-type": "application/json" } }) });
  assert.equal(response.status, 200);
  assert.deepEqual(pickDiagnostic(records[0]), { stage: "upstream", failureKind: "response_invalid", upstreamAttempted: true });
});

test("malformed local request is counted as unknown-model error, never paid fallback", async () => {
  const records = [];
  const server = createProxy({ records, enforcement: [], fetchImpl: async () => {
    throw new Error("upstream should not be called");
  } });
  const address = await listenLoopback(server);
  try {
    const response = await fetch(`http://127.0.0.1:${address.port}/api/v1/chat/completions`, {
      method: "POST", headers: { "content-type": "application/json" }, body: '{"PRIVATE_BODY_SENTINEL":',
    });
    assert.equal(response.status, 502);
    const body = await response.text();
    const metadata = summarizeRequestMetadata(records);
    assert.equal(metadata.paidFallbackObserved, false);
    assert.equal(metadata.nullModelErrorCount, 1);
    assert.equal(metadata.requests[0].failureKind, "request_invalid");
    assert.equal(metadata.requests[0].upstreamAttempted, false);
    assertPrivacy(metadata, body);
  } finally { await closeProxy(server); }
});

async function sendRequest({ tools: toolList, records, enforcement, fetchImpl, model = "openrouter/free", messages }) {
  const server = createProxy({ records, enforcement, fetchImpl });
  const address = await listenLoopback(server);
  try { return await post(address.port, { model, tools: toolList, messages }); }
  finally { await closeProxy(server); }
}

function createProxy({ records, enforcement, fetchImpl }) {
  return createEditingProxy({ apiKey: "local-only-placeholder", toolNameMap: editingToolNameMap,
    readRoots: ["docs"], requestKey: "private-request-key", schema: editingSchema("private-request-key"),
    enforced: enforcement, record: value => records.push(value), fetchImpl });
}

async function post(port, body) {
  return fetch(`http://127.0.0.1:${port}/api/v1/chat/completions`, {
    method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body),
  });
}

function pickDiagnostic(record) {
  return { stage: record.stage, failureKind: record.failureKind, upstreamAttempted: record.upstreamAttempted };
}

function assertPrivacy(...values) {
  const serialized = JSON.stringify(values);
  for (const sentinel of ["PROMPT_SENTINEL", "CREDENTIAL_SENTINEL", "BODY_SENTINEL", "private-request-key"]) {
    assert.equal(serialized.includes(sentinel), false, `diagnostics leaked ${sentinel}`);
  }
}
