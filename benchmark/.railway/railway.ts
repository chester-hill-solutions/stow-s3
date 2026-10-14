import { defineRailway, project, service, volume } from "railway/iac";

export const partial = "stow-benchmark";

export default defineRailway((ctx) => {
  if (!ctx.isEnvironment("benchmark")) throw new Error("Use the benchmark environment");
  const region = "us-east4-eqdc4a";
  const target = service("benchmark-target", {
    build: { builder: "DOCKERFILE", dockerfilePath: "benchmark/Dockerfile" },
    start: "node benchmark/target.mjs",
    healthcheck: "/health",
    replicas: { [region]: 1 },
    deploy: {
      restartPolicyType: "NEVER",
      limitOverride: { containers: { cpu: 2, memoryBytes: 2 * 1024 ** 3 } },
    },
    env: {
      PORT: "8081",
      BENCH_DATA_ROOT: "/data",
      BENCH_CONTROL_TOKEN: ctx.shared.BENCH_CONTROL_TOKEN,
    },
    volumeMounts: { "/data": volume("benchmark-data", { sizeMB: 5120, region }) },
  });
  const runner = service("benchmark-runner", {
    build: { builder: "DOCKERFILE", dockerfilePath: "benchmark/Dockerfile" },
    start: "node benchmark/runner.mjs",
    healthcheck: "/health",
    replicas: { [region]: 1 },
    deploy: {
      restartPolicyType: "NEVER",
      limitOverride: { containers: { cpu: 4, memoryBytes: 4 * 1024 ** 3 } },
    },
    env: {
      PORT: "8082",
      BENCH_RESULTS_ROOT: "/results",
      BENCH_TARGET_HOST: target.env.RAILWAY_PRIVATE_DOMAIN,
      BENCH_CONTROL_TOKEN: target.env.BENCH_CONTROL_TOKEN,
    },
    volumeMounts: { "/results": volume("benchmark-results", { sizeMB: 5120, region }) },
  });
  return project(ctx.projectName ?? "stow-conformance-scratch", { resources: [target, runner] });
});
