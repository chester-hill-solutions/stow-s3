import { cp, mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { execFileSync, spawnSync } from "node:child_process";
import { join } from "node:path";
import assert from "node:assert/strict";

if (process.cwd() !== "/src" || process.env.BENCH_CLOUD_AB !== "1") throw new Error("variant builds require the Railway build image");
const digest = data => createHash("sha256").update(data).digest("hex");
const specification = JSON.parse(await readFile("benchmark/variants/write-preparation.json", "utf8"));
assert.equal(digest(await readFile("benchmark/variants/before-write-preparation.patch")), specification.beforePatchSha256);
for (const file of specification.files) assert.equal(digest(await readFile(file.path)), file.afterSha256, `current source drift: ${file.path}`);
const before = "/tmp/stow-before";
await mkdir(before);
for (const entry of ["go.mod", "go.sum", "cmd", "internal", "pkg", "benchmark/go"]) {
  const target = join(before, entry);
  await mkdir(join(target, ".."), {recursive:true});
  await cp(entry, target, {recursive:true});
}
const patch = "/src/benchmark/variants/before-write-preparation.patch";
for (const args of [["apply", "--check", patch], ["apply", patch]]) execFileSync("git", args, {cwd:before, stdio:"inherit"});
for (const file of specification.files) {
  const contents = await readFile(join(before, file.path)).catch(error => { if (error.code === "ENOENT") return null; throw error; });
  assert.equal(contents === null ? null : digest(contents), file.beforeSha256, `baseline reconstruction drift: ${file.path}`);
}
const red = spawnSync("go", ["test", "./internal/storage/fs", "-run", "^TestComputedChecksumSaveReplay$", "-v"], {cwd:before, encoding:"utf8"});
const redLog = red.stdout + red.stderr;
assert.equal(red.status, 1);
for (const name of ["put", "receipt"]) assert.ok(redLog.includes(`--- FAIL: TestComputedChecksumSaveReplay/${name}`));
await mkdir("/out", {recursive:true});
await writeFile("/out/baseline-regression.txt", redLog);

async function productionHash(root) {
  const hash = createHash("sha256");
  async function tree(relative) {
    const path = join(root, relative);
    for (const entry of (await readdir(path, {withFileTypes:true})).sort((a,b)=>a.name.localeCompare(b.name))) {
      const name = join(relative, entry.name);
      if (entry.isDirectory()) await tree(name);
      else if (entry.isFile() && entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) hash.update(name).update(await readFile(join(root, name)));
    }
  }
  for (const name of ["go.mod", "go.sum"]) hash.update(name).update(await readFile(join(root,name)));
  for (const name of ["cmd", "internal", "pkg"]) await tree(name);
  return hash.digest("hex");
}

const variants = {};
for (const [variant, root, suffix] of [["after", "/src", ""], ["before", before, "-before"]]) {
  const run = (args, cwd = root) => execFileSync("go", args, {cwd, stdio:"inherit", env:{...process.env, CGO_ENABLED:"1"}});
  const args = ["test", "-race", "./internal/storage/...", "./internal/runtime/...", "./pkg/stow/..."];
  if (variant === "before") args.push("-skip", "^(TestComputedChecksumSaveReplay|TestStorePreparesEmptyBody)$");
  run(args);
  const server = `/out/stow-s3${suffix}`;
  execFileSync("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", server, "./cmd/stow-s3"], {cwd:root, stdio:"inherit", env:{...process.env,CGO_ENABLED:"0"}});
  const module = join(root, "benchmark/go");
  run(["mod", "download"], module);
  run(["test", "-race", "./..."], module);
  run(["test", "-run", "^$", "-bench", "BenchmarkComparison/stow-(memory|filesystem)/.*/1048576B$", "-benchtime", "1x", "-cpu", "4", "."], module);
  run(["test", "-run", "^$", "-bench", "^BenchmarkManagedSave$", "-benchtime", "1x", "-cpu", "4", "."], module);
  run(["test", "-c", "-trimpath", "-o", `/out/embedded${suffix}.test`, "."], module);
  variants[variant] = {productionSourceSha256:await productionHash(root), serverSha256:digest(await readFile(server)), embeddedSha256:digest(await readFile(`/out/embedded${suffix}.test`)), raceTestsPassed:true,
    ...(variant === "before" ? {excludedTests:["TestComputedChecksumSaveReplay", "TestStorePreparesEmptyBody"], expectedRetryRegressionsReproduced:true} : {})};
}
await cp("benchmark/go/go.sum", "/out/benchmark-go.sum");
await writeFile("/out/build-variants.json", JSON.stringify({specification, variants, go:execFileSync("go",["version"],{encoding:"utf8"}).trim(), source:JSON.parse(await readFile("benchmark/source.json","utf8"))},null,2)+"\n");
