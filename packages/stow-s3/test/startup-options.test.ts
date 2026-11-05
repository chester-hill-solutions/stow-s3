import assert from "node:assert/strict";
import { PutObjectCommand } from "@aws-sdk/client-s3";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, it } from "node:test";

import { StowProtocolError } from "../dist/ready.js";
import { stowBinaryAvailable } from "../dist/bin.js";
import { openStow } from "../dist/session.js";
import { startStowWithReady } from "../dist/start.js";

/**
 * Asserts a rejection is a cancellation, and returns true so it can be handed
 * straight to assert.rejects.
 */
function assertCancelled(error: unknown): true {
  assert.ok(
    error instanceof StowProtocolError,
    `expected a StowProtocolError, got ${String(error)}`,
  );
  assert.equal(error.code, "cancelled", "cancellation must not be reported as an internal error");
  return true;
}

/**
 * EphemeralStowOptions declared timeoutMs and signal and neither was ever
 * forwarded to the spawner, so a caller's timeout was replaced by a hardcoded
 * ten seconds and an abort signal did nothing. These assert the options now
 * take effect, which is only observable against a real child process.
 */
const integration = describe;
integration("startup options", { skip: !stowBinaryAvailable() }, () => {
  it("honors timeoutMs rather than a built-in default", async () => {
    const dataDir = await mkdtemp(join(tmpdir(), "stow-timeout-"));
    // Long enough to fail only if the option is actually read; the default is
    // 10s, so a 1ms budget can only be honored by the caller.
    await assert.rejects(
      () =>
        startStowWithReady({
          dataDir,
          backend: "memory",
          timeoutMs: 1,
        }),
      /Timed out waiting for STOW_READY|timed out|cancelled/i,
      "a 1ms startup budget should time out, which is only possible if timeoutMs is read",
    );
  });

  it("rejects immediately when the signal is already aborted", async () => {
    const dataDir = await mkdtemp(join(tmpdir(), "stow-aborted-"));
    const controller = new AbortController();
    controller.abort();
    await assert.rejects(
      () => startStowWithReady({ dataDir, backend: "memory", signal: controller.signal }),
      assertCancelled,
    );
  });

  it("stops the child when aborted during startup", async () => {
    const dataDir = await mkdtemp(join(tmpdir(), "stow-abort-mid-"));
    const controller = new AbortController();
    const start = startStowWithReady({
      dataDir,
      backend: "memory",
      // Long enough that only the abort can end it.
      timeoutMs: 30_000,
      signal: controller.signal,
    });
    // A macrotask, not 25ms: the child has been spawned but cannot possibly be
    // ready, because spawning a process takes milliseconds. Timing this against
    // the real readiness time would be a race.
    setTimeout(() => controller.abort(), 0);
    let startup;
    try {
      await assert.rejects(start, assertCancelled);
    } catch (error) {
      // If the server somehow won the race, stop it rather than leak the child
      // and hang the suite.
      startup = await start.catch(() => undefined);
      await startup?.instance.stop().catch(() => undefined);
      throw error;
    }
  });

  it("uses the region the server reports", async () => {
    const dataDir = await mkdtemp(join(tmpdir(), "stow-region-"));
    // A region other than the default, so the assertion can fail. With the
    // server hardcoded to us-east-1 this test would pass no matter what the
    // client did, which is what made the original version of it worthless.
    const startup = await startStowWithReady({
      dataDir,
      backend: "memory",
      region: "eu-west-2",
      buckets: ["uploads"],
    });
    try {
      assert.equal(startup.ready.region, "eu-west-2", "server should report the region it was started with");
      assert.equal(
        startup.instance.awsSdkV3Config().region,
        "eu-west-2",
        "client must sign with the region the server reported, not a hardcoded default",
      );
      // A signed request is the only assertion that proves the region is
      // usable: a client signing for the wrong region is rejected outright.
      await startup.instance.putFixture("uploads", "hello.txt", "hello world");
    } finally {
      await startup.instance.stop();
    }
  });
});

integration("session options", { skip: !stowBinaryAvailable() }, () => {
  it("reports cancellation as cancelled, not internal", async () => {
    const controller = new AbortController();
    controller.abort();
    await assert.rejects(() => openStow({ signal: controller.signal }), assertCancelled);
  });
});

integration("request body limits", { skip: !stowBinaryAvailable() }, () => {
  it("reports a configured request limit from the native server", async () => {
    const session = await openStow({ maxRequestBytes: 1024, maxConcurrentRequests: 3 });
    try {
      assert.equal(session.capabilities().maxRequestBytes, 1024);
      assert.equal(session.capabilities().maxConcurrentRequests, 3);
      await assert.rejects(() => session.s3.send(new PutObjectCommand({ Bucket: session.bucket, Key: "oversized", Body: Buffer.alloc(1025) })),
        (error: unknown) => error instanceof Error && error.name === "EntityTooLarge");
    }
    finally { await session.close(); }
  });
  it("rejects nonpositive and fractional explicit request limits", async () => {
    for (const maxRequestBytes of [0, -1, 1.5]) {
      await assert.rejects(() => startStowWithReady({ maxRequestBytes }), /positive integer/);
      await assert.rejects(() => startStowWithReady({ maxConcurrentRequests: maxRequestBytes }), /positive integer/);
    }
  });
});


describe("explicit remote launcher options", () => {
  it("forwards consent and isolated admin provisioning without placing a token in argv", async () => {
    const directory = await mkdtemp(join(tmpdir(), "stow-remote-options-"));
    const capture = join(directory, "captured.json");
    const preload = join(directory, "capture.cjs");
    const previousBinary = process.env.STOW_BIN;
    const previousNodeOptions = process.env.NODE_OPTIONS;
    await writeFile(preload, `require("node:fs").writeFileSync(${JSON.stringify(capture)}, JSON.stringify({ args: process.argv.slice(2), token: process.env.STOW_ADMIN_TOKEN })); process.stderr.write("fixture exits before listening"); process.exit(1);`);
    try {
      process.env.STOW_BIN = process.execPath;
      process.env.NODE_OPTIONS = `--require=${JSON.stringify(preload)}`;
      for (const consent of [false, true]) {
        await assert.rejects(() => startStowWithReady({
          dataDir: directory, port: 0, host: "192.0.2.1", timeoutMs: 5000,
          allowInsecureRemote: consent, adminToken: "fixture-admin-token",
          accessKey: "fixture-key", secretKey: "fixture-secret", isolatedEnvironment: true,
        }));
        const recorded: unknown = JSON.parse(await readFile(capture, "utf8"));
        assert.deepEqual(recorded, {
          args: ["--port", "0", "--data-dir", directory, "--host", "192.0.2.1", ...(consent ? ["--allow-insecure-remote"] : []), "--ready-fd", "3"],
          token: "fixture-admin-token",
        });
      }
    } finally {
      if (previousBinary === undefined) delete process.env.STOW_BIN;
      else process.env.STOW_BIN = previousBinary;
      if (previousNodeOptions === undefined) delete process.env.NODE_OPTIONS;
      else process.env.NODE_OPTIONS = previousNodeOptions;
      await rm(directory, { recursive: true, force: true });
    }
  });
});
