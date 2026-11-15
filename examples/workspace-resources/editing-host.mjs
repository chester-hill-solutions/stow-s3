import { createFreeOnlyProxy, buildFreeOnlyOpenCodeConfig, proxyGuardError } from "./free-only.mjs";
import { parseOpenCodeJSONLines } from "./run.mjs";
import { isCanonicalWorkspaceReadPath } from "./editing-verifier.mjs";

export const editingTools = [
  "stow_workspace_file_read_for_save", "stow_workspace_file_save",
  "stow_workspace_file_resolve_save", "stow_workspace_file_release_observation",
];
export const editingToolNameMap = Object.fromEntries(editingTools.map(name => [`stow_${name}`, name]));

export function editingSchema(requestKey, generation) {
  return {
    type: "object", additionalProperties: false,
    required: ["total_units", "entry_count", "outcome", "path", "request_key", "generation"],
    properties: {
      total_units: { type: "integer" }, entry_count: { type: "integer" },
      outcome: { type: "string", enum: ["committed"] },
      path: { type: "string", enum: ["docs/STATE.json"] },
      request_key: { type: "string", enum: [requestKey] },
      generation: generation ? { type: "string", enum: [generation] } : { type: "string", minLength: 1 },
    },
  };
}

// This wrapper changes only the opt-in editing pilot. The existing resource
// runners keep their original proxy and verifier behavior.
export function createEditingProxy({ toolNameMap, requestKey, schema, readRoots, fetchImpl = fetch, enforced = [], ...options }) {
  validateToolNameMap(toolNameMap);
  if (typeof requestKey !== "string" || requestKey.length === 0) throw new Error("workspace request key is required");
  if (!Array.isArray(readRoots) || readRoots.length === 0) throw new Error("workspace read roots are required");
  const allowed = new Set(Object.keys(toolNameMap));
  const readToolName = Object.entries(toolNameMap).find(([, name]) => name === "stow_workspace_file_read_for_save")?.[0];
  const saveToolName = Object.entries(toolNameMap).find(([, name]) => name === "stow_workspace_file_save")?.[0];
  let initialReadRequested = false;
  let initialReadVerified = false;
  let commitProof = null;
  return createFreeOnlyProxy({ ...options, fetchImpl: async (url, init) => {
    if (init.method !== "POST") return fetchImpl(url, init);
    let body;
    try { body = JSON.parse(init.body.toString()); } catch { throw proxyGuardError("request_invalid"); }
    const catalog = body.tools ?? [];
    if (!Array.isArray(catalog)) throw proxyGuardError("request_invalid");
    const selected = catalog.filter(tool => allowed.has(tool?.function?.name));
    const names = selected.map(tool => tool.function.name);
    if (!hasCompleteToolCatalog(names, toolNameMap)) {
      enforced.push({ initialReadRequired: true, phase: "catalog_refused", initialReadVerified, schemaEnforced: false, toolNames: names });
      throw proxyGuardError("catalog_refused");
    }
    body.tools = selected;
    body.provider = { ...body.provider, require_parameters: true };
    if (!initialReadRequested) {
      initialReadRequested = true;
      body.tool_choice = { type: "function", function: { name: readToolName } };
      delete body.response_format;
      enforced.push({ initialReadRequired: true, phase: "initial_read", initialReadVerified: false,
        schemaEnforced: false, toolNames: names });
      return fetchImpl(url, { ...init, body: JSON.stringify(body) });
    }
    const history = inspectEditingHistory(body.messages, toolNameMap, readToolName, saveToolName, requestKey, readRoots);
    if (!history.valid || !history.readVerified) {
      enforced.push({ initialReadRequired: true, phase: "history_refused", initialReadVerified: false,
        commitProved: false, schemaEnforced: false, toolNames: names });
      throw proxyGuardError("history_refused");
    }
    initialReadVerified = true;
    if (!history.commit) {
      body.tool_choice = "required";
      delete body.response_format;
      enforced.push({ initialReadRequired: true, phase: "editing_before_commit", initialReadVerified: true,
        commitProved: false, schemaEnforced: false, toolChoice: "required", toolNames: names });
      return fetchImpl(url, { ...init, body: JSON.stringify(body) });
    }
    if (commitProof && commitProof !== history.commit.generation) {
      throw proxyGuardError("history_refused");
    }
    commitProof = history.commit.generation;
    body.tool_choice = "none";
    const terminalSchema = editingSchema(requestKey, commitProof);
    body.response_format = { type: "json_schema", json_schema: { name: "stow_editing_result", strict: true, schema: terminalSchema } };
    enforced.push({ initialReadRequired: true, phase: "terminal_after_commit", initialReadVerified: true,
      commitProved: true, schemaEnforced: true, toolChoice: "none", generation: commitProof, toolNames: names });
    return fetchImpl(url, { ...init, body: JSON.stringify(body) });
  } });
}

