import { performance } from "node:perf_hooks";
import { setTimeout as delay } from "node:timers/promises";
import { Histogram, round } from "./metrics.mjs";

export async function runLoad({ durationMs, concurrency, rate, maxQueue, task, timeoutMs }) {
  const start = performance.now();
  const deadline = start + durationMs;
  const queue = [];
  const free = Array.from({ length: concurrency }, (_, index) => index);
  const active = new Set();
  const response = new Histogram();
  const service = new Histogram();
  const wait = new Histogram();
  const schedulerLag = new Histogram();
  const errors = new Map();
  const seconds = [];
  let offered = 0, dropped = 0, failed = 0, successful = 0, windowSuccessful = 0, peakQueue = 0, expiredBeforeStart = 0;
  const bucket = () => {
    const index = Math.min(Math.floor((performance.now() - start) / 1000), Math.ceil(durationMs / 1000) - 1);
    return seconds[index] ??= { second: index, offered: 0, dropped: 0, successful: 0, failed: 0, peakQueue: 0 };
  };
  async function executeJob(job, worker) {
    const began = performance.now();
    let executed = false;
    wait.add(began - job.scheduled);
    try {
      const remainingMs = timeoutMs - (began - job.scheduled);
      if (remainingMs <= 0) {
        expiredBeforeStart++;
        const error = new Error("file expired in queue"); error.name = "QueueExpired"; throw error;
      }
      executed = true;
      await task({ worker, sequence: job.sequence, deadline, signal: AbortSignal.timeout(Math.ceil(remainingMs)) });
      successful++;
      if (performance.now() <= deadline) { windowSuccessful++; bucket().successful++; }
    } catch (error) {
      failed++;
      const name = error.name ?? "Error";
      errors.set(name, (errors.get(name) ?? 0) + 1);
      if (performance.now() <= deadline) bucket().failed++;
    } finally {
      const ended = performance.now();
      if (executed) service.add(ended - began);
      response.add(ended - job.scheduled);
    }
  }
  function launch(job) {
    const worker = free.pop();
    const promise = Promise.resolve().then(() => executeJob(job, worker)).finally(() => {
        free.push(worker);
        active.delete(promise);
        if (queue.length) launch(queue.shift());
    });
    active.add(promise);
  }
  function offer(scheduled) {
    const sequence = offered++;
    schedulerLag.add(performance.now() - scheduled);
    bucket().offered++;
    const job = { scheduled, sequence };
    if (free.length) launch(job);
    else if (queue.length < maxQueue) {
      queue.push(job);
      peakQueue = Math.max(peakQueue, queue.length);
      bucket().peakQueue = Math.max(bucket().peakQueue, queue.length);
    } else { dropped++; bucket().dropped++; }
  }
  if (rate === 0) {
    const workers = Array.from({ length: concurrency }, async (_, worker) => {
      while (performance.now() < deadline) {
        const scheduled = performance.now();
        const sequence = offered++;
        bucket().offered++;
        await executeJob({ scheduled, sequence }, worker);
      }
    });
    await Promise.all(workers);
  } else {
    const planned = Math.ceil(durationMs * rate / 1000);
    for (let index = 0; index < planned;) {
      const scheduled = start + index * 1000 / rate;
      const now = performance.now();
      if (now >= deadline) {
        const missed = planned - index;
        offered += missed; dropped += missed;
        bucket().offered += missed; bucket().dropped += missed;
        break;
      }
      if (now < scheduled) { await delay(Math.min(20, scheduled - now)); continue; }
      offer(scheduled); index++;
    }
    if (performance.now() < deadline) await delay(deadline - performance.now());
    while (active.size) await Promise.allSettled([...active]);
  }
  return {
    mode: rate === 0 ? "closed-loop capacity" : "scheduled arrivals",
    offered, dropped, failed, successful, windowSuccessful, peakQueue, expiredBeforeStart, errors: Object.fromEntries(errors),
    measuredSeconds: durationMs / 1000, drainMs: round(Math.max(0, performance.now() - deadline)),
    filesPerSecond: round(windowSuccessful / (durationMs / 1000)),
    responseLatencyMs: response.summary(), serviceLatencyMs: service.summary(),
    queueWaitMs: wait.summary(), schedulerLagMs: schedulerLag.summary(), perSecond: seconds.filter(Boolean),
  };
}
