#!/usr/bin/env node
// Whether a package can be installed by someone outside the publishing organisation.
//
// This exists because the release that shipped 0.2.0 published five packages that
// answered 401 to an unauthenticated request: present, and private. Nothing in the
// pipeline noticed, because every step that touched those packages did so with a
// token, and the local consumer that was installed was installed from a tarball rather
// than from the registry. The release was green and the front page promised an install
// that could not work.
//
// The check runs before publishing rather than after, because after publishing the
// only way to report the problem is to fail a job that has already published to npm,
// which is the half-done release this repository already avoids elsewhere. Before
// publishing it is advice; after, it is a postmortem.
//
// Publishing a new version cannot change the visibility of the scope, so the version
// that already exists answers for the one about to be written. When nothing is
// published yet there is nothing to inspect and the check records that it was skipped
// rather than passing silently.

import { join } from "node:path";

export const GITHUB_NPM = "https://npm.pkg.github.com";

// Why the response means what it means, separated from doing the request so the
// decision can be tested against statuses nobody can reproduce on demand.
//
// 404 is the ambiguous one. A private package and an absent package are different
// facts, and on GitHub Packages the same 404 covers both unless you are already
// authorised. So an unauthenticated 404 cannot distinguish "not published yet" from
// "private", and reporting it as either would be a guess. It is reported as unknown,
// which is the only claim the evidence supports.
export function classify(status) {
  if (status === 200) return { verdict: "installable", why: "the registry served it with no credentials" };
  if (status === 401 || status === 403) {
    return { verdict: "private", why: "the registry demanded credentials, so it exists and is not public" };
  }
  if (status === 404) {
    return { verdict: "unknown", why: "404 is both 'not published' and 'private' without credentials, so this cannot tell them apart" };
  }
  return { verdict: "unknown", why: `the registry answered ${status}, which this check does not interpret` };
}

// statusFor is the one thing this module cannot decide for itself, injected so the
// statuses above can be tested without a network or a private package.
export async function checkInstallable(packages, fetchStatus) {
  const problems = [];
  const checked = [];
  for (const name of packages) {
    const status = await fetchStatus(name);
    const { verdict, why } = classify(status);
    checked.push({ name, status, verdict, why });
    if (verdict === "private") problems.push(`${name} answers ${status} to an unauthenticated request, so it exists and is private: ${why}`);
  }
  return { checked, problems };
}

// shouldBlock is the whole decision, exported because it is the part worth pinning.
// It lived in the CLI block below, where no test could reach it, and a deliberate break
// showed why that mattered: reclassifying 401 as unknown let the check exit 0, because
// unknown is deliberately non-blocking for a first release. That is the correct rule for
// an ambiguous 404 and the wrong rule for a 401, and with nothing testing the
// difference the wrong one was a one-word change away.
export function shouldBlock(checked) {
  return checked.some((entry) => entry.verdict === "private");
}

export function packageNames(manifest) {
  const names = [manifest.name];
  for (const [name, version] of Object.entries(manifest.optionalDependencies ?? {})) {
    // The platform packages are what a user actually downloads, so a scope that is
    // public at the top and private per platform still fails to install on some
    // machines. Checking only the wrapper would have passed that.
    if (version === manifest.version) names.push(name);
  }
  return names;
}

// fetchUnauthenticated deliberately sends no Authorization header and no npm token.
// That is the whole test: a request that carries credentials cannot distinguish a
// private package from a public one.
export async function fetchUnauthenticated(name, registry = GITHUB_NPM) {
  const response = await fetch(join(registry, encodeURIComponent(name)), {
    headers: { accept: "application/json" },
    redirect: "manual",
  });
  return response.status;
}

// report prints one line per package and the remedy once. The per-package line already
// says which packages are private, so repeating the whole explanation for each of five
// of them makes the log harder to read than the finding deserves.
export function report(checked, problems, log) {
  for (const entry of checked) {
    log(`  ${entry.name}: HTTP ${entry.status} — ${entry.verdict} (${entry.why})`);
  }
  if (problems.length > 0) {
    log("");
    log(`${problems.length} package(s) are not installable by a stranger. Publishing the next version will not change that, because visibility belongs to a package and not to a release. Make them public in the registry's settings, or document an authenticated install on the site, before cutting a tag.`);
  }
}

if (process.argv[1] && import.meta.url.endsWith(process.argv[1].split("/").pop())) {
  const registry = process.env.STOW_NPM_REGISTRY || GITHUB_NPM;
  const { readFileSync } = await import("node:fs");
  const { resolve } = await import("node:path");
  const manifestPath = process.argv[2] || resolve(process.argv[1], "../../packages/stow-s3/package.json");
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  const { checked, problems } = await checkInstallable(packageNames(manifest), (name) =>
    fetchUnauthenticated(name, registry),
  );
  console.log(`Checking whether ${manifest.name}@${manifest.version} would be installable by a stranger:`);
  report(checked, problems, console.log);

  const privateOnes = checked.filter((entry) => entry.verdict === "private");
  if (shouldBlock(checked)) {
    console.error("");
    console.error(`Refusing to publish: ${privateOnes.length} package(s) are private.`);
    console.error("A release nobody can install is worse than no release, and this is recoverable before a tag and not after one.");
    process.exit(1);
  }
  const unknowns = checked.filter((entry) => entry.verdict === "unknown");
  if (unknowns.length > 0) {
    // Not a failure. Nothing is published yet on a first release, and there is no
    // evidence of a problem, so blocking here would make a first release impossible
    // on the grounds that nothing exists to check.
    console.warn("");
    console.warn(`::warning title=Publish visibility unknown::${unknowns.map((e) => e.name).join(", ")} could not be classified: nothing published yet, or the registry answered an ambiguous status. Visibility is an organisation setting and no run of this workflow can change it; confirm it is public before the first tag.`);
  }
  console.log("");
  console.log("Every checked package is installable without credentials.");
}
