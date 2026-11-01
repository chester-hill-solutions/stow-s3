#!/usr/bin/env node
// Run only after separate source review, merge and interactive-publication approval.
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync, copyFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createInterface } from "node:readline/promises";
import { ACCOUNT, ARTIFACT, assertRun, FIRST_VERSION, firstPlan, PUBLICATION_ORDER, runFirstPlan } from "./npm-first-release.mjs";
import { assertClientVersions, REGISTRY, REPOSITORY } from "./npm-publication.mjs";
import { isPublished, publishEnvironment, withVerifiedArtifact } from "./publish-if-absent.mjs";

const root = resolve(import.meta.dirname, "..");
const run = (command, args, options = {}) => execFileSync(command, args, { cwd: root, encoding: "utf8", timeout: 15_000, ...options });
const api = path => JSON.parse(run("gh", ["api", `repos/${REPOSITORY}/${path}`]));

export function assertInteractive({ env, stdinTTY, stdoutTTY, username }) {
  if (env.CI || env.GITHUB_ACTIONS || !stdinTTY || !stdoutTTY) throw Error("first publication requires a local interactive terminal");
  if (username !== ACCOUNT) throw Error(`npm account must be ${ACCOUNT}`);
}

export function anonymousInstallArgs(prefix, name, userconfig, globalconfig, cache) {
  if (!PUBLICATION_ORDER.includes(name)) throw Error("unexpected consumer package");
  if (!cache) throw Error("fresh anonymous cache required");
  return ["install", "--prefix", prefix, "--ignore-scripts", "--force", "--no-audit", "--no-fund", `--registry=${REGISTRY}`, `--userconfig=${userconfig}`, `--globalconfig=${globalconfig}`, `--cache=${cache}`, `${name}@${FIRST_VERSION}`];
}

export async function main(args) {
  const [mode, source, rehearsal, ci] = args;
  if (args.length !== 4 || !["--check", "--publish"].includes(mode) || !/^[a-f0-9]{40}$/.test(source ?? "") || ![rehearsal, ci].every(id => /^[1-9]\d*$/.test(id ?? ""))) throw Error("usage: bootstrap-first-npm.mjs --check|--publish <approved-main-sha> <rehearsal-run> <push-ci-run>");
  if (run("git", ["status", "--porcelain"]).trim() || run("git", ["rev-parse", "HEAD"]).trim() !== source || api("commits/main").sha !== source) throw Error("clean checkout of reviewed current main required");
  if (JSON.parse(readFileSync(join(root, "packages/stow-s3/package.json"))).version !== FIRST_VERSION) throw Error("bootstrap is only for 0.3.5");
  assertRun(api(`actions/runs/${rehearsal}`), source, "release.yml");
  assertRun(api(`actions/runs/${ci}`), source, "ci.yml");
  const directory = mkdtempSync(join(tmpdir(), "stow-first-npm-"));
  try {
    run("gh", ["run", "download", rehearsal, "--repo", REPOSITORY, "--name", ARTIFACT, "--dir", directory], { timeout: 120_000 });
    const receipt = JSON.parse(readFileSync(join(directory, "receipt.json"), "utf8"));
    const plan = await firstPlan(directory, receipt, source, rehearsal, isPublished);
    console.log(JSON.stringify({ source, version: FIRST_VERSION, liveProviders: receipt.liveProviderCompatibility, plan: plan.map(({ name, integrity, action }) => ({ name, integrity, action })) }, null, 2));
    const globalconfig = join(directory, "empty-global.npmrc"); writeFileSync(globalconfig, "");
    // Use only the previously authorized local login. Never read its contents.
    const config = [`--registry=${REGISTRY}`, `--@chester-hill-solutions:registry=${REGISTRY}`, `--userconfig=${join(homedir(), ".npmrc")}`, `--globalconfig=${globalconfig}`];
    const env = publishEnvironment(process.env);
    await runFirstPlan(plan, mode, {
      authorize: async () => {
        const context = { env: process.env, stdinTTY: process.stdin.isTTY, stdoutTTY: process.stdout.isTTY };
        assertInteractive({ ...context, username: ACCOUNT });
        assertClientVersions(process.versions.node, run("npm", ["--version"]).trim());
        let username;
        try { username = JSON.parse(run("npm", ["whoami", "--json", ...config], { env, cwd: directory })).trim(); }
        catch { throw Error("npm login verification failed; no publication attempted"); }
        assertInteractive({ ...context, username });
        const terminal = createInterface({ input: process.stdin, output: process.stdout });
        try {
          if (await terminal.question(`Separately approved publication of five public 0.3.5 packages? Type ${source}: `) !== source) throw Error("publication not confirmed");
        } finally { terminal.close(); }
      },
      lookup: isPublished,
      publish: step => withVerifiedArtifact(step, (artifact, cwd) => run("npm", ["publish", "--ignore-scripts", "--access=public", ...config, artifact], { cwd, env, stdio: "inherit", timeout: 300_000 })),
      verifyConsumers: steps => {
        const empty = join(directory, "empty-user.npmrc"); writeFileSync(empty, "");
        // Explicitly install every identity at the exact version, even carriers
        // for other hosts. None of their install scripts execute.
        for (const [index, step] of steps.entries()) {
          run("npm", anonymousInstallArgs(join(directory, `consumer-${index}`), step.name, empty, globalconfig, join(directory, `anonymous-cache-${index}`)), { env, cwd: directory, timeout: 120_000, stdio: "inherit" });
        }
        const consumer = join(directory, "consumer-4");
        copyFileSync(join(root, "scripts/smoke-npm-consumer.mjs"), join(consumer, "smoke.mjs"));
        copyFileSync(join(root, "scripts/assert-consumer-binary.mjs"), join(consumer, "assert-consumer-binary.mjs"));
        const clean = { ...env }; delete clean.STOW_BIN; delete clean.NODE_PATH;
        run("node", ["smoke.mjs", FIRST_VERSION], { env: clean, cwd: consumer, timeout: 120_000, stdio: "inherit" });
      },
    });
  } finally { rmSync(directory, { recursive: true, force: true }); }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { await main(process.argv.slice(2)); } catch (error) { console.error(error.message); process.exitCode = 1; }
}
