import { existsSync, readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { dirname, isAbsolute, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { pages } from "../site/docs.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

export function localLinks(markdown) {
  const prose = markdown.replace(/^(`{3,}|~{3,}).*\n[\s\S]*?^\1\s*$/gm, "");
  const inline = [...prose.matchAll(/!?\[[^\]\n]*\]\((<[^>\n]+>|[^)\s]+)(?:\s+"[^"\n]*")?\)/g)].map((match) => match[1]);
  const definitions = [...prose.matchAll(/^\[[^\]\n]+\]:\s*(<[^>\n]+>|\S+)/gm)].map((match) => match[1]);
  return [...inline, ...definitions].map((href) => href.replace(/^<|>$/g, ""))
    .filter((href) => !/^[a-z][a-z\d+.-]*:/i.test(href) && !href.startsWith("//") && !href.startsWith("#"));
}

export function linkProblems(markdown, source, directory = root) {
  const problems = [];
  for (const href of localLinks(markdown)) {
    const path = decodeURIComponent(href.split(/[?#]/, 1)[0]);
    if (!path) continue;
    let target;
    if (path.startsWith("/") && source.startsWith("site/")) {
      const asset = resolve(directory, "site", path.slice(1));
      const page = pages.find((page) => page.route === path);
      if (existsSync(asset)) target = asset;
      else if (page) target = resolve(directory, page.source);
      else target = resolve(directory, "site/.build", path.slice(1));
    } else if (isAbsolute(path)) {
      problems.push(`${source}: machine-specific link ${href}`);
      continue;
    } else target = resolve(directory, dirname(source), path);
    if (relative(directory, target).startsWith("../")) problems.push(`${source}: link leaves the repository: ${href}`);
    else if (!existsSync(target)) problems.push(`${source}: missing local target ${href}`);
  }
  return problems;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const files = [...new Set(execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "-z"], { cwd: root, encoding: "utf8" }).split("\0"))]
    .filter((path) => path.endsWith(".md") && existsSync(resolve(root, path)));
  const problems = files.flatMap((path) => linkProblems(readFileSync(resolve(root, path), "utf8"), path));
  if (problems.length) {
    console.error(problems.join("\n"));
    process.exitCode = 1;
  } else console.log(`Local documentation links OK (${files.length} Markdown files)`);
}
