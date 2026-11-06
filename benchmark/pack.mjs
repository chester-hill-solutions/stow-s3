import { cp, mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { dirname, join, resolve, sep } from "node:path";
import { packagingOptions } from "./packaging/options.mjs";
import { payloadDigest, sourceCopier, workingFiles } from "./packaging/source.mjs";

// Validate the complete request before creating or copying any output.
const { out, full, dockerfile, environment, extraEntries } = packagingOptions(process.argv.slice(2));
await mkdir(out, { recursive: false });
const copySource = sourceCopier(out);
if (full) {
  const names = workingFiles();
  for (const name of names) await copySource(name);
  for (const name of names.filter(path => path.startsWith("scripts/baselines/") && path.endsWith(".json"))) {
    let data;
    try { data = execFileSync("git", ["show", `HEAD^:${name}`], { stdio: ["ignore", "pipe", "ignore"] }); }
    catch { continue; }
    const target = join(out, ".benchmark-baseline-parent", name);
    await mkdir(dirname(target), { recursive: true });
    await writeFile(target, data);
  }
} else {
  for (const entry of ["go.mod", "go.sum", "cmd", "internal", "pkg",
    "packages/stow-s3/package.json", "packages/stow-s3/package-lock.json",
    "packages/stow-s3/dist", "packages/stow-s3/scripts/performance"]) await copySource(entry);
}
for (const entry of await readdir("benchmark")) {
  const source = resolve("benchmark", entry);
  if (source === out || out.startsWith(source + sep)) continue;
  await copySource(join("benchmark", entry));
}
for (const entry of extraEntries) await copySource(entry);
await cp(dockerfile, join(out, "benchmark/Dockerfile"));
if (environment.length) {
  const path = join(out, "benchmark/Dockerfile");
  const image = await readFile(path, "utf8");
  if (!image.includes("BENCH_CLOUD_VERIFY=1")) throw new Error(`${dockerfile} is missing its verification environment`);
  await writeFile(path, image.replace("BENCH_CLOUD_VERIFY=1", `BENCH_CLOUD_VERIFY=1 ${environment.join(" ")}`));
}
const source = {
  commit: execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim(),
  dirtyFiles: execFileSync("git", ["status", "--porcelain"], { encoding: "utf8" }).trim().split("\n").filter(Boolean),
  payloadSha256: await payloadDigest(out),
  note: "Explicit source snapshot; includes uncommitted benchmark files, excludes credentials and caches",
};
await writeFile(join(out, "benchmark/source.json"), JSON.stringify(source, null, 2) + "\n");
console.log(JSON.stringify({ out, ...source }, null, 2));
