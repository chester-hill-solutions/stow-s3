#!/usr/bin/env node
// Measure the installed TypeScript workspace lifecycle against the native CLI.
// This is a measurement tool, not a timing gate.
//
//   node scripts/benchmark-workspace.mjs [--runs N] [--json path]
import { mkdtemp, mkdir, rm, stat, writeFile } from "node:fs/promises";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { performance } from "node:perf_hooks";
import {
  checkpointWorkspace,
  destroyWorkspace,
  diffWorkspaces,
  exportWorkspaceCheckpoint,
  handoffWorkspace,
  importWorkspaceCheckpoint,
  prepareWorkspace,
  restoreWorkspaceCheckpoint,
  resumeWorkspace,
  runWorkspaceCommand,
} from "../dist/workspace.js";

const PACKAGE_ROOT = resolve(import.meta.dirname, "..");
const REPO_ROOT = resolve(PACKAGE_ROOT, "..", "..");
const STOW_BIN = resolve(REPO_ROOT, "bin", "stow-s3");

function argument(name, fallback) {
  const index = process.argv.indexOf(`--${name}`);
  return index === -1 ? fallback : process.argv[index + 1];
}

const RUNS = Number(argument("runs", "7"));
const JSON_OUT = argument("json", join(tmpdir(), "stow-workspace-baseline.json"));
const MEMORY_ONLY = process.argv.includes("--memory-only");
const PARALLEL_WORKSPACE_LEVELS = [1, 4, 8];
const AGENT_FILES = [
  ["src/parser/tokenizer.go", 4200], ["src/parser/grammar.go", 11800],
  ["src/parser/ast.go", 7300], ["src/parser/parser.go", 18600],
  ["src/parser/errors.go", 2100], ["src/parser/lexer.go", 9400],
  ["src/store/store.go", 6700], ["src/store/memory.go", 5200],
  ["src/store/fs.go", 12900], ["src/store/workspace.go", 15300],
  ["src/s3api/server.go", 21400], ["src/s3api/handlers.go", 34100],
  ["src/s3api/router.go", 8800], ["src/s3api/errors.go", 3300],
  ["src/auth/sigv4.go", 16700], ["src/auth/presign.go", 5900],
  ["go.mod", 880], ["go.sum", 2570], ["Makefile", 6481],
  ["README.md", 19783], ["CHANGELOG.md", 74215],
  ["docs/workspace-contract.md", 18920], ["docs/agent-workspace-plan.md", 28314],
  ["testdata/fixture-large.bin", 262144],
];
const WORKLOADS = [
  { name: "tiny", files: 2, bytes: 70, specs: [["tiny/a.txt", 32], ["tiny/b.txt", 38]] },
  { name: "agent", files: AGENT_FILES.length, bytes: AGENT_FILES.reduce((sum, [, size]) => sum + size, 0), specs: AGENT_FILES },
  { name: "scale", files: 500, bytes: 500 * 32768 },
];

function round(value) {
  return Math.round(value * 100) / 100;
}

function percentile(values, fraction) {
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.ceil(fraction * sorted.length) - 1)];
}

function summarize(values, unit = "Ms") {
  return {
    samples: values.length,
    [`min${unit}`]: round(Math.min(...values)),
    [`p50${unit}`]: round(percentile(values, 0.5)),
    [`p95${unit}`]: round(percentile(values, 0.95)),
    [`max${unit}`]: round(Math.max(...values)),
  };
}

async function timed(samples, name, action) {
  const start = performance.now();
  const result = await action();
  samples[name].push(performance.now() - start);
  return result;
}

async function createInputs(base, workload) {
  const inputDir = join(base, "inputs");
  await mkdir(inputDir, { recursive: true });
  const inputs = [];
  for (let index = 0; index < workload.files; index += 1) {
    const generatedPath = workload.specs?.[index]?.[0] ??
      (index % 10 === 9
        ? `testdata/fixture-${String(index).padStart(4, "0")}.bin`
        : `src/module-${String(index).padStart(4, "0")}.go`);
    const size = workload.specs?.[index]?.[1] ?? 32768;
    const source = join(inputDir, generatedPath);
    await mkdir(resolve(source, ".."), { recursive: true });
    await writeFile(source, generatedContent(size, index, generatedPath.endsWith(".bin")));
    inputs.push({ source, destination: `seed/${generatedPath}` });
  }
  return { inputDir, inputs };
}

function generatedContent(size, seed, binary) {
  const result = Buffer.alloc(size);
  if (binary) {
    let state = (seed + 1) * 0x9e3779b1;
    for (let offset = 0; offset < size; offset += 4) {
      state ^= state << 13;
      state ^= state >>> 17;
      state ^= state << 5;
      result.writeUInt32LE(state >>> 0, offset);
    }
    return result;
  }
  const pattern = Buffer.from(
    `// Workspace benchmark source fixture ${seed}\npackage module${seed}\n\nfunc Value${seed}() int { return ${seed} }\n\n`,
  );
  for (let offset = 0; offset < size; offset += pattern.length) {
    pattern.copy(result, offset, 0, Math.min(pattern.length, size - offset));
  }
  return result;
}

