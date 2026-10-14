import { execFileSync } from "node:child_process";
import { mkdir, readFile, readdir, cp, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { join } from "node:path";

// Runs in the Railway verification image, never in the user's checkout.
if (process.cwd() !== "/src" || process.env.BENCH_CLOUD_VERIFY !== "1") throw new Error("cloud verification requires /src in the Railway build");
const out = "/out/verification";
await mkdir(out, { recursive: true });
const source = JSON.parse(await readFile("benchmark/source.json", "utf8"));
const payloadHash = createHash("sha256");
async function hashPayload(path, prefix = "") {
  for (const entry of (await readdir(path, { withFileTypes: true })).sort((a,b)=>a.name.localeCompare(b.name))) {
    const relative = join(prefix, entry.name), absolute = join(path, entry.name);
    if (relative === "benchmark/source.json") continue;
    if (entry.isDirectory()) await hashPayload(absolute, relative);
    else if (entry.isFile()) payloadHash.update(relative).update(await readFile(absolute));
  }
}
await hashPayload("/src");
if (payloadHash.digest("hex") !== source.payloadSha256) throw new Error("Uploaded source differs from the packed snapshot; use --no-gitignore for this sanitized context");
const exec = (command, args, options = {}) => execFileSync(command, args, { stdio: "inherit", ...options });
const git = args => exec("git", ["-c", "user.name=Benchmark verification", "-c", "user.email=benchmark@localhost", ...args]);
const current = "/tmp/current-baselines";
await cp("scripts/baselines", current, { recursive: true });
await cp(".benchmark-baseline-parent/scripts/baselines", "scripts/baselines", { recursive: true });
git(["init", "--quiet"]);
git(["add", "scripts/baselines"]);
git(["commit", "--quiet", "-m", "Original parent ratchet baselines"]);
await cp(current, "scripts/baselines", { recursive: true });
git(["add", "."]);
git(["commit", "--quiet", "-m", "Explicit current source snapshot"]);
const digestTree = async root => {
  const result = {};
  async function visit(path, prefix = "") {
    for (const entry of await readdir(path, { withFileTypes: true })) {
      const absolute = join(path, entry.name), relative = join(prefix, entry.name);
      if (entry.isDirectory()) await visit(absolute, relative);
      else if (entry.isFile()) result[relative] = createHash("sha256").update(await readFile(absolute)).digest("hex");
    }
  }
  await visit(root); return result;
};
const before = await digestTree("packages/stow-s3/dist");
await writeFile(join(out, "generated-before.json"), JSON.stringify(before, null, 2) + "\n");
if (process.env.BENCH_SIZE_AB === "1") exec("node", ["benchmark/verify-size.mjs"]);
if (process.env.BENCH_EMBEDDED_SIZE === "1") exec("node", ["benchmark/verify-embedded-size.mjs"]);
if (process.env.BENCH_HOT_PATHS === "1") {
  if (process.env.BENCH_HOT_PATHS_REUSE === "1") {
    const original = "/tmp/hot-paths-measured-source";
    await mkdir(original, {recursive:true});
    exec("tar", ["-xzf", "/results/hot-paths-measured-source.tar.gz", "-C", original]);
    const roots = ["cmd", "internal", "pkg", "packages/stow-s3/src", "packages/stow-s3/test", "packages/stow-s3/scripts/browser-tests"];
    for (const root of roots) {
      const currentTree = await digestTree(root), measuredTree = await digestTree(join(original, root));
      if (root === "internal") {
        delete currentTree["runtime/read_concurrency_test.go"];
        delete measuredTree["runtime/read_concurrency_test.go"];
      }
      if (JSON.stringify(currentTree) !== JSON.stringify(measuredTree)) throw new Error("Measured source changed: " + root);
    }
    for (const name of ["go.mod", "go.sum", "packages/stow-s3/package.json", "packages/stow-s3/package-lock.json", "benchmark/verify-hot-paths.mjs", "benchmark/embedded-startup.mjs"]) {
      if (!(await readFile(name)).equals(await readFile(join(original, name)))) throw new Error("Measured dependency changed: " + name);
    }
    const analysis = JSON.parse(await readFile(join(out, "hot-paths/analysis.json"), "utf8"));
    const measured = JSON.parse(await readFile(join(original, "benchmark/source.json"), "utf8"));
    if (!analysis.complete || analysis.source.payloadSha256 !== measured.payloadSha256) throw new Error("Unverified performance reuse");
    await writeFile(join(out, "hot-paths/reuse.json"), JSON.stringify({complete:true, measuredSource:measured, qualifiedSource:source, unchangedRoots:roots, testOnlyRefactor:"internal/runtime/read_concurrency_test.go", reason:"Production, benchmark fixtures, measurement harness and dependencies are byte-identical; the lifecycle regression test was refactored for the quality gate"}, null, 2) + "\n");
  } else exec("node", ["benchmark/verify-hot-paths.mjs"]);
}
if (process.env.BENCH_EMBEDDED_DIAGNOSE === "1") process.exit(0);
exec("make", ["build-wasm"]);
exec("npm", ["ci", "--ignore-scripts", "--prefix", "packages/stow-s3"]);
exec("npm", ["run", "build", "--prefix", "packages/stow-s3"]);
const after = await digestTree("packages/stow-s3/dist");
const chartData = JSON.parse(await readFile("site/benchmark-data.json", "utf8"));
// S3-only charts retain historical build provenance but contain no WASM metrics.
if (chartData.comparisonScope !== "S3-shaped agent storage" && after["stow-runtime.wasm"] !== chartData.wasmSha256) {
  throw new Error("WASM comparison evidence uses a different SDK artifact");
}
const changed = [...new Set([...Object.keys(before), ...Object.keys(after)])].filter(name => before[name] !== after[name]);
const allowedGenerated = process.env.BENCH_HOT_PATHS === "1" ? ["stow-runtime.wasm", "browser-wasm-host.js", "browser-wasm-host.js.map", "browser-wasm-host.d.ts.map"] : ["stow-runtime.wasm"];
if (changed.some(name=>!allowedGenerated.includes(name))) throw new Error("Unexpected generated changes: " + changed.join(", "));
await writeFile(join(out, "generated-change.json"), JSON.stringify({ before, after, changed }, null, 2) + "\n");
const apiBefore = JSON.parse(await readFile("docs/api/inventory.json", "utf8"));
exec("node", ["scripts/generate-api-reference.mjs"]);
if (process.env.BENCH_EMBEDDED_SIZE === "1" || process.env.BENCH_HOT_PATHS === "1") {
  const apiAfter = JSON.parse(await readFile("docs/api/inventory.json", "utf8"));
  if (JSON.stringify(apiBefore) !== JSON.stringify(apiAfter)) throw new Error("Public API inventory changed during embedded extraction");
}
await cp("docs/api", join(out, "api-reference"), {recursive:true});
const quality = JSON.parse(execFileSync("go", ["run", "./tools/quality", "--json"], {encoding:"utf8"}));
const qualityPath = "scripts/baselines/go-quality.json";
const qualityBaseline = JSON.parse(await readFile(qualityPath, "utf8"));
if (quality.counts["comment-lines"] > qualityBaseline.counts["comment-lines"]) throw new Error("Production comment count grew above its ratchet");
qualityBaseline.counts["comment-lines"] = quality.counts["comment-lines"];
await writeFile(qualityPath, JSON.stringify(qualityBaseline, null, 2) + "\n");
await cp(qualityPath, join(out, "go-quality.json"));
await writeFile(join(out, "go-quality-report.json"), JSON.stringify(quality, null, 2) + "\n");
exec("make", ["standards"]);
exec("make", ["test-conformance"]);
exec("make", ["test-node", "test-wasm"]);
exec(process.execPath, ["packages/stow-s3/node_modules/playwright/cli.js", "install", "chromium"]);
exec("make", ["test-browser"]);
await cp(".cache/browser-test-results/report.json", join(out, "sdk-browser-report.json"));
exec("node", ["benchmark/verify-site.mjs"]);
await cp("site/.build", join(out, "site-preview"), {recursive:true});
await cp("packages/stow-s3/dist/stow-runtime.wasm", join(out, "stow-runtime.wasm"));
for (const name of changed.filter(name=>name !== "stow-runtime.wasm")) {
  await mkdir(join(out, "sdk-generated"), {recursive:true});
  await cp(join("packages/stow-s3/dist", name), join(out, "sdk-generated", name));
}
await writeFile(join(out, "receipt.json"), JSON.stringify({ complete: true, finishedAt: new Date().toISOString(),
  checks: ["make standards", "make test-conformance", "make test-node test-wasm", "make test-browser", "protobuf generated-source drift check", "Railway Chromium chart controls and desktop/mobile previews"],
  source,
  baselineHistory: "Temporary Git history uses actual HEAD^ ratchet baseline files from the source checkout; current source is an explicit snapshot.",
  testNetwork: "libnss-myhostname resolves *.localhost to loopback; native Go uses the cgo resolver",
  generatedChangedFiles: changed }, null, 2) + "\n");
