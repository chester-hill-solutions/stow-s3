import assert from "node:assert/strict";
import { test } from "node:test";
import { readFile } from "node:fs/promises";
import { HeadObjectCommand } from "@aws-sdk/client-s3";
import { schemaLogFilter, SENSITIVE_STRING } from "@smithy/core/client";
import type { StaticSimpleSchema, StaticStructureSchema } from "@smithy/types";
import { createStowS3Client } from "../src/s3-client.js";

test("the installed Smithy logger filter redacts fields inside lists and maps", () => {
  const secret: StaticSimpleSchema = [0, "stow.test", "Private", { sensitive: 1 }, 0];
  const item: StaticStructureSchema = [3, "stow.test", "Item", 0, ["label", "secret"], [0, secret]];
  const schema: StaticStructureSchema = [3, "stow.test", "Nested", 0, ["items", "lookup"], [
    [1, "stow.test", "Items", 0, item],
    [2, "stow.test", "Lookup", 0, 0, item],
  ]];
  const input = {
    items: [{ label: "visible", secret: "list-private-value" }],
    lookup: { example: { label: "visible", secret: "map-private-value" } },
  };
  assert.deepEqual(schemaLogFilter(schema, input), {
    items: [{ label: "visible", secret: SENSITIVE_STRING }],
    lookup: { example: { label: "visible", secret: SENSITIVE_STRING } },
  });
  assert.equal(input.items[0]?.secret, "list-private-value");
  assert.equal(input.lookup.example.secret, "map-private-value");
});

test("Stow's actual S3 client preserves sensitive-field filtering for custom loggers", async () => {
  const records: unknown[] = [];
  const client = createStowS3Client({
    endpoint: "http://127.0.0.1:9",
    region: "us-east-1",
    forcePathStyle: true,
    maxAttempts: 1,
    credentials: { accessKeyId: "LOCALTEST", secretAccessKey: "local-test-credential" },
    logger: {
      info: (...messages: unknown[]) => { records.push(...messages); },
      debug: () => {}, warn: () => {}, error: () => {},
    },
    requestHandler: { handle: async () => ({ response: { statusCode: 200, headers: { "content-length": "0" } } }) },
  });
  try {
    await client.send(new HeadObjectCommand({ Bucket: "logging-test", Key: "object", SSECustomerKey: "private-sse-key-value" }));
    assert.ok(records.length > 0, "the custom logger must actually receive a result");
    const logged = JSON.stringify(records);
    assert.ok(logged.includes(SENSITIVE_STRING));
    assert.equal(logged.includes("private-sse-key-value"), false);
  } finally {
    client.destroy();
  }
});

test("all locked Smithy core copies retain the compatible redaction fix", async () => {
  const lock: { packages: Record<string, { version?: string; deprecated?: string }> } = JSON.parse(
    await readFile(new URL("../package-lock.json", import.meta.url), "utf8"),
  );
  const copies = Object.entries(lock.packages).filter(([name]) => name.endsWith("/@smithy/core"));
  assert.ok(copies.length > 0);
  for (const [name, record] of copies) {
    const [major, minor, patch] = (record.version ?? "").split(".").map(Number);
    assert.ok(major === 3 && minor !== undefined && (minor > 38 || minor === 38 && patch !== undefined && patch >= 1), `${name}: ${record.version}`);
    assert.equal(record.deprecated, undefined, `${name} must not select a deprecated redaction release`);
  }
});
