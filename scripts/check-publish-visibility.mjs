#!/usr/bin/env node
// Metadata is a prerequisite; exact anonymous installation is checked after publication.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { manifestProblems, PACKAGE_NAMES, REGISTRY } from "./npm-publication.mjs";

export function classify(status, document, name) {
  if (status === 200) {
    if (document?.name === name && document.versions && typeof document.versions === "object" && !Array.isArray(document.versions) && Object.keys(document.versions).length > 0) {
      return { verdict: "public", why: "anonymous registry metadata identifies the package" };
    }
    return { verdict: "unknown", why: "HTTP 200 lacks the expected package metadata" };
  }
  if (status === 401 || status === 403) return { verdict: "denied", why: "anonymous access denied; existence and visibility are not established" };
  if (status === 404) return { verdict: "absent", why: "no public package returned; bootstrap needs separate authorization" };
  if (status === 429 || status >= 500) return { verdict: "unavailable", why: `registry answered HTTP ${status}` };
  return { verdict: "unknown", why: `unexpected HTTP ${status}` };
}

export async function checkInstallable(packages, fetchPackage) {
  const checked = [], problems = [];
  for (const name of packages) {
    const { status, document } = await fetchPackage(name);
    const entry = { name, status, ...classify(status, document, name) };
    checked.push(entry);
    if (entry.verdict !== "public") problems.push(`${name}: ${entry.verdict}: ${entry.why}`);
  }
  return { checked, problems };
}

export function shouldBlock(checked) {
  return checked.length === 0 || checked.some(entry => entry.verdict !== "public");
}

export function packageNames(manifest) {
  const problems = manifestProblems(manifest);
  if (manifest.name !== PACKAGE_NAMES[0] || Object.keys(manifest.optionalDependencies ?? {}).length !== 4 ||
      PACKAGE_NAMES.slice(1).some(name => manifest.optionalDependencies?.[name] !== manifest.version)) {
    problems.push("core manifest must identify all four same-version native packages");
  }
  if (problems.length) throw new Error(problems.join("\n"));
  return PACKAGE_NAMES;
}

export async function fetchUnauthenticated(name, registry = REGISTRY, fetchPackage = fetch) {
  if (registry !== REGISTRY) throw new Error(`visibility checks must target ${REGISTRY}`);
  const response = await fetchPackage(new URL(encodeURIComponent(name), `${registry}/`).href, {
    headers: { accept: "application/json" }, credentials: "omit", redirect: "manual", signal: AbortSignal.timeout(12_000),
  });
  return { status: response.status, document: response.status === 200 ? await response.json() : undefined };
}

export function report(checked, problems, log) {
  for (const entry of checked) log(`  ${entry.name}: HTTP ${entry.status} — ${entry.verdict} (${entry.why})`);
  for (const problem of problems) log(problem);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const manifest = JSON.parse(readFileSync(new URL("../packages/stow-s3/package.json", import.meta.url), "utf8"));
    const { checked, problems } = await checkInstallable(packageNames(manifest), fetchUnauthenticated);
    report(checked, problems, console.log);
    if (shouldBlock(checked)) throw new Error("Refusing npm publication: all five packages require positive anonymous public metadata");
    console.log("All five packages have public metadata; exact-version anonymous consumer checks remain required.");
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
