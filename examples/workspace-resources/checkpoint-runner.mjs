import { promisify } from "node:util";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { execFile } from "node:child_process";
import {
  assertOpenCodeCLI, closeProxy, failureCategory, isolatedOpenCodeEnvironment,
  listenLoopback, parseOpenCodeJSONLines, remainingTime, safeToolTraceEntry,
  verifyFreeRouteEvidence, writeBoundedDiagnostic, writeSanitizedTrace,
} from "./run.mjs";
import {
  buildFreeOnlyOpenCodeConfig, createFreeOnlyProxy, freeSelector,
  loadFreePilotProfile, loadOpenRouterCredential, proxyPlaceholder,
} from "./free-only.mjs";
import { prepareCheckpointContinuation, removeCheckpointFixture } from "./checkpoint-fixture.mjs";
import { verifiedCheckpointResult, verifyCheckpointEvidence } from "./checkpoint-verifier.mjs";

const exec = promisify(execFile);
const promptPrefix = "Use only MCP resource tools. List Stow resources and read the checkpoint overview URI below. Follow its root_uri verbatim, then follow each tree entry uri and next_uri verbatim. Never construct or request a /files/ URI for a directory, and never invent resource URIs. Continue the prior calculation using the saved progress and next-batch data, then return only JSON shaped as {total_units:number,entry_count:number,sources:[PROGRESS_URI,DATA_URI]}. Include the exact resource URIs you used. Do not use shell or filesystem tools.\n\nCheckpoint resource root: ";

export async function runCheckpointContinuationPilot(profilePath, {
  tracePath = "/private/tmp/stow-w25-openrouter-checkpoint-trace.json",
  diagnosticPath = "/private/tmp/stow-w25-openrouter-checkpoint-events.jsonl",
} = {}) {
  const profile = await loadFreePilotProfile(profilePath);
  const deadline = new AbortController();
  const startedAt = Date.now();
  const timer = setTimeout(() => deadline.abort(), profile.deadlineMs);
  let tempRoot;
  let fixture;
  let proxy;
  let stage = "credential";
  const requests = [];
  let evidence;
  let outcome;
  try {
    const credential = await loadOpenRouterCredential(profile);
    tempRoot = await mkdtemp(join(tmpdir(), "stow-w25-free-checkpoint-"));
    const environment = isolatedOpenCodeEnvironment(tempRoot);
    stage = "opencode_cli_check";
    await assertOpenCodeCLI(profile, environment, tempRoot, deadline.signal, startedAt);
    stage = "checkpoint_fixture";
    fixture = await prepareCheckpointContinuation(profile.stowBinary, tempRoot, {
      signal: deadline.signal, timeout: remainingTime(profile.deadlineMs, startedAt),
    });
    if (!fixture.sourcePathsRemoved || fixture.workspaceId === fixture.sourceWorkspaceId) {
      throw new Error("receiver checkpoint fixture did not detach from sender paths");
    }
    const runDir = join(tempRoot, "opencode-project");
    await mkdir(runDir, { recursive: true, mode: 0o700 });
    stage = "loopback_guard";
    proxy = createFreeOnlyProxy({ apiKey: credential, signal: deadline.signal, record: item => requests.push(item) });
    const proxyAddress = await listenLoopback(proxy);
    const proxyBaseURL = `http://127.0.0.1:${proxyAddress.port}/api/v1`;
    const config = buildFreeOnlyOpenCodeConfig(profile, { ...fixture, proxyBaseURL, checkpointId: fixture.checkpointId });
    await writeFile(join(runDir, "opencode.json"), JSON.stringify(config, null, 2), { mode: 0o600, flag: "wx" });
    stage = "opencode_run";
    const { stdout } = await exec(profile.opencodeBinary,
      ["run", "--standalone", "--format", "json", "--model", freeSelector, "--agent", "build", "--title", "Checkpoint continuation", promptPrefix + fixture.overviewURI],
      { cwd: runDir, env: { ...environment, OPENROUTER_API_KEY: proxyPlaceholder }, timeout: remainingTime(profile.deadlineMs, startedAt), signal: deadline.signal, maxBuffer: 16 << 20 });
    await writeBoundedDiagnostic(diagnosticPath, stdout);
    evidence = parseOpenCodeJSONLines(stdout);
    stage = "evidence_verification";
    verifyCheckpointEvidence(evidence, fixture);
    const terminalResult = verifiedCheckpointResult(evidence.finalText);
    const modelRequests = requests.filter(item => item.path.endsWith("/chat/completions"));
    verifyFreeRouteEvidence(modelRequests);
    outcome = passedTrace(profile, fixture, evidence, modelRequests, terminalResult, diagnosticPath);
  } catch (error) {
    await preserveDiagnostics(error, diagnosticPath);
    const modelRequests = requests.filter(item => item.path.endsWith("/chat/completions"));
    outcome = failedTrace(stage, failureCategory(error, stage, deadline.signal.aborted), fixture, evidence, modelRequests, diagnosticPath);
  } finally {
    clearTimeout(timer);
    deadline.abort();
    await closeProxy(proxy);
    if (fixture && tempRoot) await removeCheckpointFixture(tempRoot);
    else if (tempRoot) await rm(tempRoot, { recursive: true, force: true });
  }
  await writeSanitizedTrace(tracePath, outcome);
  if (outcome.status !== "passed") throw Object.assign(new Error(`free-only checkpoint pilot failed: ${outcome.category}`), { trace: outcome });
  return outcome;
}

