import { createHash } from "node:crypto";

const statePath = "docs/STATE.json";
export const editingReadRoots = Object.freeze(["docs"]);
const batches = {
  stage1: { path: "docs/batch-a.csv", text: "item,units\nalpha,3\nbeta,5\ngamma,7\n", prior: { total_units: 11, entry_count: 2, next_batch: "docs/batch-a.csv" },
    next: { total_units: 26, entry_count: 5, next_batch: "docs/batch-b.csv" } },
  stage2: { path: "docs/batch-b.csv", text: "item,units\ndelta,13\nepsilon,17\n", prior: { total_units: 26, entry_count: 5, next_batch: "docs/batch-b.csv" },
    next: { total_units: 56, entry_count: 7, next_batch: null } },
};
const allowedTools = new Set([
  "stow_workspace_file_read_for_save", "stow_workspace_file_save",
  "stow_workspace_file_resolve_save", "stow_workspace_file_release_observation",
]);

export function expectedEditingState(stage) {
  const spec = stageSpec(stage);
  return { ...spec.next };
}

export function isCanonicalWorkspaceReadPath(path, readRoots) {
  if (typeof path !== "string" || !path || path.startsWith("/") || path.includes("\\") ||
      /[\u0000-\u001f\u007f]/u.test(path)) return false;
  const parts = path.split("/");
  if (parts.some(part => !part || part === "." || part === "..")) return false;
  return Array.isArray(readRoots) && readRoots.some(root => {
    if (typeof root !== "string" || !root || root.startsWith("/") || root.includes("\\") ||
        /[\u0000-\u001f\u007f]/u.test(root)) return false;
    const rootParts = root.split("/");
    if (rootParts.some(part => !part || part === "." || part === "..")) return false;
    return path === root || path.startsWith(`${root}/`);
  });
}

export function verifyEditingStageEvidence(evidence, fixture, stage) {
  const spec = stageSpec(stage);
  const name = stageName(stage);
  const key = fixture.requestKeys[name];
  const priorDigest = fixture.priorStateDigests?.[name];
  const trace = evidence?.toolTrace;
  verifyTerminal(evidence?.terminalStructured, spec.next, key);
  if (!Array.isArray(trace) || !trace.length || trace.some(item => !allowedTools.has(item?.name) ||
      item.status !== "completed" || item.failed === true || item.resultValid === false || item.output?.isError === true)) {
    throw new Error("editing trace contains an unapproved, failed, or incomplete MCP call");
  }
  const reads = trace.filter(item => item.name === "stow_workspace_file_read_for_save");
  const readRoots = fixture.readRoots;
  if (reads.some(item => !successfulScopedRead(item, readRoots))) {
    throw new Error("editing trace contains an invalid or out-of-scope file read");
  }
  const stateReads = reads.filter(item => item.input?.path === statePath);
  const batchReads = reads.filter(item => item.input?.path === spec.path);
  const saveEntries = trace.map((item, index) => ({ item, index })).filter(entry => entry.item.name === "stow_workspace_file_save");
  if (saveEntries.length !== 1) throw new Error("editing trace must contain exactly one file save");
  const saveIndex = saveEntries[0].index;
  const priorStateReads = stateReads.filter(item => readMatches(item, spec.prior, priorDigest));
  const priorStateReadsBeforeSave = priorStateReads.filter(item => trace.indexOf(item) < saveIndex);
  const fullBatchReads = batchReads.filter(item => bodyMatches(item.output, spec.path, spec.text));
  if (!priorDigest || !priorStateReadsBeforeSave.length ||
      !fullBatchReads.some(item => trace.indexOf(item) < saveIndex)) {
    throw new Error("editing trace is missing exact prior state or batch bytes");
  }
  verifyReleaseChronology(trace, statePath, saveIndex);
  verifySave(saveEntries[0].item, priorStateReadsBeforeSave, key, spec.next, evidence.terminalStructured.generation);
  if (trace.some(item => item.name === "stow_workspace_file_resolve_save")) {
    throw new Error("model must not resolve host-owned save receipts");
  }
  return true;
}

function verifyTerminal(answer, expected, requestKey) {
  const keys = ["total_units", "entry_count", "outcome", "path", "request_key", "generation"];
  if (!hasExactKeys(answer, keys) || !Number.isSafeInteger(answer.total_units) ||
      !Number.isSafeInteger(answer.entry_count) || answer.total_units !== expected.total_units ||
      answer.entry_count !== expected.entry_count || answer.outcome !== "committed" || answer.path !== statePath ||
      answer.request_key !== requestKey || typeof answer.generation !== "string" || !answer.generation) {
    throw new Error("terminal structured editing result failed verification");
  }
}

