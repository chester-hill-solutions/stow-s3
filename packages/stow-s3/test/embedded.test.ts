import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  EmbeddedStow,
  EmbeddedStowError,
  type EmbeddedHost,
} from "../dist/index.js";

class FakeHost implements EmbeddedHost {
  readonly requests: Record<string, unknown>[] = [];

  call(request: string): string {
    const parsed = JSON.parse(request) as Record<string, unknown>;
    this.requests.push(parsed);
    switch (parsed.op) {
      case "open":
        return response({
          handle: 7,
          capabilities: {
            backend: "memory",
            maxBytes: 10,
            maxObjects: 2,
            persistent: false,
            multipart: false,
            upstream: false,
            conditionalWrites: true,
          },
        });
      case "getObject":
        return response({
          bucket: parsed.bucket,
          key: parsed.key,
          data: "aGVsbG8=",
          size: 5,
          etag: "etag",
          metadata: { owner: "test" },
        });
      case "putObject":
        return response({
          bucket: parsed.bucket,
          key: parsed.key,
          size: 5,
          etag: "etag",
        });
      case "listBuckets":
        return response({ buckets: [{ name: "assets" }] });
      case "listObjects":
        return response({
          objects: [{ bucket: "assets", key: "hello.txt", size: 5, etag: "etag" }],
          truncated: false,
        });
      case "usage":
        return response({ bytes: 5, objects: 1 });
      case "copyObject":
        return response({ bucket: "assets", key: "copy.txt", size: 5, etag: "etag" });
      default:
        return response(undefined);
    }
  }
}

function response(result: unknown, version = 1): string {
  return JSON.stringify({ version, ok: true, result });
}