export function buildEditingConfig(profile, fixture, proxyBaseURL, toolNameMap) {
  validateToolNameMap(toolNameMap);
  const config = buildFreeOnlyOpenCodeConfig(profile, { ...fixture, proxyBaseURL });
  config.mcp.servers.stow.codemode = false;
  const readArgs = fixture.readRoots.flatMap(root => ["--read-root", root]);
  config.mcp.servers.stow.command = [profile.stowBinary, "mcp", "--registry-dir", fixture.registryDir,
    "--workspace-id", fixture.workspaceId, "--workspace-files", "--workspace-edit", "--timeout", "10s",
    ...readArgs, "--write-root", "docs", "--coordination", "host_quiesced"];
  config.permissions = [{ action: "*", resource: "*", effect: "deny" },
    ...Object.keys(toolNameMap).map(action => ({ action, resource: "*", effect: "allow" }))];
  return config;
}

export function editingPrompt(requestKey) {
  return `Use only the provided Stow MCP file tools. Read docs/STATE.json and the CSV file named by its next_batch field. You may read other files within the granted docs root if useful; do not access paths outside that root. Add the named CSV's units to total_units and its number of data rows to entry_count. When processing docs/batch-a.csv, set next_batch to docs/batch-b.csv; when processing docs/batch-b.csv, set next_batch to null. Save only docs/STATE.json, preserving exactly those three state keys, using the observation_token from reading that state and request_key ${requestKey}. Make exactly one save; do not retry or replace. If any operation fails, stop. Do not call resolve_save or release_observation; the host verifies the retained receipt and owns cleanup. Do not use shell, native filesystem tools, resources, or checkpoints. After the tool reports the committed save, return only the required JSON object reporting the saved total_units and entry_count, outcome, path, request_key and generation copied from that save response. No Markdown or trailing prose.`;
}

export function parseEditingEvents(stdout, toolNameMap) {
  validateToolNameMap(toolNameMap);
  if (Buffer.byteLength(stdout) > 4 << 20) throw new Error("editing output exceeds bounds");
  const parsed = parseOpenCodeJSONLines(stdout);
  const values = stdout.split(/\r?\n/).filter(line => line.trim()).map(line => JSON.parse(line));
  if (values.some(event => event.type === "error" || event.error)) throw new Error("OpenCode reported an error");
  const toolTrace = [];
  let lastToolIndex = -1;
  for (const [eventIndex, event] of values.entries()) {
    const part = event.part;
    if (!(event.type === "tool" || event.type === "tool_use" || event.type === "tool_call" || part?.type === "tool")) continue;
    lastToolIndex = eventIndex;
    const state = part?.state ?? event.state ?? {};
    const rawName = part?.tool ?? part?.name ?? event.tool ?? event.name ?? "unknown";
    const rawOutput = state.output ?? event.output;
    let output;
    try { output = typeof rawOutput === "string" ? JSON.parse(rawOutput) : rawOutput; } catch { output = null; }
    // Preserve unknown tools for the verifier to reject; never filter the trace.
    toolTrace.push({ name: toolNameMap[rawName] ?? rawName, status: state.status ?? event.status ?? "unknown",
      input: state.input ?? event.input ?? part?.input, output,
      failed: Boolean(state.error || event.error || output?.isError), resultValid: Boolean(output && typeof output === "object") });
  }
  if (lastToolIndex < 0) throw new Error("editing tool evidence is missing");
  const afterTools = parseOpenCodeJSONLines(values.slice(lastToolIndex + 1).map(event => JSON.stringify(event)).join("\n"));
  if (!afterTools.finalText || afterTools.finalText !== parsed.finalText) {
    throw new Error("terminal editing result did not follow completed tools");
  }
  // The entire terminal assistant message must be JSON. Never extract a fenced
  // block, substring or earlier correct answer from a failed terminal response.
  const terminalStructured = JSON.parse(parsed.finalText);
  return { terminalStructured, toolTrace, eventCount: parsed.events };
}

export function validateToolNameMap(map) {
  if (!map || typeof map !== "object" || Object.keys(map).length !== editingTools.length ||
      new Set(Object.values(map)).size !== editingTools.length ||
      Object.entries(map).some(([key, value]) => editingToolNameMap[key] !== value)) throw new Error("editing tool map is invalid");
}

function hasCompleteToolCatalog(names, toolNameMap) {
  const expected = Object.keys(toolNameMap).sort();
  return names.length === expected.length && new Set(names).size === names.length &&
    [...names].sort().every((name, index) => name === expected[index]);
}

