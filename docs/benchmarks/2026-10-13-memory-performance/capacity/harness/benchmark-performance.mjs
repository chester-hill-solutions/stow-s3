#!/usr/bin/env node
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { createHash } from "node:crypto";
import { mkdtemp, mkdir, writeFile, readFile, rm, statfs } from "node:fs/promises";
import { join, resolve, relative, dirname } from "node:path";
import { tmpdir, cpus, totalmem, platform, arch, release } from "node:os";
import { options, scenarios } from "./performance/options.mjs";
import { startServer } from "./performance/server.mjs";
import { monitor, footprint } from "./performance/resources.mjs";
import { prepare, workload, verifyFinal, leftovers, recoveryProbe } from "./performance/workload.mjs";
import { runLoad } from "./performance/load.mjs";
import { Measurements } from "./performance/metrics.mjs";
import { writeReport } from "./performance/report.mjs";

const execute = promisify(execFile);
const selected = await options(process.argv.slice(2));
const { config, out, binary } = selected;
await mkdir(out, { recursive: true });
await writeFile(join(out, "config.json"), JSON.stringify(config, null, 2) + "\n", { flag: "wx" });
const root = await mkdtemp(join(selected.dataRoot ?? tmpdir(), "stow-performance-"));
let suite;
try {
  const metadata = await machineMetadata();
  suite = { version: 1, config, metadata, results: [], recovery: null, complete: false };
  await save();
  for (const scenario of scenarios(config)) {
    const repetitions = scenario.soak ? 1 : config.repetitions;
    for (let run = 0; run < repetitions; run++) {
      const name = `${scenario.backend}-${scenario.workload}-${scenario.bytes}-c${scenario.concurrency}-r${scenario.rate}${scenario.soak ? "-soak" : ""}-${run + 1}`;
      console.log(`Measuring ${name}`);
      const result = await measure(scenario, join(root, name));
      await writeFile(join(out, `${name}.json`), JSON.stringify(result, null, 2) + "\n");
      const resources = { ...result.resources };
      delete resources.samples;
      const load = { ...result.load };
      delete load.perSecond;
      suite.results.push({ ...result, resources, load, rawFile: `${name}.json` });
      await save();
    }
  }
  if (config.backends.includes("filesystem")) suite.recovery = await recoveryProbe(binary, join(root, "recovery"), config, startServer);
  suite.complete = true;
  await save();
  console.log(`Saved measurements and report to ${out}`);
  if (suite.results.some((r) => r.load.failed || !r.verification.complete || r.leftovers.count > 0 || !r.resources.diskHeadroomMaintained)) process.exitCode = 1;
} finally { await rm(root, { recursive: true, force: true }); }

async function save() {
  await writeFile(join(out, "suite.json"), JSON.stringify(suite, null, 2) + "\n");
  await writeReport(out, suite);
}

async function measure(scenario, directory) {
  const disk = await statfs(root);
  const required = config.minFreeDiskBytes + (scenario.backend === "filesystem" ? scenario.bytes * (config.retainedObjects + scenario.concurrency * 2) * 2 : 0);
  if (disk.bavail * disk.bsize < required) throw new Error(`insufficient disk headroom: need ${required} available bytes`);
  const server = await startServer(binary, directory, scenario, config);
  let resources;
  try {
    const state = await prepare(server.client, scenario, config);
    const loadOptions = { concurrency: scenario.concurrency, rate: scenario.rate, maxQueue: config.maxQueue, timeoutMs: config.timeoutMs };
    const warmup = config.warmupSeconds > 0 ? await runLoad({ ...loadOptions, durationMs: config.warmupSeconds * 1000,
      task: workload(server.client, scenario, config, state, new Measurements()) }) : null;
    const before = await footprint(directory);
    const sampler = await monitor(server.child.pid, config.sampleMs, directory);
    const measurements = new Measurements();
    let load;
    try {
      load = await runLoad({ ...loadOptions, durationMs: (scenario.soak ? config.soakSeconds : config.durationSeconds) * 1000,
        task: workload(server.client, scenario, config, state, measurements) });
    } finally { resources = await sampler.stop(); }
    let verification;
    try { verification = await verifyFinal(server.client, state, config.timeoutMs); }
    catch (error) { verification = { complete: false, error: error.name }; }
    const measured = measurements.summary(load.measuredSeconds);
    const writtenPayload = measured.operations.put?.successfulBytes ?? 0;
    resources.diskWriteBytesPerPayloadByte = resources.diskWrittenBytes === null || writtenPayload === 0 ? null : resources.diskWrittenBytes / writtenPayload;
    resources.diskHeadroomMaintained = resources.minimumSampledDiskAvailableBytes !== null && resources.minimumSampledDiskAvailableBytes >= config.minFreeDiskBytes;
    return { scenario, warmup, load, measurements: measured, resources,
      capabilities: server.capabilities, binaryVersion: server.binaryVersion,
      verification, leftovers: await leftovers(server.client, config.timeoutMs),
      retainedFootprint: { before, after: await footprint(directory) } };
  } finally { try { await server.close(); } finally { await rm(directory, { recursive: true, force: true }); } }
}

async function machineMetadata() {
  const [revision, status, build] = await Promise.all([
    execute("git", ["rev-parse", "HEAD"]), execute("git", ["status", "--porcelain"]), execute("go", ["version", "-m", binary]),
  ]);
  const sha256 = createHash("sha256").update(await readFile(binary)).digest("hex");
  const harnessFiles = [resolve(import.meta.dirname, "benchmark-performance.mjs"), ...["options", "metrics", "load", "server", "resources", "workload", "report"].map((name) => resolve(import.meta.dirname, `performance/${name}.mjs`))];
  const harness = createHash("sha256");
  for (const path of harnessFiles) {
    const content = await readFile(path);
    harness.update(content);
    const snapshot = join(out, "harness", relative(import.meta.dirname, path));
    await mkdir(dirname(snapshot), { recursive: true });
    await writeFile(snapshot, content);
  }
  const lockfileSha256 = createHash("sha256").update(await readFile(resolve(import.meta.dirname, "../package-lock.json"))).digest("hex");
  const fs = await statfs(root);
  return { startedAt: new Date().toISOString(), reportingTimezone: "America/Toronto",
    cpu: cpus()[0]?.model, logicalCpus: cpus().length, memoryBytes: totalmem(), platform: platform(), arch: arch(), osRelease: release(),
    node: process.version, commit: revision.stdout.trim(), dirty: Boolean(status.stdout.trim()), dirtyFiles: status.stdout.trim().split("\n").filter(Boolean),
    binarySha256: sha256, binaryBuild: build.stdout, harnessSha256: harness.digest("hex"), lockfileSha256, filesystemType: fs.type,
    availableDiskBytesAtStart: fs.bavail * fs.bsize, dataRoot: selected.dataRoot ?? tmpdir(), temporaryRoot: root,
    gc: { GOGC: process.env.GOGC ?? "Go default", GOMEMLIMIT: process.env.GOMEMLIMIT ?? "Go default" },
    transport: "loopback HTTP; SigV4; AWS SDK; no automatic retries; keep-alive", argv: process.argv.slice(2) };
}
