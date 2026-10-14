import { createServer } from "node:http";
import { statfs } from "node:fs/promises";
import { launchTarget } from "./implementations.mjs";
import { authorized, jsonBody, respond } from "./http.mjs";
import { collectServerProfiles } from "./diagnosis-profiles.mjs";
import { processResources, systemMetadata, cgroupResources } from "./resources.mjs";

const token = process.env.BENCH_CONTROL_TOKEN;
if (!token) throw new Error("BENCH_CONTROL_TOKEN is required");
const root = process.env.BENCH_DATA_ROOT ?? "/data";
const s3Port = Number(process.env.BENCH_S3_PORT ?? 8080);
let active;
let busy = false;
const server = createServer(async (request, response) => {
  try {
    if (request.url === "/health") return respond(response, 200, { ready: true });
    if (!authorized(request, token)) return respond(response, 401, { error: "unauthorized" });
    if (request.method === "POST" && request.url === "/profiles") {
      if (!active?.profiling || busy) return respond(response, 409, { error: "no profiling target" });
      return respond(response, 200, await collectServerProfiles(active.directory));
    }
    if (request.method === "GET" && request.url === "/metadata") return respond(response, 200, await systemMetadata());
    if (request.method === "GET" && request.url === "/sample") {
      if (!active || busy) return respond(response, 409, { error: "no active target" });
      const disk = await statfs(root);
      return respond(response, 200, { process: await processResources(active.child.pid), diskAvailableBytes: disk.bavail * disk.bsize,
        cgroup: await cgroupResources() });
    }
    if (request.method !== "POST" || !["/start", "/stop"].includes(request.url)) return respond(response, 404, { error: "unknown route" });
    if (busy) return respond(response, 409, { error: "target transition in progress" });
    busy = true;
    try {
      if (request.url === "/stop") {
        if (active) await active.close();
        active = undefined;
        return respond(response, 200, { stopped: true });
      }
      if (active) return respond(response, 409, { error: "stop the active target first" });
      active = await launchTarget(await jsonBody(request), root, s3Port);
      return respond(response, 200, { credentials: { accessKeyId: active.credentials.accessKeyId, secretAccessKey: active.credentials.secretAccessKey },
        region: active.credentials.region, port: s3Port, startupMs: active.startupMs,
        firstWriteMs: active.firstWriteMs, firstWriteBytes: active.firstWriteBytes, firstWriteVerified: active.firstWriteVerified });
    } finally { busy = false; }
  } catch (error) { respond(response, 500, { error: error.message }); }
});
server.listen(Number(process.env.PORT ?? 8081), "::", () => console.log("Benchmark target control ready"));
async function shutdown() { server.close(); if (active) await active.close(); process.exit(0); }
process.once("SIGTERM", shutdown);
process.once("SIGINT", shutdown);
