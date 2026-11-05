import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { test } from "node:test";

test("real Go WASM tests tolerate a large host environment and propagate failures", { timeout: 180000 }, () => {
  const root = resolve(import.meta.dirname, "..");
  const pin = readFileSync(join(root, ".go-version"), "utf8").trim();
  const host = { ...process.env, GOTOOLCHAIN: `go${pin}`, STOW_TEST_ENV_PADDING: "x".repeat(16384) };
  const goroot = execFileSync("go", ["env", "GOROOT"], { cwd: root, env: host, encoding: "utf8" }).trim();
  const directory = mkdtempSync(join(tmpdir(), "stow-wasm-environment-"));
  try {
    const binary = join(directory, "tests.wasm");
    execFileSync("go", ["test", "-c", "-o", binary, "./cmd/stow-wasm"], {
      cwd: root, env: { ...host, GOOS: "js", GOARCH: "wasm" }, timeout: 120000, stdio: "pipe",
    });
    const original = spawnSync(process.execPath, ["--stack-size=8192", join(goroot, "lib/wasm/wasm_exec_node.js"), binary], {
      env: host, encoding: "utf8", timeout: 30000,
    });
    assert.equal(original.error, undefined);
    assert.notEqual(original.status, 0);
    assert.match(original.stdout + original.stderr, /total length of command line and environment variables exceeds limit/);
    const run = args => spawnSync(process.execPath, [join(root, "scripts/run-go-wasm-test.mjs"), binary, ...args], {
      env: { ...host, STOW_GO_WASM_ROOT: goroot }, encoding: "utf8", timeout: 30000,
    });
    const passed = run(["-test.v", "-test.count=1"]);
    assert.equal(passed.error, undefined);
    assert.equal(passed.status, 0, passed.stdout + passed.stderr);
    assert.match(passed.stdout, /=== RUN\s+TestObservationTokensExpireAtTheirDeadline/);
    assert.match(passed.stdout, /=== RUN\s+TestClosedRuntimeCannotRecreateObservationTable/);
    assert.match(passed.stdout, /PASS/);
    const failed = run(["-test.timeout=not-a-duration"]);
    assert.equal(failed.error, undefined);
    assert.notEqual(failed.status, 0);
    assert.match(failed.stdout + failed.stderr, /invalid value.*test.timeout/);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
