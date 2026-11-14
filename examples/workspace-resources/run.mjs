import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { promisify } from "node:util";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import {
  buildFreeOnlyOpenCodeConfig,
  createFreeOnlyProxy,
  freeSelector,
  loadFreePilotProfile,
  loadOpenRouterCredential,
  proxyPlaceholder,
} from "./free-only.mjs";

const exec = promisify(execFile);
const promptText = "Use only MCP resource tools. List Stow resources, then read the workspace overview URI below. Read the brief and every source file it requires, following the paged directory's next_uri verbatim. Return only JSON shaped as {mismatches:[{item,source_a,source_z,difference}],sources:[URI_A,URI_Z]}. Use values from the resources; do not add explanations. Do not use filesystem or shell tools.\n\nTask root: ";
const defaultTracePath = "/private/tmp/stow-w24-openrouter-free-trace.json";

export async function runFreeWorkspacePilot(profilePath, { tracePath = defaultTracePath, diagnosticPath = "/private/tmp/stow-w24-openrouter-free-events.jsonl" } = {}) {
  const profile = await loadFreePilotProfile(profilePath);
  const deadline = new AbortController();
  const startedAt = Date.now();
  const timeout = setTimeout(() => deadline.abort(), profile.deadlineMs);
  let tempRoot;
  let proxy;
  let stage = "credential";
  let fixture;
  let records = [];
  let evidence;
  let outcome;
  try {
    const credential = await loadOpenRouterCredential(profile);
    tempRoot = await mkdtemp(join(tmpdir(), "stow-workspace-openrouter-free-"));
    const environment = isolatedOpenCodeEnvironment(tempRoot);
    stage = "opencode_cli_check";
    await assertOpenCodeCLI(profile, environment, tempRoot, deadline.signal, startedAt);
    stage = "synthetic_workspace";
    fixture = await prepareSyntheticWorkspace(profile.stowBinary, tempRoot, deadline.signal, startedAt, profile.deadlineMs);
    const runDir = join(tempRoot, "opencode-project");
    await mkdir(runDir, { recursive: true, mode: 0o700 });
    stage = "loopback_guard";
    proxy = createFreeOnlyProxy({ apiKey: credential, signal: deadline.signal, record: entry => records.push(entry) });
    const proxyAddress = await listenLoopback(proxy);
    const proxyBaseURL = `http://127.0.0.1:${proxyAddress.port}/api/v1`;
    const config = buildFreeOnlyOpenCodeConfig(profile, { ...fixture, proxyBaseURL });
    await writeFile(join(runDir, "opencode.json"), JSON.stringify(config, null, 2), { mode: 0o600, flag: "wx" });
    const opencodeEnvironment = { ...environment, OPENROUTER_API_KEY: proxyPlaceholder };
    stage = "opencode_run";
    const { stdout } = await exec(profile.opencodeBinary,
      ["run", "--standalone", "--format", "json", "--model", freeSelector, "--agent", "build", "--title", "Workspace resource review", promptText + fixture.overviewURI],
      { cwd: runDir, env: opencodeEnvironment, timeout: remainingTime(profile.deadlineMs, startedAt), signal: deadline.signal, maxBuffer: 16 << 20 });
    await writeBoundedDiagnostic(diagnosticPath, stdout);
    evidence = parseOpenCodeJSONLines(stdout);
    stage = "evidence_verification";
    verifyPilotEvidence(evidence, fixture);
    const modelEvidence = records.filter(record => record.path.endsWith("/chat/completions"));
    verifyFreeRouteEvidence(modelEvidence);
    outcome = {
      status: "passed",
      opencodeVersion: profile.expectedVersion,
      requestedSelector: freeSelector,
      workspaceId: fixture.workspaceId,
      taskRootURI: fixture.overviewURI,
      answerVerified: true,
      diagnosticPath,
      resourceToolTrace: evidence.toolTrace.map(safeToolTraceEntry),
      modelRequests: modelEvidence.map(({ method, path, requestedModel, resolvedModel, status }) => ({ method, path, requestedModel, resolvedModel, status })),
    };
  } catch (error) {
    if (typeof error?.stdout === "string") {
      try { await writeBoundedDiagnostic(diagnosticPath, error.stdout); } catch { /* Keep the sanitized failure trace authoritative. */ }
    }
    if (typeof error?.stderr === "string") {
      try { await writeBoundedDiagnostic(`${diagnosticPath}.stderr`, error.stderr); } catch { /* Keep the sanitized failure trace authoritative. */ }
    }
    const modelEvidence = records.filter(record => record.path.endsWith("/chat/completions"));
    outcome = {
      status: "failed",
      category: failureCategory(error, stage, deadline.signal.aborted),
      stage,
      requestedSelector: freeSelector,
      workspaceId: fixture?.workspaceId ?? "",
      taskRootURI: fixture?.overviewURI ?? "",
      answerVerified: false,
      diagnosticPath,
      resourceToolTrace: evidence?.toolTrace?.map(safeToolTraceEntry) ?? [],
      modelRequests: modelEvidence.map(({ method, path, requestedModel, resolvedModel, status }) => ({ method, path, requestedModel, resolvedModel, status })),
    };
  } finally {
    clearTimeout(timeout);
    deadline.abort();
    await closeProxy(proxy);
    if (tempRoot) await rm(tempRoot, { recursive: true, force: true });
  }
  await writeSanitizedTrace(tracePath, outcome);
  if (outcome.status !== "passed") throw Object.assign(new Error(`free-only pilot failed: ${outcome.category}`), { trace: outcome });
  return outcome;
}

