import { monitorEventLoopDelay } from "node:perf_hooks";
import { remote } from "./http.mjs";
import { cgroupResources } from "./resources.mjs";

export async function sampleRemote(base, sampleMs) {
  const samples = [];
  const start = performance.now();
  const cpuStart = process.cpuUsage();
  const eventLoop = monitorEventLoopDelay({ resolution: 20 });
  eventLoop.enable();
  let pending, timer, failure, stopped = false;
  async function sample() {
    const server = await remote(base, "/sample");
    samples.push({ elapsedSeconds: (performance.now() - start) / 1000, ...server, generatorRssBytes: process.memoryUsage().rss,
      generatorCgroup: await cgroupResources() });
  }
  function tick() {
    pending = sample().catch(error=>{ failure ??= error; }).finally(() => { if (!stopped) timer = setTimeout(tick, sampleMs); });
  }
  await sample();
  timer = setTimeout(tick, sampleMs);
  return { async stop() {
    stopped = true; clearTimeout(timer); await pending; await sample(); eventLoop.disable();
    if (failure) throw failure;
    const first = samples[0].process, last = samples.at(-1).process;
    const cpu = process.cpuUsage(cpuStart), duration = (performance.now() - start) / 1000;
    return { intervalSeconds: duration, sampleIntervalMs: sampleMs, samples,
      serverPeakSampledRssBytes: Math.max(...samples.map(s=>s.process.rssBytes)),
      serverHighWaterRssBytes: Math.max(...samples.map(s=>s.process.highWaterRssBytes ?? 0)),
      serverAverageCpuPercent: (last.cpuSeconds - first.cpuSeconds) / duration * 100,
      generatorPeakSampledRssBytes: Math.max(...samples.map(s=>s.generatorRssBytes)),
      generatorCpuSeconds: (cpu.user + cpu.system) / 1e6,
      generatorEventLoopDelayMs: { mean: Number.isFinite(eventLoop.mean) ? eventLoop.mean / 1e6 : null, max: eventLoop.max / 1e6 },
      diskReadBytes: first.readBytes === null ? null : last.readBytes - first.readBytes,
      diskWrittenBytes: first.writtenBytes === null ? null : last.writtenBytes - first.writtenBytes,
      minimumSampledDiskAvailableBytes: Math.min(...samples.map(s=>s.diskAvailableBytes)),
      diskHeadroomMaintained: samples.every(s=>s.diskAvailableBytes >= 1024 ** 3) };
  } };
}
