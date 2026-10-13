import { before, after } from "node:test";
import { loadNodeWasmHost, type NodeWasmHost } from "../dist/node-wasm-host.js";
export function sharedWasmHost(): () => NodeWasmHost {
  let host: NodeWasmHost;
  before(async () => { host = await loadNodeWasmHost(); });
  after(async () => { await host.close(); });
  return () => host;
}
