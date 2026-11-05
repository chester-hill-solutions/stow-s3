import assert from "node:assert/strict";
import { test } from "node:test";
import { AsyncEmbeddedStow } from "../dist/async-embedded.js";
import { decodeEmbeddedResponse, type EmbeddedCapabilities } from "../dist/embedded.js";
import { loadNodeWasmHost } from "../dist/node-wasm-host.js";
import { ContentObjectHost } from "../src/content-object-host.js";
import { ContentStore } from "../src/content-store.js";
import { MemoryContentAdapter } from "./content-fixtures.js";

for (const backend of ["memory", "host"] as const) {
  test(`real async ${backend} delimiter pages preserve prefix rows and UTF-8 cursors`, async () => {
    const wasm = await loadNodeWasmHost();
    const bridge = { callBinary: (request: string, data?: Uint8Array) => wasm.callBinaryAsync(request, data) };
    let runtime: AsyncEmbeddedStow | undefined;
    try {
      if (backend === "memory") {
        runtime = await AsyncEmbeddedStow.open(bridge);
      } else {
        const adapter = new MemoryContentAdapter();
        await adapter.open("listing");
        const storage = new ContentObjectHost(new ContentStore(adapter));
        const response = await wasm.openHost(JSON.stringify({ version: 2, op: "open", options: {} }), storage);
        const opened = decodeEmbeddedResponse<{ handle: number; capabilities: EmbeddedCapabilities }>(JSON.parse(response.metadata), 2);
        runtime = AsyncEmbeddedStow.attach(bridge, opened.handle, opened.capabilities);
      }
      await runtime.createBucket("listing");
      for (const key of ["dir/\u{10000}/a", "dir/\uE000/a", "dir/\uE000/b", "dir/a.txt", "other/b"]) {
        await runtime.putObject("listing", key, new Uint8Array([1]));
      }
      let cursor: string | undefined;
      for (const [index, expected] of ["dir/a.txt", "dir/\uE000/", "dir/\u{10000}/"].entries()) {
        const page = await runtime.listObjects("listing", { prefix: "dir/", delimiter: "/", limit: 1, cursor });
        assert.deepEqual(page.objects.map((object) => object.key), index === 0 ? [expected] : []);
        assert.deepEqual(page.commonPrefixes ?? [], index === 0 ? [] : [expected]);
        assert.equal(page.truncated, index < 2);
        if (page.truncated) assert.equal(page.nextCursor, expected);
        cursor = page.nextCursor;
      }
    } finally {
      try { await runtime?.close(); } finally { await wasm.close(); }
    }
  });
}
