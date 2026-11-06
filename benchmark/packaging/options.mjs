import { resolve } from "node:path";

const modes = {
  "--verify": { image: "verify", full: true },
  "--site": { image: "site", full: true },
  "--size": { image: "verify", full: true, environment: "BENCH_SIZE_AB=1" },
  "--embedded-size": { image: "verify", full: true, environment: "BENCH_EMBEDDED_SIZE=1" },
  "--hot-paths": { image: "verify", full: true, environment: "BENCH_HOT_PATHS=1" },
  "--controlled": { image: "controlled" },
  "--diagnosis": { image: "diagnosis" },
  "--memory-staging-red": { image: "memory-staging-red" },
  "--owned-body-red": { image: "owned-body-red" },
  "--embedded-api-red": { image: "embedded-api-red" },
  "--read-performance": { image: "read-performance", extra: ["scripts/wasm-isolate-host.mjs", "scripts/wasm-agent-workload.mjs"] },
  "--disk-performance": { image: "disk-performance" },
  "--owned-body": { image: "owned-body" },
  "--wasm-size-probe": { image: "wasm-size-probe", extra: ["wasm", "scripts/wasm-isolate-probe.mjs", "scripts/wasm-isolate-host.mjs"] },
};
const modifiers = {
  "--embedded-diagnose": { requires: "--embedded-size", environment: "BENCH_EMBEDDED_DIAGNOSE=1" },
  "--wasm-network-probe": { requires: "--wasm-size-probe", environment: "BENCH_WASM_NETWORK_PROBE=1" },
};

export function packagingOptions(args) {
  const flags = new Set(args.filter(arg => arg.startsWith("--")));
  const paths = args.filter(arg => !arg.startsWith("--"));
  if (paths.length > 1) throw new Error("Supply one output directory");
  for (const flag of flags) {
    if (flag !== "--full" && !modes[flag] && !modifiers[flag]) throw new Error(`Unknown packaging option: ${flag}`);
  }
  const selected = Object.keys(modes).filter(flag => flags.has(flag));
  if (selected.length > 1) throw new Error(`Choose one image mode: ${selected.join(", ")}`);
  const mode = modes[selected[0]] ?? {};
  const full = flags.has("--full");
  if (mode.full && !full) throw new Error(`${selected[0]} requires --full`);
  const environment = mode.environment ? [mode.environment] : [];
  for (const [flag, modifier] of Object.entries(modifiers)) {
    if (!flags.has(flag)) continue;
    if (!flags.has(modifier.requires)) throw new Error(`${flag} requires ${modifier.requires}`);
    environment.push(modifier.environment);
  }
  return { out: resolve(paths[0] ?? "benchmark/context"), full, environment,
    dockerfile: mode.image ? `benchmark/docker/${mode.image}.Dockerfile` : "benchmark/Dockerfile",
    extraEntries: mode.extra ?? [] };
}