describe("EmbeddedStow", () => {
  it("adapts the host bridge with byte-safe object operations", () => {
    const host = new FakeHost();
    const stow = EmbeddedStow.open(host, { maxBytes: 10, maxObjects: 2 });

    assert.equal(stow.handle, 7);
    assert.equal(stow.capabilities().backend, "memory");
    stow.createBucket("assets");
    assert.equal(host.requests[0]?.version, 1);
    assert.equal(host.requests[1]?.handle, 7);
    stow.putObject("assets", "hello.txt", new TextEncoder().encode("hello"), {
      contentType: "text/plain",
      metadata: { owner: "test" },
      ifMatch: '"original"',
      ifNoneMatch: '"excluded"',
    });
    assert.equal(host.requests[2]?.ifMatch, '"original"');
    assert.equal(host.requests[2]?.ifNoneMatch, '"excluded"');
    const object = stow.getObject("assets", "hello.txt");
    assert.deepEqual(object.data, new TextEncoder().encode("hello"));
    assert.equal(object.metadata?.owner, "test");
    assert.deepEqual(stow.listBuckets(), [{ name: "assets" }]);
    assert.equal(stow.listObjects("assets").objects[0]?.key, "hello.txt");
    assert.deepEqual(stow.usage(), { bytes: 5, objects: 1 });
    assert.equal(stow.copyObject("assets", "hello.txt", "assets", "copy.txt").key, "copy.txt");
    stow.reset();
    stow.close();
    stow.close();
    assert.throws(() => stow.getObject("assets", "hello.txt"), EmbeddedStowError);
  });

  it("refuses write conditions on an older host without sending a write", () => {
    const requests: Record<string, unknown>[] = [];
    const host: EmbeddedHost = {
      call: (request) => {
        requests.push(JSON.parse(request) as Record<string, unknown>);
        return response({ handle: 7, capabilities: {} });
      },
    };
    const stow = EmbeddedStow.open(host);
    for (const options of [{ ifMatch: "old" }, { ifNoneMatch: "*" }]) {
      assert.throws(
        () => stow.putObject("assets", "hello.txt", new Uint8Array(), options),
        (error: unknown) => hasCode(error, "conditional_write_unsupported"),
      );
    }
    assert.equal(requests.length, 1);
    stow.putObject("assets", "hello.txt", new Uint8Array());
    assert.equal(requests.length, 2);
  });

  it("refuses observation-based saves a host cannot honour, and shapes them when it can", () => {
    const refused: Record<string, unknown>[] = [];
    const plainHost: EmbeddedHost = {
      call: (request) => {
        refused.push(JSON.parse(request) as Record<string, unknown>);
        return response({ handle: 7, capabilities: {} });
      },
    };
    const plain = EmbeddedStow.open(plainHost);
    assert.throws(
      () => plain.readForSave("assets", "a.txt"),
      (error: unknown) => hasCode(error, "guarded_saves_unsupported"),
    );
    assert.throws(
      () => plain.saveObject("assets", "a.txt", new Uint8Array(), { replace: true }),
      (error: unknown) => hasCode(error, "guarded_saves_unsupported"),
    );
    assert.throws(
      () => plain.resolveSave("assets", "a.txt", "rk-1"),
      (error: unknown) => hasCode(error, "save_requests_unsupported"),
    );
    assert.equal(refused.length, 1, "a refused save must not reach the bridge");
    plain.close();

    const sent: Record<string, unknown>[] = [];
    const capableHost: EmbeddedHost = {
      call: (request) => {
        const parsed = JSON.parse(request) as Record<string, unknown>;
        sent.push(parsed);
        if (parsed.op === "open") {
          return response({ handle: 7, capabilities: { guardedSaves: true } });
        }
        if (parsed.op === "readForSave") {
          return response({
            object: { bucket: "assets", key: "a.txt", size: 3, etag: "'" + 'e' + "'" },
            absent: false,
            observationToken: "tok-1",
          });
        }
        return response({
          object: { bucket: "assets", key: "a.txt", size: 3, etag: "'" + 'e' + "'" },
          outcome: "committed",
          replayed: false,
        });
      },
    };
    const capable = EmbeddedStow.open(capableHost);
    const observation = capable.readForSave("assets", "a.txt");
    assert.equal(observation.observationToken, "tok-1");
    assert.equal(observation.absent, false);
    const saved = capable.saveObject("assets", "a.txt", new TextEncoder().encode("new"), {
      observationToken: "tok-1",
      contentType: "text/plain",
    });
    assert.equal(saved.outcome, "committed");
    const saveRequest = sent.find((request) => request.op === "saveObject");
    assert.equal(saveRequest?.observationToken, "tok-1");
    assert.equal(saveRequest?.replace, false);
    assert.equal(saveRequest?.contentType, "text/plain");

    // An unobserved save and a request key a host cannot retain never reach it.
    assert.throws(
      () => capable.saveObject("assets", "a.txt", new Uint8Array(), {}),
      (error: unknown) => hasCode(error, "invalid_save_condition"),
    );
    assert.throws(
      () => capable.saveObject("assets", "a.txt", new Uint8Array(), { replace: true, requestKey: "rk-1" }),
      (error: unknown) => hasCode(error, "save_requests_unsupported"),
    );
    assert.equal(sent.filter((request) => request.op === "saveObject").length, 1);
    capable.close();
  });

  it("rejects protocol version mismatches", () => {
    const host: EmbeddedHost = {
      call: () => response({ handle: 7, capabilities: {} }, 2),
    };
    assert.throws(
      () => EmbeddedStow.open(host),
      (error: unknown) => hasCode(error, "protocol_version"),
    );
  });

  it("preserves structured bridge error codes", () => {
    const host: EmbeddedHost = {
      call: (request) => {
        const parsed = JSON.parse(request) as { op?: string };
        if (parsed.op === "open") {
          return response({ handle: 7, capabilities: {} });
        }
        return JSON.stringify({
          version: 1,
          ok: false,
          error: { code: "quota_exceeded", message: "quota exceeded" },
        });
      },
    };
    const stow = EmbeddedStow.open(host);
    assert.throws(
      () => stow.createBucket("assets"),
      (error: unknown) => hasCode(error, "quota_exceeded"),
    );
  });
});

function hasCode(error: unknown, code: string): boolean {
  return error instanceof EmbeddedStowError && Reflect.get(error, "code") === code;
}
