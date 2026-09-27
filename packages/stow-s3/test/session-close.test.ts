import assert from "node:assert/strict";
import { access, rm } from "node:fs/promises";
import { describe, it } from "node:test";
import { openStow } from "../dist/session.js";
import type { StowSession } from "../dist/session.js";
import { stowBinaryAvailable } from "../dist/bin.js";

// Closing a session releases three things: the S3 client, the server process, and
// the temporary directory. They were released in sequence with no isolation, so a
// throw from the first abandoned the other two — the server kept running and the
// directory stayed on disk. The failure that triggers it is rare, which is what
// makes it a leak rather than a crash: the caller gets an exception, believes the
// session is closed, and leaves a process and a directory behind.
//
// disposeQuietly, a few lines below close() in the same file, already does this
// correctly for the failed-start path. The discipline existed; it had not been
// applied where a leak is possible.
//
// These tests open a real session and break one step on purpose, because the
// interesting assertions are about the *other* two still having run.
//
// Each provokes the leak deliberately, so each cleans up unconditionally. That is
// not tidiness: the whole point is that the server is left running, and a leaked
// server keeps the event loop alive, so without a guaranteed teardown the file
// hangs and the runner reports the still-pending tests as failed assertions rather
// than as the hang they are.

const session = { skip: !stowBinaryAvailable() };

async function assertRemoved(directory: string): Promise<void> {
  // Written as an explicit access rather than assert.rejects, because
  // assert.rejects reports its own failure as "Missing expected rejection" — which
  // reads as though the thing under test failed to reject, when in fact the
  // rejection happened and the leak is what was observed. A leak that reports
  // itself as a missing rejection sends the next reader to the wrong line.
  try {
    await access(directory);
  } catch {
    return;
  }
  assert.fail(`close() left ${directory} on disk, so a step after the failing one never ran`);
}

/**
 * The session's compiled class is a plain object, so its private field is reachable
 * at runtime and the tests need it: stopping the server is one of the three
 * releases under test and there is no way to make it fail from the public
 * interface.
 *
 * The cast is to an intersection, which is a subtype of StowSession, so a single
 * downward `as` is legal. The alternative — `as unknown as` — is what the
 * type-escape ratchet exists to keep out of the tree, and rightly so: it silences
 * the compiler instead of satisfying it.
 */
type WithInstance = StowSession & { instance: { stop: () => Promise<void> } };

interface BrokenOptions {
  /** Replace the client's destroy with one that throws. */
  readonly breakClient?: boolean;
  /** Replace the instance's stop with one that throws. */
  readonly breakServer?: boolean;
  /** Counted across calls, so a test can tell one release from two. */
  readonly clientCalls?: { count: number };
}

interface BrokenSession {
  readonly stow: StowSession;
  readonly cleanup: () => Promise<void>;
}

async function openBroken(options: BrokenOptions): Promise<BrokenSession> {
  const stow = await openStow();
  const dataDir = stow.dataDir;
  const internal: WithInstance = stow as WithInstance;

  // The originals are captured as values, not as `() => internal.instance.stop()`.
  // A closure that looks the method up when it runs would, after cleanup put the
  // wrapper back, call itself — the teardown would recurse until the stack gave
  // out. Calling through .call keeps `this` without re-reading the property.
  //
  // destroy is void, not Promise<void>: in SDK v3 the S3 client is disposed
  // synchronously. close() awaits it regardless, which is harmless, and the
  // substitutes below are async because they have to be able to reject.
  const originalStop = internal.instance.stop;
  const originalDestroy = stow.s3.destroy;
  const realStop = (): Promise<void> => originalStop.call(internal.instance);
  const realDestroy = (): void => originalDestroy.call(stow.s3);

  if (options.breakClient) {
    stow.s3.destroy = async () => {
      if (options.clientCalls) {
        options.clientCalls.count += 1;
      }
      throw new Error("client destroy failed");
    };
  }
  if (options.breakServer) {
    internal.instance.stop = async () => {
      throw new Error("server stop failed");
    };
  }

  const cleanup = async (): Promise<void> => {
    // Restore first, so the real stop runs even if the test left a substitute in
    // place. Without this the teardown would call the substitute and throw.
    internal.instance.stop = realStop;
    stow.s3.destroy = realDestroy;
    await realStop().catch(() => undefined);
    await rm(dataDir, { recursive: true, force: true }).catch(() => undefined);
  };
  return { stow, cleanup };
}

describe("session close releases everything even when a step fails", session, () => {
  it("still stops the server and removes the directory when the client fails to destroy", async () => {
    const { stow, cleanup } = await openBroken({ breakClient: true });
    try {
      await assert.rejects(stow.close(), /client destroy failed/);
      // The directory is the observable half of the leak. Had the sequence aborted
      // at the first failure, this is the assertion that fails.
      await assertRemoved(stow.dataDir);
    } finally {
      await cleanup();
    }
  });

  it("still removes the directory when stopping the server fails", async () => {
    const { stow, cleanup } = await openBroken({ breakServer: true });
    try {
      await assert.rejects(stow.close(), /server stop failed/);
      await assertRemoved(stow.dataDir);
    } finally {
      await cleanup();
    }
  });

  it("reports every failure rather than only the first", async () => {
    const { stow, cleanup } = await openBroken({ breakClient: true, breakServer: true });
    try {
      const error = await stow.close().then(
        () => undefined,
        (caught: unknown) => caught,
      );

      assert.ok(error instanceof Error, "close() resolved despite two failing steps");
      // A caller who breaks one step should not lose the information that another
      // also broke. Reporting only the first is how the second stays unknown.
      const text =
        error instanceof AggregateError
          ? error.errors.map(String).join("; ")
          : `${(error as Error).message} ${String((error as Error).cause ?? "")}`;
      assert.match(text, /client destroy failed/, "the client failure was not reported");
      assert.match(text, /server stop failed/, "the server failure was not reported");
      await assertRemoved(stow.dataDir);
    } finally {
      await cleanup();
    }
  });

  it("stays closed and does not retry after a failed close", async () => {
    const clientCalls = { count: 0 };
    const { stow, cleanup } = await openBroken({ breakClient: true, clientCalls });
    try {
      await assert.rejects(stow.close(), /client destroy failed/);
      // close() memoises its promise, so a second call must return the same settled
      // promise rather than starting the teardown again. Retrying would double
      // every release and could remove a directory a later session had adopted.
      await assert.rejects(stow.close(), /client destroy failed/);
      assert.equal(clientCalls.count, 1, "close() ran its releases more than once");
      await assertRemoved(stow.dataDir);
    } finally {
      await cleanup();
    }
  });
});
