import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");
const packageRoot = join(root, "packages/stow-s3");
const outputRoot = join(packageRoot, "dist");

function digestTree(directory) {
  const hash = createHash("sha256");
  function visit(current, relative = "") {
    for (const entry of readdirSync(current).sort()) {
      const child = join(current, entry);
      const name = relative ? `${relative}/${entry}` : entry;
      if (statSync(child).isDirectory()) visit(child, name);
      else hash.update(name).update("\0").update(readFileSync(child));
    }
  }
  visit(directory);
  return hash.digest("hex");
}

const first = digestTree(outputRoot);
execFileSync("npm", ["run", "build"], { cwd: packageRoot, stdio: "inherit" });
const second = digestTree(outputRoot);
if (first !== second) {
  throw new Error("generated TypeScript package output changed between identical builds");
}
console.log("Generated package output is reproducible");
