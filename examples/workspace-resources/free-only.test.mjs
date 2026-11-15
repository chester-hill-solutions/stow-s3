import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdtemp, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createFreeOnlyProxy } from "./free-only.mjs";
import {
  buildFreeOnlyOpenCodeConfig,
  handleProxyRequest,
  loadOpenRouterCredential,
  validateFreePilotProfile,
} from "./free-only.mjs";
import { fileURLToPath } from "node:url";
import { parseOpenCodeJSONLines, prepareSyntheticWorkspace, verifyPilotEvidence } from "./run.mjs";

const secretSentinel = "secret-sentinel-that-must-not-enter-config-or-records";

test("pilot fixture prepares through the native manifest with required inputs beyond page one", async t => {
  const root = await mkdtemp(join(tmpdir(), "stow-pilot-fixture-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const binary = fileURLToPath(new URL("../../bin/stow-s3", import.meta.url));
  const fixture = await prepareSyntheticWorkspace(binary, root, undefined, Date.now(), 30000);
  const names = (await readdir(join(fixture.workspaceRoot, "inputs"))).sort();
  assert.equal(names.length, 103);
  assert.deepEqual(names.slice(100), ["10-brief.txt", "20-source-a.csv", "90-source-z.csv"]);
  assert.equal(fixture.requiredFileURIs.length, 3);
  assert.equal(fixture.answerFileURIs.length, 2);
});

test("profile and isolated config pin every OpenCode model path to Free Router", () => {
  const profile = validProfile();
  assert.doesNotThrow(() => validateFreePilotProfile(profile));
  assert.throws(() => validateFreePilotProfile({ ...profile, allowPaidModels: true }), /free-only/);
  assert.throws(() => validateFreePilotProfile({ ...profile, model: { providerID: "openrouter", id: "openai/gpt-paid" } }), /Free Router/);
  assert.throws(() => validateFreePilotProfile({ ...profile, deadlineMs: 300001 }), /five minutes/);
  const config = buildFreeOnlyOpenCodeConfig(profile, {
    workspaceId: "workspace-test", registryDir: "/tmp/registry", workspaceRoot: "/tmp/workspace",
    proxyBaseURL: "http://127.0.0.1:43123/api/v1",
  });
  assert.equal(config.model, "openrouter/openrouter/free");
  for (const agent of ["title", "compaction", "summary"]) assert.equal(config.agents[agent].model, config.model);
  assert.deepEqual(config.policies, [
    { action: "provider.use", resource: "*", effect: "deny" },
    { action: "provider.use", resource: "openrouter", effect: "allow" },
  ]);
  assert.deepEqual(config.providers.openrouter.settings, { baseURL: "http://127.0.0.1:43123/api/v1" });
  assert.deepEqual(config.mcp.servers.stow.command, [profile.stowBinary, "mcp", "--registry-dir", "/tmp/registry", "--workspace-id", "workspace-test",
    "--workspace-files", "--resources-only", "--timeout", "10s", "--read-root", "inputs"]);
  assert.equal(config.mcp.servers.stow.environment.OPENROUTER_API_KEY, "");
  assert.ok(!JSON.stringify(config).includes(secretSentinel));
  assert.ok(!JSON.stringify(config).includes(profile.credentialFile));
});

test("credential loader uses process override or private file without exposing bytes", async t => {
  const directory = await mkdtemp(join(tmpdir(), "stow-free-credential-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const credentialFile = join(directory, "key");
  await writeFile(credentialFile, `${secretSentinel}\n`, { mode: 0o600 });
  const profile = { credentialEnv: "OPENROUTER_API_KEY", credentialFile };
  assert.equal(await loadOpenRouterCredential(profile, {}), secretSentinel);
  assert.equal(await loadOpenRouterCredential(profile, { OPENROUTER_API_KEY: "env-only" }), "env-only");
});

test("loopback proxy rejects paid model IDs before upstream and records resolved ID only", async t => {
  const records = [];
  const requests = [];
  const options = {
    apiKey: secretSentinel,
    record: value => records.push(value),
    fetchImpl: async (url, requestOptions) => {
      requests.push({ url, options: requestOptions });
      if (url.endsWith("/models")) {
        return new Response(JSON.stringify({ data: [{ id: "openrouter/free" }, { id: "provider/paid-model" }] }), { status: 200, headers: { "content-type": "application/json" } });
      }
      return new Response(`data: {"model":"provider/free-model"}\n\n`, { status: 200, headers: { "content-type": "text/event-stream" } });
    },
  };
  for (const payload of [
    { model: "provider/paid-model" },
    { model: "openrouter/free", models: ["provider/paid-model"] },
    { model: "openrouter/free", route: { allow_fallbacks: true } },
  ]) {
    const response = mockResponse();
    await handleProxyRequest(mockRequest("POST", "/api/v1/chat/completions", payload), response, options);
    assert.equal(response.status, 403);
  }
  assert.equal(requests.length, 0, "blocked model/fallback reached upstream fetch");

  const free = mockResponse();
  await handleProxyRequest(mockRequest("POST", "/api/v1/chat/completions", { model: "openrouter/free", messages: [] }), free, options);
  assert.equal(free.status, 200);
  assert.match(free.body.toString(), /provider\/free-model/);
  assert.equal(requests[0].url, "https://openrouter.ai/api/v1/chat/completions");
  assert.equal(requests[0].options.headers.authorization, `Bearer ${secretSentinel}`);
  assert.equal(JSON.parse(requests[0].options.body.toString()).model, "openrouter/free");

  const catalog = mockResponse();
  await handleProxyRequest(mockRequest("GET", "/api/v1/models"), catalog, options);
  const models = JSON.parse(catalog.body.toString());
  assert.deepEqual(models.data.map(model => model.id), ["openrouter/free"]);
  const blockedRecord = records.find(entry => entry.status === 403);
  const freeRecord = records.find(entry => entry.path.endsWith("chat/completions") && entry.status === 200);
  const catalogRecord = records.find(entry => entry.method === "GET");
  assert.deepEqual([blockedRecord.stage, blockedRecord.upstreamAttempted, blockedRecord.requestedModel], ["guard", false, "provider/paid-model"]);
  assert.deepEqual([freeRecord.method, freeRecord.stage, freeRecord.upstreamAttempted], ["POST", "upstream", true]);
  assert.deepEqual([catalogRecord.path, catalogRecord.stage, catalogRecord.upstreamAttempted, catalogRecord.requestedModel],
    ["/api/v1/models", "upstream", true, null]);
  assert.equal(records.find(entry => entry.path.endsWith("chat/completions") && entry.status === 200).resolvedModel, "provider/free-model");
  assert.ok(!JSON.stringify(records).includes(secretSentinel));
});

test("pilot verifier requires final assistant answer, both sources and completed page continuation", () => {
  const workspaceId = "workspace-test";
  const overviewURI = `stow://workspaces/${workspaceId}/v1`;
  const briefURI = fileURI(workspaceId, "inputs/10-brief.txt");
  const firstURI = fileURI(workspaceId, "inputs/20-source-a.csv");
  const secondURI = fileURI(workspaceId, "inputs/90-source-z.csv");
  const fixture = { workspaceId, overviewURI, requiredFileURIs: [briefURI, firstURI, secondURI], answerFileURIs: [firstURI, secondURI] };
  const events = [
    resourceTool("list_mcp_resources", "completed", { server: "stow" }),
    resourceTool("read_mcp_resource", "completed", { uri: overviewURI }),
    resourceTool("read_mcp_resource", "completed", { uri: "stow://workspaces/workspace-test/tree-pages/v1/Lg" }),
    resourceTool("read_mcp_resource", "completed", { uri: `stow://workspaces/${workspaceId}/tree-pages/v1/aW5wdXRz/eyJ2IjoxLCJvIjoiYWJjIiwibiI6MTAwfQ` }),
    resourceTool("read_mcp_resource", "completed", { uri: briefURI }),
    resourceTool("read_mcp_resource", "completed", { uri: firstURI }),
    resourceTool("read_mcp_resource", "completed", { uri: secondURI }),
    assistantText("terminal", "answer", JSON.stringify(expectedAnswer(firstURI, secondURI))),
  ];
  const evidence = parseOpenCodeJSONLines(events.map(event => JSON.stringify(event)).join("\n"));
  assert.doesNotThrow(() => verifyPilotEvidence(evidence, fixture));
  const withoutPaging = { ...evidence, toolTrace: evidence.toolTrace.filter(item => !item.uri?.includes("/tree-pages/v1/aW5wdXRz/")) };
  assert.throws(() => verifyPilotEvidence(withoutPaging, fixture), /continuation/);
  const withShell = { ...evidence, toolTrace: [...evidence.toolTrace, { name: "shell", status: "completed", uri: "" }] };
  assert.throws(() => verifyPilotEvidence(withShell, fixture), /non-resource/);
});

test("canonical CSV integer strings preserve the exact numeric answer without coercion", () => {
  const fixture = answerFixture();
  const answer = expectedAnswer(...fixture.answerFileURIs);
  const mismatch = { item: "beta", source_a: "23", source_z: "24", difference: "1" };
  const verify = value => verifyPilotEvidence(parseEvents([
    ...validResourceEvents(fixture),
    assistantText("terminal", "answer", JSON.stringify({ ...answer, mismatches: [value] })),
  ]), fixture);
  assert.doesNotThrow(() => verify(mismatch));
  for (const value of [" 23", "+23", "023", "23.0", "2.3e1", "23suffix", "9007199254740993", 23.5, null]) {
    assert.throws(() => verify({ ...mismatch, source_a: value }), /expected mismatch/);
  }
  assert.throws(() => verify({ ...mismatch, difference: "2" }), /expected mismatch/);
});

test("only the terminal assistant message can satisfy strict JSON answer verification", () => {
  const fixture = answerFixture();
  const correct = JSON.stringify(expectedAnswer(...fixture.answerFileURIs));
  const wrong = JSON.stringify({ ...expectedAnswer(...fixture.answerFileURIs), mismatches: [{ item: "beta", source_a: 23, source_z: 25, difference: 2 }] });
  const trace = validResourceEvents(fixture);
  const interimThenWrong = [assistantText("interim", "part-1", correct), assistantText("terminal", "part-1", wrong)];
  assert.throws(() => verifyPilotEvidence(parseEvents([...trace, ...interimThenWrong]), fixture), /expected mismatch/);

  const answerJSON = JSON.stringify(expectedAnswer(...fixture.answerFileURIs));
  const splitAt = Math.floor(answerJSON.length / 2);
  const multipart = [
    assistantText("terminal", "part-1", answerJSON.slice(0, splitAt)),
    assistantText("terminal", "part-2", answerJSON.slice(splitAt)),
  ];
  assert.doesNotThrow(() => verifyPilotEvidence(parseEvents([...trace, ...multipart]), fixture));

  const replacedPart = [
    assistantText("terminal", "same-part", wrong),
    assistantText("terminal", "same-part", answerJSON),
  ];
  assert.doesNotThrow(() => verifyPilotEvidence(parseEvents([...trace, ...replacedPart]), fixture));
});

test("workspace URI scope rejects malformed and traversing canonical paths", () => {
  const fixture = answerFixture();
  const nestedAllowed = validResourceEvents(fixture);
  nestedAllowed.push(resourceTool("read_mcp_resource", "completed", {
    uri: `stow://workspaces/${fixture.workspaceId}/tree-pages/v1/${Buffer.from("inputs/nested").toString("base64url")}`,
  }));
  nestedAllowed.push(assistantText("terminal", "answer", JSON.stringify(expectedAnswer(...fixture.answerFileURIs))));
  assert.doesNotThrow(() => verifyPilotEvidence(parseEvents(nestedAllowed), fixture));
  const badURIs = [
    `stow://workspaces/${fixture.workspaceId}/files/${Buffer.from("inputs/../inputs-private/secret.txt").toString("base64url")}`,
    `stow://workspaces/${fixture.workspaceId}/files/${Buffer.from("inputs/./secret.txt").toString("base64url")}`,
    `stow://workspaces/${fixture.workspaceId}/files/${Buffer.from("inputs/a\\b").toString("base64url")}`,
    `stow://workspaces/${fixture.workspaceId}/files/${Buffer.from("inputs/a\0b").toString("base64url")}`,
    `stow://workspaces/${fixture.workspaceId}/files/${Buffer.from([0xff]).toString("base64url")}`,
    `stow://workspaces/${fixture.workspaceId}/files/${Buffer.from("inputs/a").toString("base64url")}=` ,
    `stow://workspaces/${fixture.workspaceId}/files/${Buffer.from("inputs/a").toString("base64url")}?download=1`,
    `stow://workspaces/${fixture.workspaceId}/tree-pages/v1/${Buffer.from("inputs").toString("base64url")}/${Buffer.from(".").toString("base64url")}`,
  ];
  for (const uri of badURIs) {
    const events = validResourceEvents(fixture);
    events.push(resourceTool("read_mcp_resource", "completed", { uri }));
    events.push(assistantText("terminal", "answer", JSON.stringify(expectedAnswer(...fixture.answerFileURIs))));
    assert.throws(() => verifyPilotEvidence(parseEvents(events), fixture), undefined, uri);
  }
});

test("loopback proxy listener enforces the same free-only request guard", async t => {
  const proxy = createFreeOnlyProxy({
    apiKey: secretSentinel,
    fetchImpl: async () => new Response(`data: {"model":"provider/free-model"}\n\n`, {
      status: 200, headers: { "content-type": "text/event-stream" },
    }),
  });
  await new Promise((resolve, reject) => {
    proxy.once("error", reject);
    proxy.listen(0, "127.0.0.1", resolve);
  });
  t.after(async () => {
    proxy.closeAllConnections?.();
    await new Promise(resolve => proxy.close(resolve));
  });
  const address = proxy.address();
  const denied = await fetch(`http://127.0.0.1:${address.port}/api/v1/chat/completions`, {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ model: "provider/paid", messages: [] }),
  });
  assert.equal(denied.status, 403);
  const allowed = await fetch(`http://127.0.0.1:${address.port}/api/v1/chat/completions`, {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ model: "openrouter/free", messages: [] }),
  });
  assert.equal(allowed.status, 200);
  assert.match(await allowed.text(), /provider\/free-model/);
});

