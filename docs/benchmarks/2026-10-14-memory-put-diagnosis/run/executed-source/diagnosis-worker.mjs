import { randomBytes } from "node:crypto";
import { monitorEventLoopDelay } from "node:perf_hooks";
import { Session } from "node:inspector";
import { promisify } from "node:util";
import { setTimeout as delay } from "node:timers/promises";
import { benchmarkClient } from "./client.mjs";
import { runLoad } from "../packages/stow-s3/scripts/performance/load.mjs";
import { workload, verifyFinal } from "../packages/stow-s3/scripts/performance/workload.mjs";
import { Measurements } from "../packages/stow-s3/scripts/performance/metrics.mjs";

let connection, config, scenario, state;
process.on("message", async message => {
  try {
    if (message.type === "init") {
      ({ config, scenario } = message);
      connection = benchmarkClient(message.endpoint, message.ready, scenario.concurrency, config.timeoutMs);
      state = { fixtures: [], payloads: Array.from({ length: scenario.concurrency }, () => randomBytes(scenario.bytes)),
        latest: new Map(), unknown: new Set(), workerOffset: message.workerOffset };
      process.send({ type: "ready" });
      return;
    }
    if (message.type !== "warmup" && message.type !== "measure") throw new Error("unknown worker command");
    let session, post;
    if (message.profile) {
      session = new Session(); session.connect(); post = promisify(session.post.bind(session));
      await post("Profiler.enable"); await post("Profiler.start");
    }
    await delay(Math.max(0, message.startAt - Date.now()));
    const startedAt = Date.now(), cpuStart = process.cpuUsage(), loop = monitorEventLoopDelay({ resolution: 20 });
    let peakRssBytes = process.memoryUsage().rss;
    loop.enable();
    const timer = setInterval(() => { peakRssBytes = Math.max(peakRssBytes, process.memoryUsage().rss); }, 100);
    const measurements = new Measurements();
    let load;
    try { load = await runLoad({ durationMs: message.durationMs, concurrency: scenario.concurrency, rate: 0,
      maxQueue: config.maxQueue, timeoutMs: config.timeoutMs, task: workload(connection.client, scenario, config, state, measurements) }); }
    finally { clearInterval(timer); loop.disable(); }
    const endedAt = Date.now(), cpu = process.cpuUsage(cpuStart);
    const profile = session ? (await post("Profiler.stop")).profile : null;
    session?.disconnect();
    const result = { type: message.type, workerOffset: state.workerOffset, startedAt, endedAt, load,
      cpuSeconds: (cpu.user + cpu.system) / 1e6, peakRssBytes,
      eventLoopDelayMs: { mean: Number.isFinite(loop.mean) ? loop.mean / 1e6 : null, max: loop.max / 1e6 },
      measurements: measurements.summary(message.durationMs / 1000), profile };
    if (message.type === "measure") result.verification = await verifyFinal(connection.client, state, config.timeoutMs);
    process.send(result, () => { if (message.type === "measure") { connection.close(); process.disconnect(); } });
  } catch (error) {
    process.send?.({ type: "error", error: error.message }, () => { connection?.close(); process.exit(1); });
  }
});
