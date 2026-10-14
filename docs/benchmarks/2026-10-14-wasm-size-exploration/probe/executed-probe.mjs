import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { readFile, writeFile, mkdir, copyFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { gzipSync, brotliCompressSync, constants } from "node:zlib";

assert.equal(process.cwd(), "/src");
assert.equal(process.env.BENCH_CLOUD_VERIFY, "1");
const out = "/out/verification/wasm-size-probe";
await mkdir(out, { recursive: true });
await mkdir("bin", { recursive: true });
const facadePath = "pkg/stow/workspace_facade.go";
const utilityPath = "internal/storage/util.go";
const facade = await readFile(facadePath, "utf8");
const utility = await readFile(utilityPath, "utf8");
const env = { ...process.env, GOOS: "js", GOARCH: "wasm" };
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
const sdkBefore = await readFile("packages/stow-s3/dist/stow-runtime.wasm");
const typeBlock = (start, end) => facade.slice(facade.indexOf(start), facade.indexOf(end));
// Attribution experiment only. A shipping implementation must preserve native facade behavior.
const isolatedFacade = `package stow
import "context"
${typeBlock("type WorkspaceFacade struct", "type workspaceServer struct")}
${typeBlock("type WorkspaceFacadeOptions struct", "func (w *Workspace) Facade()")}
type workspaceServer struct{}
func (w *Workspace) Facade() (*WorkspaceFacade, error) { return w.FacadeWithOptions(WorkspaceFacadeOptions{}) }
func (w *Workspace) FacadeWithOptions(WorkspaceFacadeOptions) (*WorkspaceFacade, error) { return nil, ErrUnsupportedBackend }
func (w *Workspace) closeRuntimeLocked() error { if w.closed { return nil }; w.closing = true; return w.Runtime.Close() }
func (w *Workspace) closeLocked() error { if err := w.closeRuntimeLocked(); err != nil { return err }; if err := w.session.Release(); err != nil { return err }; w.closed = true; return nil }
func (w *Workspace) RefreshUsage(ctx context.Context) error { return w.Runtime.inner.RefreshUsage(ctx) }
`;
await writeFile(`${out}/experimental-facade.go.txt`, isolatedFacade);
const netipUtility = utility.replace('"net"', '"net/netip"').replace("if net.ParseIP(name) != nil {", "if _, err := netip.ParseAddr(name); err == nil {");
assert.notEqual(netipUtility, utility);
const sections = bytes => {
  const values = [];
  let position = 8;
  const uleb = () => { let value = 0, shift = 0, byte; do { byte = bytes[position++]; value += (byte & 127) * 2 ** shift; shift += 7; } while (byte & 128); return value; };
  while (position < bytes.length) {
    const id = bytes[position++], length = uleb(), start = position;
    let name;
    if (id === 0) { const count = uleb(); name = bytes.subarray(position, position + count).toString(); }
    values.push({ id, name, bytes: length });
    position = start + length;
  }
  assert.equal(position, bytes.length);
  return values;
};
const results = [];
const build = name => execFileSync("go", ["build", "-buildvcs=false", "-trimpath", "-ldflags", "-s -w", ...(name === "combined-noinline" ? ["-gcflags=all=-l"] : []), "-o", `${out}/${name}.wasm`, "./cmd/stow-wasm"], { env, stdio: "inherit", timeout: 300000 });
for (const name of ["baseline", "netip-only", "facade-only", "combined", "combined-noinline", "combined-wasm-opt", "baseline-wasm-opt"]) {
  try {
    const isolate = name.startsWith("combined") || name === "facade-only";
    const netip = name.startsWith("combined") || name === "netip-only";
    await writeFile(facadePath, isolate ? isolatedFacade : facade);
    await writeFile(utilityPath, netip ? netipUtility : utility);
    const dependencies = execFileSync("go", ["list", "-deps", "./cmd/stow-wasm"], { env, encoding: "utf8" });
    await writeFile(`${out}/${name}-dependencies.txt`, dependencies);
    if (name.endsWith("wasm-opt")) {
      const input = name.startsWith("combined") ? "combined" : "baseline";
      execFileSync("wasm-opt", [`${out}/${input}.wasm`, "-Oz", "--enable-bulk-memory", "--enable-sign-ext", "-o", `${out}/${name}.wasm`], { stdio: "inherit", timeout: 300000 });
    } else build(name);
    const bytes = await readFile(`${out}/${name}.wasm`);
    if (name === "baseline") assert.equal(hash(bytes), hash(sdkBefore), "Baseline must reproduce the currently validated SDK");
    assert.ok(WebAssembly.validate(bytes));
    await copyFile(`${out}/${name}.wasm`, "bin/stow-runtime.wasm");
    await copyFile(`${out}/${name}.wasm`, "packages/stow-s3/dist/stow-runtime.wasm");
    const tests = spawnSync(process.execPath, ["--test", "wasm/runtime.test.mjs"], { encoding: "utf8", timeout: 60000 });
    await writeFile(`${out}/${name}-tests.txt`, tests.stdout + tests.stderr);
    const smoke = spawnSync(process.execPath, ["scripts/wasm-isolate-probe.mjs"], { encoding: "utf8", timeout: 60000 });
    await writeFile(`${out}/${name}-isolate.txt`, smoke.stdout + smoke.stderr);
    const result = { name, bytes: bytes.length, sha256: hash(bytes), gzipBytes: gzipSync(bytes, { level: 9 }).length,
      brotliBytes: brotliCompressSync(bytes, { params: { [constants.BROTLI_PARAM_QUALITY]: 11 } }).length,
      sections: sections(bytes), dependencyCount: dependencies.trim().split("\n").length, runtimeTestsExit: tests.status, isolateProbeExit: smoke.status };
    results.push(result);
    console.log(JSON.stringify(result));
  } catch (error) {
    results.push({ name, failed: true, error: String(error.message).slice(0, 1000) });
    console.log(JSON.stringify({ name, failed: true }));
  } finally {
    await writeFile(facadePath, facade);
    await writeFile(utilityPath, utility);
    await writeFile("packages/stow-s3/dist/stow-runtime.wasm", sdkBefore);
  }
}
const baseline = results.find(item => item.name === "baseline");
assert.ok(baseline && !baseline.failed && baseline.runtimeTestsExit === 0 && baseline.isolateProbeExit === 0);
for (const result of results.filter(item => !item.failed)) {
  result.savedBytes = baseline.bytes - result.bytes;
  result.savedPercent = 100 * result.savedBytes / baseline.bytes;
}
await copyFile(`${out}/baseline.wasm`, "bin/stow-runtime.wasm");
await writeFile(`${out}/result.json`, JSON.stringify({ complete: true, experimentalOnly: true, sdkUnchanged: true,
  finishedAt: new Date().toISOString(), toolchain: execFileSync("go", ["version"], { encoding: "utf8" }).trim(),
  binaryen: execFileSync("wasm-opt", ["--version"], { encoding: "utf8" }).trim(),
  source: JSON.parse(await readFile("benchmark/source.json", "utf8")), results,
  limitations: ["Experimental facade exclusion is an attribution probe, not a shipping patch or full validation.", "No inlining and Binaryen variants require performance and full browser/SDK validation before adoption.", "Compression sizes are encoding measurements, not deployed transfer measurements."] }, null, 2) + "\n");