export function isolatedOpenCodeEnvironment(root) {
  const home = join(root, "home");
  return {
    PATH: process.env.PATH ?? "/usr/bin:/bin",
    HOME: home,
    XDG_CONFIG_HOME: join(home, ".config"),
    XDG_DATA_HOME: join(home, ".local", "share"),
    XDG_CACHE_HOME: join(home, ".cache"),
    XDG_STATE_HOME: join(home, ".local", "state"),
    TMPDIR: root,
    NO_COLOR: "1",
    OPENROUTER_API_KEY: proxyPlaceholder,
  };
}

export async function assertOpenCodeCLI(profile, environment, root, signal, startedAt) {
  const common = { cwd: root, env: environment, signal, maxBuffer: 4096 };
  const { stdout: version } = await exec(profile.opencodeBinary, ["--version"], {
    ...common, timeout: Math.min(15000, remainingTime(profile.deadlineMs, startedAt)),
  });
  if (version.trim() !== profile.expectedVersion) throw new Error("OpenCode binary version differs from the pinned profile");
  const { stdout: help } = await exec(profile.opencodeBinary, ["run", "--help"], {
    ...common, timeout: Math.min(15000, remainingTime(profile.deadlineMs, startedAt)),
  });
  for (const flag of ["--standalone", "--format", "--model", "--agent", "--title"]) {
    if (!help.includes(flag)) throw new Error("OpenCode run flags differ from the pinned profile");
  }
}

