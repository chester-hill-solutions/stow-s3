import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync } from "node:fs";
import { access, cp, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const packer = join(repository, "benchmark/pack.mjs");

async function verifyReceipt(directory) {
  const hash = createHash("sha256");
  async function visit(path, prefix = "") {
    const entries = (await readdir(path, { withFileTypes: true })).sort((a, b) => a.name.localeCompare(b.name));
    for (const entry of entries) {
      const name = join(prefix, entry.name), absolute = join(path, entry.name);
      if (name === join("benchmark", "source.json")) continue;
      if (entry.isDirectory()) await visit(absolute, name);
      else if (entry.isFile()) hash.update(name).update(await readFile(absolute));
    }
  }
  await visit(directory);
  const receipt = JSON.parse(await readFile(join(directory, "benchmark/source.json"), "utf8"));
  assert.equal(receipt.payloadSha256, hash.digest("hex"), "Receipt must identify the final upload tree without its own metadata");
}

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), "stow-benchmark-pack-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const put = async (path, text) => {
    await mkdir(dirname(join(root, path)), { recursive: true });
    await writeFile(join(root, path), text);
  };
  for (const path of ["cmd/main.go", "internal/store.go", "pkg/store.go",
    "packages/stow-s3/dist/index.js", "packages/stow-s3/scripts/performance/run.mjs"]) {
    await put(path, "// fixture source\n");
  }
  await put("go.mod", "module example.invalid/stow\n\ngo 1.25.0\n");
  await put("go.sum", "");
  for (const path of ["packages/stow-s3/package.json", "packages/stow-s3/package-lock.json"]) await put(path, "{}\n");
  const names = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "-z", "benchmark"], {
    cwd: repository, encoding: "utf8",
  }).split("\0").filter(Boolean);
  for (const name of new Set(names.filter(name => existsSync(join(repository, name))))) {
    await mkdir(dirname(join(root, name)), { recursive: true });
    await cp(join(repository, name), join(root, name));
  }
  await put(".gitignore", ".env*\n.aws/\n.codex/\nnode_modules/\ncontext/\n");
  const git = args => execFileSync("git", args, { cwd: root, stdio: "ignore" });
  git(["init", "-q"]);
  git(["add", "."]);
  git(["-c", "user.name=Pack test", "-c", "user.email=pack@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "Fixture"]);
  return { root, put, run: flags => spawnSync(process.execPath, [packer, join(root, "context"), ...flags], {
    cwd: root, encoding: "utf8",
  }) };
}

for (const flags of [[], ["--full"]]) {
  test(`source packaging excludes private files in ${flags.length ? "full" : "minimal"} mode`, async t => {
    const f = await fixture(t);
    const privateFiles = ["benchmark/.env.local", "benchmark/nested/.env.production",
      "benchmark/nested/.aws/credentials", "benchmark/nested/.codex/config.toml", "cmd/private/.env.test"];
    for (const path of privateFiles) await f.put(path, "synthetic test value\n");
    await f.put("benchmark/nested/keep.mjs", "export const keep = true;\n");
    await f.put("benchmark/source.json", '{"staleReceipt":true}\n');
    const result = f.run(flags);
    assert.equal(result.status, 0, result.stderr);
    for (const path of privateFiles) {
      await assert.rejects(access(join(f.root, "context", path)), { code: "ENOENT" });
      assert.equal(await readFile(join(f.root, path), "utf8"), "synthetic test value\n");
    }
    await access(join(f.root, "context/benchmark/nested/keep.mjs"));
    await verifyReceipt(join(f.root, "context"));
  });
}

for (const flags of [["--site"], ["--controlled", "--owned-body"], ["--embedded-diagnose"], ["--wasm-network-probe"], ["--unknown-mode"]]) {
  test(`invalid packaging options leave no partial output: ${flags.join(" ")}`, async t => {
    const f = await fixture(t);
    const result = f.run(flags);
    assert.notEqual(result.status, 0);
    await assert.rejects(access(join(f.root, "context")), { code: "ENOENT" });
  });
}

test("full packaging respects a deletion in the current working tree", async t => {
  const f = await fixture(t);
  await rm(join(f.root, "internal/store.go"));
  const result = f.run(["--full"]);
  assert.equal(result.status, 0, result.stderr);
  await assert.rejects(access(join(f.root, "context/internal/store.go")), { code: "ENOENT" });
});

test("the controlled mode selects its recipe from the image folder", async t => {
  const f = await fixture(t);
  const result = f.run(["--controlled"]);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(await readFile(join(f.root, "context/benchmark/Dockerfile"), "utf8"),
    await readFile(join(f.root, "benchmark/docker/controlled.Dockerfile"), "utf8"));
  const receipt = JSON.parse(await readFile(join(f.root, "context/benchmark/source.json"), "utf8"));
  assert.deepEqual(receipt.dirtyFiles, []);
});

test("full size verification uses the verification image with its size flag", async t => {
  const f = await fixture(t);
  const result = f.run(["--full", "--size"]);
  assert.equal(result.status, 0, result.stderr);
  const dockerfile = await readFile(join(f.root, "context/benchmark/Dockerfile"), "utf8");
  assert.match(dockerfile, /BENCH_CLOUD_VERIFY=1 BENCH_SIZE_AB=1/);
  await access(join(f.root, "context/packages/stow-s3/dist/index.js"));
  await verifyReceipt(join(f.root, "context"));
});
