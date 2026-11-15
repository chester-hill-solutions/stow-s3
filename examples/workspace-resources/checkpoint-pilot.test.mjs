import assert from "node:assert/strict";
import { test } from "node:test";
import { access, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { createRequire } from "node:module";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { buildFreeOnlyOpenCodeConfig } from "./free-only.mjs";
import { prepareCheckpointContinuation } from "./checkpoint-fixture.mjs";
import { verifyCheckpointEvidence } from "./checkpoint-verifier.mjs";
import { parseOpenCodeJSONLines } from "./run.mjs";

const repoRoot = resolve(new URL("../..", import.meta.url).pathname);
const extensionRequire = createRequire(join(repoRoot, "extensions/stow/package.json"));

test("checkpoint fixture removes sender paths and reads the immutable receiver snapshot", async t => {
  const root = await mkdtemp(join(tmpdir(), "stow-checkpoint-pilot-test-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const cli = fileURLToPath(new URL("../../bin/stow-s3", import.meta.url));
  const fixture = await prepareCheckpointContinuation(cli, root);
  assert.notEqual(fixture.sourceWorkspaceId, fixture.workspaceId);
  for (const path of ["source-workspace", "source-registry", "source-tree"]) {
    await assert.rejects(access(join(root, path)));
  }
  assert.equal(await readFile(join(fixture.workspaceRoot, "inputs", "next-batch.csv"), "utf8"), "item,units\ndelta,700\nepsilon,1300\n");

  const { Client } = extensionRequire("@modelcontextprotocol/sdk/client/index.js");
  const { StdioClientTransport, getDefaultEnvironment } = extensionRequire("@modelcontextprotocol/sdk/client/stdio.js");
  const transport = new StdioClientTransport({
    command: cli,
    args: ["mcp", "--registry-dir", fixture.registryDir, "--workspace-id", fixture.workspaceId,
      "--workspace-files", "--resources-only", "--timeout", "10s", "--read-root", "inputs", "--checkpoint-id", fixture.checkpointId],
    env: { ...getDefaultEnvironment(), OPENROUTER_API_KEY: "" }, cwd: fixture.workspaceRoot, stderr: "pipe",
  });
  const client = new Client({ name: "stow-checkpoint-pilot-test", version: "1" });
  t.after(async () => { await client.close().catch(() => undefined); await transport.close().catch(() => undefined); });
  await client.connect(transport);
  assert.equal((await client.listTools()).tools.length, 0);
  assert.equal((await client.listPrompts()).prompts.length, 0);
  const overview = JSON.parse((await client.readResource({ uri: fixture.overviewURI })).contents[0].text);
  assert.equal(overview.workspace_id, fixture.workspaceId);
  assert.equal(overview.checkpoint_id, fixture.checkpointId);
  assert.equal((await client.readResource({ uri: fixture.progressURI })).contents[0].text, fixture.savedProgress);
  assert.equal((await client.readResource({ uri: fixture.dataURI })).contents[0].text, fixture.savedNextBatch);

  const privateURI = checkpointFileURI(fixture, "inputs-private/sentinel.txt");
  const wrongCheckpointURI = fixture.overviewURI.replace(fixture.checkpointId, "cp-not-selected");
  const wrongWorkspaceURI = fixture.overviewURI.replace(fixture.workspaceId, fixture.sourceWorkspaceId);
  const liveURI = "stow://workspaces/not-the-selected-workspace/v1";
  for (const uri of [privateURI, wrongCheckpointURI, wrongWorkspaceURI, liveURI]) {
    await assert.rejects(client.readResource({ uri }));
  }
});

test("checkpoint config selects immutable profile and keeps filesystem tools denied", () => {
  const config = buildFreeOnlyOpenCodeConfig(profile(), {
    workspaceId: "receiver", checkpointId: "cp-selected", registryDir: "/tmp/registry",
    workspaceRoot: "/tmp/receiver", proxyBaseURL: "http://127.0.0.1:41234/api/v1",
  });
  assert.deepEqual(config.mcp.servers.stow.command.slice(-2), ["--checkpoint-id", "cp-selected"]);
  assert.ok(config.permissions.some(rule => rule.action === "*" && rule.effect === "deny"));
  assert.throws(() => buildFreeOnlyOpenCodeConfig(profile(), {
    workspaceId: "receiver", checkpointId: "", registryDir: "/tmp/registry",
    workspaceRoot: "/tmp/receiver", proxyBaseURL: "http://127.0.0.1:41234/api/v1",
  }), /checkpoint selector/);
});

test("checkpoint verifier requires exact prior-state and data reads plus strict computed JSON", () => {
  const fixture = {
    workspaceId: "receiver", checkpointId: "cp-selected", expectedTotal: 66, expectedEntryCount: 5,
    overviewURI: "stow://workspaces/receiver/checkpoints/cp-selected/v1",
    progressURI: "stow://workspaces/receiver/checkpoints/cp-selected/files/aW5wdXRzL3Byb2dyZXNzLnR4dA",
    dataURI: "stow://workspaces/receiver/checkpoints/cp-selected/files/aW5wdXRzL25leHQtYmF0Y2guY3N2",
    savedProgress: "Prior state: three accepted entries total 46 units. Continue from that verified total using the next batch.",
    savedNextBatch: "item,units\ndelta,7\nepsilon,13\n",
  };
  const completed = validEvidence(fixture);
  assert.ok(completed.toolTrace.every(item => item.resultValid === true));
  assert.doesNotThrow(() => verifyCheckpointEvidence(completed, fixture));
  assert.throws(() => verifyCheckpointEvidence(parseEvents(fixture, answerText(fixture), [], {
    [fixture.progressURI]: "",
  }), fixture), /saved fixture/);
  assert.throws(() => verifyCheckpointEvidence(parseEvents(fixture, answerText(fixture), [], {
    [fixture.dataURI]: "item,units\ndelta,700\nepsilon,1300\n",
  }), fixture), /saved fixture/);
  assert.throws(() => verifyCheckpointEvidence(parseEvents(fixture, answerText(fixture), [], {
    [fixture.overviewURI]: "",
  }), fixture), /overview text/);
  assert.doesNotThrow(() => verifyCheckpointEvidence(parseEvents(fixture, JSON.stringify({
    total_units: "66", entry_count: "5", sources: [fixture.progressURI, fixture.dataURI],
  })), fixture));
  for (const uri of [fixture.progressURI, fixture.dataURI]) {
    const missing = { ...completed, toolTrace: completed.toolTrace.filter(item => item.uri !== uri) };
    assert.throws(() => verifyCheckpointEvidence(missing, fixture), /required checkpoint reads/);
  }
  for (const badText of [
    JSON.stringify({ total_units: 65, entry_count: 5, sources: [fixture.progressURI, fixture.dataURI] }),
    JSON.stringify({ total_units: 66, entry_count: 5, sources: [fixture.progressURI] }),
    JSON.stringify({ total_units: 66, entry_count: 5, sources: [fixture.progressURI, fixture.dataURI], note: "extra" }),
    JSON.stringify({ total_units: "6.6e1", entry_count: 5, sources: [fixture.progressURI, fixture.dataURI] }),
  ]) {
    const wrong = parseEvents(fixture, badText);
    assert.throws(() => verifyCheckpointEvidence(wrong, fixture), /terminal checkpoint/);
  }
  const failedRead = tool("read_mcp_resource", { uri: fixture.progressURI }, {
    uri: fixture.progressURI, isError: true, contents: [{ type: "text", uri: fixture.progressURI, text: "denied" }],
  });
  const failed = parseEvents(fixture, JSON.stringify({
    total_units: 66, entry_count: 5, sources: [fixture.progressURI, fixture.dataURI],
  }), [failedRead]);
  assert.equal(failed.toolTrace.at(-1).status, "completed");
  assert.equal(failed.toolTrace.at(-1).failed, true);
  assert.throws(() => verifyCheckpointEvidence(failed, fixture), /error or invalid result/);
  assert.ok(completed.toolTrace.every(item => item.failed === false));
});

test("checkpoint verifier rejects noncanonical, unknown, traversing and foreign resource routes", () => {
  const fixture = sampleFixture();
  const canonicalUris = [
    checkpointRoute(fixture, `tree/v1/${Buffer.from(".").toString("base64url")}`),
    checkpointRoute(fixture, `tree/v1/${Buffer.from("inputs").toString("base64url")}/100`),
    checkpointRoute(fixture, `ranges/v1/${Buffer.from("inputs/next-batch.csv").toString("base64url")}/0/65536`),
  ];
  const canonical = parseEvents(fixture, JSON.stringify({
    total_units: 66, entry_count: 5, sources: [fixture.progressURI, fixture.dataURI],
  }), canonicalUris.map(uri => tool("read_mcp_resource", { uri })));
  assert.doesNotThrow(() => verifyCheckpointEvidence(canonical, fixture));
  const invalidURIs = [
    `stow://workspaces/${fixture.workspaceId}/checkpoints/${fixture.checkpointId}/tree-pages/v1/aW5wdXRz`,
    checkpointFileURI(fixture, "inputs/../inputs-private/secret.txt"),
    `${fixture.progressURI}=`,
    `${fixture.progressURI}?raw=1`,
    checkpointRoute(fixture, `tree/v1/${Buffer.from("inputs/../inputs-private/secret.txt").toString("base64url")}`),
    checkpointRoute(fixture, `tree/v1/${Buffer.from("inputs").toString("base64url")}/00`),
    checkpointRoute(fixture, `files/${Buffer.from("inputs").toString("base64url")}`),
    fixture.overviewURI.replace(fixture.checkpointId, "another-checkpoint"),
    fixture.overviewURI.replace(fixture.workspaceId, "another-workspace"),
    checkpointRoute(fixture, "tree/v1/aW5wdXRz/00"),
    checkpointRoute(fixture, "tree/v1/aW5wdXRz/10001"),
    checkpointRoute(fixture, "ranges/v1/aW5wdHMvZmlsZS8/0/0"),
    checkpointRoute(fixture, "ranges/v1/aW5wdHMvZmlsZS8/0/65537"),
    checkpointRoute(fixture, "ranges/v1/aW5wdHMvZmlsZS8/67108865/1"),
  ];
  for (const uri of invalidURIs) {
    const evidence = parseEvents(fixture, JSON.stringify({
      total_units: 66, entry_count: 5, sources: [fixture.progressURI, fixture.dataURI],
    }), [tool("read_mcp_resource", { uri })]);
    assert.throws(() => verifyCheckpointEvidence(evidence, fixture), /selected checkpoint/);
  }
});

function validEvidence(fixture) {
  return parseEvents(fixture, JSON.stringify({
    total_units: 66, entry_count: 5, sources: [fixture.progressURI, fixture.dataURI],
  }));
}

function parseEvents(fixture, answer, extraReads = [], readOverrides = {}) {
  const events = [
    tool("list_mcp_resources", { server: "stow" }),
    ...[fixture.overviewURI, fixture.progressURI, fixture.dataURI].map(uri => {
      const text = readOverrides[uri] ?? expectedReadText(fixture, uri);
      return tool("read_mcp_resource", { uri }, { uri, contents: [{ type: "text", uri, text, mimeType: "text/plain" }] });
    }),
    ...extraReads,
    { type: "text", part: { type: "text", messageID: "final", id: "answer", text: answer } },
  ];
  return parseOpenCodeJSONLines(events.map(item => JSON.stringify(item)).join("\n"));
}

function tool(name, input, output) {
  const result = output ?? (name === "list_mcp_resources"
    ? { resources: [], templates: [] }
    : { uri: input.uri, contents: [{ type: "text", uri: input.uri, text: "{}", mimeType: "application/json" }] });
  return {
    type: "tool",
    part: { type: "tool", tool: name, state: {
      status: "completed", input, output: JSON.stringify(result),
      metadata: { metadata: {}, content: [], providerCall: {}, providerResult: { executed: false }, rawInput: input },
    } },
  };
}

function checkpointFileURI(fixture, path) {
  return `stow://workspaces/${fixture.workspaceId}/checkpoints/${fixture.checkpointId}/files/${Buffer.from(path).toString("base64url")}`;
}

function checkpointRoute(fixture, route) {
  return `stow://workspaces/${fixture.workspaceId}/checkpoints/${fixture.checkpointId}/${route}`;
}

function sampleFixture() {
  const fixture = {
    workspaceId: "receiver", checkpointId: "cp-selected", expectedTotal: 66, expectedEntryCount: 5,
    savedProgress: "Prior state: three accepted entries total 46 units. Continue from that verified total using the next batch.",
    savedNextBatch: "item,units\ndelta,7\nepsilon,13\n",
  };
  fixture.overviewURI = checkpointRoute(fixture, "v1");
  fixture.progressURI = checkpointFileURI(fixture, "inputs/progress.txt");
  fixture.dataURI = checkpointFileURI(fixture, "inputs/next-batch.csv");
  return fixture;
}

function expectedReadText(fixture, uri) {
  if (uri === fixture.progressURI) return fixture.savedProgress;
  if (uri === fixture.dataURI) return fixture.savedNextBatch;
  return JSON.stringify({ view: "checkpoint files", workspace_id: fixture.workspaceId, checkpoint_id: fixture.checkpointId });
}

function answerText(fixture) {
  return JSON.stringify({ total_units: 66, entry_count: 5, sources: [fixture.progressURI, fixture.dataURI] });
}

function profile() {
  return {
    version: 1, opencodeBinary: "/opt/opencode", expectedVersion: "opencode v2.2.7", stowBinary: "/opt/stow-s3",
    model: { providerID: "openrouter", id: "openrouter/free" }, credentialEnv: "OPENROUTER_API_KEY",
    credentialFile: "/private/openrouter.key", deadlineMs: 300000, maxTurns: 2, allowPaidModels: false,
  };
}
