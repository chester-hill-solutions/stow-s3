import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { isAbsolute } from "node:path";

export const freeSelector = "openrouter/openrouter/free";
export const openRouterFreeModel = "openrouter/free";
export const proxyPlaceholder = "stow-local-openrouter-proxy-only";
const failureKinds = new Set(["none", "catalog_refused", "history_refused", "request_invalid",
  "upstream_exception", "upstream_http_error", "response_invalid"]);
const openRouterOrigin = "https://openrouter.ai";
const modelListPath = "/api/v1/models";
const completionPath = "/api/v1/chat/completions";
const maxRequestBytes = 4 << 20;
const maxResponseBytes = 8 << 20;

export async function loadFreePilotProfile(filePath) {
  const profile = JSON.parse(await readFile(filePath, "utf8"));
  validateFreePilotProfile(profile);
  return profile;
}

export function validateFreePilotProfile(profile) {
  if (profile?.version !== 1 || profile.expectedVersion !== "opencode v2.2.7") throw new Error("unsupported OpenCode pilot profile");
  if (!isAbsolute(profile.opencodeBinary) || !isAbsolute(profile.stowBinary)) throw new Error("pilot binaries must be absolute paths");
  if (profile.model?.providerID !== "openrouter" || profile.model.id !== openRouterFreeModel) throw new Error("pilot model must be OpenRouter Free Router");
  if (profile.credentialEnv !== "OPENROUTER_API_KEY" || !isAbsolute(profile.credentialFile)) throw new Error("pilot credential source is invalid");
  if (!Number.isInteger(profile.deadlineMs) || profile.deadlineMs <= 0 || profile.deadlineMs > 300_000) throw new Error("pilot deadline exceeds five minutes");
  if (profile.maxTurns !== 2 || profile.allowPaidModels !== false) throw new Error("pilot must remain free-only and limited to two prompts");
}

export async function loadOpenRouterCredential(profile, environment = process.env) {
  const fromEnvironment = environment[profile.credentialEnv]?.trim();
  const credential = fromEnvironment || (await readFile(profile.credentialFile, "utf8")).trim();
  if (!credential) throw new Error("OpenRouter credential is unavailable");
  return credential;
}

export function buildFreeOnlyOpenCodeConfig(profile, { workspaceId, registryDir, workspaceRoot, proxyBaseURL, checkpointId }) {
  validateFreePilotProfile(profile);
  if (!workspaceId || !registryDir || !workspaceRoot || !proxyBaseURL.startsWith("http://127.0.0.1:")) {
    throw new Error("isolated workspace proxy configuration is incomplete");
  }
  if (checkpointId !== undefined && (typeof checkpointId !== "string" || !checkpointId.trim())) {
    throw new Error("checkpoint selector is invalid");
  }
  const scope = `stow:stow://workspaces/${workspaceId}/*`;
  const command = [profile.stowBinary, "mcp", "--registry-dir", registryDir, "--workspace-id", workspaceId,
    "--workspace-files", "--resources-only", "--timeout", "10s", "--read-root", "inputs"];
  if (checkpointId !== undefined) command.push("--checkpoint-id", checkpointId);
  return {
    $schema: "https://opencode.ai/config.json",
    model: freeSelector,
    agents: {
      title: { model: freeSelector },
      compaction: { model: freeSelector },
      summary: { model: freeSelector },
    },
    providers: { openrouter: { settings: { baseURL: proxyBaseURL } } },
    policies: [
      { action: "provider.use", resource: "*", effect: "deny" },
      { action: "provider.use", resource: "openrouter", effect: "allow" },
    ],
    permissions: [
      { action: "*", resource: "*", effect: "deny" },
      { action: "opencode_list_mcp_resources", resource: "stow", effect: "allow" },
      { action: "opencode_read_mcp_resource", resource: scope, effect: "allow" },
    ],
    mcp: {
      servers: {
        stow: {
          type: "local",
          command,
          cwd: workspaceRoot,
          environment: { OPENROUTER_API_KEY: "" },
        },
      },
    },
  };
}

export function createFreeOnlyProxy({ apiKey, fetchImpl = fetch, signal, record = () => {} }) {
  if (!apiKey) throw new Error("proxy credential is unavailable");
  return createServer(async (request, response) => {
    const context = { method: request.method ?? "", path: safePath(request.url), requestedModel: null,
      resolvedModel: null, stage: "guard", failureKind: "request_invalid", upstreamAttempted: false };
    try {
      await handleProxyRequest(request, response, { apiKey, fetchImpl, signal, record, context });
    } catch (error) {
      const diagnostic = { ...requestDiagnostic(context), ...safeProxyDiagnostic(error?.proxyDiagnostic) };
      record({ ...diagnostic, status: 502 });
      if (!response.headersSent) response.writeHead(502, { "content-type": "application/json" });
      response.end('{"error":"free model proxy unavailable"}');
    }
  });
}

export function proxyGuardError(failureKind) {
  const kind = failureKinds.has(failureKind) && failureKind !== "none" ? failureKind : "request_invalid";
  const error = new Error("proxy request refused");
  error.proxyDiagnostic = { stage: "guard", failureKind: kind, upstreamAttempted: false };
  return error;
}

