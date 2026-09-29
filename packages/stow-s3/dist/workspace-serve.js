import { spawn } from "node:child_process";
import { resolveStowBinary } from "./bin.js";
import { decodeWorkspaceJSON } from "./workspace.js";
import { stopChild, assertRequestLimit } from "./start.js";
export async function serveWorkspace(options) {
    assertRequestLimit(options.maxRequestBytes);
    assertRequestLimit(options.maxConcurrentRequests, "maxConcurrentRequests");
    if (!options.id)
        throw new Error("workspace id is required");
    if (options.signal?.aborted)
        throw new Error("workspace startup cancelled");
    const args = ["workspace", "serve", "--id", options.id, "--ready-fd", "3", "--parent-pid", String(process.pid)];
    const flags = { "max-request-bytes": options.maxRequestBytes, "max-concurrent-requests": options.maxConcurrentRequests, "registry-dir": options.registryDir, team: options.team, "max-bytes": options.maxBytes, "max-objects": options.maxObjects };
    for (const [key, value] of Object.entries(flags)) {
        if (value !== undefined)
            args.push(`--${key}`, String(value));
    }
    const child = spawn(resolveStowBinary(), args, { stdio: ["ignore", "ignore", "pipe", "pipe"] });
    let closing;
    const close = () => closing ??= stopChild(child, 11_000, 15_000);
    try {
        const ready = await readWorkspaceReady(child, options);
        return { ready, close };
    }
    catch (error) {
        await close();
        throw error;
    }
}
function readWorkspaceReady(child, options) {
    const stream = child.stdio[3];
    let diagnostics = "";
    child.stderr?.on("data", (chunk) => { diagnostics = (diagnostics + chunk.toString()).slice(-65536); });
    return new Promise((resolve, reject) => {
        let buffer = "";
        const finish = (error, ready) => {
            clearTimeout(timer);
            stream.off("data", onData);
            stream.off("end", onEnd);
            child.off("error", onError);
            child.off("exit", onExit);
            options.signal?.removeEventListener("abort", onAbort);
            if (error)
                reject(error);
            else if (ready)
                resolve(ready);
        };
        const onError = (error) => finish(error);
        const onExit = () => finish(new Error(`workspace server exited before readiness: ${diagnostics}`));
        const onEnd = () => finish(new Error(`workspace readiness ended before a record: ${diagnostics}`));
        const onAbort = () => finish(new Error("workspace startup cancelled"));
        const onData = (chunk) => {
            buffer += chunk.toString();
            if (buffer.length > 262144)
                return finish(new Error("workspace readiness exceeds limit"));
            const newline = buffer.indexOf("\n");
            if (newline < 0)
                return;
            try {
                const ready = decodeWorkspaceJSON(buffer.slice(0, newline));
                for (const name of ["workspace_id", "root", "bucket", "endpoint", "region", "access_key_id", "secret_access_key"]) {
                    if (typeof ready[name] !== "string" || ready[name] === "")
                        throw new Error(`invalid workspace readiness field ${name}`);
                }
                if (ready["version"] !== 1)
                    throw new Error("unsupported workspace readiness version");
                finish(undefined, ready);
            }
            catch (error) {
                finish(error instanceof Error ? error : new Error(String(error)));
            }
        };
        const timer = setTimeout(() => finish(new Error(`workspace readiness timed out: ${diagnostics}`)), options.timeoutMs ?? 15_000);
        stream.on("data", onData);
        stream.once("end", onEnd);
        child.once("error", onError);
        child.once("exit", onExit);
        options.signal?.addEventListener("abort", onAbort, { once: true });
        if (options.signal?.aborted)
            onAbort();
    });
}
//# sourceMappingURL=workspace-serve.js.map