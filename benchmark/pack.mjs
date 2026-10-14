import { cp, mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { join, resolve } from "node:path";

const out = resolve(process.argv[2] ?? "benchmark/context");
await mkdir(out, { recursive: false });
const full = process.argv.includes("--full");
if (full) {
  const names = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "-z"], { encoding: "utf8" }).split("\0").filter(Boolean);
  for (const name of names) {
    if (/(?:^|\/)(?:\.env(?:\.[^/]*)?|\.aws|\.codex|\.git|node_modules)(?:\/|$)/.test(name)) continue;
    const target = join(out, name);
    await mkdir(resolve(target, ".."), { recursive: true });
    await cp(name, target);
  }
  for (const name of names.filter(n=>n.startsWith("scripts/baselines/") && n.endsWith(".json"))) {
    const data = (()=>{ try { return execFileSync("git", ["show", `HEAD^:${name}`], { stdio: ["ignore", "pipe", "ignore"] }); } catch { return null; } })();
    if (!data) continue;
    const target = join(out, ".benchmark-baseline-parent", name);
    await mkdir(resolve(target, ".."), { recursive: true });
    await writeFile(target, data);
  }
}
const entries = ["go.mod", "go.sum", "cmd", "internal", "pkg", "packages/stow-s3/package.json", "packages/stow-s3/package-lock.json",
  "packages/stow-s3/dist", "packages/stow-s3/scripts/performance"];
for (const entry of full ? [] : entries) {
  const target = join(out, entry);
  await mkdir(resolve(target, ".."), { recursive: true });
  await cp(entry, target, { recursive: true });
}
await mkdir(join(out, "benchmark"), { recursive: true });
const excluded = new Set(["node_modules", "context", "verify-context", "results", "plans", ".git", ".env"]);
for (const entry of await readdir("benchmark")) {
  if (excluded.has(entry) || out === resolve("benchmark", entry) || out.startsWith(resolve("benchmark", entry) + "/")) continue;
  await cp(join("benchmark", entry), join(out, "benchmark", entry), { recursive: true,
    filter: source=>!/(?:^|\/)(?:node_modules|\.git)(?:\/|$)/.test(source) && !source.endsWith(".env") });
}
const hash = createHash("sha256");
async function tree(path, prefix = "") {
  for (const entry of (await readdir(path, { withFileTypes: true })).sort((a,b)=>a.name.localeCompare(b.name))) {
    const relative = join(prefix, entry.name), absolute = join(path, entry.name);
    if (entry.isDirectory()) await tree(absolute, relative);
    else if (entry.isFile()) { hash.update(relative); hash.update(await readFile(absolute)); }
  }
}
if (process.argv.includes("--verify") || process.argv.includes("--site") || process.argv.includes("--size") || process.argv.includes("--embedded-size")) {
  if (!full) throw new Error("verification requires --full");
  await cp(process.argv.includes("--site")?"benchmark/site.Dockerfile":"benchmark/verify.Dockerfile", join(out, "benchmark/Dockerfile"));
}
if (process.argv.includes("--size")) {
  const dockerfile = join(out, "benchmark/Dockerfile");
  await writeFile(dockerfile, (await readFile(dockerfile, "utf8")).replace("BENCH_CLOUD_VERIFY=1", "BENCH_CLOUD_VERIFY=1 BENCH_SIZE_AB=1"));
}
if (process.argv.includes("--embedded-size")) {
  const dockerfile = join(out, "benchmark/Dockerfile");
  const diagnosis = process.argv.includes("--embedded-diagnose") ? " BENCH_EMBEDDED_DIAGNOSE=1" : "";
  await writeFile(dockerfile, (await readFile(dockerfile, "utf8")).replace("BENCH_CLOUD_VERIFY=1", "BENCH_CLOUD_VERIFY=1 BENCH_EMBEDDED_SIZE=1" + diagnosis));
}
if (process.argv.includes("--controlled")) await cp("benchmark/controlled.Dockerfile", join(out, "benchmark/Dockerfile"));
if (process.argv.includes("--diagnosis")) await cp("benchmark/diagnosis.Dockerfile", join(out, "benchmark/Dockerfile"));
if (process.argv.includes("--memory-staging-red")) await cp("benchmark/memory-staging-red.Dockerfile", join(out, "benchmark/Dockerfile"));
if (process.argv.includes("--owned-body-red")) await cp("benchmark/owned-body-red.Dockerfile", join(out, "benchmark/Dockerfile"));
if (process.argv.includes("--embedded-api-red")) await cp("benchmark/embedded-api-red.Dockerfile", join(out, "benchmark/Dockerfile"));
if (process.argv.includes("--read-performance")) {
  await cp("benchmark/read-performance.Dockerfile", join(out, "benchmark/Dockerfile"));
  for (const name of ["wasm-isolate-host.mjs", "wasm-agent-workload.mjs"]) {
    await mkdir(join(out, "scripts"), {recursive:true});
    await cp(join("scripts", name), join(out, "scripts", name));
  }
}
if (process.argv.includes("--disk-performance")) await cp("benchmark/disk-performance.Dockerfile", join(out, "benchmark/Dockerfile"));
if (process.argv.includes("--hot-paths")) {
  if (!full) throw new Error("hot-paths qualification requires --full");
  const dockerfile = join(out, "benchmark/Dockerfile");
  await cp("benchmark/verify.Dockerfile", dockerfile);
  await writeFile(dockerfile, (await readFile(dockerfile, "utf8")).replace("BENCH_CLOUD_VERIFY=1", "BENCH_CLOUD_VERIFY=1 BENCH_HOT_PATHS=1"));
}
if (process.argv.includes("--owned-body")) await cp("benchmark/owned-body.Dockerfile", join(out, "benchmark/Dockerfile"));
if (process.argv.includes("--wasm-size-probe")) {
  await cp("benchmark/wasm-size-probe.Dockerfile", join(out, "benchmark/Dockerfile"));
  if (process.argv.includes("--wasm-network-probe")) {
    const dockerfile = join(out, "benchmark/Dockerfile");
    await writeFile(dockerfile, (await readFile(dockerfile, "utf8")).replace("BENCH_CLOUD_VERIFY=1", "BENCH_CLOUD_VERIFY=1 BENCH_WASM_NETWORK_PROBE=1"));
  }
  await cp("wasm", join(out, "wasm"), { recursive: true });
  await mkdir(join(out, "scripts"), { recursive: true });
  for (const name of ["wasm-isolate-probe.mjs", "wasm-isolate-host.mjs"]) await cp(join("scripts", name), join(out, "scripts", name));
}
await tree(out);
const source = { commit: execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim(),
  dirtyFiles: execFileSync("git", ["status", "--porcelain"], { encoding: "utf8" }).trim().split("\n"),
  payloadSha256: hash.digest("hex"), note: "Explicit source snapshot; includes uncommitted benchmark files, excludes credentials and caches" };
await writeFile(join(out, "benchmark/source.json"), JSON.stringify(source, null, 2) + "\n");
console.log(JSON.stringify({ out, ...source }, null, 2));