export async function prepareSyntheticWorkspace(stowBinary, tempRoot, signal, startedAt, deadlineMs) {
  const sourceRoot = join(tempRoot, "source");
  const registryDir = join(tempRoot, "registry");
  const workspaceRoot = join(tempRoot, "workspace");
  await mkdir(join(sourceRoot, "inputs"), { recursive: true, mode: 0o700 });
  await mkdir(join(sourceRoot, "inputs-private"), { recursive: true, mode: 0o700 });
  const inputs = [];
  for (let index = 0; index < 100; index++) {
    const name = `00-decoy-${String(index).padStart(3, "0")}.txt`;
    const source = join(sourceRoot, "inputs", name);
    await writeFile(source, `unused ${index}`, { mode: 0o600 });
    inputs.push({ source, destination: `inputs/${name}` });
  }
  const briefPath = join(sourceRoot, "inputs", "10-brief.txt");
  const firstDataPath = join(sourceRoot, "inputs", "20-source-a.csv");
  const secondDataPath = join(sourceRoot, "inputs", "90-source-z.csv");
  await writeFile(briefPath, "Compare the values in both source files by item. Return mismatches with fields item, source_a, source_z, difference (source Z minus source A), and a sources array containing the exact resource URI for each source file in A-then-Z order. Use only values found in the source files.", { mode: 0o600 });
  await writeFile(firstDataPath, "item,value\nalpha,17\nbeta,23\ngamma,5\n", { mode: 0o600 });
  await writeFile(secondDataPath, "item,value\nalpha,17\nbeta,24\ngamma,5\n", { mode: 0o600 });
  inputs.push({ source: briefPath, destination: "inputs/10-brief.txt" });
  inputs.push({ source: firstDataPath, destination: "inputs/20-source-a.csv" });
  inputs.push({ source: secondDataPath, destination: "inputs/90-source-z.csv" });
  const privatePath = join(sourceRoot, "inputs-private", "secret.txt");
  await writeFile(privatePath, "PRIVATE_SENTINEL_DO_NOT_READ", { mode: 0o600 });
  inputs.push({ source: privatePath, destination: "inputs-private/secret.txt" });
  const manifestPath = join(tempRoot, "manifest.json");
  await writeFile(manifestPath, JSON.stringify({
    version: 1,
    root: workspaceRoot,
    registry_dir: registryDir,
    working_directory: "inputs",
    max_bytes: 65536,
    max_objects: 110,
    inputs,
  }), { mode: 0o600, flag: "wx" });
  const { stdout } = await exec(stowBinary, ["workspace", "prepare", "--manifest", manifestPath], {
    cwd: tempRoot, timeout: Math.min(30000, remainingTime(deadlineMs, startedAt)), signal, maxBuffer: 1 << 20,
    env: { PATH: process.env.PATH ?? "/usr/bin:/bin", HOME: join(tempRoot, "stow-home"), TMPDIR: tempRoot },
  });
  const prepared = JSON.parse(stdout);
  const fileURI = filePath => `stow://workspaces/${prepared.workspace_id}/files/${Buffer.from(filePath).toString("base64url")}`;
  return {
    workspaceId: prepared.workspace_id,
    registryDir: prepared.registry_dir,
    workspaceRoot: prepared.root,
    overviewURI: `stow://workspaces/${prepared.workspace_id}/v1`,
    requiredFileURIs: ["inputs/10-brief.txt", "inputs/20-source-a.csv", "inputs/90-source-z.csv"].map(fileURI),
    answerFileURIs: ["inputs/20-source-a.csv", "inputs/90-source-z.csv"].map(fileURI),
  };
}

export function listenLoopback(server) {
  return new Promise((resolveListen, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolveListen(server.address());
    });
  });
}

export async function closeProxy(server) {
  if (!server?.listening) return;
  server.closeAllConnections?.();
  await new Promise(resolveClose => server.close(resolveClose));
}

export function remainingTime(deadlineMs, startedAt) {
  const remaining = deadlineMs - (Date.now() - startedAt);
  if (remaining <= 0) throw new Error("pilot deadline elapsed");
  return remaining;
}

export function failureCategory(error, stage, timedOut) {
  if (timedOut || error?.name === "AbortError" || error?.code === "ETIMEDOUT") return "deadline";
  if (stage === "credential") return "credential_unavailable";
  if (stage === "opencode_cli_check") return "pinned_cli_check_failed";
  if (stage === "loopback_guard" && error?.code === "EPERM") return "loopback_proxy_unavailable";
  if (stage === "evidence_verification") return "evidence_rejected";
  if (stage === "opencode_run") return "model_run_failed";
  return "pilot_setup_failed";
}

export function safeToolTraceEntry({ name, status, uri }) {
  return { name, status, uri };
}

export async function writeSanitizedTrace(path, trace) {
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  await writeFile(path, `${JSON.stringify(trace, null, 2)}\n`, { mode: 0o600 });
}

