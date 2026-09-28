// Runs conformance/workspace/cases.json through the TypeScript wrapper.
//
// The case file is the contract. This module is a driver for it and nothing more:
// if a judgement about what a verb returns lives here, it is a fact about the
// wrapper rather than about the contract, and the other two drivers cannot see it.
//
// Every verb goes through the exported wrapper rather than through execFile. That
// is the whole point. The wrapper is a subprocess veneer over the same binary the
// Go driver runs, and a veneer that mistranslates a document is invisible to any
// test that skips it — which is how both wrappers shipped the same bug.

import { execFile } from "node:child_process";
import { mkdtemp, mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, relative, resolve, sep } from "node:path";
import { promisify } from "node:util";

import { resolveStowBinary } from "../src/bin.js";
import {
  adoptWorkspaceHandoff,
  applyWorkspaceDelta,
  checkpointWorkspace,
  collectWorkspaces,
  pruneWorkspaces,
  createWorkspaceDelta,
  destroyWorkspace,
  diffWorkspaces,
  listWorkspaces,
  handoffWorkspace,
  prepareWorkspace,
  resumeWorkspace,
} from "../src/workspace.js";
import { documentFor } from "./workspace-contract-alterations.js";
import { checkField, lookupField, sameJSON } from "./workspace-contract-matchers.js";
import type { ContractExpectation, JsonValue } from "./workspace-contract-matchers.js";

const execFileAsync = promisify(execFile);

export interface ContractInput {
  readonly body: string;
  readonly destination: string;
}

/** One file a step puts into the workspace, the way an agent editing one would. */
export interface ContractWrite {
  readonly path: string;
  readonly body: string;
}

export interface ContractManifestSpec {
  readonly root: string;
  readonly team?: string;
  readonly inputs?: readonly ContractInput[];
}

export interface ContractStep {
  readonly id: string;
  readonly verb: string;
  readonly registry?: string;
  readonly team?: string;
  readonly args?: Readonly<Record<string, string>>;
  /**
   * prune only: forget adopted entries whose directory is gone. Declared on the step
   * rather than left in the free-form argument map because the driver passes it as a
   * typed option, and a flag that matters for one verb is easier to find here.
   */
  readonly includeAdopted?: boolean;
  readonly root?: string;
  readonly manifest?: ContractManifestSpec;
  readonly write?: readonly ContractWrite[];
  readonly capture?: Readonly<Record<string, string>>;
  readonly expect?: Readonly<Record<string, ContractExpectation>>;
  readonly alsoWritten?: string;
  readonly returns?: string;
  readonly tamper?: string;
  /**
   * Builds a substituted document: the named change is moved to a different path,
   * in the change list and the content map together, and the result re-encoded. It
   * is how a document altered on the way to a receiver is built, and the case file
   * uses it to state the substitution as a refusal.
   */
  readonly renameInTransit?: ContractRename;
  readonly fail?: ContractFailure;
}

/** One substitution to perform on a document a previous step produced. */
export interface ContractRename {
  readonly from: string;
  readonly path: string;
  readonly to: string;
}

/** What a step expects a refusal to say, and that it happened at all. */
export interface ContractFailure {
  readonly contains: string;
}

export interface Contract {
  readonly version: number;
  readonly steps: readonly ContractStep[];
}

export interface ContractProblem {
  readonly step: string;
  readonly field: string;
  readonly message: string;
}

const RETURNS_FILE = "file";
const REGISTRY_A = "a";

export async function loadContract(): Promise<Contract> {
  const path = resolve(import.meta.dirname, "..", "..", "..", "conformance", "workspace", "cases.json");
  return JSON.parse(await readFile(path, "utf8")) as Contract;
}

export interface Run {
  readonly work: string;
  readonly captures: Map<string, string>;
  readonly problems: ContractProblem[];
}

export async function runContract(contract: Contract): Promise<ContractProblem[]> {
  const work = await mkdtemp(join(tmpdir(), "stow-workspace-contract-"));
  const run: Run = { work, captures: new Map([["work", work]]), problems: [] };

  for (const step of contract.steps) {
    if (run.problems.length > 0) {
      // The steps share a registry and a work directory, so continuing past a
      // failure reports one missing workspace as a dozen failures and buries the
      // one that broke.
      break;
    }
    await runStep(run, step);
  }
  return run.problems;
}

async function runStep(run: Run, step: ContractStep): Promise<void> {
  try {
    switch (step.verb) {
      case "noop":
        await applyWrites(run, step);
        return;
      case "read":
        await judge(run, step, await readTree(substitute(run, step.root ?? "")));
        return;
      case "prepare":
        await judge(run, step, await prepare(run, step));
        return;
      default:
        await judge(run, step, await invoke(run, step));
    }
  } catch (error) {
    if (step.fail !== undefined) {
      judgeRefusal(run, step, error);
      return;
    }
    throw new Error(`step ${JSON.stringify(step.id)}: ${describe(error)}`, { cause: error });
  }
}

