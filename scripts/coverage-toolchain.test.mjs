import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { delimiter, join } from "node:path";
import { tmpdir } from "node:os";
import { after, test } from "node:test";
import { selectCoverageToolchain } from "./coverage-toolchain.mjs";

const directory = mkdtempSync(join(tmpdir(), "stow-coverage-toolchain-"));
after(() => rmSync(directory, { recursive: true, force: true }));
let count = 0;
function fixture() {
  const root = join(directory, String(count++)), selected = join(root, "selected");
  mkdirSync(join(selected, "bin"), { recursive: true });
  writeFileSync(join(root, ".go-version"), "1.25.6\n");
  const command = join(selected, "bin", process.platform === "win32" ? "go.exe" : "go");
  writeFileSync(command, "fixture"); chmodSync(command, 0o755);
  return { root, selected, command, info: { GOROOT: selected, GOVERSION: "go1.25.6" } };
}

test("coverage selects the repository pin and binds nested commands to that installation", () => {
  const f = fixture(), env = { Path: "older-launcher", GOTOOLCHAIN: "go1.26.0", FIXTURE: "retained" };
  const result = selectCoverageToolchain(f.root, { env, run: (command, args, options) => {
    assert.equal(command, "go"); assert.deepEqual(args, ["env", "-json", "GOROOT", "GOVERSION"]);
    assert.equal(options.env.GOTOOLCHAIN, "go1.25.6"); return JSON.stringify(f.info);
  } });
  assert.equal(result.command, f.command); assert.equal(result.env.GOTOOLCHAIN, "go1.25.6");
  assert.equal(result.env.GOROOT, f.selected);
  assert.equal(result.env.PATH, join(f.selected, "bin") + delimiter + "older-launcher");
  assert.equal(result.env.Path, undefined); assert.equal(result.env.FIXTURE, "retained");
  assert.equal(env.GOTOOLCHAIN, "go1.26.0");
  assert.equal(existsSync(join(f.selected, "pkg/tool/covdata")), false, "on-demand tools need no prebuilt covdata");
});

test("unavailable, malformed and wrong toolchains fail with an actionable diagnostic", () => {
  const f = fixture();
  for (const run of [() => { throw Error("download failed"); }, () => "not JSON", () => JSON.stringify({ ...f.info, GOVERSION: "go1.22.5" }), () => JSON.stringify({ ...f.info, GOROOT: "relative" }), () => JSON.stringify({ ...f.info, GOROOT: join(f.root, "missing") })]) {
    assert.throws(() => selectCoverageToolchain(f.root, { run }), /Coverage toolchain:.*Go 1\.25\.6.*Coverage and baselines were not changed/);
  }
  writeFileSync(join(f.root, ".go-version"), "auto\n");
  assert.throws(() => selectCoverageToolchain(f.root, { run: () => { throw Error("must not select an unpinned toolchain"); } }), /explicit patch version/);
});

test("an older PATH launcher cannot handle the nested coverage tool command", { skip: process.platform === "win32" ? "POSIX executable fixtures; selection assertions above are portable" : false }, () => {
  const f = fixture(), oldBin = join(f.root, "old-bin"), marker = join(f.root, "old-invoked"); mkdirSync(oldBin);
  const executable = (path, source) => { writeFileSync(path, `#!${process.execPath}\n${source}\n`); chmodSync(path, 0o755); };
  executable(join(oldBin, "go"), `const fs=require('fs'); if(process.argv[2]==='env'){if(process.env.GOTOOLCHAIN!=='go1.25.6')process.exit(2);console.log(${JSON.stringify(JSON.stringify(f.info))});}else{fs.writeFileSync(${JSON.stringify(marker)},'wrong launcher');process.stderr.write('go: no such tool covdata');process.exit(1);}`);
  executable(f.command, `if(process.argv[2]==='tool'){console.log('selected coverage tool');}else{const r=require('child_process').spawnSync('go',['tool','covdata'],{env:process.env,encoding:'utf8'});process.stdout.write(r.stdout||'');process.stderr.write(r.stderr||'');process.exit(r.status??1);}`);
  const env = { ...process.env, PATH: oldBin, GOTOOLCHAIN: "auto" };
  assert.throws(() => execFileSync(f.command, ["test"], { env, stdio: "pipe" }), /no such tool covdata/);
  assert.equal(existsSync(marker), true); rmSync(marker);
  const result = selectCoverageToolchain(f.root, { env });
  assert.equal(execFileSync(result.command, ["test"], { env: result.env, encoding: "utf8" }).trim(), "selected coverage tool");
  assert.equal(existsSync(marker), false);
});
