import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

export const defaults = {
  version: 1, label: "local exploratory", warmupSeconds: 1, durationSeconds: 5,
  repetitions: 3, timeoutMs: 10000, sampleMs: 250, maxQueue: 8,
  maxBytes: 268435456, maxObjects: 4096, serverConcurrency: 128,
  scannerDelayMs: 2, retainedObjects: 16, soakSeconds: 30,
  minFreeDiskBytes: 1073741824,
  backends: ["memory", "filesystem"], workloads: ["put", "get", "quarantine"],
  sizes: [1048576], concurrency: [4], rates: [0, 100],
};

export function validateConfig(config) {
  if (config.version !== 1) throw new Error("unsupported performance config version");
  const bounds = {
    warmupSeconds: [0, 3600], durationSeconds: [0.1, 86400], repetitions: [1, 100],
    timeoutMs: [1, 300000], sampleMs: [100, 60000], maxQueue: [0, 100000],
    maxBytes: [1, Number.MAX_SAFE_INTEGER], maxObjects: [1, 10000000],
    serverConcurrency: [1, 10000], scannerDelayMs: [0, 60000],
    retainedObjects: [1, 1000000], soakSeconds: [0, 86400],
    minFreeDiskBytes: [0, Number.MAX_SAFE_INTEGER],
  };
  for (const [key, [min, max]] of Object.entries(bounds)) {
    const value = config[key];
    if (!Number.isFinite(value) || value < min || value > max) throw new Error(`invalid ${key}`);
    if (!key.endsWith("Seconds") && !Number.isSafeInteger(value)) throw new Error(`invalid ${key}`);
  }
  for (const [key, allowed] of [["backends", ["memory", "filesystem"]], ["workloads", ["put", "get", "quarantine"]]]) {
    if (!Array.isArray(config[key]) || !config[key].length || config[key].some((v) => !allowed.includes(v))) throw new Error(`invalid ${key}`);
  }
  for (const [key, min, max] of [["sizes", 16, 1073741824], ["concurrency", 1, 1024], ["rates", 0, 100000]]) {
    if (!Array.isArray(config[key]) || !config[key].length || config[key].some((v) => !Number.isSafeInteger(v) || v < min || v > max)) throw new Error(`invalid ${key}`);
  }
  for (const bytes of config.sizes) for (const concurrency of config.concurrency) {
    if (bytes * (config.retainedObjects + concurrency * 2) > config.maxBytes) throw new Error("maxBytes cannot hold fixtures and concurrent transfers");
    if (config.retainedObjects + concurrency * 2 > config.maxObjects) throw new Error("maxObjects cannot hold fixtures and concurrent transfers");
  }
  if (typeof config.label !== "string" || !config.label.length) throw new Error("label is required");
  return config;
}

export async function options(args) {
  const flags = new Map();
  for (let i = 0; i < args.length; i++) {
    const key = args[i];
    if (key === "--smoke") { flags.set(key, true); continue; }
    if (!["--config", "--out", "--binary", "--data-root"].includes(key) || !args[i + 1]) throw new Error(`unknown or incomplete option: ${key}`);
    flags.set(key, args[++i]);
  }
  const input = flags.has("--config") ? JSON.parse(await readFile(flags.get("--config"), "utf8")) : {};
  const unknown = Object.keys(input).filter((key) => !(key in defaults));
  if (unknown.length) throw new Error(`unknown config keys: ${unknown.join(", ")}`);
  const config = { ...defaults, ...input };
  if (flags.has("--smoke")) Object.assign(config, { label: "smoke; not a capacity measurement", durationSeconds: 0.3, warmupSeconds: 0, repetitions: 1, sizes: [4096], concurrency: [2], rates: [0, 100], soakSeconds: 0 });
  return {
    config: validateConfig(config),
    out: resolve(flags.get("--out") ?? ".cache/performance/latest"),
    binary: resolve(flags.get("--binary") ?? "bin/stow-s3"),
    dataRoot: flags.has("--data-root") ? resolve(flags.get("--data-root")) : undefined,
  };
}

export function scenarios(config) {
  const result = [];
  for (const backend of config.backends) for (const workload of config.workloads) {
    for (const bytes of config.sizes) for (const concurrency of config.concurrency) for (const rate of config.rates) {
      result.push({ backend, workload, bytes, concurrency, rate, soak: false });
    }
  }
  if (config.soakSeconds > 0) for (const backend of config.backends) {
    result.push({ backend, workload: "quarantine", bytes: Math.max(...config.sizes), concurrency: Math.max(...config.concurrency), rate: Math.max(...config.rates), soak: true });
  }
  return result;
}
