#!/usr/bin/env node
// Publish npm tarballs, skipping any whose exact version is already on the registry.
//
//   node scripts/publish-if-absent.mjs <tarball> [<tarball> ...]
//
// Two reasons this exists rather than a bare `npm publish`:
//
// `npm publish` takes one package spec. Publishing the four platform packages
// with `npm publish ./dist/npm-platform/*.tgz` hands npm four positional
// arguments and it exits EUSAGE without contacting the registry, which reads
// like a credential problem and is not one. Each tarball is published here, one
// at a time.
//
// A release has to be re-runnable. A tag is immutable once consumers can see
// it, but a publish is several independent writes, and any of them can fail
// after an earlier one has already landed. Re-running the whole release then
// fails on the packages that succeeded, which is how a single transient error
// turns into a version that can never be completed. Checking the registry
// first makes the second run publish only what is missing.
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

export function manifestOf(tarball) {
  const raw = execFileSync("tar", ["-xzOf", tarball, "package/package.json"], {
    encoding: "utf8",
  });
  const manifest = JSON.parse(raw);
  if (!manifest.name || !manifest.version) {
    throw new Error(`${tarball} has no name or version in package/package.json`);
  }
  return manifest;
}

export function isPublished(name, version) {
  // A non-zero exit is the signal, not the output: npm prints a 404 to stderr
  // and writes nothing to stdout for a version that does not exist.
  try {
    execFileSync("npm", ["view", `${name}@${version}`, "version"], {
      stdio: ["ignore", "ignore", "ignore"],
    });
    return true;
  } catch {
    return false;
  }
}

// publishPlan decides, for each tarball, whether this run publishes it.
//
// It is separated from the publishing so the re-runnability claim can be tested
// without a registry. That claim is the reason this script exists: a release is
// several independent writes, any of which can fail after an earlier one landed,
// and a second run that fails on the packages that succeeded turns one transient
// error into a version that can never be completed.
export function publishPlan(tarballs, alreadyPublished) {
  return tarballs.map((tarball) => {
    const { name, version } = manifestOf(tarball);
    return {
      tarball,
      name,
      version,
      action: alreadyPublished(name, version) ? "skip" : "publish",
    };
  });
}

// main is the command line, separated from the module so the plan can be tested.
// The guard below matters for that: a script whose entry point runs on import
// cannot be imported at all, which is why this had no test.
export function main(argv) {
  const tarballs = argv.slice(2);
  if (tarballs.length === 0) {
    console.error("usage: publish-if-absent.mjs <tarball> [<tarball> ...]");
    return 64;
  }

  for (const step of publishPlan(tarballs, isPublished)) {
    if (step.action === "skip") {
      console.log(`skip  ${step.name}@${step.version} (already on the registry)`);
      continue;
    }

    console.log(`publish ${step.name}@${step.version}`);
    // A failure here throws and the release stops: the point of the plan above is
    // that re-running resumes, not that a failure is swallowed.
    execFileSync("npm", ["publish", "--ignore-scripts", "--access", "public", step.tarball], {
      stdio: "inherit",
    });
  }
  return 0;
}

const invokedDirectly =
  process.argv[1] !== undefined && resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (invokedDirectly) {
  process.exit(main(process.argv));
}
