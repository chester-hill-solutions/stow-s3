#!/usr/bin/env node
// Host-neutral conversion eval: OpenCode subject, project-local skill, locked-down agent.
// The cases and rubrics are the same files `claude plugin eval` reads.
import { execFile } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync, copyFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { discoverCases } from "./lib/eval-cases.mjs";

const run = promisify(execFile);
const root = resolve(fileURLToPath(new URL(".", import.meta.url)), "..");
const skill = resolve(root, "plugins", "stow", "skills", "stow-s3", "SKILL.md");
const permissions = [
  { action: "*", resource: "*", effect: "deny" },
  { action: "skill", resource: "*", effect: "allow" },
];
const projectConfig = {
  $schema: "https://opencode.ai/config.json",
  mcp: {},
  agents: { eval: { description: "Read-only evaluation agent", mode: "primary", permissions } },
};

const args = process.argv.slice(2);
const value = (name, fallback) => {
  const index = args.indexOf(name);
  return index === -1 ? fallback : args[index + 1];
};
const caseGlob = value("--case", "*");
const runsOverride = value("--runs") ? Number(value("--runs")) : 0;
const model = value("--model");
const judgeModel = value("--judge-model");
const maxCost = value("--max-cost-usd") ? Number(value("--max-cost-usd")) : Infinity;
const jsonPath = value("--json");
const timeoutSeconds = Number(value("--timeout", "180"));
const keepTemp = args.includes("--keep-temp");

const glob = new RegExp(`^${caseGlob.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*")}$`);
const cases = discoverCases(resolve(root, "plugins", "stow", "evals")).filter((item) => glob.test(item.name));
if (cases.length === 0) {
  console.error(`no cases match ${caseGlob}`);
  process.exit(1);
}

function project(withSkill) {
  const dir = mkdtempSync(join(tmpdir(), "stow-eval-"));
  writeFileSync(join(dir, "opencode.json"), JSON.stringify(projectConfig, null, 2));
  if (withSkill) {
    mkdirSync(join(dir, ".opencode", "skills", "stow-s3"), { recursive: true });
    copyFileSync(skill, join(dir, ".opencode", "skills", "stow-s3", "SKILL.md"));
  }
  return dir;
}

async function subject(prompt, withSkill) {
  const dir = project(withSkill);
  try {
    const command = ["run", "--agent", "eval", "--format", "json"];
    if (model) command.push("--model", model);
    command.push(prompt);
    const { stdout } = await run("opencode", command, { cwd: dir, env: { ...process.env, PWD: dir }, timeout: timeoutSeconds * 1000, maxBuffer: 32 * 1024 * 1024 });
    const events = stdout.split("\n").filter(Boolean).flatMap((line) => {
      try { return [JSON.parse(line)]; } catch { return []; }
    });
    const texts = events.filter((event) => event.type === "text").map((event) => event.part.text).filter(Boolean);
    const tools = events.filter((event) => event.type === "tool").map((event) => ({ tool: event.part.tool, input: event.part.state?.input ?? {} }));
    const cost = events.filter((event) => event.type === "step_finish").reduce((total, event) => total + (event.part.cost ?? 0), 0);
    return { text: texts.join("\n"), tools, cost };
  } finally {
    if (keepTemp) console.error(`kept ${dir}`);
    else rmSync(dir, { recursive: true, force: true });
  }
}

