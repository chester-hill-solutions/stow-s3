import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { promisify } from "node:util";
import {
  assertOpenCodeCLI, closeProxy, failureCategory, isolatedOpenCodeEnvironment,
  listenLoopback, remainingTime, verifyFreeRouteEvidence, writeBoundedDiagnostic, writeSanitizedTrace,
} from "./run.mjs";
import {
  buildEditingConfig, createEditingProxy, editingPrompt, editingSchema, parseEditingEvents, validateToolNameMap,
} from "./editing-host.mjs";
import { freeSelector, loadFreePilotProfile, loadOpenRouterCredential, proxyPlaceholder } from "./free-only.mjs";
import {
  connectEditingHost, handoffEditingFixture, hostVerifyAndCheckpoint, prepareEditingFixture, readCheckpointState,
} from "./editing-fixture.mjs";
import { verifyEditingStageEvidence } from "./editing-verifier.mjs";

const exec = promisify(execFile);
const maxTurns = 2;

export async function runEditingPilot(profilePath, { toolNameMap, tracePath, diagnosticPrefix, fetchImpl = fetch } = {}) {
  validateToolNameMap(toolNameMap);
  if (!tracePath || !diagnosticPrefix) throw new Error("editing pilot trace and diagnostic paths are required");
  const profile = await loadFreePilotProfile(profilePath);
  const deadline = new AbortController();
  const startedAt = Date.now();
  const timer = setTimeout(() => deadline.abort(), Math.min(profile.deadlineMs, 300_000));
  let tempRoot;
  let fixture;
  let stage = "credential";
  let currentStage = null;
  let host;
  let outcome;
  let modelPrompts = 0;
  const requestRecords = [];
  const stages = [];
  try {
    const credential = await loadOpenRouterCredential(profile);
    tempRoot = await mkdtemp(join(tmpdir(), "stow-w26-editing-pilot-"));
    const checkRoot = join(tempRoot, "cli-check");
    await mkdir(checkRoot, { recursive: true, mode: 0o700 });
    stage = "opencode_cli_check";
    await assertOpenCodeCLI(profile, isolatedOpenCodeEnvironment(join(checkRoot, "session")), checkRoot, deadline.signal, startedAt);
    stage = "editing_fixture";
    fixture = await prepareEditingFixture(profile.stowBinary, tempRoot,
      { signal: deadline.signal, deadlineAt: startedAt + profile.deadlineMs });
    for (const stageName of ["stage1", "stage2"]) {
      currentStage = { name: stageName, workspaceId: fixture.workspaceId, requestKey: fixture.requestKeys[stageName],
        modelRequests: [], toolTrace: [], schemaEnforcement: [] };
      stage = `${stageName}_model`;
      const model = await runModelStage(profile, fixture, stageName, toolNameMap, credential, deadline, startedAt,
        tempRoot, diagnosticPrefix, requestRecords, fetchImpl, () => { modelPrompts++; });
      Object.assign(currentStage, model.summary);
      currentStage.toolTrace = model.evidence.toolTrace.map(sanitizedToolEntry);
      stage = `${stageName}_host_verification`;
      checkDeadline(deadline.signal, profile, startedAt);
      host = await connectEditingHost(fixture, { recover: true });
      let saved;
      try {
        saved = await hostVerifyAndCheckpoint(host.client, fixture, stageName, model.evidence);
      } finally {
        await host.close();
        host = null;
      }
      checkDeadline(deadline.signal, profile, startedAt);
      currentStage.checkpointId = saved.checkpointId;
      currentStage.savedStateSHA256 = saved.stateSHA256;
      currentStage.savedStateBytes = saved.stateBytes;
      currentStage.generation = model.evidence.terminalStructured.generation;
      currentStage.terminalResult = terminalReceipt(model.evidence.terminalStructured);
      if (stageName === "stage1") {
        stage = "stage1_handoff";
        checkDeadline(deadline.signal, profile, startedAt);
        await handoffEditingFixture(profile.stowBinary, fixture, saved.checkpointId, stageName);
        checkDeadline(deadline.signal, profile, startedAt);
        if (!fixture.sourcePathsRemoved || fixture.workspaceId === fixture.sourceWorkspaceId) {
          throw new Error("stage one sender paths were not removed before continuation");
        }
      } else {
        stage = "stage2_checkpoint_readback";
        checkDeadline(deadline.signal, profile, startedAt);
        const checkpointState = await readCheckpointState(fixture, saved.checkpointId, { evidence: true });
        checkDeadline(deadline.signal, profile, startedAt);
        verifyFinalCheckpointState(checkpointState.state, model.evidence.terminalStructured);
        if (checkpointState.sha256 !== saved.stateSHA256 || checkpointState.bytes !== saved.stateBytes) {
          throw new Error("final checkpoint bytes did not match the host-verified saved state");
        }
        currentStage.checkpointStateSHA256 = checkpointState.sha256;
        currentStage.checkpointStateBytes = checkpointState.bytes;
        currentStage.checkpointStateVerified = true;
      }
      stages.push(currentStage);
      currentStage = null;
    }
    checkDeadline(deadline.signal, profile, startedAt);
    if (modelPrompts !== maxTurns) throw new Error("pilot did not complete both required prompts");
    outcome = passedReceipt(profile, fixture, stages, requestRecords, tempRoot, diagnosticPrefix, modelPrompts,
      Date.now() - startedAt, profile.deadlineMs);
  } catch (error) {
    await preserveFailureOutput(error, currentStage, diagnosticPrefix);
    outcome = failedReceipt(profile, stage, currentStage, fixture, stages, requestRecords, tempRoot, diagnosticPrefix,
      modelPrompts, Date.now() - startedAt,
      failureCategory(error, error?.editingFailureStage ?? failureStage(stage), deadline.signal.aborted));
  } finally {
    clearTimeout(timer);
    deadline.abort();
    if (host) await host.close().catch(() => undefined);
  }
  await writeSanitizedTrace(tracePath, outcome);
  if (outcome.status !== "passed") throw Object.assign(new Error(`workspace editing pilot failed: ${outcome.category}`), { trace: outcome });
  return outcome;
}

