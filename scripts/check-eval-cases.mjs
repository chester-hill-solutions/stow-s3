#!/usr/bin/env node
// Structural check for plugin eval cases. The native harness validates these too,
// but it is early-access gated, so a typo would otherwise sit undetected until the
// day someone can run it. This checks shape, not behaviour.
import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { discoverCases } from "./lib/eval-cases.mjs";

const root = resolve(fileURLToPath(new URL(".", import.meta.url)), "..");
const casesDir = resolve(root, "plugins", "stow", "evals");
const PROMPT_KEYS = new Set(["schema_version", "name", "description", "tags", "plugins", "runs", "expected_outcome", "model", "max_turns", "timeout_seconds", "allowed_tools", "append_system_prompt", "env"]);
const GRADER_KEYS = new Set(["type", "weight", "arm", "pattern", "flags", "match", "target", "tool", "input_match", "min", "max", "before", "after", "path", "exists", "criteria", "focus", "baseline_file"]);
const GRADER_TYPES = new Set(["llm", "regex", "tool_used", "tool_order", "file_exists", "baseline"]);

const problems = [];
if (!existsSync(casesDir)) {
  console.error("no eval cases directory");
  process.exit(1);
}
const cases = discoverCases(casesDir);
let positive = 0;
let negative = 0;
let heldout = 0;
for (const testCase of cases) {
  const entry = { name: testCase.name };
  if (!testCase.prompt) problems.push(`${entry.name}: prompt body is empty`);
  for (const key of Object.keys(testCase.fields ?? {})) {
    if (!PROMPT_KEYS.has(key)) problems.push(`${entry.name}: unknown prompt key ${key}`);
  }
  const tags = testCase.fields?.tags ?? "";
  if (tags.includes("positive")) positive++;
  else if (tags.includes("negative")) negative++;
  else problems.push(`${entry.name}: tag the case positive or negative`);
  if (tags.includes("heldout")) heldout++;
  if (Number(testCase.fields?.runs ?? 0) < 3) {
    problems.push(`${entry.name}: runs must be at least 3`);
  }
  if (testCase.graders.length === 0) problems.push(`${entry.name}: no grader files`);
  for (const grader of testCase.graders) {
    const file = `${entry.name}/graders/${grader.name}.md`;
    const type = grader.fields?.type;
    if (!GRADER_TYPES.has(type)) problems.push(`${file}: unknown grader type ${type}`);
    for (const key of Object.keys(grader.fields ?? {})) {
      if (!GRADER_KEYS.has(key)) problems.push(`${file}: unknown grader key ${key}`);
    }
    if (type === "llm" && !grader.body) problems.push(`${file}: llm grader has no rubric`);
  }
}
if (positive === 0 || negative === 0) problems.push(`the set needs both arms: positive=${positive} negative=${negative}`);
if (problems.length) {
  console.error(problems.join("\n"));
  process.exit(1);
}
if (heldout === 0) console.log("warning: no held-out cases; the set will be tuned on the same data it is measured with");
console.log(`Eval cases OK (${cases.length} cases: ${positive} positive, ${negative} negative, ${heldout} held out)`);