function mockRequest(method, url, payload) {
  return {
    method,
    url,
    async *[Symbol.asyncIterator]() {
      if (payload !== undefined) yield Buffer.from(JSON.stringify(payload));
    },
  };
}

function mockResponse() {
  return {
    status: null,
    headers: null,
    body: null,
    writeHead(status, headers) { this.status = status; this.headers = headers; },
    end(body) { this.body = Buffer.from(body); },
  };
}

function fileURI(id, path) { return `stow://workspaces/${id}/files/${Buffer.from(path).toString("base64url")}`; }

function resourceTool(name, status, input) {
  return { type: "tool", part: { type: "tool", tool: name, state: { status, input } } };
}

function assistantText(messageID, id, text) {
  return { type: "text", part: { type: "text", messageID, id, text } };
}

function answerFixture() {
  const workspaceId = "workspace-test";
  const overviewURI = `stow://workspaces/${workspaceId}/v1`;
  const requiredFileURIs = ["inputs/10-brief.txt", "inputs/20-source-a.csv", "inputs/90-source-z.csv"].map(path => fileURI(workspaceId, path));
  return { workspaceId, overviewURI, requiredFileURIs, answerFileURIs: requiredFileURIs.slice(1) };
}

function expectedAnswer(firstURI, secondURI) {
  return { mismatches: [{ item: "beta", source_a: 23, source_z: 24, difference: 1 }], sources: [firstURI, secondURI] };
}