function passedTrace(profile, fixture, evidence, modelRequests, terminalResult, diagnosticPath) {
  return {
    status: "passed", opencodeVersion: profile.expectedVersion, requestedSelector: freeSelector,
    sourceWorkspaceId: fixture.sourceWorkspaceId, sourceCheckpointId: fixture.senderCheckpointId,
    importedSourceCheckpointId: fixture.importedSourceCheckpointId,
    receiverWorkspaceId: fixture.workspaceId, receiverCheckpointId: fixture.checkpointId,
    taskRootURI: fixture.overviewURI, terminalResult,
    sourcePathsRemovedBeforeModel: fixture.sourcePathsRemoved, answerVerified: true, diagnosticPath,
    resourceToolTrace: evidence.toolTrace.map(checkpointToolTraceEntry),
    modelRequests: modelRequests.map(({ method, path, requestedModel, resolvedModel, status }) => ({ method, path, requestedModel, resolvedModel, status })),
  };
}

function failedTrace(stage, category, fixture, evidence, modelRequests, diagnosticPath) {
  return {
    status: "failed", stage, category, requestedSelector: freeSelector,
    sourceWorkspaceId: fixture?.sourceWorkspaceId ?? "", sourceCheckpointId: fixture?.senderCheckpointId ?? "",
    importedSourceCheckpointId: fixture?.importedSourceCheckpointId ?? "", receiverWorkspaceId: fixture?.workspaceId ?? "",
    receiverCheckpointId: fixture?.checkpointId ?? "", sourcePathsRemovedBeforeModel: fixture?.sourcePathsRemoved ?? false,
    taskRootURI: fixture?.overviewURI ?? "", answerVerified: false, diagnosticPath,
    resourceToolTrace: evidence?.toolTrace?.map(checkpointToolTraceEntry) ?? [],
    modelRequests: modelRequests.map(({ method, path, requestedModel, resolvedModel, status }) => ({ method, path, requestedModel, resolvedModel, status })),
  };
}

function checkpointToolTraceEntry(item) {
  return {
    ...safeToolTraceEntry(item), failed: item.failed === true, resultValid: item.resultValid === true,
    ...(Number.isInteger(item.contentBytes) ? { contentBytes: item.contentBytes, contentSHA256: item.contentSHA256 } : {}),
  };
}

async function preserveDiagnostics(error, diagnosticPath) {
  if (typeof error?.stdout === "string") await writeBoundedDiagnostic(diagnosticPath, error.stdout).catch(() => undefined);
  if (typeof error?.stderr === "string") await writeBoundedDiagnostic(`${diagnosticPath}.stderr`, error.stderr).catch(() => undefined);
}

if (process.argv[1] && import.meta.url === new URL(`file://${resolve(process.argv[1])}`).href) {
  const profile = process.argv[2] ?? process.env.STOW_W25_PROFILE ?? "/private/tmp/stow-w24-openrouter-free.json";
  const trace = process.argv[3] ?? process.env.STOW_W25_TRACE ?? "/private/tmp/stow-w25-openrouter-checkpoint-trace.json";
  const diagnostic = process.argv[4] ?? process.env.STOW_W25_DIAGNOSTIC ?? "/private/tmp/stow-w25-openrouter-checkpoint-events.jsonl";
  runCheckpointContinuationPilot(resolve(profile), { tracePath: trace, diagnosticPath: diagnostic })
    .then(value => process.stdout.write(`${JSON.stringify(value)}\n`))
    .catch(error => {
      process.stderr.write(`${JSON.stringify(error.trace ?? { status: "failed", category: "profile_or_setup_failed" })}\n`);
      process.exitCode = 1;
    });
}
