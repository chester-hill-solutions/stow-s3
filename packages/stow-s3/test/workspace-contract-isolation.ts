import { mkdir } from "node:fs/promises";
import { join } from "node:path";

/**
 * Points the default registry at this run's temporary home, and returns the undo.
 * The wrapper spawns the binary without passing an environment, so a step naming no
 * --registry-dir resolves against the developer's own home; why that matters is in
 * TestContractDriversIsolateTheDefaultRegistry.
 */
export async function isolateHome(work: string): Promise<() => Promise<void>> {
  const home = join(work, "home");
  await mkdir(join(home, ".config"), { recursive: true });
  const names = ["HOME", "XDG_CONFIG_HOME", "APPDATA"] as const;
  const saved = new Map(names.map((name) => [name, process.env[name]]));
  process.env.HOME = home;
  process.env.XDG_CONFIG_HOME = join(home, ".config");
  process.env.APPDATA = join(home, "AppData", "Roaming");
  return async () => {
    for (const [name, value] of saved) {
      if (value === undefined) delete process.env[name];
      else process.env[name] = value;
    }
  };
}
