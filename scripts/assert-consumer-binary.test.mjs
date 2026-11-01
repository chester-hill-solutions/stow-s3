import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, copyFileSync, mkdirSync, mkdtempSync, realpathSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { pathToFileURL } from "node:url";
import { test } from "node:test";
import { assertConsumerBinary } from "./assert-consumer-binary.mjs";

test("real SDK resolver cannot pass with a missing carrier and same-version PATH binary", async () => {
  const root = mkdtempSync(join(tmpdir(), "stow-carrier-test-"));
  const previous = { PATH: process.env.PATH, STOW_BIN: process.env.STOW_BIN };
  try {
    const sdk = join(root, "node_modules/@chester-hill-solutions/stow-s3"); mkdirSync(join(sdk, "dist"), { recursive: true });
    writeFileSync(join(sdk, "package.json"), JSON.stringify({ type: "module", exports: "./dist/bin.js" }));
    // Exercise the unchanged compiled SDK implementation in a clean installation.
    copyFileSync(new URL("../packages/stow-s3/dist/bin.js", import.meta.url), join(sdk, "dist/bin.js"));
    const { resolveStowBinary, stowBinaryAvailable } = await import(pathToFileURL(join(sdk, "dist/bin.js")));
    const pathDir = join(root, "path"); mkdirSync(pathDir);
    const fallback = join(pathDir, "stow-s3"); writeFileSync(fallback, "#!/bin/sh\nprintf 'stow-s3 0.3.5\\n'\n"); chmodSync(fallback, 0o755);
    process.env.PATH = pathDir; delete process.env.STOW_BIN;
    assert.equal(execFileSync(fallback, ["--version"], { encoding: "utf8" }).trim(), "stow-s3 0.3.5");
    assert.equal(stowBinaryAvailable(), true); assert.equal(resolveStowBinary(), "stow-s3");
    const consumerURL = pathToFileURL(join(root, "smoke.mjs"));
    assert.throws(() => assertConsumerBinary(resolveStowBinary, consumerURL), /missing its installed host carrier/);
    const carrier = join(root, `node_modules/@chester-hill-solutions/stow-s3-${process.platform}-${process.arch}`);
    mkdirSync(join(carrier, "bin"), { recursive: true });
    writeFileSync(join(carrier, "package.json"), JSON.stringify({ exports: { "./bin/stow-s3": "./bin/stow-s3" } }));
    const binary = join(carrier, "bin/stow-s3"); copyFileSync(fallback, binary); chmodSync(binary, 0o755);
    assert.equal(assertConsumerBinary(resolveStowBinary, consumerURL), realpathSync(binary));
    assert.throws(() => assertConsumerBinary(() => fallback, consumerURL), /SDK must select/);
    rmSync(binary); symlinkSync(process.execPath, binary);
    assert.throws(() => assertConsumerBinary(resolveStowBinary, consumerURL), /inside the clean consumer/);
  } finally {
    for (const [key, value] of Object.entries(previous)) { if (value === undefined) delete process.env[key]; else process.env[key] = value; }
    rmSync(root, { recursive: true, force: true });
  }
});
