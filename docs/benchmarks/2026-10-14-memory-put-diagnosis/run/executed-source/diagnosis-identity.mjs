import { readFile, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
const hash = async path => createHash("sha256").update(await readFile(path)).digest("hex");
const binaries = {};
for (const name of ["stow-s3", "stow-s3-profile", "pprof"]) binaries[name] = await hash(`bin/${name}`);
if (binaries["stow-s3"] !== "7c80ec1b79aa0d457e2c4f7e7eb680ae2a8f13bf060fef2961a86d170dca70a6") throw new Error("server differs from retained controlled baseline");
await writeFile("benchmark/build-identity.json", JSON.stringify({ binaries, source: JSON.parse(await readFile("benchmark/source.json", "utf8")),
  wasmSha256: await hash("packages/stow-s3/dist/stow-runtime.wasm"), sdkLockfileSha256: await hash("packages/stow-s3/package-lock.json"),
  profileOverlaySha256: await hash("benchmark/go/profiling_benchmark.go.txt") }, null, 2) + "\n");
