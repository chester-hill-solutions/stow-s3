import { execFileSync } from "node:child_process";
import { accessSync, constants, readFileSync } from "node:fs";
import { delimiter, isAbsolute, join } from "node:path";

export function selectCoverageToolchain(root, { run = execFileSync, env = process.env } = {}) {
  const pin = readFileSync(join(root, ".go-version"), "utf8").trim();
  const problem = detail => new Error(`Coverage toolchain: ${detail}. Install/select the full pinned Go ${pin} distribution from https://go.dev/dl/ and rerun make check-coverage. Coverage and baselines were not changed.`);
  if (!/^\d+\.\d+\.\d+$/.test(pin)) throw problem(".go-version is not an explicit patch version");
  const pinned = { ...env, GOTOOLCHAIN: `go${pin}` };
  let selected;
  try {
    selected = JSON.parse(run("go", ["env", "-json", "GOROOT", "GOVERSION"], {
      cwd: root, env: pinned, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"],
    }));
  } catch { throw problem(`could not select go${pin}`); }
  if (selected.GOVERSION !== `go${pin}` || typeof selected.GOROOT !== "string" || !isAbsolute(selected.GOROOT)) {
    throw problem(`go env did not identify the selected go${pin} installation`);
  }
  const bin = join(selected.GOROOT, "bin");
  const command = join(bin, process.platform === "win32" ? "go.exe" : "go");
  try { accessSync(command, constants.X_OK); }
  catch { throw problem(`selected Go executable is unavailable at ${command}`); }
  // Go 1.25.6 invokes "go tool covdata" by PATH for testless packages.
  // Keep that nested command on the selected version too (golang/go#75031).
  const pathKey = Object.keys(pinned).find(key => key.toLowerCase() === "path");
  const path = pathKey ? pinned[pathKey] : "";
  const childEnv = Object.fromEntries(Object.entries(pinned).filter(([key]) => key.toLowerCase() !== "path"));
  childEnv.PATH = bin + (path ? delimiter + path : "");
  childEnv.GOROOT = selected.GOROOT;
  return { command, env: childEnv };
}