export async function handleProxyRequest(request, response, options) {
  const path = safePath(request.url);
  const context = options.context ?? { method: request.method ?? "", path, requestedModel: null,
    resolvedModel: null, stage: "guard", failureKind: "request_invalid", upstreamAttempted: false };
  if (request.method === "GET" && path === modelListPath && !new URL(request.url, "http://127.0.0.1").search) {
    context.stage = "upstream";
    context.failureKind = "upstream_exception";
    context.upstreamAttempted = true;
    const upstream = await options.fetchImpl(openRouterOrigin + modelListPath, {
      method: "GET", headers: { authorization: `Bearer ${options.apiKey}` }, redirect: "error", signal: proxySignal(options.signal),
    });
    context.stage = "upstream";
    context.failureKind = "response_invalid";
    const body = await readBoundedBody(upstream.body, maxResponseBytes);
    const filtered = filteredFreeModelCatalog(body);
    options.record({ ...requestDiagnostic(context), failureKind: upstream.ok ? "none" : "upstream_http_error", status: upstream.status });
    send(response, upstream.status, upstream.headers.get("content-type") ?? "application/json", filtered);
    return;
  }
  if (request.method !== "POST" || path !== completionPath || new URL(request.url, "http://127.0.0.1").search) {
    context.failureKind = "request_invalid";
    options.record({ ...requestDiagnostic(context), status: 404 });
    send(response, 404, "application/json", Buffer.from('{"error":"route is not allowed"}'));
    return;
  }
  const requestBody = await readRequestBody(request, maxRequestBytes);
  const payload = JSON.parse(requestBody.toString("utf8"));
  const requestedModel = typeof payload.model === "string" ? payload.model.slice(0, 160) : "";
  context.requestedModel = requestedModel || null;
  if (payload.model !== openRouterFreeModel || Object.hasOwn(payload, "models") || Object.hasOwn(payload, "route")) {
    context.failureKind = "request_invalid";
    options.record({ ...requestDiagnostic(context), status: 403 });
    send(response, 403, "application/json", Buffer.from('{"error":"only OpenRouter Free Router is allowed"}'));
    return;
  }
  context.stage = "upstream";
  context.failureKind = "upstream_exception";
  context.upstreamAttempted = true;
  const upstream = await options.fetchImpl(openRouterOrigin + completionPath, {
    method: "POST",
    headers: { authorization: `Bearer ${options.apiKey}`, "content-type": "application/json" },
    body: requestBody,
    redirect: "error",
    signal: proxySignal(options.signal),
  });
  context.stage = "upstream";
  context.failureKind = "response_invalid";
  const body = await readBoundedBody(upstream.body, maxResponseBytes);
  const resolvedModel = modelFromResponse(body, upstream.headers.get("content-type"));
  context.resolvedModel = resolvedModel;
  options.record({ ...requestDiagnostic(context), failureKind: !upstream.ok ? "upstream_http_error" : resolvedModel ? "none" : "response_invalid", status: upstream.status });
  send(response, upstream.status, upstream.headers.get("content-type") ?? "application/json", body);
}

function requestDiagnostic(context) {
  return { method: context.method, path: context.path, requestedModel: context.requestedModel,
    resolvedModel: context.resolvedModel, stage: context.stage, failureKind: context.failureKind,
    upstreamAttempted: context.upstreamAttempted };
}

function safeProxyDiagnostic(value) {
  if (!value || typeof value !== "object") return {};
  const safe = {};
  if (["guard", "upstream"].includes(value.stage)) safe.stage = value.stage;
  if (failureKinds.has(value.failureKind)) safe.failureKind = value.failureKind;
  if (typeof value.upstreamAttempted === "boolean") safe.upstreamAttempted = value.upstreamAttempted;
  return safe;
}

function proxySignal(pilotSignal) {
  const timeout = AbortSignal.timeout(290_000);
  return pilotSignal ? AbortSignal.any([pilotSignal, timeout]) : timeout;
}

function filteredFreeModelCatalog(body) {
  let catalog;
  try { catalog = JSON.parse(body.toString("utf8")); } catch { throw new Error("model catalog is invalid"); }
  if (!Array.isArray(catalog.data)) throw new Error("model catalog is invalid");
  const free = catalog.data.filter(model => model?.id === openRouterFreeModel);
  if (free.length !== 1) throw new Error("OpenRouter Free Router is unavailable");
  return Buffer.from(JSON.stringify({ ...catalog, data: free }));
}

function modelFromResponse(body, contentType) {
  if (contentType?.includes("json")) {
    try { return JSON.parse(body.toString("utf8")).model ?? null; } catch { return null; }
  }
  const models = new Set();
  for (const line of body.toString("utf8").split("\n")) {
    if (!line.startsWith("data: ")) continue;
    try {
      const model = JSON.parse(line.slice(6)).model;
      if (typeof model === "string") models.add(model);
    } catch {}
  }
  return [...models][0] ?? null;
}

function safePath(requestURL) {
  try { return new URL(requestURL ?? "/", "http://127.0.0.1").pathname.slice(0, 160); } catch { return "invalid"; }
}

async function readRequestBody(request, limit) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > limit) throw new Error("request exceeds bounds");
    chunks.push(chunk);
  }
  return Buffer.concat(chunks, size);
}

async function readBoundedBody(stream, limit) {
  if (!stream) return Buffer.alloc(0);
  const chunks = [];
  let size = 0;
  for await (const chunk of stream) {
    size += chunk.length;
    if (size > limit) throw new Error("response exceeds bounds");
    chunks.push(chunk);
  }
  return Buffer.concat(chunks, size);
}

function send(response, status, contentType, body) {
  response.writeHead(status, { "content-type": contentType });
  response.end(body);
}
