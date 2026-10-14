import { randomBytes, createHash } from "node:crypto";

const implementation = process.argv[2], bytes = Number(process.argv[3]);
if (!["stow-wasm-memory", "node-buffer-memory", "node-sqlite-memory"].includes(implementation) || ![4096,1048576].includes(bytes)) throw new Error("invalid Node agent case");
const body = randomBytes(bytes);
let store, sqliteVersion, sqlite;
try {
  if (implementation === "stow-wasm-memory") {
    const { loadNodeWasmHost, EmbeddedStow } = await import("../packages/stow-s3/dist/index.js");
    const host = await loadNodeWasmHost();
    const runtime = EmbeddedStow.open(host, {maxBytes:64*1024**2,maxObjects:16});
    runtime.createBucket("agent");
    store = {put:data=>runtime.putObject("agent","first",data), get:()=>runtime.getObject("agent","first").data,
      close:async()=>{runtime.close();await host.close();}};
  } else if (implementation === "node-sqlite-memory") {
    const { DatabaseSync } = await import("node:sqlite");
    const db = new DatabaseSync(":memory:"); sqlite=db;
    db.exec("CREATE TABLE objects (key TEXT PRIMARY KEY, body BLOB NOT NULL)");
    store = {put:data=>db.prepare("INSERT INTO objects (key,body) VALUES ('first',?)").run(data),
      get:()=>db.prepare("SELECT body FROM objects WHERE key='first'").get().body,close:()=>db.close()};
  } else {
    const objects = new Map();
    store = {put:data=>objects.set("first",Buffer.from(data)),get:()=>objects.get("first"),close:()=>objects.clear()};
  }
  process.send({phase:"ready"});
  store.put(body);
  process.send({phase:"written"});
  const received = store.get();
  const digest = value=>createHash("sha256").update(value).digest("hex");
  if (received.length !== body.length || digest(received) !== digest(body)) throw new Error("Node first-write verification failed");
  if (sqlite) sqliteVersion=sqlite.prepare("SELECT sqlite_version() AS version").get().version;
  const peakRssBytes = process.resourceUsage().maxRSS * 1024;
  await store.close(); store = undefined;
  process.send({phase:"complete",verified:true,peakRssBytes,sqliteVersion});
  process.disconnect();
} catch (error) {
  await store?.close();
  console.error(error.message);
  process.exitCode=1;
  if (process.connected) process.disconnect();
}
