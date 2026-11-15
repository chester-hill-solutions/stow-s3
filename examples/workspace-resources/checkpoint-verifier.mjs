import { createHash } from "node:crypto";
import { exactInteger } from "./run.mjs";

export function verifyCheckpointEvidence(evidence, fixture) {
  verifyCheckpointAnswer(evidence.finalText, fixture);
  const trace = evidence.toolTrace;
  const listName = name => name === "list_mcp_resources" || name === "opencode_list_mcp_resources";
  const readName = name => name === "read_mcp_resource" || name === "opencode_read_mcp_resource";
  if (!trace.some(item => listName(item.name) && item.status === "completed")) {
    throw new Error("completed resource listing is missing");
  }
  if (trace.some(item => (!listName(item.name) && !readName(item.name)) || item.status !== "completed")) {
    throw new Error("non-resource or incomplete tool call was present");
  }
  if (trace.some(item => item.failed || item.resultValid !== true)) {
    throw new Error("resource tool returned an error or invalid result");
  }
  const reads = trace.filter(item => readName(item.name));
  if (reads.some(item => !isCheckpointURI(item.uri, fixture))) {
    throw new Error("resource trace escaped the selected checkpoint");
  }
  const readURIs = reads.map(item => item.uri);
  for (const uri of [fixture.overviewURI, fixture.progressURI, fixture.dataURI]) {
    if (!readURIs.includes(uri)) throw new Error("required checkpoint reads are missing");
  }
  requireReadBytes(reads, fixture.overviewURI);
  requireReadBytes(reads, fixture.progressURI, fixture.savedProgress);
  requireReadBytes(reads, fixture.dataURI, fixture.savedNextBatch);
}

function requireReadBytes(reads, uri, expectedText) {
  const candidates = reads.filter(item => item.uri === uri);
  if (expectedText === undefined) {
    if (!candidates.some(item => item.contentBytes > 0 && /^[a-f0-9]{64}$/.test(item.contentSHA256 ?? ""))) {
      throw new Error("checkpoint overview text was empty or unavailable");
    }
    return;
  }
  const expectedBytes = Buffer.from(expectedText, "utf8");
  const expectedDigest = createHash("sha256").update(expectedBytes).digest("hex");
  if (!candidates.some(item => item.contentBytes === expectedBytes.length && item.contentSHA256 === expectedDigest)) {
    throw new Error("checkpoint read bytes did not match the saved fixture");
  }
}

function isCheckpointURI(uri, fixture) {
  if (typeof uri !== "string" || uri.length > 2048 || /[?#\u0000-\u001f\u007f]/.test(uri)) return false;
  const prefix = `stow://workspaces/${fixture.workspaceId}/checkpoints/${fixture.checkpointId}/`;
  if (!uri.startsWith(prefix)) return false;
  const route = uri.slice(prefix.length);
  if (route === "v1") return uri === fixture.overviewURI;
  const tree = route.match(/^tree\/v1\/([A-Za-z0-9_-]+)(?:\/(0|[1-9]\d*))?$/);
  if (tree) {
    const path = decodeBase64URLPath(tree[1]);
    return isAllowedPath(path, true) && (tree[2] === undefined ||
      (isCanonicalInteger(tree[2]) && Number(tree[2]) <= 10000));
  }
  const file = route.match(/^files\/([A-Za-z0-9_-]+)$/);
  if (file) return isAllowedPath(decodeBase64URLPath(file[1]), false);
  const range = route.match(/^ranges\/v1\/([A-Za-z0-9_-]+)\/(0|[1-9]\d*)\/([1-9]\d*)$/);
  if (range) {
    const path = decodeBase64URLPath(range[1]);
    const length = Number(range[3]);
    return isAllowedPath(path, false) && isCanonicalInteger(range[2]) && Number(range[2]) <= 67_108_864 &&
      isCanonicalInteger(range[3]) && length <= 65536;
  }
  return false;
}

function decodeBase64URLPath(encoded) {
  try {
    const bytes = Buffer.from(encoded, "base64url");
    if (bytes.toString("base64url") !== encoded) return null;
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch { return null; }
}

function isAllowedPath(path, allowRoot) {
  if (path === ".") return allowRoot;
  if (path === "inputs") return allowRoot;
  if (typeof path !== "string" || Buffer.byteLength(path) > 1024 || !path.startsWith("inputs/") ||
      path.includes("\\") || /[\u0000-\u001f\u007f]/.test(path)) return false;
  return path.split("/").every(segment => segment && segment !== "." && segment !== "..");
}

function isCanonicalInteger(value) {
  if (!/^(0|[1-9]\d*)$/.test(value)) return false;
  return Number.isSafeInteger(Number(value));
}

export function verifiedCheckpointResult(text) {
  const answer = parseAnswer(text);
  return {
    total_units: exactInteger(answer?.total_units),
    entry_count: exactInteger(answer?.entry_count),
    sources: Array.isArray(answer?.sources) ? [...answer.sources] : [],
  };
}

function verifyCheckpointAnswer(text, fixture) {
  const answer = parseAnswer(text);
  if (!hasExactKeys(answer, ["total_units", "entry_count", "sources"]) ||
      exactInteger(answer.total_units) !== fixture.expectedTotal || exactInteger(answer.entry_count) !== fixture.expectedEntryCount ||
      !Array.isArray(answer.sources) || answer.sources.length !== 2 ||
      answer.sources[0] !== fixture.progressURI || answer.sources[1] !== fixture.dataURI) {
    throw new Error("terminal checkpoint continuation JSON failed verification");
  }
}

function parseAnswer(text) {
  const content = text.trim().replace(/^```json\s*([\s\S]*?)\s*```$/i, "$1").replace(/^```\s*([\s\S]*?)\s*```$/, "$1");
  try { return JSON.parse(content); } catch { return null; }
}

function hasExactKeys(value, keys) {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).length === keys.length && keys.every(key => Object.hasOwn(value, key));
}