async function judge(criteria, result) {
  const dir = project(false);
  try {
    const prompt = `You are grading one transcript. Reply with PASS or FAIL on the first line, then one sentence.\n\nCRITERION:\n${criteria}\n\nAGENT FINAL MESSAGE:\n${result.text.slice(0, 6000)}\n\nTOOLS USED:\n${result.tools.map((tool) => tool.tool).join(", ") || "(none)"}`;
    const command = ["run", "--agent", "eval", "--format", "json"];
    if (judgeModel ?? model) command.push("--model", judgeModel ?? model);
    command.push(prompt);
    const { stdout } = await run("opencode", command, { cwd: dir, env: { ...process.env, PWD: dir }, timeout: timeoutSeconds * 1000, maxBuffer: 8 * 1024 * 1024 });
    const text = stdout.split("\n").flatMap((line) => {
      try {
        const event = JSON.parse(line);
        return event.type === "text" && event.part?.text ? [event.part.text] : [];
      } catch { return []; }
    }).join("\n");
    const cost = stdout.split("\n").flatMap((line) => {
      try {
        const event = JSON.parse(line);
        return event.type === "step_finish" ? [event.part?.cost ?? 0] : [];
      } catch { return []; }
    }).reduce((total, value) => total + value, 0);
    return { pass: /^\s*PASS\b/i.test(text) && !/^\s*FAIL\b/i.test(text), verdict: text.slice(0, 200), cost };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

function toolCalls(result, grader) {
  const name = String(grader.fields.tool ?? "").toLowerCase() === "skill" ? "skill" : String(grader.fields.tool ?? "").toLowerCase();
  const pattern = grader.fields.input_match ? new RegExp(grader.fields.input_match) : null;
  return result.tools.filter((tool) => tool.tool.toLowerCase() === name && (!pattern || pattern.test(JSON.stringify(tool.input))));
}

async function scoreCase(testCase, arm) {
  const result = await subject(testCase.prompt, arm === "with");
  let scored = 0;
  let passed = 0;
  let cost = result.cost;
  let skillFired = false;
  const verdicts = [];
  for (const grader of testCase.graders) {
    const withOnly = String(grader.fields.arm ?? "").trim() === "with-only";
    if (withOnly && arm === "baseline") continue;
    if (grader.fields.type === "tool_used") {
      const calls = toolCalls(result, grader);
      const min = Number(grader.fields.min ?? 1);
      const max = grader.fields.max === undefined ? Infinity : Number(grader.fields.max);
      const pass = calls.length >= min && calls.length <= max;
      if (grader.fields.tool?.toLowerCase() === "skill") skillFired = calls.length > 0;
      verdicts.push({ grader: grader.name, pass, detail: `${calls.length} call(s)` });
      if (!withOnly) { scored++; if (pass) passed++; }
      continue;
    }
    if (grader.fields.type === "llm") {
      const judged = await judge(grader.body, result);
      cost += judged.cost;
      verdicts.push({ grader: grader.name, pass: judged.pass, detail: judged.verdict });
      if (!withOnly) { scored++; if (judged.pass) passed++; }
      continue;
    }
    throw new Error(`unsupported grader type ${grader.fields.type} in ${testCase.name}/${grader.name}`);
  }
  return { score: scored === 0 ? 0 : passed / scored, passed, scored, cost, skillFired, verdicts };
}

const report = { started: new Date().toISOString(), model: model ?? "(default)", cases: [], cost: 0 };
for (const testCase of cases) {
  const runs = runsOverride || testCase.runs;
  const perArm = { with: [], baseline: [] };
  for (let index = 0; index < runs; index++) {
    for (const arm of ["with", "baseline"]) {
      if (report.cost > maxCost) {
        console.error(`cost ceiling reached at $${report.cost.toFixed(3)}; stopping`);
        break;
      }
      const result = await scoreCase(testCase, arm);
      report.cost += result.cost;
      perArm[arm].push(result);
      console.log(`${testCase.name} ${arm} run ${index + 1}/${runs}: score ${result.score.toFixed(2)} skill=${result.skillFired} $${result.cost.toFixed(4)}`);
    }
  }
  const mean = (results) => results.length === 0 ? 0 : results.reduce((total, item) => total + item.score, 0) / results.length;
  report.cases.push({
    name: testCase.name, kind: testCase.kind, heldout: testCase.heldout,
    with: Number(mean(perArm.with).toFixed(3)), baseline: Number(mean(perArm.baseline).toFixed(3)),
    delta: Number((mean(perArm.with) - mean(perArm.baseline)).toFixed(3)),
    skillFired: perArm.with.some((item) => item.skillFired),
    verdicts: perArm.with.flatMap((item) => item.verdicts),
  });
}
report.totalCost = Number(report.cost.toFixed(4));
if (jsonPath) writeFileSync(jsonPath, JSON.stringify(report, null, 2));
console.log("\nCASE                                  WITH  BASE  DELTA SKILL");
for (const item of report.cases) {
  console.log(`${item.name.padEnd(37)} ${item.with.toFixed(2)}  ${item.baseline.toFixed(2)}  ${item.delta >= 0 ? "+" : ""}${item.delta.toFixed(2)}  ${item.skillFired ? "yes" : "no"}`);
}
const positives = report.cases.filter((item) => item.kind === "positive");
const negatives = report.cases.filter((item) => item.kind === "negative");
const askRate = positives.length ? positives.filter((item) => item.with > 0).length / positives.length : 0;
const silenceRate = negatives.length ? negatives.filter((item) => item.with === 1).length / negatives.length : 0;
console.log(`\npositive ask rate (mean score > 0): ${(askRate * 100).toFixed(0)}% of ${positives.length}`);
console.log(`negative silence rate (score 1.0): ${(silenceRate * 100).toFixed(0)}% of ${negatives.length}`);
console.log(`cost $${report.totalCost}`);
