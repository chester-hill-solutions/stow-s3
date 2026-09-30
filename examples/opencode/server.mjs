import { spawn, execFileSync } from "node:child_process";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { stopChild } from "../../packages/stow-s3/dist/start.js";

export async function startOpenCode({ binary, stateDir, directory, signal }) {
  const env = { ...process.env };
  for (const name of Object.keys(env)) {
    // Keep the Zen provider credential; strip ambient OpenCode runtime/config
    // overrides so this remains a dedicated caller-controlled process.
    if (name.startsWith("OPENCODE_") && name !== "OPENCODE_API_KEY") delete env[name];
  }
  // Align HOME and XDG so provider setup has one location even when a pinned
  // build resolves some paths directly from HOME.
  const paths = { HOME: "home", XDG_DATA_HOME: "home/.local/share", XDG_CONFIG_HOME: "home/.config", XDG_CACHE_HOME: "home/.cache", XDG_STATE_HOME: "home/.local/state" };
  for (const [name, leaf] of Object.entries(paths)) {
    env[name] = join(stateDir, "opencode", leaf);
    await mkdir(env[name], { recursive: true, mode: 0o700 });
  }
  signal?.throwIfAborted();
  if (execFileSync(binary, ["--version"], { encoding: "utf8", env, cwd: directory, timeout: 15000 }).trim() !== "opencode v2.0.16") throw new Error("pilot requires OpenCode v2.0.16");
  signal?.throwIfAborted();
  const child = spawn(binary, ["serve", "--hostname", "127.0.0.1", "--port", "0"], { cwd: directory, env, stdio: ["ignore", "pipe", "pipe"] });
  const close = () => stopChild(child, 5000, 10000);
  try { return { ...await readiness(child, signal), close }; }
  catch (error) {
    try { await close(); }
    catch (stopError) { throw Object.assign(new Error("OpenCode stop unconfirmed; storage claims retained", { cause: stopError }), { agentStopUnconfirmed: true }); }
    throw error;
  }
}

function readiness(child, signal) {
  return new Promise((resolve, reject) => {
    let buffer = "";
    let done = false;
    const finish = (error, ready) => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      child.off("error", onError);
      child.off("exit", onExit);
      signal?.removeEventListener("abort", onAbort);
      if (error) reject(error); else resolve(ready);
    };
    const onError = error => finish(error);
    const onExit = () => finish(new Error("OpenCode exited before readiness"));
    const onAbort = () => finish(new Error("OpenCode startup cancelled"));
    const onData = chunk => {
      if (done) return;
      buffer += chunk.toString();
      if (buffer.length > 65536) return finish(new Error("OpenCode readiness exceeds limit"));
      const endpoint = buffer.match(/server listening on (http:\/\/127\.0\.0\.1:\d+)/)?.[1];
      const password = buffer.match(/server password ([^\s]+)\r?\n/)?.[1];
      if (endpoint && password) finish(undefined, { endpoint, password });
    };
    const timer = setTimeout(() => finish(new Error("OpenCode readiness timeout")), 15000);
    child.stdout.on("data", onData);
    child.stderr.on("data", onData);
    child.once("error", onError);
    child.once("exit", onExit);
    signal?.addEventListener("abort", onAbort, { once: true });
    if (signal?.aborted) onAbort();
  });
}