export async function writeBoundedDiagnostic(path, stdout) {
  if (Buffer.byteLength(stdout) > 4 << 20) throw new Error("OpenCode event output exceeds diagnostic bounds");
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  await writeFile(path, stdout, { mode: 0o600 });
}

export function parseOpenCodeJSONLines(stdout) {
  const values = [];
  for (const line of stdout.split(/\r?\n/)) {
    if (!line.trim()) continue;
    try { values.push(JSON.parse(line)); } catch { throw new Error("OpenCode output is not JSON lines"); }
  }
  const finalText = terminalAssistantText(values);
  const toolTrace = values.flatMap(toolEvents);
  return { events: values.length, finalText, toolTrace };
}

function terminalAssistantText(events) {
  const keyed = new Map();
  let terminal;
  for (const event of events) {
    if (event.type !== "text") continue;
    const part = event.part;
    const messageID = part?.messageID ?? event.messageID ?? event.messageId;
    let message;
    if (typeof messageID === "string" && messageID) {
      message = keyed.get(messageID);
      if (!message) {
        message = { parts: new Map(), order: [] };
        keyed.set(messageID, message);
      }
      const partID = part?.id ?? event.partID ?? event.partId;
      const key = typeof partID === "string" && partID ? partID : `part-${message.order.length}`;
      if (!message.parts.has(key)) message.order.push(key);
      message.parts.set(key, textFromEvent(event));
      terminal = message;
      continue;
    }
    const text = textFromEvent(event);
    if (text) terminal = { text };
  }
  return terminal?.text ?? terminal?.order.map(key => terminal.parts.get(key)).filter(Boolean).join("") ?? "";
}

function textFromEvent(event) {
  const part = event.part;
  if (typeof part === "string") return part;
  return typeof part?.text === "string" ? part.text : typeof event.text === "string" ? event.text : "";
}

function toolEvents(event) {
  const part = event.part;
  if (!(event.type === "tool" || event.type === "tool_use" || event.type === "tool_call" || part?.type === "tool")) return [];
  const state = part?.state ?? event.state ?? {};
  const name = part?.tool ?? part?.name ?? event.tool ?? event.name ?? "unknown";
  const uri = findURI(state.input ?? event.input ?? part?.input);
  const rawOutput = state.output ?? event.output;
  const output = parseToolOutput(rawOutput);
  const status = state.status ?? event.status ?? "unknown";
  const malformedOutput = rawOutput !== undefined && rawOutput !== null && (!output || typeof output !== "object");
  const failed = malformedOutput || toolOutputFailed(output, state, event, status);
  const resultValid = !failed && validResourceToolResult(name, uri, output);
  const bodyEvidence = resultValid && isReadResourceToolName(name) ? resourceReadBodyEvidence(uri, output) : {};
  return [{ name, status, uri, failed, resultValid, ...bodyEvidence }];
}

function isReadResourceToolName(name) {
  return name === "read_mcp_resource" || name === "opencode_read_mcp_resource";
}

function parseToolOutput(output) {
  if (typeof output !== "string") return output;
  try { return JSON.parse(output); } catch { return null; }
}

function toolOutputFailed(output, state, event, status) {
  if (status === "error" || state.error || event.error) return true;
  if (!output || typeof output !== "object") return false;
  return containsToolError(output);
}

function containsToolError(value) {
  if (Array.isArray(value)) return value.some(containsToolError);
  if (!value || typeof value !== "object") return false;
  if (value.isError === true || (Object.hasOwn(value, "error") && value.error !== null && value.error !== false && value.error !== "")) return true;
  return Object.values(value).some(containsToolError);
}

function validResourceToolResult(name, uri, output) {
  if (!output || typeof output !== "object") return false;
  if (name === "list_mcp_resources" || name === "opencode_list_mcp_resources") {
    return Array.isArray(output.resources) && Array.isArray(output.templates);
  }
  if (!(name === "read_mcp_resource" || name === "opencode_read_mcp_resource") || !uri) return false;
  if (output.uri !== uri || !Array.isArray(output.contents) || output.contents.length === 0) return false;
  return output.contents.every(content => content?.uri === uri &&
    (typeof content.text === "string" || typeof content.blob === "string"));
}