async function runModelStage(profile, fixture, stageName, toolNameMap, credential, deadline, startedAt,
  tempRoot, diagnosticPrefix, requestRecords, fetchImpl, onPrompt) {
  const stageRoot = join(tempRoot, stageName);
  await mkdir(stageRoot, { recursive: true, mode: 0o700 });
  const environment = isolatedOpenCodeEnvironment(join(stageRoot, "isolated"));
  const runDir = join(stageRoot, "project");
  await mkdir(runDir, { recursive: true, mode: 0o700 });
  const enforcement = [];
  const requests = [];
  const stageProxy = createEditingProxy({ apiKey: credential, signal: deadline.signal, fetchImpl,
    record: entry => { requests.push(entry); requestRecords.push({ stage: stageName, ...safeRequest(entry) }); },
    toolNameMap, readRoots: fixture.readRoots, requestKey: fixture.requestKeys[stageName],
    schema: editingSchema(fixture.requestKeys[stageName]), enforced: enforcement });
  let stdout = "";
  let stderr = "";
  let parsed;
  let failureStageName = "opencode_run";
  const stdoutPath = `${diagnosticPrefix}.${stageName}.jsonl`;
  const stderrPath = `${diagnosticPrefix}.${stageName}.stderr`;
  try {
    const address = await listenLoopback(stageProxy);
    const config = buildEditingConfig(profile, fixture, `http://127.0.0.1:${address.port}/api/v1`, toolNameMap);
    await writeFile(join(runDir, "opencode.json"), JSON.stringify(config, null, 2), { mode: 0o600, flag: "wx" });
    checkDeadline(deadline.signal, profile, startedAt);
    onPrompt();
    const result = await exec(profile.opencodeBinary,
      ["run", "--standalone", "--format", "json", "--model", freeSelector, "--agent", "build",
        "--title", `Workspace editing ${stageName}`, editingPrompt(fixture.requestKeys[stageName])],
      { cwd: runDir, env: { ...environment, OPENROUTER_API_KEY: proxyPlaceholder },
        timeout: remainingTime(profile.deadlineMs, startedAt), signal: deadline.signal, maxBuffer: 16 << 20 });
    stdout = result.stdout;
    stderr = result.stderr;
    checkDeadline(deadline.signal, profile, startedAt);
    await saveDiagnostics(stdoutPath, stderrPath, stdout, stderr);
    failureStageName = "evidence_verification";
    parsed = parseEditingEvents(stdout, toolNameMap);
    const stageRequests = requests.filter(item => item.path.endsWith("/chat/completions"));
    verifyModelGate(stageRequests, requests, enforcement, toolNameMap);
    verifyEditingStageEvidence(parsed, fixture, stageName);
    return { evidence: parsed, summary: {
      toolCallCount: parsed.toolTrace.length,
      modelRequestCount: stageRequests.length,
      modelRequests: stageRequests.map(safeRequest),
      schemaEnforcement: summarizeEnforcement(enforcement, toolNameMap),
      diagnosticPaths: { stdout: stdoutPath, stderr: stderrPath },
    } };
  } catch (error) {
    stdout = typeof error?.stdout === "string" ? error.stdout : stdout;
    stderr = typeof error?.stderr === "string" ? error.stderr : stderr;
    await saveDiagnostics(stdoutPath, stderrPath, stdout, stderr).catch(() => undefined);
    error.editingEvidence = parsed;
    error.editingFailureStage = failureStageName;
    error.editingRequests = requests.filter(item => item.path.endsWith("/chat/completions")).map(safeRequest);
    error.editingEnforcement = summarizeEnforcement(enforcement, toolNameMap);
    throw error;
  } finally {
    await closeProxy(stageProxy).catch(() => undefined);
  }
}