function inspectEditingHistory(messages, toolNameMap, readToolName, saveToolName, requestKey, readRoots) {
  if (!Array.isArray(messages)) return { valid: false, readVerified: false, commit: null };
  const calls = new Map();
  const seenCallIDs = new Set();
  const seenResultIDs = new Set();
  const readTokens = new Map();
  const saveCalls = [];
  for (const [messagePosition, message] of messages.entries()) {
    if (message?.role === "assistant" && Array.isArray(message.tool_calls)) {
      for (const call of message.tool_calls) {
        const id = call?.id;
        const name = call?.function?.name;
        if (typeof id !== "string" || !id || seenCallIDs.has(id) || !Object.hasOwn(toolNameMap, name)) {
          return { valid: false, readVerified: false, commit: null };
        }
        seenCallIDs.add(id);
        const args = parseObjectJSON(call.function.arguments);
        if (!args) return { valid: false, readVerified: false, commit: null };
        if (name === readToolName && !exactKeys(args, ["path"])) {
          return { valid: false, readVerified: false, commit: null };
        }
        if (name === "stow_stow_workspace_file_resolve_save" || name === "stow_stow_workspace_file_release_observation") {
          return { valid: false, readVerified: false, commit: null };
        }
        const entry = { name, args, result: null, callPosition: messagePosition };
        calls.set(id, entry);
        if (name === saveToolName) saveCalls.push(entry);
      }
    }
    if (message?.role === "tool") {
      const id = message.tool_call_id;
      if (typeof id !== "string" || !calls.has(id) || seenResultIDs.has(id) || typeof message.content !== "string") {
        return { valid: false, readVerified: false, commit: null };
      }
      seenResultIDs.add(id);
      const entry = calls.get(id);
      entry.result = parseObjectJSON(message.content);
      if (!entry.result) return { valid: false, readVerified: false, commit: null };
      if (entry.result.isError === true || hasNonemptyError(entry.result.error)) {
        return { valid: false, readVerified: false, commit: null };
      }
      if (entry.name === readToolName) {
        if (!successfulFileRead(entry.result, entry.args.path, readRoots)) {
          return { valid: false, readVerified: false, commit: null };
        }
        if (entry.args.path === "docs/STATE.json") readTokens.set(entry.result.observation_token, messagePosition);
      }
    }
  }
  if ([...calls.keys()].some(id => !seenResultIDs.has(id)) || saveCalls.length > 1) {
    return { valid: false, readVerified: readTokens.size > 0, commit: null };
  }
  if (saveCalls.length === 0) return { valid: true, readVerified: readTokens.size > 0, commit: null };
  const save = saveCalls[0];
  const result = save.result;
  const args = save.args;
  const bodyValid = (typeof args.text === "string") !== (typeof args.data_base64 === "string");
  if (!readTokens.has(args.observation_token) || readTokens.get(args.observation_token) >= save.callPosition || args.path !== "docs/STATE.json" ||
      args.request_key !== requestKey || args.replace === true || !bodyValid || !successfulCommittedSave(result)) {
    return { valid: false, readVerified: readTokens.size > 0, commit: null };
  }
  return { valid: true, readVerified: readTokens.size > 0, commit: { generation: result.generation } };
}

function successfulFileRead(result, path, readRoots) {
  if (!result || !isCanonicalWorkspaceReadPath(path, readRoots) || result.isError === true || hasNonemptyError(result.error) ||
      result.outcome !== "observed" || result.available !== true || result.path !== path ||
      result.absent === true || typeof result.observation_token !== "string" || !result.observation_token || typeof result.data_base64 !== "string") return false;
  return canonicalNonemptyBase64(result.data_base64);
}

function canonicalNonemptyBase64(value) {
  try {
    const bytes = Buffer.from(value, "base64");
    return bytes.length > 0 && bytes.toString("base64") === value;
  } catch { return false; }
}

function successfulCommittedSave(result) {
  return result && result.isError !== true && !hasNonemptyError(result.error) &&
    result.outcome === "committed" && result.committed !== false &&
    (result.path ?? result.object?.path) === "docs/STATE.json" &&
    typeof result.generation === "string" && result.generation.length > 0;
}

function hasNonemptyError(error) {
  if (error === undefined || error === null || error === false || error === "") return false;
  if (Array.isArray(error)) return error.length > 0;
  if (typeof error === "object") return Object.keys(error).length > 0;
  return true;
}

function parseObjectJSON(value) {
  if (typeof value !== "string") return null;
  try {
    const parsed = JSON.parse(value);
    return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : null;
  } catch { return null; }
}

function exactKeys(value, keys) {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).length === keys.length && keys.every(key => Object.hasOwn(value, key));
}