function resourceReadBodyEvidence(uri, output) {
  const content = output.contents.find(item => item?.uri === uri && typeof item.text === "string");
  if (!content) return {};
  const body = Buffer.from(content.text, "utf8");
  return { contentBytes: body.length, contentSHA256: createHash("sha256").update(body).digest("hex") };
}

function findURI(value) {
  if (!value || typeof value !== "object") return "";
  for (const [key, child] of Object.entries(value)) {
    if (["uri", "resourceUri", "resource_uri"].includes(key) && typeof child === "string") return child;
    const nested = findURI(child);
    if (nested) return nested;
  }
  return "";
}

export function verifyPilotEvidence(evidence, fixture) {
  verifyFinalAnswer(evidence.finalText, fixture.answerFileURIs);
  const trace = evidence.toolTrace;
  if (!trace.some(item => isListResourceTool(item.name) && item.status === "completed")) {
    throw new Error("completed MCP resource-list call is missing");
  }
  if (trace.some(item => !isResourceTool(item.name) || item.status !== "completed")) {
    throw new Error("a non-resource or incomplete tool call was present");
  }
  const reads = trace.filter(item => isReadResourceTool(item.name));
  if (reads.some(item => !isWorkspaceURIInScope(item.uri, fixture.workspaceId))) {
    throw new Error("resource trace escaped the configured workspace scope");
  }
  const readURIs = reads.map(item => item.uri);
  if (!readURIs.includes(fixture.overviewURI) || fixture.requiredFileURIs.some(uri => !readURIs.includes(uri))) {
    throw new Error("required workspace resource reads are missing");
  }
  const continuationIndex = reads.findIndex(item => isPageContinuationURI(item.uri));
  const lateFileIndex = reads.findIndex(item => item.uri === fixture.requiredFileURIs[2]);
  if (continuationIndex < 0 || lateFileIndex <= continuationIndex) {
    throw new Error("late source was not reached through a completed page continuation");
  }
}

function verifyFinalAnswer(text, sourceURIs) {
  const answer = parseAnswerJSON(text);
  if (!answer || !Array.isArray(answer.mismatches) || answer.mismatches.length !== 1 || !Array.isArray(answer.sources)) {
    throw new Error("final assistant JSON does not match the required answer shape");
  }
  const [mismatch] = answer.mismatches;
  if (!hasExactKeys(answer, ["mismatches", "sources"]) || !hasExactKeys(mismatch, ["item", "source_a", "source_z", "difference"])) {
    throw new Error("final assistant JSON does not match the required answer shape");
  }
  if (mismatch?.item !== "beta" || exactInteger(mismatch.source_a) !== 23 || exactInteger(mismatch.source_z) !== 24 || exactInteger(mismatch.difference) !== 1) {
    throw new Error("final assistant JSON does not verify the expected mismatch");
  }
  if (answer.sources.length !== 2 || answer.sources[0] !== sourceURIs[0] || answer.sources[1] !== sourceURIs[1]) {
    throw new Error("final assistant JSON omitted exact source URIs");
  }
}

export function exactInteger(value) {
  if (typeof value === "number") return Number.isSafeInteger(value) ? value : undefined;
  if (typeof value !== "string" || !/^(?:0|-[1-9]\d*|[1-9]\d*)$/.test(value)) return undefined;
  const number = Number(value);
  return Number.isSafeInteger(number) ? number : undefined;
}

function parseAnswerJSON(text) {
  const trimmed = text.trim().replace(/^```json\s*([\s\S]*?)\s*```$/i, "$1").replace(/^```\s*([\s\S]*?)\s*```$/, "$1");
  try { return JSON.parse(trimmed); } catch { return null; }
}

