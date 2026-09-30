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

// The first digest has to be taken before anything regenerates dist, or it is a
// digest of a fresh build and the comparison below becomes a build against
// itself — which passes for a committed artifact that no source produces. That
// is not hypothetical: it is what let two commits land with a stale
// stow-runtime.wasm, and the check said "reproducible" while they did.
const committed = digestTree(outputRoot);
execFileSync("npm", ["run", "build"], { cwd: packageRoot, stdio: "inherit" });
const rebuilt = digestTree(outputRoot);
if (committed !== rebuilt) {
  throw new Error(
    "the committed generated output does not match a fresh build of this tree; " +
      "regenerate it and commit the result, or find out what changed",
  );
}
console.log("Generated package output matches a fresh build of this tree");
