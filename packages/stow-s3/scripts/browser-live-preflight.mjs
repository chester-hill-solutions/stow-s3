import assert from "node:assert/strict";
import { openBrowserTestRemote, inspectCors } from "./browser-remote.mjs";

const fixture = await openBrowserTestRemote();
if (fixture.provider !== "railway") { await fixture.close(); throw new Error("A live testing bucket must be explicitly configured"); }
const key = `${fixture.prefix}conditional-preflight.txt`;
const bytes = (text) => new TextEncoder().encode(text);
try {
  const created = await fixture.remote.putObject(fixture.bucket, key, bytes("original"), {
    basis: { absent: true }, saveId: crypto.randomUUID(), contentType: "text/plain", metadata: { purpose: "browser-preflight" },
  });
  await assert.rejects(fixture.remote.putObject(fixture.bucket, key, bytes("must not overwrite"), {
    basis: { absent: true }, saveId: crypto.randomUUID(),
  }), { code: "conflict" });
  const observed = await fixture.remote.getObject(fixture.bucket, key);
  assert.equal(new TextDecoder().decode(observed.data), "original");
  assert.equal(observed.metadata.purpose, "browser-preflight");
  const candidates = ["writer one", "writer two"].map((text) => fixture.remote.putObject(fixture.bucket, key, bytes(text), {
    basis: { etag: created.etag }, saveId: crypto.randomUUID(), contentType: "text/plain",
  }));
  const results = await Promise.allSettled(candidates);
  assert.equal(results.filter((result) => result.status === "fulfilled").length, 1);
  assert.equal(results.filter((result) => result.status === "rejected" && result.reason.code === "conflict").length, 1);
  const cors = await inspectCors(fixture.client, fixture.bucket);
  process.stdout.write(JSON.stringify({ provider: fixture.provider, bucket: fixture.bucket, conditionalCreate: "passed", concurrentConditionalUpdate: "passed", receipt: Boolean(observed.saveId), metadata: "passed", corsRules: cors }, null, 2) + "\n");
} finally {
  await fixture.close();
  process.stdout.write("Disposable preflight prefix removed and verified empty.\n");
}
