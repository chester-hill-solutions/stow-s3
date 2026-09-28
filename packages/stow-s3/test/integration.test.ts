import assert from "node:assert/strict";
import { chmod, copyFile, mkdir, mkdtemp, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import { describe, it } from "node:test";
import type * as BinModule from "../dist/bin.js";
import { stowBinaryAvailable } from "../dist/bin.js";
import { verifyObjectReadable } from "../dist/instance.js";
import { Stow, StowOwnershipError } from "../dist/index.js";
import { parseReadyLine } from "../dist/start.js";
import { runSharedCorpus } from "./shared-corpus.js";

describe("parseReadyLine", () => {
  it("parses the STOW_READY banner line", () => {
    const ready = parseReadyLine(
      "STOW_READY endpoint=http://127.0.0.1:54321 access_key=ABCDEF secret_key=ghijklmnop mode=local",
    );
    assert.equal(ready?.endpoint, "http://127.0.0.1:54321");
    assert.equal(ready?.accessKeyId, "ABCDEF");
    assert.equal(ready?.secretAccessKey, "ghijklmnop");
    assert.equal(ready?.mode, "local");
  });
});

describe("stow binary discovery", () => {
  // Derived the same way bin.ts derives it, so this exercises the real mapping
  // on whichever platform runs it.
  const platformPackage = `@chester-hill-solutions/stow-s3-${process.platform}-${process.arch}`;

  async function isolatedBin(directory: string): Promise<typeof BinModule> {
    const isolatedDist = join(directory, "isolated", "dist");
    await mkdir(isolatedDist, { recursive: true });
    await copyFile(new URL("../dist/bin.js", import.meta.url), join(isolatedDist, "bin.js"));
    return import(pathToFileURL(join(isolatedDist, "bin.js")).href);
  }

  function restore(environment: NodeJS.ProcessEnv, name: string, value: string | undefined): void {
    if (value === undefined) {
      delete environment[name];
    } else {
      environment[name] = value;
    }
  }

  it("resolves a platform package binary without PATH or environment setup", async () => {
    if (process.platform === "win32") {
      return;
    }
    const dir = await mkdtemp(join(tmpdir(), "stow-bundled-"));
    try {
      const packageDir = join(dir, "isolated", "node_modules", platformPackage);
      await mkdir(join(packageDir, "bin"), { recursive: true });
      await writeFile(
        join(packageDir, "package.json"),
        JSON.stringify({
          name: platformPackage,
          version: "0.0.0",
          exports: { "./bin/stow-s3": "./bin/stow-s3" },
        }),
      );
      const bundled = join(packageDir, "bin", "stow-s3");
      await writeFile(bundled, "#!/bin/sh\nexit 0\n");
      await chmod(bundled, 0o755);

      const isolated = await isolatedBin(dir);
      const previousPath = process.env.PATH;
      const previousBin = process.env.STOW_BIN;
      // Empty PATH and STOW_BIN prove the bundled package is what resolved,
      // rather than a fallback happening to succeed.
      process.env.PATH = "";
      process.env.STOW_BIN = "";
      try {
        // macOS exposes the temporary directory through /var while mkdtemp
        // may return its canonical /private/var path (or vice versa).
        assert.equal(await realpath(isolated.resolveStowBinary()), await realpath(bundled));
        assert.equal(isolated.stowBinaryAvailable(), true);
      } finally {
        restore(process.env, "PATH", previousPath);
        restore(process.env, "STOW_BIN", previousBin);
      }
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });

  it("falls back cleanly when no platform package is installed", async () => {
    if (process.platform === "win32") {
      return;
    }
    const dir = await mkdtemp(join(tmpdir(), "stow-no-platform-"));
    try {
      const isolated = await isolatedBin(dir);
      const previousBin = process.env.STOW_BIN;
      process.env.STOW_BIN = "";
      try {
        // No platform package and no STOW_BIN: resolution must still return a
        // usable answer rather than throwing.
        assert.equal(isolated.resolveStowBinary(), "stow-s3");
        assert.equal(isolated.stowBinaryAvailable(), false);
      } finally {
        restore(process.env, "STOW_BIN", previousBin);
      }
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });

  it("finds an executable on PATH", async () => {
    if (process.platform === "win32") {
      return;
    }
    const dir = await mkdtemp(join(tmpdir(), "stow-path-"));
    const onPath = join(dir, "stow-s3");
    await writeFile(onPath, "#!/bin/sh\nexit 0\n");
    await chmod(onPath, 0o755);
    const isolated = await loadIsolatedResolver(dir);
    // STOW_BIN outranks PATH, so a test about PATH that leaves STOW_BIN set is
    // asserting the environment rather than the resolver. It passed in CI only
    // because that job does not set STOW_BIN, and failed for anyone who exported
    // it to point at a local build — which is the documented way to run this
    // package from a source checkout.
    await withResolverEnvironment({ PATH: dir, STOW_BIN: undefined }, () => {
      assert.equal(isolated.resolveStowBinary(), "stow-s3");
      assert.equal(isolated.stowBinaryAvailable(), true);
    });
    await rm(dir, { recursive: true, force: true });
  });

  it("prefers STOW_BIN over PATH, because a pinned build outranks a floating one", async () => {
    if (process.platform === "win32") {
      return;
    }
    const dir = await mkdtemp(join(tmpdir(), "stow-precedence-"));
    const onPath = join(dir, "stow-s3");
    await writeFile(onPath, "#!/bin/sh\nexit 0\n");
    await chmod(onPath, 0o755);
    const pinned = join(dir, "pinned", "stow-s3");
    await mkdir(dirname(pinned), { recursive: true });
    await writeFile(pinned, "#!/bin/sh\nexit 0\n");
    await chmod(pinned, 0o755);
    const isolated = await loadIsolatedResolver(dir);
    await withResolverEnvironment({ PATH: dir, STOW_BIN: pinned }, () => {
      // A caller who upgrades the package and still exports a STOW_BIN from an
      // older release should get the binary that shipped with the version they
      // installed, not the stale one their shell carries. Stating that here is what
      // stops the next reader from "fixing" the precedence the other way.
      assert.equal(isolated.resolveStowBinaryDetailed().source, "environment");
      assert.equal(isolated.resolveStowBinary(), pinned);
    });
    await rm(dir, { recursive: true, force: true });
  });
});

/**
 * Loads the resolver from a copy of dist/bin.js in a directory of its own.
 *
 * The copy is what makes the test about the resolver rather than about this
 * process: a module caches its environment lookups' surroundings at import time, so
 * importing the real one would test whichever STOW_BIN and PATH happened to be set
 * when the suite started.
 */
async function loadIsolatedResolver(dir: string): Promise<typeof BinModule> {
  const isolatedDist = join(dir, "isolated", "dist");
  await mkdir(isolatedDist, { recursive: true });
  await copyFile(new URL("../dist/bin.js", import.meta.url), join(isolatedDist, "bin.js"));
  return (await import(pathToFileURL(join(isolatedDist, "bin.js")).href)) as typeof BinModule;
}

/**
 * Runs a body with the given environment variables set, restoring every one of
 * them afterwards.
 *
 * An undefined value means "remove it", which is not the same as setting it to the
 * empty string: the resolver trims and then tests for truthiness, so both would
 * read as absent, and a test that meant to prove a variable is ignored would pass
 * for the wrong reason if it only ever set the variable.
 */
async function withResolverEnvironment(
  variables: Readonly<Record<string, string | undefined>>,
  body: () => void | Promise<void>,
): Promise<void> {
  const previous = new Map<string, string | undefined>();
  for (const [name, value] of Object.entries(variables)) {
    previous.set(name, process.env[name]);
    if (value === undefined) {
      delete process.env[name];
    } else {
      process.env[name] = value;
    }
  }
  try {
    await body();
  } finally {
    for (const [name, value] of previous) {
      if (value === undefined) {
        delete process.env[name];
      } else {
        process.env[name] = value;
      }
    }
  }
}

describe("shared conformance corpus", () => {
  it("runs every corpus case against the filesystem backend", async () => {
    await runSharedCorpus("filesystem");
  });

  it("runs every corpus case against the memory backend", async () => {
    await runSharedCorpus("memory");
  });
});

describe("external connections", () => {
  it("connects without taking ownership of a process", async () => {
    const dataDir = await mkdtemp(join(tmpdir(), "stow-connect-"));
    const instance = await Stow.start({ dataDir, port: 0 });
    try {
      const connection = Stow.connect({
        endpoint: instance.endpoint,
        accessKeyId: instance.accessKeyId,
        secretAccessKey: instance.secretAccessKey,
        region: instance.region,
      });
      assert.equal(connection.endpoint, instance.endpoint);
      assert.equal(connection.awsSdkV3Config().forcePathStyle, true);
      assert.equal(typeof connection.client.destroy, "function");
      connection.disconnect();
    } finally {
      await instance.stop();
      await rm(dataDir, { recursive: true, force: true });
    }
  });
});

const integration = describe;
integration("integration", { skip: !stowBinaryAvailable() }, () => {
  it("starts, writes, reads, and stops", async () => {
    const dataDir = await mkdtemp(join(tmpdir(), "stow-test-"));
    const instance = await Stow.start({
      dataDir,
      buckets: ["uploads"],
      port: 0,
    });

    try {
      assert.match(instance.endpoint, /^http:\/\/127\.0\.0\.1:\d+$/);
      assert.ok(instance.accessKeyId.length > 0);
      assert.ok(instance.secretAccessKey.length > 0);

      const config = instance.awsSdkV3Config();
      assert.equal(config.endpoint, instance.endpoint);
      assert.equal(config.forcePathStyle, true);

      await instance.putFixture("uploads", "hello.txt", "hello world");
      const body = await verifyObjectReadable(instance, "uploads", "hello.txt");
      assert.equal(body, "hello world");

      const snapshot = await instance.snapshotObjects("uploads");
      assert.deepEqual(snapshot.map((item) => item.key), ["hello.txt"]);
    } finally {
      await instance.stop();
    }
  });

  // A reset is only allowed on a directory stow created, and the marker is
  // written by the server on start. So the first run establishes ownership and
  // the second proves the reset takes effect.
  it("resets a data directory stow created, and refuses one it did not", async () => {
    const dataDir = await mkdtemp(join(tmpdir(), "stow-reset-"));
    const first = await Stow.start({ dataDir, buckets: ["uploads"], port: 0 });
    await first.putFixture("uploads", "stale.txt", "stale");
    await first.stop();

    const second = await Stow.start({ dataDir, buckets: ["uploads"], port: 0, resetOwnedData: true });
    try {
      const remaining = await second.snapshotObjects("uploads");
      assert.deepEqual(remaining.map((item) => item.key), []);
    } finally {
      await second.stop();
    }

    // The same option on a directory stow never touched must refuse rather
    // than recursively delete it.
    const foreign = await mkdtemp(join(tmpdir(), "stow-foreign-"));
    const keep = join(foreign, "keep.txt");
    await writeFile(keep, "not stow data", "utf8");
    await assert.rejects(
      () => Stow.start({ dataDir: foreign, port: 0, resetOwnedData: true }),
      StowOwnershipError,
    );
    assert.equal(await readFile(keep, "utf8"), "not stow data");
  });
});