async function directoryBytes(path) {
  let total = 0;
  for (const entry of await (await import("node:fs/promises")).readdir(path, { withFileTypes: true })) {
    const item = join(path, entry.name);
    if (entry.isDirectory()) total += await directoryBytes(item);
    else if (entry.isFile()) total += (await stat(item)).size;
  }
  return total;
}

function nativeCliWithRss(args) {
  const timeArgs = process.platform === "darwin" ? ["-l"] : ["-v"];
  const result = spawnSync("/usr/bin/time", [...timeArgs, STOW_BIN, "workspace", ...args], {
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(result.stderr || `workspace command exited ${result.status}`);
  const match = process.platform === "darwin"
    ? result.stderr.match(/(\d+)\s+maximum resident set size/)
    : result.stderr.match(/Maximum resident set size \(kbytes\):\s*(\d+)/);
  const rssBytes = match
    ? Number(match[1]) * (process.platform === "darwin" ? 1 : 1024)
    : null;
  return { value: JSON.parse(result.stdout), peakRssMb: rssBytes === null ? null : round(rssBytes / 1048576) };
}

async function measureWorkspaceRss(workload) {
  const base = await mkdtemp(join(tmpdir(), "stow-workspace-rss-"));
  const root = join(base, "workspace");
  const registryDir = join(base, "registry");
  const importRegistry = join(base, "import-registry");
  const restoreRoot = join(base, "restored");
  const archivePath = join(base, "checkpoint.tar.gz");
  const { inputs } = await createInputs(base, workload);
  const manifestPath = join(base, "manifest.json");
  await writeFile(manifestPath, JSON.stringify({
    version: 1,
    root,
    registry_dir: registryDir,
    working_directory: ".",
    max_bytes: 64 * 1024 * 1024,
    max_objects: 1000,
    max_checkpoint_bytes: 64 * 1024 * 1024,
    max_checkpoints: 10,
    inputs,
  }));
  try {
    const prepare = nativeCliWithRss(["prepare", "--manifest", manifestPath]);
    const id = prepare.value.workspace_id;
    const checkpoint = nativeCliWithRss(["checkpoint", "--id", id, "--registry-dir", registryDir]);
    await writeFile(join(root, "result.dat"), Buffer.alloc(1024));
    const incremental = nativeCliWithRss(["checkpoint", "--id", id, "--registry-dir", registryDir, "--parent", checkpoint.value.checkpoint_id]);
    nativeCliWithRss(["export", "--checkpoint-id", incremental.value.checkpoint_id, "--registry-dir", registryDir, "--output", archivePath]);
    nativeCliWithRss(["import", "--archive", archivePath, "--registry-dir", importRegistry]);
    nativeCliWithRss(["restore", "--checkpoint-id", incremental.value.checkpoint_id, "--registry-dir", registryDir, "--root", restoreRoot]);
    return {
      preparePeakRssMb: prepare.peakRssMb,
      checkpointPeakRssMb: incremental.peakRssMb,
      note: "Native CLI high-water RSS from /usr/bin/time; null means this platform's time output was not recognized.",
    };
  } finally {
    await rm(base, { recursive: true, force: true });
  }
}

async function runParallelPrepare(count, run) {
  const base = await mkdtemp(join(tmpdir(), "stow-workspace-parallel-"));
  const registryDir = join(base, "registry");
  const { inputs } = await createInputs(base, WORKLOADS.find((workload) => workload.name === "agent"));
  const manifests = [];
  const roots = [];
  for (let index = 0; index < count; index += 1) {
    const root = join(base, `workspace-${index}`);
    const manifestPath = join(base, `manifest-${index}.json`);
    roots.push(root);
    await writeFile(manifestPath, JSON.stringify({
      version: 1,
      root,
      registry_dir: registryDir,
      working_directory: ".",
      max_bytes: 64 * 1024 * 1024,
      max_objects: 1000,
      inputs,
    }));
    manifests.push(manifestPath);
  }

  const start = performance.now();
  const finishedAt = [];
  const preparedIds = [];
  try {
    const results = await Promise.all(manifests.map(async (manifestPath) => {
      const operationStart = performance.now();
      const value = await prepareWorkspace(manifestPath);
      const done = performance.now();
      finishedAt.push(done - start);
      preparedIds.push(value.workspace_id);
      return { ms: done - operationStart, seededBytes: value.seeded_bytes };
    }));
    const wallMs = performance.now() - start;
    let rootBytes = 0;
    for (const root of roots) rootBytes += await directoryBytes(root);
    const registryBytes = await directoryBytes(registryDir);
    await Promise.all(preparedIds.map((id) => destroyWorkspace(id, registryDir)));
    return {
      concurrentWorkspaces: count,
      run,
      batchMs: wallMs,
      firstWorkspaceReadyMs: Math.min(...finishedAt),
      lastWorkspaceReadyMs: Math.max(...finishedAt),
      perPrepareMs: results.map((result) => result.ms),
      workspacesPerSecond: count / (wallMs / 1000),
      seededBytesTotal: results.reduce((sum, result) => sum + result.seededBytes, 0),
      workspaceBytesOnDiskTotal: rootBytes,
      sharedRegistryBytesOnDisk: registryBytes,
    };
  } finally {
    await Promise.all(preparedIds.map((id) => destroyWorkspace(id, registryDir).catch(() => {})));
    await rm(base, { recursive: true, force: true });
  }
}

async function runOne(workload, run) {
  const base = await mkdtemp(join(tmpdir(), "stow-workspace-bench-"));
  const root = join(base, "workspace");
  const registryDir = join(base, "registry");
  const importRegistry = join(base, "import-registry");
  const restoreRoot = join(base, "restored");
  const handoffPath = join(base, "handoff.json");
  const archivePath = join(base, "checkpoint.tar.gz");
  const { inputs } = await createInputs(base, workload);
  const manifestPath = join(base, "manifest.json");
  await writeFile(
    manifestPath,
    JSON.stringify({
      version: 1,
      root,
      registry_dir: registryDir,
      working_directory: ".",
      max_bytes: 64 * 1024 * 1024,
      max_objects: 1000,
      max_checkpoint_bytes: 64 * 1024 * 1024,
      max_checkpoints: 10,
      inputs,
    }),
  );

  const samples = {
    prepare: [], handoff: [], resume: [], initialCheckpoint: [],
    checkpointAfterEdit: [], diff: [], export: [], preview: [],
    import: [], restore: [], destroyOriginal: [], destroyRestored: [],
  };
  let workspaceId;
  let restoredId;
  let result;
  try {
    result = await timed(samples, "prepare", () => prepareWorkspace(manifestPath));
    workspaceId = result.workspace_id;
    await timed(samples, "handoff", () => handoffWorkspace(workspaceId, { registryDir, output: handoffPath }));
    await timed(samples, "resume", () => resumeWorkspace({ handoffPath }));

    const firstCheckpoint = await timed(samples, "initialCheckpoint", () =>
      checkpointWorkspace({ id: workspaceId, registryDir }),
    );
    await writeFile(join(root, "result.dat"), Buffer.alloc(1024, run + 1));
    const secondCheckpoint = await timed(samples, "checkpointAfterEdit", () =>
      checkpointWorkspace({ id: workspaceId, registryDir, parent: firstCheckpoint.checkpoint_id }),
    );
    await timed(samples, "diff", () =>
      diffWorkspaces(firstCheckpoint.checkpoint_id, secondCheckpoint.checkpoint_id, registryDir),
    );
    await timed(samples, "export", () =>
      exportWorkspaceCheckpoint(secondCheckpoint.checkpoint_id, archivePath, { registryDir }),
    );
    await timed(samples, "preview", () => runWorkspaceCommand(["preview", "--archive", archivePath]));
    await timed(samples, "import", () =>
      importWorkspaceCheckpoint(archivePath, { registryDir: importRegistry }),
    );
    const restored = await timed(samples, "restore", () =>
      restoreWorkspaceCheckpoint(secondCheckpoint.checkpoint_id, restoreRoot, registryDir),
    );
    restoredId = restored.workspace_id;

    const archiveBytes = (await stat(archivePath)).size;
    const workspaceBytes = await directoryBytes(root);
    const registryBytes = await directoryBytes(registryDir);
    const handoffBytes = (await stat(handoffPath)).size;
    await timed(samples, "destroyRestored", () => destroyWorkspace(restoredId, registryDir));
    await timed(samples, "destroyOriginal", () => destroyWorkspace(workspaceId, registryDir));
    return {
      workload: workload.name,
      run,
      seededFiles: workload.files,
      seededBytes: result.seeded_bytes,
      checkpointFiles: secondCheckpoint.files,
      checkpointBytes: secondCheckpoint.bytes,
      workspaceBytesOnDiskBeforeDestroy: workspaceBytes,
      registryBytesOnDiskBeforeDestroy: registryBytes,
      archiveBytes,
      totalWorkspaceAndRegistryBytesBeforeDestroy: workspaceBytes + registryBytes,
      handoffBytes,
      operationMs: Object.fromEntries(Object.entries(samples).map(([key, values]) => [key, values[0]])),
    };
  } finally {
    if (restoredId) await destroyWorkspace(restoredId, registryDir).catch(() => {});
    if (workspaceId) await destroyWorkspace(workspaceId, registryDir).catch(() => {});
    await rm(base, { recursive: true, force: true });
  }
}

const results = [];
if (!MEMORY_ONLY) {
  for (const workload of WORKLOADS) {
    for (let run = 0; run < RUNS; run += 1) {
      results.push(await runOne(workload, run));
      process.stdout.write(`\r${workload.name}: run ${run + 1}/${RUNS}`);
    }
    process.stdout.write("\n");
  }
}

const grouped = {};
for (const workload of WORKLOADS) {
  const rows = results.filter((row) => row.workload === workload.name);
  if (MEMORY_ONLY) {
    grouped[workload.name] = {
      seededFiles: workload.files,
      seededBytes: workload.bytes,
      nativeCliMemory: await measureWorkspaceRss(workload),
    };
    continue;
  }
  const operations = Object.keys(rows[0].operationMs);
  grouped[workload.name] = {
    seededFiles: workload.files,
    seededBytes: workload.bytes,
    operations: Object.fromEntries(
      operations.map((operation) => [operation, summarize(rows.map((row) => row.operationMs[operation]))]),
    ),
    workspaceBytesOnDiskBeforeDestroy: summarize(rows.map((row) => row.workspaceBytesOnDiskBeforeDestroy), "Bytes"),
    registryBytesOnDiskBeforeDestroy: summarize(rows.map((row) => row.registryBytesOnDiskBeforeDestroy), "Bytes"),
    workspaceAndRegistryBytesBeforeDestroy: summarize(rows.map((row) => row.totalWorkspaceAndRegistryBytesBeforeDestroy), "Bytes"),
    checkpointBytes: summarize(rows.map((row) => row.checkpointBytes), "Bytes"),
    archiveBytes: summarize(rows.map((row) => row.archiveBytes), "Bytes"),
    handoffBytes: summarize(rows.map((row) => row.handoffBytes), "Bytes"),
    nativeCliMemory: {
      note: "Run with --memory-only under system resource measurement to capture CLI high-water RSS.",
    },
  };
}

const parallelWorkspaceRuns = [];
if (!MEMORY_ONLY) {
  for (const count of PARALLEL_WORKSPACE_LEVELS) {
    for (let run = 0; run < 5; run += 1) {
      parallelWorkspaceRuns.push(await runParallelPrepare(count, run));
      process.stdout.write(`\r${count} parallel workspaces: run ${run + 1}/5`);
    }
    process.stdout.write("\n");
  }
}

const parallelWorkspaces = MEMORY_ONLY ? {} : Object.fromEntries(PARALLEL_WORKSPACE_LEVELS.map((count) => {
  const rows = parallelWorkspaceRuns.filter((row) => row.concurrentWorkspaces === count);
  return [String(count), {
    workspaces: count,
    samples: rows.length,
    batchMs: summarize(rows.map((row) => row.batchMs)),
    firstWorkspaceReadyMs: summarize(rows.map((row) => row.firstWorkspaceReadyMs)),
    lastWorkspaceReadyMs: summarize(rows.map((row) => row.lastWorkspaceReadyMs)),
    perPrepareMs: summarize(rows.flatMap((row) => row.perPrepareMs)),
    workspacesPerSecond: summarize(rows.map((row) => row.workspacesPerSecond), "WorkspacesPerSecond"),
    seededBytesTotal: summarize(rows.map((row) => row.seededBytesTotal), "Bytes"),
    workspaceBytesOnDiskTotal: summarize(rows.map((row) => row.workspaceBytesOnDiskTotal), "Bytes"),
    sharedRegistryBytesOnDisk: summarize(rows.map((row) => row.sharedRegistryBytesOnDisk), "Bytes"),
  }];
}));

const report = {
  environment: {
    measuredAt: new Date().toISOString(),
    node: process.version,
    os: `${process.platform} ${process.arch}`,
    cpu: (await import("node:os")).cpus()[0]?.model ?? "unknown",
    cpuCount: (await import("node:os")).cpus().length,
    totalMemoryMb: Math.round((await import("node:os")).totalmem() / 1048576),
    runsPerWorkload: MEMORY_ONLY ? 1 : RUNS,
    surface: "TypeScript workspace wrapper invoking the local native CLI",
    note: "Per-operation elapsed time includes a fresh CLI child process; source fixture generation is excluded.",
  },
  workloads: grouped,
  parallelWorkspacePrepare: parallelWorkspaces,
};

const output = resolve(REPO_ROOT, JSON_OUT);
await mkdir(resolve(output, ".."), { recursive: true });
await writeFile(output, `${JSON.stringify(report, null, 2)}\n`);
console.log(JSON.stringify(report, null, 2));
console.log(`\nwrote ${output}`);