/** Everything the wrappers can express, dispatched by verb. */
async function invoke(run: Run, step: ContractStep): Promise<JsonValue> {
  const args = resolveArgs(run, step);
  const team = step.team;
  const registryDir = step.registry === REGISTRY_A ? join(run.work, "registry-a") : undefined;

  // A required argument, named as such. Reading args.id and letting it be undefined
  // would hand the wrapper the string "undefined" and produce a refusal that reads
  // like a wrapper bug, which is the wrong thing to spend an hour on.
  const required = (name: string): string => {
    const value = args[name];
    if (value === undefined) {
      throw new Error(`step ${JSON.stringify(step.id)}: ${step.verb} requires --${name}`);
    }
    return value;
  };

  switch (step.verb) {
    case "checkpoint":
      return checkpointWorkspace({
        id: required("id"),
        parent: args.parent,
        registryDir,
        team,
      });
    case "diff":
      return diffWorkspaces(required("from"), required("to"), registryDir, team);
    case "handoff":
      return handoffWorkspace(required("id"), {
        checkpointId: args["checkpoint-id"],
        archive: args.archive,
        output: args.output,
        registryDir,
        team,
      });
    case "adopt":
      return adoptWorkspaceHandoff({ handoffPath: required("handoff"), root: required("root") });
    case "delta":
      return createWorkspaceDelta({
        from: required("from"),
        to: required("to"),
        output: required("output"),
        registryDir,
        team,
      });
    case "apply":
      return applyWorkspaceDelta({
        delta: await documentFor(run, step, required("delta")),
        base: required("base"),
        expectSHA256: args["expect-sha256"],
        registryDir: registryDirFor(run, step, registryDir),
        team,
      });
    case "resume":
      return resumeWorkspace({ handoffPath: required("handoff") });
    case "list":
      return listWorkspaces({
        registryDir: registryDirFor(run, step, registryDir),
        team,
      });
    case "collect":
      return collectWorkspaces(registryDirFor(run, step, registryDir));
    case "prune":
      return pruneWorkspaces({
        registryDir: registryDirFor(run, step, registryDir),
        team,
        includeAdopted: step.includeAdopted === true,
      });
    case "destroy":
      return destroyWorkspace(required("id"), registryDirFor(run, step, registryDir));
    default:
      throw new Error(`the contract runner has no wrapper call for ${JSON.stringify(step.verb)}`);
  }
}

/**
 * Resolves the declared arguments, and fails on one that names a capture no
 * earlier step produced. A silently empty argument is a scenario that quietly
 * stopped testing what it says it tests, which is the failure a shared contract
 * cannot have.
 */
function resolveArgs(run: Run, step: ContractStep): Record<string, string> {
  const resolved: Record<string, string> = {};
  for (const [name, value] of Object.entries(step.args ?? {})) {
    const substituted = substitute(run, value);
    for (const match of substituted.matchAll(/\{\{([A-Za-z0-9_]+)\}\}/g)) {
      const reference = match[1];
      if (reference !== undefined && !run.captures.has(reference)) {
        throw new Error(
          `step ${JSON.stringify(step.id)}: argument ${JSON.stringify(name)} refers to ` +
            `{{${reference}}}, which no earlier step captured`,
        );
      }
    }
    resolved[name] = substituted;
  }
  return resolved;
}

/**
 * The registry directory a step's verb is pointed at.
 *
 * A step that names --registry-dir itself is authoritative, and that is how the
 * contract reaches a team partition: prepare reports the partition it created, and
 * collect and destroy — which take --registry-dir but no --team — are pointed
 * straight at it. Every other verb gets the driver's own directory plus the team's
 * --team flag and lets the CLI compose the two, because a driver that reconstructed
 * the partition path itself would be asserting a belief about the store rather than
 * about the verb.
 */
function registryDirFor(run: Run, step: ContractStep, fallback: string | undefined): string | undefined {
  // Substituted, not read raw. The Go driver substitutes here too, and a driver
  // that passed the literal "{{registryA}}" through would point the verb at a path
  // that does not exist and get an empty result rather than an error.
  const declared = step.args?.["registry-dir"];
  return declared === undefined ? fallback : substitute(run, declared);
}

export function substitute(run: Run, text: string): string {
  let out = text;
  for (const [name, value] of run.captures) {
    out = out.replaceAll(`{{${name}}}`, value);
  }
  return out;
}

async function prepare(run: Run, step: ContractStep): Promise<JsonValue> {
  const spec = step.manifest;
  if (spec === undefined) {
    throw new Error(`step ${JSON.stringify(step.id)}: prepare has no manifest`);
  }
  const inputs: { source: string; destination: string }[] = [];
  for (const input of spec.inputs ?? []) {
    const destination = substitute(run, input.destination);
    const source = join(run.work, "inputs", ...destination.split("/"));
    await mkdir(dirname(source), { recursive: true });
    await writeFile(source, input.body);
    inputs.push({ source, destination });
  }
  const manifestPath = join(run.work, `manifest-${step.id.replaceAll(" ", "-")}.json`);
  await writeFile(
    manifestPath,
    JSON.stringify({
      version: 1,
      root: substitute(run, spec.root),
      team: spec.team,
      registry_dir: join(run.work, "registry-a"),
      inputs,
    }),
  );
  return prepareWorkspace(manifestPath);
}