function verifySave(save, stateReads, requestKey, expected, terminalGeneration) {
  const input = save.input;
  const output = save.output;
  const observedTokens = new Set(stateReads.map(item => item.output.observation_token));
  const token = input?.observation_token;
  if (!input || input.path !== statePath || input.request_key !== requestKey || input.replace === true ||
      typeof token !== "string" || !token || !observedTokens.has(token) || (typeof input.text === "string") === (typeof input.data_base64 === "string")) {
    throw new Error("workspace save did not use its matching observed state and request key");
  }
  const bytes = input.text !== undefined ? Buffer.from(input.text, "utf8") : decodeBase64(input.data_base64);
  const state = parseState(bytes);
  if (!sameState(state, expected) || output?.outcome !== "committed" || typeof output.generation !== "string" ||
      !output.generation || output.generation !== terminalGeneration ||
      (output.path ?? output.object?.path) !== statePath || output.committed === false) {
    throw new Error("workspace save result or submitted state failed verification");
  }
}

function readMatches(item, expected, digest) {
  const bytes = decodeBodyOrNull(item.output);
  return item.output?.available === true && item.output.path === statePath &&
    typeof item.output.observation_token === "string" && Boolean(item.output.observation_token) &&
    bytes !== null && Number.isSafeInteger(digest?.bytes) && bytes.length === digest.bytes &&
    /^[a-f0-9]{64}$/.test(digest.sha256 ?? "") && sha256(bytes) === digest.sha256 &&
    sameState(parseStateOrNull(bytes), expected);
}

function successfulScopedRead(item, readRoots) {
  const path = item.input?.path;
  return isCanonicalWorkspaceReadPath(path, readRoots) && item.output?.path === path &&
    item.output?.outcome === "observed" && item.output?.available === true &&
    item.output?.absent !== true &&
    typeof item.output.observation_token === "string" && item.output.observation_token.length > 0 &&
    item.output.isError !== true && !hasNonemptyError(item.output.error) &&
    decodeBodyOrNull(item.output) !== null;
}

function verifyReleaseChronology(trace, statePathValue, saveIndex) {
  const issued = new Map();
  const savedTokens = new Set();
  trace.forEach((item, index) => {
    if (item.name === "stow_workspace_file_read_for_save") {
      const token = item.output?.observation_token;
      if (typeof token === "string" && token) {
        if (issued.has(token)) throw new Error("workspace observation token was duplicated");
        issued.set(token, { path: item.input?.path, index });
      }
    }
    if (item.name === "stow_workspace_file_save") {
      const token = item.input?.observation_token;
      if (typeof token === "string" && token) savedTokens.add(token);
    }
    if (item.name === "stow_workspace_file_release_observation") {
      const token = item.input?.observation_token;
      const observation = issued.get(token);
      if (!observation || observation.index >= index || item.output?.released !== true) {
        throw new Error("workspace observation release did not match an earlier successful read");
      }
      if (observation.path === statePathValue && index < saveIndex && !savedTokens.has(token)) {
        throw new Error("workspace state observation was released before its save");
      }
      issued.delete(token);
    }
  });
}

function bodyMatches(output, expectedPath, expectedText) {
  if (output?.available !== true || output.path !== expectedPath) return false;
  const bytes = decodeBodyOrNull(output);
  return bytes !== null && bytes.length === Buffer.byteLength(expectedText) && sha256(bytes) === sha256(expectedText);
}

function decodeBodyOrNull(result) {
  if (typeof result?.data_base64 !== "string") return null;
  return decodeBase64OrNull(result.data_base64);
}

function decodeBase64(value) {
  const bytes = decodeBase64OrNull(value);
  if (!bytes) throw new Error("workspace file save body was invalid");
  return bytes;
}

function decodeBase64OrNull(value) {
  try {
    if (typeof value !== "string") return null;
    const bytes = Buffer.from(value, "base64");
    return bytes.toString("base64") === value ? bytes : null;
  } catch { return null; }
}

function parseState(bytes) {
  const state = parseStateOrNull(bytes);
  if (!state) throw new Error("workspace save body was not canonical state JSON");
  return state;
}

function parseStateOrNull(bytes) {
  if (!bytes) return null;
  try {
    const state = JSON.parse(Buffer.from(bytes).toString("utf8"));
    if (!hasExactKeys(state, ["total_units", "entry_count", "next_batch"]) ||
        !Number.isSafeInteger(state.total_units) || !Number.isSafeInteger(state.entry_count) ||
        !(typeof state.next_batch === "string" || state.next_batch === null)) return null;
    return state;
  } catch { return null; }
}

function sameState(left, right) {
  return left !== null && right !== null && left.total_units === right.total_units &&
    left.entry_count === right.entry_count && left.next_batch === right.next_batch;
}

function stageSpec(stage) { return batches[stageName(stage)]; }
function stageName(stage) {
  if (stage === 1 || stage === "stage1") return "stage1";
  if (stage === 2 || stage === "stage2") return "stage2";
  throw new Error("unknown editing fixture stage");
}
function hasExactKeys(value, keys) {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).length === keys.length && keys.every(key => Object.hasOwn(value, key));
}
function hasNonemptyError(value) {
  if (value === undefined || value === null || value === false || value === "") return false;
  if (Array.isArray(value)) return value.length > 0;
  if (typeof value === "object") return Object.keys(value).length > 0;
  return true;
}
function sha256(value) { return createHash("sha256").update(value).digest("hex"); }
