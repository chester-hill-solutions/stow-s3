import { cp, mkdir, readdir, readFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { dirname, join, relative, resolve, sep } from "node:path";

const privateParts = /(?:^|\/)(?:\.env(?:\.[^/]*)?|\.aws|\.codex|\.git|\.cache|\.cloudflare|\.wrangler|node_modules|\.DS_Store)(?:\/|$)/;
const benchmarkOutputs = /^benchmark\/(?:context|verify-context|results|plans|source\.json)(?:\/|$)/;

export function sourceCopier(out) {
  const root = process.cwd();
  const include = source => {
    const path = relative(root, resolve(source)).split(sep).join("/");
    return !privateParts.test(path) && !benchmarkOutputs.test(path) && resolve(source) !== out;
  };
  return async entry => {
    if (!include(entry)) return;
    const target = join(out, entry);
    await mkdir(dirname(target), { recursive: true });
    await cp(entry, target, { recursive: true, filter: include });
  };
}

export function workingFiles() {
  return [...new Set(execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "-z"], {
    encoding: "utf8", maxBuffer: 16 * 1024 * 1024,
  }).split("\0").filter(path => path && existsSync(path)))];
}

export async function payloadDigest(directory) {
  const hash = createHash("sha256");
  async function visit(path, prefix = "") {
    for (const entry of (await readdir(path, { withFileTypes: true })).sort((a, b) => a.name.localeCompare(b.name))) {
      const name = join(prefix, entry.name), absolute = join(path, entry.name);
      if (entry.isDirectory()) await visit(absolute, name);
      else if (entry.isFile()) hash.update(name).update(await readFile(absolute));
    }
  }
  await visit(directory);
  return hash.digest("hex");
}
