#!/usr/bin/env node
import { execFile, spawn } from "node:child_process";
import { promisify } from "node:util";
import { createHash } from "node:crypto";
import { createWriteStream } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { cpus, totalmem, platform, arch, release, tmpdir } from "node:os";
import { join, resolve } from "node:path";

const execute = promisify(execFile);
const flags = new Map();
for (let index = 2; index < process.argv.length; index += 2) {
  const key = process.argv[index], value = process.argv[index + 1];
  if (!["--out", "--count", "--benchtime", "--cpu"].includes(key) || !value) throw new Error(`unknown or incomplete option: ${key}`);
  flags.set(key, value);
}
const out = resolve(flags.get("--out") ?? `.cache/performance/embedded-${Date.now()}`);
const count = Number(flags.get("--count") ?? 10);
const benchtime = flags.get("--benchtime") ?? "2s";
const cpu = flags.get("--cpu") ?? "1,4";
if (!Number.isSafeInteger(count) || count < 1 || count > 100) throw new Error("invalid count");
if (!/^\d+(?:\.\d+)?(?:ms|s)$/.test(benchtime) || Number.parseFloat(benchtime) <= 0) throw new Error("invalid benchtime");
if (!/^[1-9]\d*(?:,[1-9]\d*)*$/.test(cpu)) throw new Error("invalid CPU counts");
const pinned = (await readFile(".go-version", "utf8")).trim();
const env = { ...process.env, GOTOOLCHAIN: `go${pinned}` };
const args = ["test", "./pkg/stow", "-run", "^$", "-bench", "^BenchmarkStorage$", "-benchmem", `-benchtime=${benchtime}`, `-count=${count}`, `-cpu=${cpu}`];
const [revision, status, compiler] = await Promise.all([
  execute("git", ["rev-parse", "HEAD"]), execute("git", ["status", "--porcelain"]), execute("go", ["version"], { env }),
]);
const metadata = { version: 1, startedAt: new Date().toISOString(), reportingTimezone: "America/Toronto",
  cpu: cpus()[0]?.model, logicalCpus: cpus().length, memoryBytes: totalmem(), platform: platform(), arch: arch(), osRelease: release(),
  commit: revision.stdout.trim(), dirty: Boolean(status.stdout.trim()), dirtyFiles: status.stdout.trim().split("\n").filter(Boolean),
  compiler: compiler.stdout.trim(), compilationProfile: "Go test binary; no release linker flags", args,
  benchmarkSourceSha256: createHash("sha256").update(await readFile("pkg/stow/performance_benchmark_test.go")).digest("hex"),
  quotaProfile: { maxBytes: 268435456, maxObjects: 4096 },
  gc: { GOGC: process.env.GOGC ?? "Go default", GOMEMLIMIT: process.env.GOMEMLIMIT ?? "Go default" },
  tempRoot: process.env.TMPDIR ?? tmpdir(), complete: false };
await mkdir(out, { recursive: true });
await writeFile(join(out, "embedded-metadata.json"), JSON.stringify(metadata, null, 2) + "\n", { flag: "wx" });
const output = createWriteStream(join(out, "embedded.txt"), { flags: "wx" });
await new Promise((resolveRun, reject) => {
  const child = spawn("go", args, { env, stdio: ["ignore", "pipe", "inherit"] });
  child.stdout.pipe(output);
  child.once("error", reject);
  output.once("error", (error) => { child.kill(); reject(error); });
  child.once("close", (code) => {
    if (code !== 0) { reject(new Error(`embedded benchmark exited ${code}`)); return; }
    if (output.writableFinished) resolveRun();
    else output.once("finish", resolveRun);
  });
});
metadata.complete = true;
metadata.finishedAt = new Date().toISOString();
await writeFile(join(out, "embedded-metadata.json"), JSON.stringify(metadata, null, 2) + "\n");
console.log(`Saved embedded measurements to ${out}`);
