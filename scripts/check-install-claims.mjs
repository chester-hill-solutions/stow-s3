#!/usr/bin/env node
// The install command is a promise. A pinned npm version that was never published
// sends a user to E404, and the docs cannot tell the difference by reading them.
// The publication receipt is the source of truth.
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL(".", import.meta.url)), "..");
const releases = resolve(root, "docs", "history", "releases");
const receipts = existsSync(releases)
  ? readdirSync(releases).filter((name) => /^npm-\d+\.\d+\.\d+\.json$/.test(name))
  : [];
if (receipts.length === 0) {
  console.error("no npm publication receipt found under docs/history/releases");
  process.exit(1);
}
const versions = receipts.map((name) => name.slice(4, -5));
const published = versions.sort((left, right) => {
  const a = left.split(".").map(Number);
  const b = right.split(".").map(Number);
  return a[0] - b[0] || a[1] - b[1] || a[2] - b[2];
}).at(-1);

const files = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "-z"], {
  cwd: root, encoding: "utf8", maxBuffer: 16 * 1024 * 1024,
}).split("\0").filter((path) => /\.(md|html|txt)$/.test(path) && existsSync(resolve(root, path)))
  .filter((path) => path === "README.md" || path === "site/index.html" || path === "site/agent.md" || path === "site/llms.txt"
    || path.startsWith("packages/") || path.startsWith("skills/") || path.startsWith("plugins/")
    || path.startsWith("docs/guides/") || path.startsWith("examples/")
    || path === "docs/development/storage-foundation-plan.md");

const problems = [];
const pattern = /npm install[^\n`"']*@chester-hill-solutions\/stow-s3@(\d+\.\d+\.\d+)/g;
// A second @version immediately after the first is a malformed pin that the
// version match above still satisfies. It shipped once in the skill, which told
// a reader to install `stow-s3@0.3.7@0.3.7` while this gate stayed green.
const doubled = /@chester-hill-solutions\/stow-s3@\d+\.\d+\.\d+@/g;
for (const file of files) {
  const text = readFileSync(resolve(root, file), "utf8");
  for (const match of text.matchAll(doubled)) {
    problems.push(`${file}: has a doubled version pin (${match[0]}); the install command is malformed`);
  }
  for (const match of text.matchAll(pattern)) {
    if (match[1] !== published) {
      problems.push(`${file}: pins @chester-hill-solutions/stow-s3@${match[1]}, but npm publishes ${published}`);
    }
  }
}
if (problems.length) {
  console.error(problems.join("\n"));
  process.exit(1);
}
console.log(`npm install claims OK (published ${published}; ${files.length} documents)`);
