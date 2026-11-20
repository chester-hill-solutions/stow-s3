#!/usr/bin/env node
// Structural check for plugin eval cases. The native harness validates these too,
// but it is early-access gated, so a typo would otherwise sit undetected until the
// day someone can run it. This checks shape, not behaviour.
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL(".", import.meta.url)), "..");
const casesDir = resolve(root, "plugins", "stow", "evals");
const PROMPT_KEYS = new Set(["schema_version", "name", "description", "tags", "plugins", "runs", "expected_outcome", "model", "max_turns", "timeout_seconds", "allowed_tools", "append_system_prompt", "env"]);
const GRADER_KEYS = new Set(["type", "weight", "arm", "pattern", "flags", "match", "target", "tool", "input_match", "min", "max", "before", "after", "path", "exists", "criteria", "focus", "baseline_file"]);
const GRADER_TYPES = new Set(["llm", "regex", "tool_used", "tool_order", "file_exists", "baseline"]);

function frontmatter(text, file, problems) {
  if (!text.startsWith("---\n")) {
    problems.push(`${file}: no frontmatter`);
    return {};
  }
  const end = text.indexOf("\n---\n", 4);
  if (end === -1) {
    problems.push(`${file}: frontmatter is never closed`);
    return {};
  }
  const fields = {};
  for (const line of text.slice(4, end).split("\n")) {
    const match = /^([a-z_]+):\s*(.*)$/.exec(line);
    if (!match) {
      problems.push(`${file}: frontmatter line is not key: value: ${line}`);
      continue;
    }
    fields[match[1]] = match[2];
  }
  return { fields, body: text.slice(end + 5).trim() };
}

const problems = [];
if (!existsSync(casesDir)) {
  console.error("no eval cases directory");
  process.exit(1);
}
const cases = readdirSync(casesDir, { withFileTypes: true }).filter((entry) => entry.isDirectory() && entry.name !== "results");
let positive = 0;
let negative = 0;
let heldout = 0;
for (const entry of cases) {
  const dir = resolve(casesDir, entry.name);
  const promptPath = resolve(dir, "prompt.md");
  if (!existsSync(promptPath)) {
    problems.push(`${entry.name}: no prompt.md`);
    continue;
  }
  const parsed = frontmatter(readFileSync(promptPath, "utf8"), `${entry.name}/prompt.md`, problems);
  if (!parsed.body) problems.push(`${entry.name}: prompt body is empty`);
  for (const key of Object.keys(parsed.fields ?? {})) {
    if (!PROMPT_KEYS.has(key)) problems.push(`${entry.name}: unknown prompt key ${key}`);
  }
  const tags = parsed.fields?.tags ?? "";
  if (tags.includes("positive")) positive++;
  else if (tags.includes("negative")) negative++;
  else problems.push(`${entry.name}: tag the case positive or negative`);
  if (tags.includes("heldout")) heldout++;
  if (!/^runs:\s*[3-9]|^runs:\s*[1-9][0-9]+/m.test(readFileSync(promptPath, "utf8"))) {
    problems.push(`${entry.name}: runs must be at least 3`);
  }
  const gradersDir = resolve(dir, "graders");
  if (!existsSync(gradersDir)) {
    problems.push(`${entry.name}: no graders`);
    continue;
  }
  const graders = readdirSync(gradersDir).filter((name) => name.endsWith(".md"));
  if (graders.length === 0) problems.push(`${entry.name}: no grader files`);
  for (const grader of graders) {
    const file = `${entry.name}/graders/${grader}`;
    const graderParsed = frontmatter(readFileSync(resolve(gradersDir, grader), "utf8"), file, problems);
    const type = graderParsed.fields?.type;
    if (!GRADER_TYPES.has(type)) problems.push(`${file}: unknown grader type ${type}`);
    for (const key of Object.keys(graderParsed.fields ?? {})) {
      if (!GRADER_KEYS.has(key)) problems.push(`${file}: unknown grader key ${key}`);
    }
    if (type === "llm" && !graderParsed.body) problems.push(`${file}: llm grader has no rubric`);
  }
}
if (positive === 0 || negative === 0) problems.push(`the set needs both arms: positive=${positive} negative=${negative}`);
if (problems.length) {
  console.error(problems.join("\n"));
  process.exit(1);
}
if (heldout === 0) console.log("warning: no held-out cases; the set will be tuned on the same data it is measured with");
console.log(`Eval cases OK (${cases.length} cases: ${positive} positive, ${negative} negative, ${heldout} held out)`);