async function applyWrites(run: Run, step: ContractStep): Promise<void> {
  const root = run.captures.get("root");
  if (root === undefined) {
    throw new Error("a step writes into the workspace before any step has prepared one");
  }
  for (const write of step.write ?? []) {
    const full = join(root, ...substitute(run, write.path).split("/"));
    await mkdir(dirname(full), { recursive: true });
    await writeFile(full, write.body);
  }
}

/** Turns a workspace root into the flat path map a "read" step asserts against. */
async function readTree(root: string): Promise<Record<string, JsonValue>> {
  const files: Record<string, JsonValue> = {};
  const walk = async (directory: string): Promise<void> => {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const full = join(directory, entry.name);
      if (entry.isDirectory()) {
        await walk(full);
        continue;
      }
      files[relative(root, full).split(sep).join("/")] = await readFile(full, "utf8");
    }
  };
  try {
    await walk(root);
  } catch (error) {
    throw new Error(`read the workspace tree at ${root}: ${describe(error)}`, { cause: error });
  }
  return files;
}

/** Resolves a path a case file names: absolute as given, otherwise under the work directory. */
export function workPath(run: Run, name: string): string {
  return name.startsWith("/") ? name : join(run.work, name);
}

async function judge(run: Run, step: ContractStep, result: JsonValue): Promise<void> {
  if (step.alsoWritten !== undefined) {
    await judgeAlsoWritten(run, step, result);
  }
  const expectations = step.expect ?? {};
  for (const field of Object.keys(expectations).sort()) {
    const want = expectations[field];
    if (want === undefined) {
      continue;
    }
    const value = lookupDeclared(step, result, field);
    for (const problem of checkField(field, value, want, substituteFor(run))) {
      run.problems.push({ step: step.id, ...problem });
    }
  }
  for (const [name, field] of Object.entries(step.capture ?? {}).sort()) {
    const value = lookupField(result, field);
    if (typeof value !== "string") {
      throw new Error(
        `step ${JSON.stringify(step.id)}: nothing captured ${JSON.stringify(name)} because ` +
          `${JSON.stringify(field)} holds ${typeof value}, and only a string can be substituted ` +
          "into a later argument",
      );
    }
    run.captures.set(name, value);
  }
}

function substituteFor(run: Run): (text: string) => string {
  return (text: string) => substitute(run, text);
}

async function judgeAlsoWritten(run: Run, step: ContractStep, result: JsonValue): Promise<void> {
  const name = step.alsoWritten;
  if (name === undefined) {
    return;
  }
  let raw: string;
  try {
    raw = await readFile(workPath(run, name), "utf8");
  } catch (error) {
    throw new Error(
      `step ${JSON.stringify(step.id)}: the verb was asked to write ${name} and did not: ` +
        describe(error),
      { cause: error },
    );
  }
  if (step.returns !== RETURNS_FILE) {
    return;
  }
  const written = JSON.parse(raw) as JsonValue;
  if (!sameJSON(written, result)) {
    run.problems.push({
      step: step.id,
      field: name,
      message:
        `the document written to ${name} is not the one the wrapper returned.\n` +
        `on disk: ${JSON.stringify(written)}\nreturned: ${JSON.stringify(result)}`,
    });
  }
}

/**
 * A "read" step's keys are file paths and are taken literally, because a workspace
 * is full of names that contain dots — "seed.txt" is a file, not a field called seed
 * inside a field called txt. Every other verb's keys are field paths.
 */
function lookupDeclared(step: ContractStep, result: JsonValue, field: string): JsonValue | undefined {
  if (step.verb === "read") {
    return (result as Record<string, JsonValue> | undefined)?.[field];
  }
  return lookupField(result, field);
}

function judgeRefusal(run: Run, step: ContractStep, error: unknown): void {
  const message = describe(error);
  const wanted = step.fail?.contains ?? "";
  if (wanted !== "" && !message.includes(wanted)) {
    run.problems.push({
      step: step.id,
      field: "refusal",
      message: `the refusal does not say ${JSON.stringify(wanted)}.\n${message}`,
    });
  }
}

function describe(error: unknown): string {
  if (error instanceof Error) {
    // execFile's error carries the child's stderr in its message, and the refusal
    // text a step asserts on lives there rather than on the error's own message.
    const stderr = (error as { stderr?: string }).stderr ?? "";
    return stderr.trim() === "" ? error.message : `${error.message}\n${stderr}`;
  }
  return String(error);
}

export { resolveStowBinary, execFileAsync };
