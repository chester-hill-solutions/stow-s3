import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";

const root = process.env.STOW_GO_WASM_ROOT;
if (!root) throw new Error("STOW_GO_WASM_ROOT must name the pinned Go installation");
const environment = { TMPDIR: tmpdir() };
for (const key of ["GOGC", "GOMEMLIMIT", "GODEBUG", "GOTRACEBACK", "TZ", "SystemRoot", "RUNNER_TRACKING_ID"]) {
  if (process.env[key] !== undefined) environment[key] = process.env[key];
}
const result = spawnSync(process.execPath, [
  "--stack-size=8192", join(root, "lib/wasm/wasm_exec_node.js"), ...process.argv.slice(2),
], { env: environment, stdio: "inherit" });
if (result.error) throw result.error;
if (result.signal) process.kill(process.pid, result.signal);
else process.exit(result.status ?? 1);
