import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { readFile, readdir, stat, statfs } from "node:fs/promises";
import { join } from "node:path";
import { performance, monitorEventLoopDelay } from "node:perf_hooks";
import { round } from "./metrics.mjs";

const execute = promisify(execFile);

export function cpuSeconds(value) {
  const [days, clock] = value.includes("-") ? value.split("-") : ["0", value];
  const fields = clock.split(":").map(Number);
  return Number(days) * 86400 + fields.reduce((sum, v) => sum * 60 + v, 0);
}

async function processSample(pid) {
  try {
    const { stdout } = await execute("ps", ["-o", "rss=", "-o", "time=", "-p", String(pid)], { timeout: 2000 });
    const [rss, cpu] = stdout.trim().split(/\s+/);
    return { rssBytes: Number(rss) * 1024, cpuSeconds: cpuSeconds(cpu) };
  } catch { return null; }
}

async function diskIO(pid) {
  if (process.platform !== "linux") return null;
  try {
    const input = await readFile(`/proc/${pid}/io`, "utf8");
    return Object.fromEntries(input.trim().split("\n").map((line) => {
      const [key, value] = line.split(":"); return [key, Number(value.trim())];
    }));
  } catch { return null; }
}

export async function monitor(pid, sampleMs, directory) {
  const began = performance.now();
  const cpuStart = process.cpuUsage();
  const ioStart = await diskIO(pid);
  const eventLoop = monitorEventLoopDelay({ resolution: 20 });
  eventLoop.enable();
  const samples = [];
  let timer, pending, stopped = false;
  async function sample() {
    const server = await processSample(pid);
    const disk = await statfs(directory).catch(() => null);
    samples.push({ elapsedSeconds: round((performance.now() - began) / 1000), server,
      diskAvailableBytes: disk ? disk.bavail * disk.bsize : null,
      generatorRssBytes: process.memoryUsage().rss });
  }
  function tick() {
    pending = sample().finally(() => { if (!stopped) timer = setTimeout(tick, sampleMs); });
  }
  await sample();
  timer = setTimeout(tick, sampleMs);
  return {
    async stop() {
      stopped = true; clearTimeout(timer); await pending; await sample();
      eventLoop.disable();
      const ioEnd = await diskIO(pid);
      const cpu = process.cpuUsage(cpuStart);
      const duration = (performance.now() - began) / 1000;
      const serverSamples = samples.flatMap((sample) => sample.server ? [sample.server] : []);
      const first = serverSamples[0], last = serverSamples.at(-1);
      const serverCPU = first && last ? last.cpuSeconds - first.cpuSeconds : null;
      const availableDisk = samples.map((sample) => sample.diskAvailableBytes).filter((value) => value !== null);
      return { intervalSeconds: round(duration), sampleIntervalMs: sampleMs,
        minimumSampledDiskAvailableBytes: availableDisk.length ? Math.min(...availableDisk) : null,
        serverPeakSampledRssBytes: serverSamples.length ? Math.max(...serverSamples.map((s) => s.rssBytes)) : null,
        serverCpuSeconds: serverCPU, serverAverageCpuPercent: serverCPU === null ? null : round(serverCPU / duration * 100),
        generatorPeakSampledRssBytes: Math.max(...samples.map((s) => s.generatorRssBytes)),
        generatorCpuSeconds: round((cpu.user + cpu.system) / 1000000),
        generatorEventLoopDelayMs: { mean: Number.isFinite(eventLoop.mean) ? round(eventLoop.mean / 1000000) : null, max: round(eventLoop.max / 1000000) },
        diskReadBytes: ioStart && ioEnd ? ioEnd.read_bytes - ioStart.read_bytes : null,
        diskWrittenBytes: ioStart && ioEnd ? ioEnd.write_bytes - ioStart.write_bytes : null,
        allocationBytes: null, samples };
    },
  };
}

export async function footprint(directory) {
  const total = { apparentBytes: 0, allocatedBytes: 0, files: 0 };
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      const child = await footprint(path);
      for (const key of Object.keys(total)) total[key] += child[key];
    } else if (entry.isFile()) {
      const info = await stat(path);
      total.apparentBytes += info.size; total.allocatedBytes += info.blocks * 512; total.files++;
    }
  }
  return total;
}
