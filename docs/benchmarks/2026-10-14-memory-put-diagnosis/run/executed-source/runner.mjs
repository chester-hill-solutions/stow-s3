import { createServer } from "node:http";
import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { authorized, jsonBody, respond } from "./http.mjs";

const token = process.env.BENCH_CONTROL_TOKEN;
if (!token) throw new Error("BENCH_CONTROL_TOKEN is required");
const root = process.env.BENCH_RESULTS_ROOT ?? "/results";
let job;
const server = createServer(async (request, response) => {
  try {
    if (request.url === "/health") return respond(response, 200, { ready: true });
    if (!authorized(request, token)) return respond(response, 401, { error: "unauthorized" });
    if (request.method === "GET" && request.url === "/status") return respond(response, 200, job ? { id: job.id, profile: job.profile, running: job.running, exitCode: job.exitCode } : { running: false });
    if (request.method !== "POST" || request.url !== "/run") return respond(response, 404, { error: "unknown route" });
    if (job?.running) return respond(response, 409, { error: "benchmark already running" });
    const { profile = "smoke" } = await jsonBody(request);
    if (!["smoke", "comparison", "regression", "controlled", "diagnosis"].includes(profile)) return respond(response, 400, { error: "unknown profile" });
    const id = randomUUID();
    const child = spawn(process.execPath, profile === "diagnosis" ? ["benchmark/diagnosis.mjs", join(root, id)] : ["benchmark/run.mjs", profile, join(root, id)], { stdio: ["ignore", "inherit", "inherit"] });
    job = { id, profile, running: true, exitCode: null, child };
    const current = job;
    const deadline = setTimeout(()=>child.kill("SIGTERM"), 3600000);
    child.once("error", ()=>{ current.running = false; current.exitCode = 1; clearTimeout(deadline); });
    child.once("close", async code=>{
      current.running = false; current.exitCode = code; clearTimeout(deadline);
      const result = await readFile(join(root, id, "run.json"), "utf8").then(JSON.parse).catch(()=>null);
      console.log(JSON.stringify({ benchmarkFinished: id, exitCode: code, complete: result?.complete ?? false }));
    });
    return respond(response, 202, { id, profile });
  } catch (error) { respond(response, 500, { error: error.message }); }
});
server.listen(Number(process.env.PORT ?? 8082), "::", ()=>console.log("Benchmark runner ready"));
function shutdown() { job?.child.kill("SIGTERM"); server.close(()=>process.exit(0)); }
process.once("SIGTERM", shutdown);
process.once("SIGINT", shutdown);
