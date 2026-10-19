import { readFile } from "node:fs/promises";
import { cpus, totalmem, platform, release, arch } from "node:os";
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execute = promisify(execFile);
const read = (path) => readFile(path, "utf8").then((value) => value.trim()).catch(() => null);
let ticks;

export function parseProcStat(text, clockTicks = 100) {
  const fields = text.slice(text.lastIndexOf(")") + 2).trim().split(/\s+/);
  return (Number(fields[11]) + Number(fields[12])) / clockTicks;
}

export async function processResources(pid) {
  if (platform() !== "linux") {
    const { stdout } = await execute("ps", ["-o", "rss=", "-o", "time=", "-p", String(pid)]);
    const [rss, clock] = stdout.trim().split(/\s+/);
    return { rssBytes: Number(rss) * 1024, cpuSeconds: clock.split(":").reduce((n, v) => n * 60 + Number(v), 0), readBytes: null, writtenBytes: null };
  }
  ticks ??= execute("getconf", ["CLK_TCK"]).then((r) => Number(r.stdout.trim()));
  const [status, stat, io, clockTicks] = await Promise.all([
    read(`/proc/${pid}/status`), read(`/proc/${pid}/stat`), read(`/proc/${pid}/io`), ticks,
  ]);
  if (!status || !stat) throw new Error("benchmark process exited before sampling");
  const field = (text, name) => Number(text?.match(new RegExp(`^${name}:\\s+(\\d+)`, "m"))?.[1] ?? 0);
  return { rssBytes: field(status, "VmRSS") * 1024, highWaterRssBytes: field(status, "VmHWM") * 1024,
    cpuSeconds: parseProcStat(stat, clockTicks), readBytes: io ? field(io, "read_bytes") : null,
    writtenBytes: io ? field(io, "write_bytes") : null };
}

export async function systemMetadata() {
  const [cpuMax, memoryMax, cpuStat, mountInfo, identity] = await Promise.all([
    read("/sys/fs/cgroup/cpu.max"), read("/sys/fs/cgroup/memory.max"), read("/sys/fs/cgroup/cpu.stat"),
    read("/proc/self/mountinfo"), read("benchmark/build-identity.json"),
  ]);
  return { recordedAt: new Date().toISOString(), cpu: cpus()[0]?.model, hostLogicalCpus: cpus().length,
    hostMemoryBytes: totalmem(), platform: platform(), arch: arch(), release: release(), node: process.version,
    cgroup: { cpuMax, memoryMax, cpuStat }, mountInfo,
    deployment: Object.fromEntries(["RAILWAY_DEPLOYMENT_ID", "RAILWAY_REPLICA_ID", "RAILWAY_SERVICE_ID", "RAILWAY_ENVIRONMENT_ID", "RAILWAY_REGION"].flatMap((key) => process.env[key] ? [[key, process.env[key]]] : [])),
    build: identity ? JSON.parse(identity) : null };
}

export async function cgroupResources() {
  const [cpuMax, memoryMax, cpuStat] = await Promise.all([
    read("/sys/fs/cgroup/cpu.max"), read("/sys/fs/cgroup/memory.max"), read("/sys/fs/cgroup/cpu.stat"),
  ]);
  return { cpuMax, memoryMax, cpuStat };
}