function verifyModelGate(requests, allRequests, enforcement, toolNameMap) {
  if (!requests.length) throw new Error("stage made no guarded chat request");
  verifyFreeRouteEvidence(requests);
  const [initial, ...phases] = enforcement;
  const terminal = phases.at(-1);
  const editing = phases.slice(0, -1);
  if (enforcement.length !== requests.length || !initial ||
      initial.phase !== "initial_read" || initial.initialReadRequired !== true || initial.initialReadVerified !== false ||
      initial.schemaEnforced !== false || !terminal || terminal.phase !== "terminal_after_commit" ||
      terminal.initialReadRequired !== true || terminal.initialReadVerified !== true || terminal.commitProved !== true ||
      terminal.schemaEnforced !== true || terminal.toolChoice !== "none" ||
      typeof terminal.generation !== "string" || !terminal.generation ||
      editing.length === 0 || editing.some(item => item.phase !== "editing_before_commit" ||
        item.initialReadRequired !== true || item.initialReadVerified !== true || item.commitProved !== false ||
        item.schemaEnforced !== false || item.toolChoice !== "required") ||
      enforcement.some(item => !fullCatalog(item.toolNames, toolNameMap)) ||
      allRequests.some(item => item.status < 200 || item.status >= 300 ||
      !["/api/v1/models", "/api/v1/chat/completions"].includes(item.path) ||
      (item.requestedModel !== null && item.requestedModel !== "openrouter/free")) ||
      phases.filter(item => item.phase === "terminal_after_commit").length !== 1) {
    throw new Error("stage lacked initial read, required editing phase, one committed-save terminal phase, or full tool catalog");
  }
}

function fullCatalog(names, map) {
  const expected = Object.keys(map).sort();
  return Array.isArray(names) && names.length === expected.length && [...names].sort().every((name, index) => name === expected[index]);
}

function verifyFinalCheckpointState(state, terminal) {
  if (!state || typeof state !== "object" || Array.isArray(state) ||
      Object.keys(state).sort().join(",") !== "entry_count,next_batch,total_units" ||
      !Number.isSafeInteger(state.total_units) || !Number.isSafeInteger(state.entry_count) || state.next_batch !== null ||
      state.total_units !== terminal.total_units || state.entry_count !== terminal.entry_count) {
    throw new Error("final immutable checkpoint state did not match the committed result");
  }
}

