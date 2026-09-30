import { readFile, realpath } from "node:fs/promises";
import { resolve, join, relative } from "node:path";
import { createHash } from "node:crypto";
import { serveWorkspace } from "../../packages/stow-s3/dist/workspace.js";
import { TurnController } from "./controller.mjs";
import { OpenCodeAgent } from "./opencode.mjs";
import { jsonStore, atomicJSON, acquireCallerState } from "./local-store.mjs";
import { startOpenCode } from "./server.mjs";
import { nativeStorage } from "./native-storage.mjs";
import { shutdownPilot } from "./shutdown.mjs";
import { agentDirectory, snapshotWorkspace } from "./workspace-files.mjs";

async function main() {
  const [configPath, promptPath] = process.argv.slice(2);
  if (!configPath || !promptPath) throw new Error("usage: node examples/opencode/run.mjs config.json prompt.txt|--recover");
  const config = JSON.parse(await readFile(configPath, "utf8"));
  for (const key of ["registryDir", "stateDir", "workspaceID", "stowBinary", "opencodeBinary"]) {
    if (typeof config[key] !== "string" || !config[key]) throw new Error(`missing ${key}`);
  }
  for (const key of ["deadlineMs", "maxBytes", "maxFiles"]) {
    if (!Number.isSafeInteger(config[key]) || config[key] <= 0) throw new Error(`positive ${key} required`);
  }
  if (!config.model?.providerID || !config.model?.id) throw new Error("explicit model.providerID and model.id required");
  process.env.STOW_BIN = resolve(config.stowBinary);
  const holder = await serveWorkspace({ id: config.workspaceID, registryDir: config.registryDir });
  let release;
  let server;
  let failure;
  const abort = new AbortController();
  const cancel = () => abort.abort(new Error("caller interrupted"));
  process.once("SIGINT", cancel);
  process.once("SIGTERM", cancel);
  try {
    const root = await realpath(holder.ready.root);
    release = await acquireCallerState(config.stateDir, root);
    const identity = createHash("sha256").update(JSON.stringify(config)).digest("hex");
    let agent;
    if (promptPath !== "--recover") {
      const directory = await agentDirectory(holder.ready);
      server = await startOpenCode({ binary: config.opencodeBinary, stateDir: resolve(config.stateDir), directory, signal: abort.signal });
      agent = new OpenCodeAgent({ ...config, endpoint: server.endpoint, password: server.password, directory,
        progressPath: relative(directory, join(root, "STOW_PROGRESS.json")),
        sessionStore: jsonStore(join(config.stateDir, "agent.json"), identity) });
    }
    const controller = new TurnController({
      store: jsonStore(join(config.stateDir, "turn.json"), identity), agent, storage: nativeStorage(config),
      quiesce: signal => agent.quiesce(signal), deadlineMs: config.deadlineMs,
      writeProgress: progress => atomicJSON(join(root, "STOW_PROGRESS.json"), progress),
      snapshot: signal => snapshotWorkspace(root, { ...config, signal }),
    });
    const result = promptPath === "--recover" ? await controller.recover({ signal: abort.signal }) : await controller.turn(await readFile(promptPath, "utf8"), { signal: abort.signal });
    process.stdout.write(JSON.stringify(result) + "\n");
    if (result.reviewRequired || result.terminal !== "succeeded") process.exitCode = 2;
  } catch (error) {
    failure = error;
    throw error;
  } finally {
    await shutdownPilot({ server, release, holder }, failure);
    process.off("SIGINT", cancel);
    process.off("SIGTERM", cancel);
  }

}

main().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
