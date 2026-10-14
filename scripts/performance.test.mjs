import assert from "node:assert/strict";
import { test } from "node:test";
import { setTimeout as delay } from "node:timers/promises";
import { defaults, validateConfig, scenarios, options } from "../packages/stow-s3/scripts/performance/options.mjs";
import { Histogram, Measurements } from "../packages/stow-s3/scripts/performance/metrics.mjs";
import { runLoad } from "../packages/stow-s3/scripts/performance/load.mjs";
import { cpuSeconds } from "../packages/stow-s3/scripts/performance/resources.mjs";
import { summarizeRuns, chart } from "../packages/stow-s3/scripts/performance/report.mjs";

test("histograms preserve sample counts and bounded percentile error", () => {
  const histogram = new Histogram();
  for (let value = 1; value <= 10000; value++) histogram.add(value);
  const summary = histogram.summary();
  assert.equal(summary.samples, 10000);
  assert.equal(summary.mean, 5000.5);
  assert.ok(summary.p99 >= 9900 && summary.p99 <= 10000);
  assert.ok(histogram.bins.size < 1000);
  assert.equal(new Histogram().summary().p99, null);
  const single = new Histogram(); single.add(1234.56);
  assert.equal(single.summary().p99, single.summary().max);
});

test("throughput excludes drain and errors while preserving latency samples", () => {
  const measurements = new Measurements();
  measurements.record("put", 2, 1048576, null, true);
  measurements.record("put", 3, 1048576, null, false);
  measurements.record("put", 4, 1048576, { name: "Timeout", $metadata: { httpStatusCode: 503 } }, true);
  const result = measurements.summary(2);
  assert.equal(result.operations.put.payloadMiBPerSecond, 0.5);
  assert.equal(result.operations.put.requestsPerSecond, 0.5);
  assert.equal(result.operations.put.latencyMs.samples, 3);
  assert.equal(result.operations.put.failed, 1);
  assert.equal(result.errors.HTTP_503, 1);
});

test("scheduled arrivals bound queue and count rejected files under slow service", async () => {
  let active = 0, peak = 0;
  const result = await runLoad({ durationMs: 150, concurrency: 2, rate: 500, maxQueue: 3, timeoutMs: 1000,
    async task() { active++; peak = Math.max(peak, active); await delay(25); active--; } });
  assert.equal(result.offered, 75);
  assert.ok(result.dropped > 0);
  assert.equal(peak, 2);
  assert.equal(result.peakQueue, 3);
  assert.equal(result.offered, result.dropped + result.failed + result.successful);
  assert.ok(result.responseLatencyMs.p99 >= result.serviceLatencyMs.p99);
  assert.ok(result.queueWaitMs.max > 0);
  assert.ok(result.windowSuccessful <= result.successful);
});

test("synchronous task failures count without breaking scheduler or drain", async () => {
  const result = await runLoad({ durationMs: 40, concurrency: 1, rate: 100, maxQueue: 1, timeoutMs: 1000,
    task() { throw new Error("test failure"); } });
  assert.equal(result.offered, 4);
  assert.equal(result.failed + result.dropped, 4);
  assert.ok(result.failed > 0);
  assert.equal(result.successful, 0);
});

test("queued files expire against arrival deadlines without starting requests", async () => {
  let calls = 0;
  const result = await runLoad({ durationMs: 100, concurrency: 1, rate: 200, maxQueue: 32, timeoutMs: 20,
    async task() { calls++; await delay(80); } });
  assert.ok(result.expiredBeforeStart > 0);
  assert.equal(result.errors.QueueExpired, result.expiredBeforeStart);
  assert.equal(result.serviceLatencyMs.samples, calls);
  assert.equal(result.offered, result.successful + result.failed + result.dropped);
});

test("configuration refuses impossible fixture budgets and fractional limits", async () => {
  assert.throws(() => validateConfig({ ...defaults, maxBytes: 1 }), /fixtures/);
  assert.throws(() => validateConfig({ ...defaults, repetitions: 1.5 }), /repetitions/);
  assert.throws(() => validateConfig({ ...defaults, rates: [-1] }), /rates/);
  await assert.rejects(options(["--unknown"]), /unknown/);
  const selected = await options(["--smoke", "--out", "/tmp/stow-perf-test"]);
  assert.equal(selected.config.repetitions, 1);
  assert.equal(scenarios(selected.config).length, 12);
});

test("CPU time accepts macOS and Linux clocks", () => {
  assert.equal(cpuSeconds("0:01.25"), 1.25);
  assert.equal(cpuSeconds("01:02:03"), 3723);
  assert.equal(cpuSeconds("2-01:02:03"), 176523);
});

test("report distinguishes workflow files from individual API requests", () => {
  const scenario = { backend: "memory", workload: "quarantine", bytes: 1048576, concurrency: 4, rate: 100, soak: false };
  const result = { scenario, load: { filesPerSecond: 100, responseLatencyMs: { p99: 25 }, serviceLatencyMs: { p99: 20 }, failed: 0, dropped: 0, successful: 10000 },
    measurements: { operations: { put: { requestsPerSecond: 100 }, get: { requestsPerSecond: 100 }, verdict: { requestsPerSecond: 100 }, delete: { requestsPerSecond: 200 } } },
    resources: { serverPeakSampledRssBytes: 104857600 }, verification: { complete: true }, leftovers: { count: 0 } };
  const [row] = summarizeRuns([result, result]);
  assert.equal(row.filesPerSecond, 100);
  assert.equal(row.payloadMiBPerSecond, 100);
  assert.equal(row.successfulRequestsPerSecond, 500);
  assert.equal(row.repetitions, 2);
  assert.match(chart([row]), /100 MiB\/s/);
});