export function summarizeEnforcement(enforcement, map) {
  return { requests: enforcement.length,
    initialReadRequired: enforcement.some(item => item.initialReadRequired === true),
    initialReadVerified: enforcement.some(item => item.initialReadVerified === true),
    initialReadPhaseCount: enforcement.filter(item => item.phase === "initial_read").length,
    editingPhaseCount: enforcement.filter(item => item.phase === "editing_before_commit").length,
    commitProofCount: enforcement.filter(item => item.commitProved === true).length,
    terminalAfterCommitCount: enforcement.filter(item => item.phase === "terminal_after_commit" && item.schemaEnforced === true).length,
    terminalGeneration: enforcement.find(item => item.phase === "terminal_after_commit")?.generation ?? null,
    schemaEnforced: enforcement.some(item => item.schemaEnforced === true),
    catalogRefusalCount: enforcement.filter(item => item.phase === "catalog_refused").length,
    historyRefusalCount: enforcement.filter(item => item.phase === "history_refused").length,
    fullToolCatalog: enforcement.some(item => fullCatalog(item.toolNames, map)),
    toolCount: Math.max(0, ...enforcement.map(item => Array.isArray(item.toolNames) ? item.toolNames.length : 0)) };
}

function terminalReceipt(result) {
  return { total_units: result.total_units, entry_count: result.entry_count, outcome: result.outcome,
    path: result.path, request_key: result.request_key, generation: result.generation };
}

function sanitizedToolEntry(item) {
  return { name: item.name, status: item.status, failed: item.failed === true, resultValid: item.resultValid === true };
}

function safeRequest(item) {
  const taskStage = ["stage1", "stage2"].includes(item.stage) ? item.stage : undefined;
  const proxyStageValue = item.proxyStage ?? item.stage;
  const proxyStage = ["guard", "upstream"].includes(proxyStageValue) ? proxyStageValue : null;
  return { method: item.method, path: item.path, requestedModel: item.requestedModel ?? null,
    resolvedModel: item.resolvedModel ?? null, status: item.status,
    ...(taskStage ? { stage: taskStage } : {}), proxyStage,
    failureKind: safeFailureKind(item.failureKind), upstreamAttempted: item.upstreamAttempted === true };
}

const requestFailureKinds = new Set(["none", "catalog_refused", "history_refused", "request_invalid",
  "upstream_exception", "upstream_http_error", "response_invalid"]);

export function summarizeRequestMetadata(requests) {
  const safeRequests = requests.map(safeRequest);
  const chatRequests = safeRequests.filter(item => item.path.endsWith("/chat/completions"));
  return { count: chatRequests.length, requests: safeRequests,
    paidFallbackObserved: chatRequests.some(item => item.upstreamAttempted &&
      typeof item.requestedModel === "string" && item.requestedModel.length > 0 && item.requestedModel !== "openrouter/free"),
    nonFreeModelRequestCount: chatRequests.filter(item => typeof item.requestedModel === "string" &&
      item.requestedModel.length > 0 && item.requestedModel !== "openrouter/free").length,
    nullModelErrorCount: chatRequests.filter(item => (item.requestedModel === null || item.requestedModel === "") &&
      item.status >= 400).length };
}

function safeFailureKind(value) {
  return requestFailureKinds.has(value) ? value : "request_invalid";
}

function passedReceipt(profile, fixture, stages, requests, retainedRoot, diagnosticPrefix, modelPrompts, elapsedMs, deadlineMs) {
  return { status: "passed", opencodeVersion: profile.expectedVersion, requestedSelector: freeSelector,
    modelPrompts, elapsedMs, deadlineMs, receiverWorkspaceId: fixture.workspaceId, sourceWorkspaceId: fixture.sourceWorkspaceId,
    sourcePathsRemovedBeforeStage2: fixture.sourcePathsRemoved === true,
    stageResults: stages, requestMetadata: summarizeRequestMetadata(requests), retainedRoot, diagnosticPrefix };
}

