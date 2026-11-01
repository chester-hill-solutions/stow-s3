#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { copyFileSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { join, resolve } from "node:path";
import { assertPublisherContext, assertReadiness, manifestProblems, PACKAGE_NAMES, readReadiness, REGISTRY } from "./npm-publication.mjs";
import { checkInstallable, fetchUnauthenticated, shouldBlock } from "./check-publish-visibility.mjs";
export { REGISTRY };

export function manifestOf(tarball) {
  const manifest = JSON.parse(execFileSync("tar", ["-xzOf", tarball, "package/package.json"], { encoding: "utf8" }));
  const problems = manifestProblems(manifest);
  if (!/^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/.test(manifest.version ?? "")) problems.push("tarball has no valid version");
  if (problems.length) throw new Error(`${tarball}: ${problems.join("; ")}`);
  return manifest;
}
export function integrityOf(tarball) {
  return `sha512-${createHash("sha512").update(readFileSync(tarball)).digest("base64")}`;
}
export function registryResult({ status, document }, name, version) {
  if (status === 404) return null;
  if (status !== 200) throw new Error(`npm registry lookup failed: HTTP ${status}`);
  const integrity = document?.dist?.integrity;
  const hash = typeof integrity === "string" && integrity.match(/^sha512-([A-Za-z0-9+/]+={0,2})$/)?.[1];
  if (document?.name !== name || document?.version !== version || !hash || Buffer.from(hash, "base64").length !== 64 || Buffer.from(hash, "base64").toString("base64") !== hash) {
    throw new Error("published package has invalid identity or integrity metadata");
  }
  return { integrity };
}
export async function isPublished(name, version, _tarball, fetchVersion = fetch) {
  const response = await fetchVersion(`${REGISTRY}/${encodeURIComponent(name)}/${encodeURIComponent(version)}`, {
    headers: { accept: "application/json" }, credentials: "omit", redirect: "manual", signal: AbortSignal.timeout(12_000),
  });
  return registryResult({ status: response.status, document: response.status === 200 ? await response.json() : undefined }, name, version);
}
export async function publishPlan(tarballs, lookup) {
  const plan = [], names = new Set();
  for (const tarball of tarballs) {
    const { name, version } = manifestOf(tarball);
    if (names.has(name)) throw new Error(`duplicate package artifact: ${name}`);
    names.add(name);
    const integrity = integrityOf(tarball), existing = await lookup(name, version, tarball);
    if (existing !== null && existing.integrity !== integrity) throw new Error(`immutable version conflict: ${name}@${version} differs from this tarball`);
    plan.push({ tarball: resolve(tarball), name, version, integrity, action: existing === null ? "publish" : "skip" });
  }
  return plan;
}
export function publishEnvironment(env) {
  return Object.fromEntries(Object.entries(env).filter(([key]) => !/^npm_config_/i.test(key) && !["NPM_TOKEN", "NODE_AUTH_TOKEN"].includes(key)));
}
export function publishTarball(step, run = execFileSync, env = process.env) {
  const directory = mkdtempSync(join(tmpdir(), "stow-npm-publish-"));
  try {
    const artifact = join(directory, "package.tgz");
    copyFileSync(step.tarball, artifact);
    if (integrityOf(artifact) !== step.integrity) throw new Error("artifact changed after publication preflight");
    const manifest = manifestOf(artifact);
    if (manifest.name !== step.name || manifest.version !== step.version) {
      throw new Error("artifact changed after publication preflight");
    }
    const userconfig = join(directory, "user.npmrc"), globalconfig = join(directory, "global.npmrc");
    writeFileSync(userconfig, ""); writeFileSync(globalconfig, "");
    run("npm", ["publish", "--ignore-scripts", "--access=public", `--registry=${REGISTRY}`, `--userconfig=${userconfig}`, `--globalconfig=${globalconfig}`, artifact], {
      cwd: directory, env: publishEnvironment(env), stdio: "inherit",
    });
  } finally { rmSync(directory, { recursive: true, force: true }); }
}
async function authorizePublication(tarballs) {
  const policy = JSON.parse(readFileSync(new URL("./release-policy.json", import.meta.url), "utf8"));
  if (policy.publishPackages !== true) throw new Error("npm publication is disabled by release policy");
  assertReadiness(readReadiness());
  const { version } = JSON.parse(readFileSync(new URL("../packages/stow-s3/package.json", import.meta.url), "utf8"));
  if (tarballs.some(tarball => manifestOf(tarball).version !== version)) throw new Error("artifact version differs from release source");
  assertPublisherContext(process.env, process.versions.node, execFileSync("npm", ["--version"], { encoding: "utf8" }).trim(), version);
  const { checked } = await checkInstallable(PACKAGE_NAMES, fetchUnauthenticated);
  if (shouldBlock(checked)) throw new Error("all five public packages must exist before trusted publication");
}
export async function main(argv, { lookup = isPublished, publish = publishTarball, authorize = authorizePublication, log = console.log } = {}) {
  const args = argv.slice(2), simulate = ["--check", "--dry-run"].includes(args[0]);
  const tarballs = simulate ? args.slice(1) : args;
  if (tarballs.length === 0 || tarballs.some(path => path.startsWith("--"))) return 64;
  if (!simulate) await authorize(tarballs);
  // Finish all reads before the first write. Read errors never become absence.
  for (const step of await publishPlan(tarballs, lookup)) {
    log(`${simulate ? "preflight" : step.action} ${step.name}@${step.version}`);
    if (simulate || step.action === "skip") continue;
    await publish(step);
    if ((await lookup(step.name, step.version, step.tarball))?.integrity !== step.integrity) throw new Error(`published integrity not confirmed for ${step.name}@${step.version}`);
  }
  return 0;
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { process.exitCode = await main(process.argv); }
  catch (error) { console.error(error.message); process.exitCode = 1; }
}