function hasExactKeys(value, keys) {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).length === keys.length && keys.every(key => Object.hasOwn(value, key));
}

function isListResourceTool(name) {
  return name === "list_mcp_resources" || name === "opencode_list_mcp_resources";
}

function isReadResourceTool(name) {
  return name === "read_mcp_resource" || name === "opencode_read_mcp_resource";
}

function isResourceTool(name) { return isListResourceTool(name) || isReadResourceTool(name); }

function isPageContinuationURI(uri) {
  const route = uri.split("/tree-pages/v1/")[1];
  return typeof route === "string" && route.split("/").length === 2;
}

function isWorkspaceURIInScope(uri, workspaceId) {
  const prefix = `stow://workspaces/${workspaceId}/`;
  if (!uri.startsWith(prefix) || /[?#\u0000-\u001f\u007f]/.test(uri)) return false;
  const route = uri.slice(prefix.length);
  if (route === "v1") return true;
  const page = route.match(/^tree-pages\/v1\/([A-Za-z0-9_-]+)(?:\/([A-Za-z0-9_-]+))?$/);
  if (page) {
    const directory = decodeCanonicalBase64URL(page[1]);
    if (directory === null || !isSafeResourcePath(directory, true)) return false;
    if (directory !== "." && directory !== "inputs" && !directory.startsWith("inputs/")) return false;
    return page[2] === undefined || isCanonicalPageCursor(page[2]);
  }
  const file = route.match(/^files\/([A-Za-z0-9_-]+)$/);
  if (!file) return false;
  const path = decodeCanonicalBase64URL(file[1]);
  return path !== null && isSafeResourcePath(path, false) && path.startsWith("inputs/");
}

function isCanonicalPageCursor(encoded) {
  const value = decodeCanonicalBase64URL(encoded);
  if (value === null) return false;
  try {
    const cursor = JSON.parse(value);
    return cursor !== null && typeof cursor === "object" && !Array.isArray(cursor) &&
      Object.keys(cursor).length === 3 && cursor.v === 1 && typeof cursor.o === "string" && cursor.o.length > 0 &&
      Number.isInteger(cursor.n) && cursor.n > 0;
  } catch { return false; }
}

function decodeCanonicalBase64URL(value) {
  try {
    const bytes = Buffer.from(value, "base64url");
    if (bytes.toString("base64url") !== value) return null;
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch { return null; }
}

function isSafeResourcePath(path, allowRoot) {
  if (path === ".") return allowRoot;
  if (!path || path.length > 1024 || path.includes("\\") || /[\u0000-\u001f\u007f]/.test(path)) return false;
  const segments = path.split("/");
  return segments.every(segment => segment && segment !== "." && segment !== "..");
}

export function verifyFreeRouteEvidence(requests) {
  if (requests.length === 0 || requests.some(request => request.requestedModel !== "openrouter/free" || request.status < 200 || request.status >= 300)) {
    throw new Error("proxy observed a non-free or failed model request");
  }
  if (!requests.some(request => request.resolvedModel)) throw new Error("proxy did not record a concrete routed model");
}

if (process.argv[1] && import.meta.url === new URL(`file://${resolve(process.argv[1])}`).href) {
  const profilePath = process.argv[2] ?? process.env.STOW_W24_PROFILE ?? "/private/tmp/stow-w24-openrouter-free.json";
  const tracePath = process.argv[3] ?? process.env.STOW_W24_TRACE ?? defaultTracePath;
  const diagnosticPath = process.argv[4] ?? process.env.STOW_W24_DIAGNOSTIC ?? "/private/tmp/stow-w24-openrouter-free-events.jsonl";
  runFreeWorkspacePilot(resolve(profilePath), { tracePath, diagnosticPath }).then(trace => process.stdout.write(`${JSON.stringify(trace)}\n`)).catch(error => {
    const trace = error.trace ?? { status: "failed", category: "profile_or_setup_failed" };
    process.stderr.write(`${JSON.stringify(trace)}\n`);
    process.exitCode = 1;
  });
}