function failedReceipt(profile, stage, current, fixture, stages, requests, retainedRoot, diagnosticPrefix, modelPrompts, elapsedMs, category) {
  return { status: "failed", category, stage, opencodeVersion: profile.expectedVersion, requestedSelector: freeSelector,
    modelPrompts, elapsedMs, deadlineMs: profile.deadlineMs, receiverWorkspaceId: fixture?.workspaceId ?? "",
    sourceWorkspaceId: fixture?.sourceWorkspaceId ?? "", sourcePathsRemovedBeforeStage2: fixture?.sourcePathsRemoved === true,
    stageResults: stages, currentStage: current ? { name: current.name, workspaceId: current.workspaceId,
      requestKey: current.requestKey, toolCallCount: current.toolCallCount ?? current.toolTrace.length,
      modelRequestCount: current.modelRequestCount ?? current.modelRequests.length,
      modelRequests: current.modelRequests.length ? current.modelRequests : current.errorRequests ?? [],
      schemaEnforcement: current.schemaEnforcement ?? current.errorEnforcement ?? null,
      toolTrace: current.toolTrace } : null,
    requestMetadata: summarizeRequestMetadata(requests),
    retainedRoot: retainedRoot ?? "", diagnosticPrefix };
}

async function saveDiagnostics(stdoutPath, stderrPath, stdout, stderr) {
  await writeBoundedDiagnostic(stdoutPath, stdout);
  await writeBoundedDiagnostic(stderrPath, stderr);
}

async function preserveFailureOutput(error, current, diagnosticPrefix) {
  const stageName = current?.name ?? "setup";
  const stdoutPath = `${diagnosticPrefix}.${stageName}.jsonl`;
  const stderrPath = `${diagnosticPrefix}.${stageName}.stderr`;
  if (typeof error?.stdout === "string") await writeBoundedDiagnostic(stdoutPath, error.stdout).catch(() => undefined);
  if (typeof error?.stderr === "string") await writeBoundedDiagnostic(stderrPath, error.stderr).catch(() => undefined);
  if (current) {
    current.toolTrace = error?.editingEvidence?.toolTrace?.map(sanitizedToolEntry) ?? current.toolTrace;
    current.modelRequests = error?.editingRequests ?? current.modelRequests;
    current.modelRequestCount = current.modelRequests.length;
    current.schemaEnforcement = error?.editingEnforcement ?? current.schemaEnforcement;
    current.toolCallCount = current.toolTrace.length;
  }
}

function checkDeadline(signal, profile, startedAt) {
  if (signal.aborted) throw new Error("pilot deadline elapsed");
  remainingTime(profile.deadlineMs, startedAt);
}

function failureStage(stage) {
  if (stage.endsWith("_model")) return "opencode_run";
  if (stage.includes("verification") || stage.includes("readback")) return "evidence_verification";
  return stage;
}

if (process.argv[1] && import.meta.url === new URL(`file://${resolve(process.argv[1])}`).href) {
  const [profilePath, mapPath, tracePath, diagnosticPrefix] = process.argv.slice(2);
  if (!profilePath || !mapPath || !tracePath || !diagnosticPrefix) {
    process.stderr.write("usage: node editing-runner.mjs PROFILE TOOL_NAME_MAP_JSON TRACE_JSON DIAGNOSTIC_PREFIX\n");
    process.exitCode = 2;
  } else {
    readFile(resolve(mapPath), "utf8").then(JSON.parse).then(toolNameMap => runEditingPilot(resolve(profilePath), {
      toolNameMap, tracePath: resolve(tracePath), diagnosticPrefix: resolve(diagnosticPrefix),
    })).then(trace => process.stdout.write(`${JSON.stringify(trace)}\n`)).catch(error => {
      process.stderr.write(`${JSON.stringify(error.trace ?? { status: "failed", category: "profile_or_setup_failed" })}\n`);
      process.exitCode = 1;
    });
  }
}
