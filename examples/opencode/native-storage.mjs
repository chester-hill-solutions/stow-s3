import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { SaveFailure } from "./controller.mjs";

const exec = promisify(execFile);

export function nativeStorage(config) {
  async function call(request, resolve, signal) {
    const args = ["workspace", "checkpoint", "--id", config.workspaceID, "--registry-dir", config.registryDir,
      "--request-key", request.key, "--timeout", `${config.deadlineMs}ms`, "--portable",
      "--max-bytes", String(config.maxBytes), "--max-files", String(config.maxFiles)];
    if (request.parent) args.push("--parent", request.parent);
    if (resolve) args.push("--resolve");
    let output;
    try { output = await exec(config.stowBinary, args, { signal, maxBuffer: 1024 * 1024 }); }
    catch (error) {
      if (!error.stdout) throw new SaveFailure("native capture reply unavailable", { outcome: "unknown", cause: error });
      output = error;
    }
    const envelope = JSON.parse(output.stdout);
    if (envelope.version !== 1) throw new Error("unsupported capture response");
    if (envelope.error) throw new SaveFailure(envelope.error.code, envelope.error);
    return envelope.result;
  }
  return { capture: (request, signal) => call(request, false, signal), resolve: (request, signal) => call(request, true, signal) };
}
