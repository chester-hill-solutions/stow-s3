export class Histogram {
  bins = new Map();
  count = 0;
  sum = 0;
  max = 0;

  add(value) {
    const bounded = Math.max(0, value);
    const bin = bounded === 0 ? 0 : Math.ceil(Math.log1p(bounded * 1000) / Math.log(1.01));
    this.bins.set(bin, (this.bins.get(bin) ?? 0) + 1);
    this.count++;
    this.sum += bounded;
    this.max = Math.max(this.max, bounded);
  }

  percentile(fraction) {
    if (!this.count) return null;
    const target = Math.ceil(this.count * fraction);
    let count = 0;
    for (const [bin, n] of [...this.bins].sort((a, b) => a[0] - b[0])) {
      count += n;
      if (count >= target) return round(Math.min(this.max, Math.expm1(bin * Math.log(1.01)) / 1000));
    }
    return this.max;
  }

  summary() {
    return { samples: this.count, mean: this.count ? round(this.sum / this.count) : null,
      p50: this.percentile(0.5), p95: this.percentile(0.95), p99: this.percentile(0.99), max: this.count ? round(this.max) : null };
  }
}

export function round(value) { return value === null || !Number.isFinite(value) ? null : Math.round(value * 1000) / 1000; }

export class Measurements {
  operations = new Map();
  errors = new Map();

  record(operation, durationMs, bytes, error, inWindow) {
    let metric = this.operations.get(operation);
    if (!metric) {
      metric = { latency: new Histogram(), successful: 0, failed: 0, successfulBytes: 0, windowSuccessful: 0, windowFailed: 0, windowBytes: 0 };
      this.operations.set(operation, metric);
    }
    metric.latency.add(durationMs);
    if (error) {
      metric.failed++;
      if (inWindow) metric.windowFailed++;
      const code = error.$metadata?.httpStatusCode ? `HTTP_${error.$metadata.httpStatusCode}` : error.name ?? "Error";
      this.errors.set(code, (this.errors.get(code) ?? 0) + 1);
    } else {
      metric.successful++;
      metric.successfulBytes += bytes;
      if (inWindow) { metric.windowSuccessful++; metric.windowBytes += bytes; }
    }
  }

  summary(seconds) {
    return {
      operations: Object.fromEntries([...this.operations].map(([key, m]) => [key, {
        latencyMs: m.latency.summary(), successful: m.successful, failed: m.failed,
        windowSuccessful: m.windowSuccessful, windowFailed: m.windowFailed, windowBytes: m.windowBytes, successfulBytes: m.successfulBytes,
        requestsPerSecond: round(m.windowSuccessful / seconds), payloadMiBPerSecond: round(m.windowBytes / 1048576 / seconds),
        completedAttemptsPerSecond: round((m.windowSuccessful + m.windowFailed) / seconds),
      }])),
      errors: Object.fromEntries(this.errors),
    };
  }
}