function validResourceEvents(fixture) {
  return [
    resourceTool("list_mcp_resources", "completed", { server: "stow" }),
    resourceTool("read_mcp_resource", "completed", { uri: fixture.overviewURI }),
    resourceTool("read_mcp_resource", "completed", { uri: `stow://workspaces/${fixture.workspaceId}/tree-pages/v1/Lg` }),
    resourceTool("read_mcp_resource", "completed", { uri: `stow://workspaces/${fixture.workspaceId}/tree-pages/v1/aW5wdXRz/eyJ2IjoxLCJvIjoiYWJjIiwibiI6MTAwfQ` }),
    ...fixture.requiredFileURIs.map(uri => resourceTool("read_mcp_resource", "completed", { uri })),
  ];
}

function parseEvents(events) { return parseOpenCodeJSONLines(events.map(event => JSON.stringify(event)).join("\n")); }

function validProfile() {
  return {
    version: 1,
    opencodeBinary: "/opt/opencode",
    expectedVersion: "opencode v2.2.7",
    stowBinary: "/opt/stow-s3",
    model: { providerID: "openrouter", id: "openrouter/free" },
    credentialEnv: "OPENROUTER_API_KEY",
    credentialFile: "/private/keys/openrouter.key",
    deadlineMs: 300000,
    maxTurns: 2,
    allowPaidModels: false,
  };
}
