import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
const execute = promisify(execFile);
let collecting = false;
export async function collectServerProfiles(directory) {
  if (collecting) throw new Error("profile collection already active");
  collecting = true;
  try {
    const profiles = {};
    for (const kind of ["profile", "mutex", "block", "heap"]) {
      const url = `http://127.0.0.1:6060/debug/pprof/${kind}${kind === "profile" ? "?seconds=23" : ""}`;
      const response = await fetch(url, { signal: AbortSignal.timeout(40000) });
      if (!response.ok) throw new Error(`profile collection failed: ${kind}`);
      const bytes = Buffer.from(await response.arrayBuffer());
      const file = join(directory, `${kind}.pprof`);
      await writeFile(file, bytes);
      const args = ["-top", "-nodecount=40", ...(kind === "heap" ? ["-sample_index=alloc_space"] : []), "bin/stow-s3-profile", file];
      const { stdout } = await execute("bin/pprof", args, { timeout: 30000, maxBuffer: 1024 * 1024 });
      const cumulative = await execute("bin/pprof", ["-top", "-cum", "-nodecount=40", ...(kind === "heap" ? ["-sample_index=alloc_space"] : []), "bin/stow-s3-profile", file], { timeout: 30000, maxBuffer: 1024 * 1024 });
      profiles[kind] = { data: bytes.toString("base64"), top: stdout, cumulative: cumulative.stdout };
    }
    return profiles;
  } finally { collecting = false; }
}
