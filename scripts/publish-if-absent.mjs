#!/usr/bin/env node
import { execFileSync, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

export const REGISTRY = "https://npm.pkg.github.com";

export function manifestOf(tarball) {
  const manifest = JSON.parse(execFileSync("tar", ["-xzOf", tarball, "package/package.json"], { encoding: "utf8" }));
  if (!manifest.name || !manifest.version) throw new Error(`${tarball} has no name or version`);
  // GitHub Packages takes its visibility from a package setting, so `access` is gone from
// the manifests and asserting it here would refuse every tarball. The registry is the
// whole of the check: publishing to the wrong one is the failure that matters, because
// the package then lands where the documented install does not look.
  if (manifest.publishConfig?.registry !== REGISTRY) throw new Error(`${tarball} does not target ${REGISTRY}`);
  return manifest;
}

export function integrityOf(tarball) {
  return `sha512-${createHash("sha512").update(readFileSync(tarball)).digest("base64")}`;
}

export function registryResult(result) {
  if (result.error) throw result.error;
  let document;
  try { document = JSON.parse(result.stdout || "{}"); }
  catch { throw new Error("npm registry returned invalid JSON"); }
  if (result.status !== 0) {
    if (document.error?.code === "E404") return null;
    throw new Error(`npm registry lookup failed: ${document.error?.code ?? result.status}`);
  }
  if (typeof document !== "string" || !document.startsWith("sha512-")) throw new Error("published package has no supported integrity hash");
  return { integrity: document };
}

function registryFlags(name) {
 const flags=[`--registry=${REGISTRY}`];
 if (name.startsWith("@")) flags.push(`--${name.split("/")[0]}:registry=${REGISTRY}`);
 return flags;
}

export function isPublished(name, version) {
  return registryResult(spawnSync("npm", ["view", `${name}@${version}`, "dist.integrity", "--json", ...registryFlags(name), "--fetch-retries=2", "--fetch-retry-mintimeout=250", "--fetch-retry-maxtimeout=1000"], { encoding: "utf8", timeout: 30_000 }));
}

export function publishPlan(tarballs, lookup) {
  return tarballs.map((tarball) => {
    const { name, version } = manifestOf(tarball);
    const integrity = integrityOf(tarball);
    const existing = lookup(name, version, tarball);
    if (existing !== null && existing.integrity !== integrity) throw new Error(`immutable version conflict: ${name}@${version} differs from this tarball`);
    return { tarball, name, version, integrity, action: existing === null ? "publish" : "skip" };
  });
}

export function main(argv) {
  const args = argv.slice(2);
  const checkOnly = args[0] === "--check";
  const tarballs = checkOnly ? args.slice(1) : args;
  if (tarballs.length === 0) { console.error("usage: publish-if-absent.mjs [--check] <tarballs...>"); return 64; }
  for (const step of publishPlan(tarballs, isPublished)) {
    console.log(`${checkOnly ? "preflight" : step.action} ${step.name}@${step.version}`);
    if (checkOnly || step.action === "skip") continue;
    // No --provenance: npm rejects it here with EUSAGE, which is how the v0.2.0
    // release died at this step. The flag was removed from the inline publish
    // commands in release.yml and came back when those were replaced by this
    // script, and nothing noticed until a tag was cut. This project does not
    // publish provenance. If it is ever wanted it needs wiring and verifying
    // deliberately: the note left with the original removal calls it a GitHub
    // Packages limitation, and this registry is npmjs, so it does not describe
    // this setup.
    execFileSync("npm", ["publish", "--ignore-scripts", "--access", "public", ...registryFlags(step.name), step.tarball], { stdio: "inherit" });
    const receipt = isPublished(step.name, step.version);
    if (receipt?.integrity !== step.integrity) throw new Error(`published integrity not confirmed for ${step.name}@${step.version}`);
  }
  return 0;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) process.exit(main(process.argv));
