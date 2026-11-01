import { readFileSync } from "node:fs";

export const REGISTRY = "https://registry.npmjs.org";
export const REPOSITORY = "chester-hill-solutions/stow-s3";
export const REPOSITORY_URL = `git+https://github.com/${REPOSITORY}.git`;
export const WORKFLOW = "release.yml";
export const PACKAGE_NAMES = [
  "@chester-hill-solutions/stow-s3",
  ...["darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64"].map(target => `@chester-hill-solutions/stow-s3-${target}`),
];

export function manifestProblems(manifest) {
  const problems = [];
  if (!PACKAGE_NAMES.includes(manifest.name)) problems.push(`unexpected npm package: ${manifest.name}`);
  if (manifest.publishConfig?.registry !== REGISTRY) problems.push(`${manifest.name} must target ${REGISTRY}`);
  if (manifest.publishConfig?.access !== "public") problems.push(`${manifest.name} must declare public access`);
  if (manifest.repository?.type !== "git" || manifest.repository?.url !== REPOSITORY_URL) {
    problems.push(`${manifest.name} must identify ${REPOSITORY_URL}`);
  }
  return problems;
}

export function readinessProblems(record, now = Date.now()) {
  const problems = [];
  if (record.registry !== REGISTRY || record.repository !== REPOSITORY || record.workflow !== WORKFLOW) {
    problems.push("npm readiness must identify the public registry, exact repository and release.yml");
  }
  const packages = record.packages ?? {};
  if (Object.keys(packages).length !== PACKAGE_NAMES.length || Object.keys(packages).some(name => !PACKAGE_NAMES.includes(name))) {
    problems.push("npm readiness must account for exactly the five release packages");
  }
  for (const name of PACKAGE_NAMES) {
    const entry = packages[name] ?? {};
    for (const flag of ["publicPackageVerified", "trustedPublisherVerified", "allowPublishVerified"]) {
      if (entry[flag] !== true) problems.push(`${name}: ${flag} is not confirmed`);
    }
    if (entry.oidcPublishVerified !== true) {
      const created = Date.parse(entry.trustCreatedAt);
      if (!Number.isFinite(created) || created > now || now >= created + 2 * 24 * 60 * 60 * 1000) {
        problems.push(`${name}: unvalidated trust needs its first OIDC publish within two days of creation`);
      }
    }
  }
  return problems;
}

export function assertReadiness(record, now) {
  const problems = readinessProblems(record, now);
  if (problems.length) throw new Error(`npm publication is not ready:\n${problems.join("\n")}`);
}

function atLeast(actual, minimum) {
  const match = actual.replace(/^v/, "").match(/^(\d+)\.(\d+)\.(\d+)$/);
  if (!match) return false;
  const parts = match.slice(1).map(Number);
  for (let i = 0; i < 3; i++) if (parts[i] !== minimum[i]) return parts[i] > minimum[i];
  return true;
}

export function assertClientVersions(nodeVersion, npmVersion) {
  if (!atLeast(nodeVersion, [22, 14, 0]) || !atLeast(npmVersion, [11, 5, 1])) {
    throw new Error("npm OIDC publication requires Node >=22.14.0 and npm >=11.5.1");
  }
}
export function assertPublisherContext(env, nodeVersion, npmVersion, version) {
  assertClientVersions(nodeVersion, npmVersion);
  const ref = `refs/tags/v${version}`;
  if (env.GITHUB_ACTIONS !== "true" || env.RUNNER_ENVIRONMENT !== "github-hosted" ||
      env.GITHUB_REPOSITORY !== REPOSITORY || env.GITHUB_EVENT_NAME !== "push" || env.GITHUB_REF !== ref ||
      env.GITHUB_WORKFLOW_REF !== `${REPOSITORY}/.github/workflows/${WORKFLOW}@${ref}`) {
    throw new Error("npm publication requires the exact tagged release workflow on a GitHub-hosted runner");
  }
  if (!env.ACTIONS_ID_TOKEN_REQUEST_URL || !env.ACTIONS_ID_TOKEN_REQUEST_TOKEN) {
    throw new Error("npm publication requires the workflow's existing OIDC capability");
  }
}

export function readReadiness() {
  return JSON.parse(readFileSync(new URL("./npm-publish-readiness.json", import.meta.url), "utf8"));
}
